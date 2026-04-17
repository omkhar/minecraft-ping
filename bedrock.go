package main

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"net/netip"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	bedrockUnconnectedPingPacketID byte = 0x01
	bedrockUnconnectedPongPacketID byte = 0x1c
	bedrockStatusMinFields              = 6
)

var bedrockMagic = [16]byte{
	0x00, 0xff, 0xff, 0x00,
	0xfe, 0xfe, 0xfe, 0xfe,
	0xfd, 0xfd, 0xfd, 0xfd,
	0x12, 0x34, 0x56, 0x78,
}

type bedrockStatus struct {
	Brand         string
	MOTD          string
	Protocol      int
	Version       string
	PlayersOnline int
	PlayersMax    int
	ServerID      string
	MapName       string
	GameMode      string
}

type bedrockPreparedProbe struct {
	client        pingClient
	target        targetSpec
	candidates    []dialCandidate
	displayTarget netip.AddrPort
}

func newBedrockClient() pingClient {
	return newPingClient()
}

func prepareBedrockProbe(ctx context.Context, client pingClient, target targetSpec, options pingOptions) (preparedProbe, error) {
	client = client.withDefaults()
	candidates, err := client.resolveBedrockCandidates(ctx, target, options)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve bedrock server %s: %w", target.Host, err)
	}

	prepared := &bedrockPreparedProbe{
		client:     client,
		target:     target,
		candidates: candidates,
	}
	if len(candidates) == 1 {
		prepared.displayTarget = candidates[0].address
	}
	return prepared, nil
}

func (p *bedrockPreparedProbe) banner(numeric bool) string {
	label := p.summaryLabel(numeric)
	displayPort := p.target.defaultPort(addressFamilyAny, editionBedrock)
	if p.displayTarget.IsValid() {
		displayPort = int(p.displayTarget.Port())
	}

	if p.displayTarget.IsValid() && !numeric && p.target.Host != p.displayTarget.Addr().String() {
		return fmt.Sprintf("PING %s (%s) port %d [%s]:", p.target.Host, p.displayTarget.Addr(), displayPort, editionBedrock)
	}
	if p.displayTarget.IsValid() {
		return fmt.Sprintf("PING %s port %d [%s]:", p.displayTarget.Addr(), displayPort, editionBedrock)
	}
	return fmt.Sprintf("PING %s port %d [%s]:", label, displayPort, editionBedrock)
}

func (p *bedrockPreparedProbe) summaryLabel(numeric bool) string {
	if numeric && p.displayTarget.IsValid() {
		return p.displayTarget.Addr().String()
	}
	return p.target.Host
}

func (p *bedrockPreparedProbe) observeSample(sample probeSample) {
	if sample.remote.IsValid() {
		p.displayTarget = sample.remote
	}
}

func (p *bedrockPreparedProbe) probe(ctx context.Context, timeout time.Duration) (probeSample, error) {
	return pingBedrockCandidates(ctx, p.client, p.candidates, timeout)
}

func pingBedrock(target endpoint, timeout time.Duration, options pingOptions) (int, error) {
	client := newPingClient().withDefaults()
	targetSpec := bedrockTargetSpecFromEndpoint(target, options.addressFamily)
	options.edition = editionBedrock

	request, err := newPingRequest(targetSpec, timeout, options)
	if err != nil {
		return 0, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), request.timeout)
	defer cancel()

	prepared, err := prepareBedrockProbe(ctx, client, newTargetSpec(request.target.Host, request.target.Port, request.explicitPort), request.options)
	if err != nil {
		return 0, err
	}

	sample, err := prepared.probe(ctx, request.timeout)
	if err != nil {
		return 0, fmt.Errorf("failed to ping server %s: %w", request.target, err)
	}

	latencyMs := int(sample.latency / time.Millisecond)
	if latencyMs < 1 {
		latencyMs = 1
	}
	return latencyMs, nil
}

func bedrockTargetSpecFromEndpoint(target endpoint, family addressFamily) targetSpec {
	var explicitPort bool

	if literal, ok := target.literalIP(); ok {
		explicitPort = target.Port != newTargetSpec(target.Host, 0, false).portForAddr(literal, editionBedrock)
	} else {
		explicitPort = target.Port != newTargetSpec(target.Host, 0, false).defaultPort(family, editionBedrock)
	}

	return newTargetSpec(target.Host, target.Port, explicitPort)
}

func (c pingClient) resolveBedrockCandidates(ctx context.Context, target targetSpec, options pingOptions) ([]dialCandidate, error) {
	c = c.withDefaults()

	if err := target.validate(); err != nil {
		return nil, err
	}
	if err := options.addressFamily.validate(); err != nil {
		return nil, err
	}

	if parsed, ok := target.literalIP(); ok {
		if !options.addressFamily.matches(parsed) {
			return nil, fmt.Errorf("%s is an %s address but %s was requested", target.Host, addressFamilyForAddr(parsed), options.addressFamily.forcedFlag())
		}
		if !options.allowPrivateAddresses && isNonPublicAddr(parsed) {
			return nil, fmt.Errorf("refusing to connect to non-public address %s (pass --allow-private to override)", target.Host)
		}

		port, err := toUint16(target.portForAddr(parsed, editionBedrock))
		if err != nil {
			return nil, err
		}
		return []dialCandidate{{
			address: netip.AddrPortFrom(parsed, port),
		}}, nil
	}

	addrs, err := c.resolver.LookupNetIP(ctx, options.addressFamily.resolverNetwork(), target.Host)
	if err != nil {
		return nil, err
	}
	return dialCandidatesForResolvedIPsByAddr(target.Host, addrs, options.addressFamily, options.allowPrivateAddresses, func(addr netip.Addr) uint16 {
		port, _ := toUint16(target.portForAddr(addr, editionBedrock))
		return port
	})
}

func pingBedrockCandidates(ctx context.Context, client pingClient, candidates []dialCandidate, timeout time.Duration) (probeSample, error) {
	if len(candidates) == 0 {
		return probeSample{}, errors.New("no dial candidates available")
	}

	var errs []error
	hasDeadline := false
	deadline := time.Time{}
	if d, ok := ctx.Deadline(); ok {
		deadline = d
		hasDeadline = true
	}

	for _, candidate := range candidates {
		attemptTimeout := timeout
		if hasDeadline {
			remaining := time.Until(deadline)
			if remaining <= 0 {
				break
			}
			if remaining < attemptTimeout {
				attemptTimeout = remaining
			}
		}

		sample, err := pingBedrockCandidate(ctx, client, candidate, attemptTimeout)
		if err == nil {
			return sample, nil
		}
		errs = append(errs, fmt.Errorf("%s: %w", candidate, err))
	}

	if len(errs) == 0 {
		return probeSample{}, context.DeadlineExceeded
	}
	return probeSample{}, errors.Join(errs...)
}

func pingBedrockCandidate(ctx context.Context, client pingClient, candidate dialCandidate, timeout time.Duration) (probeSample, error) {
	client = client.withDefaults()

	conn, err := client.dialContext(ctx, candidate.UDPNetwork(), candidate.String())
	if err != nil {
		return probeSample{}, err
	}
	defer conn.Close()

	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(timeout)
	}
	if err := conn.SetDeadline(deadline); err != nil {
		return probeSample{}, err
	}

	request, expectedPingTime, err := buildBedrockStatusRequest(client.now())
	if err != nil {
		return probeSample{}, err
	}

	start := client.now()
	if _, err := conn.Write(request); err != nil {
		return probeSample{}, err
	}

	var buf [2048]byte
	n, err := conn.Read(buf[:])
	if err != nil {
		return probeSample{}, err
	}
	if _, err := parseBedrockStatusResponse(buf[:n], expectedPingTime); err != nil {
		return probeSample{}, err
	}

	latency := client.now().Sub(start)
	if latency < 0 {
		latency = 0
	}
	return probeSample{
		latency: latency,
		remote:  remoteAddrPort(conn.RemoteAddr()),
	}, nil
}

func buildBedrockStatusRequest(now time.Time) ([]byte, uint64, error) {
	clientGUID, err := randomUint64()
	if err != nil {
		return nil, 0, err
	}
	pingTime := uint64(now.UnixMilli())

	payload := make([]byte, 0, 1+8+len(bedrockMagic)+8)
	payload = append(payload, bedrockUnconnectedPingPacketID)

	var scratch [8]byte
	binary.BigEndian.PutUint64(scratch[:], pingTime)
	payload = append(payload, scratch[:]...)
	payload = append(payload, bedrockMagic[:]...)
	binary.BigEndian.PutUint64(scratch[:], clientGUID)
	payload = append(payload, scratch[:]...)
	return payload, pingTime, nil
}

func randomUint64() (uint64, error) {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return 0, fmt.Errorf("failed to generate random identifier: %w", err)
	}
	return binary.BigEndian.Uint64(buf[:]), nil
}

func parseBedrockStatusResponse(payload []byte, expectedPingTime uint64) (bedrockStatus, error) {
	if len(payload) < 35 {
		return bedrockStatus{}, fmt.Errorf("bedrock pong too short: %d", len(payload))
	}
	if payload[0] != bedrockUnconnectedPongPacketID {
		return bedrockStatus{}, fmt.Errorf("unexpected bedrock pong packet id: %d", payload[0])
	}

	pingTime := binary.BigEndian.Uint64(payload[1:9])
	if pingTime != expectedPingTime {
		return bedrockStatus{}, errors.New("bedrock pong ping time mismatch")
	}
	if payloadMagic := payload[17:33]; string(payloadMagic) != string(bedrockMagic[:]) {
		return bedrockStatus{}, errors.New("bedrock pong magic mismatch")
	}

	nameLength := int(binary.BigEndian.Uint16(payload[33:35]))
	if len(payload) < 35+nameLength {
		return bedrockStatus{}, ioErrUnexpectedEOF("bedrock status payload")
	}
	if len(payload) != 35+nameLength {
		return bedrockStatus{}, fmt.Errorf("bedrock pong length mismatch: got %d want %d", len(payload), 35+nameLength)
	}

	statusBytes := payload[35 : 35+nameLength]
	if !utf8.Valid(statusBytes) {
		return bedrockStatus{}, errors.New("bedrock status payload is not valid UTF-8")
	}

	return parseBedrockStatusText(string(statusBytes))
}

func parseBedrockStatusText(statusText string) (bedrockStatus, error) {
	fields := strings.Split(statusText, ";")
	if len(fields) < bedrockStatusMinFields {
		return bedrockStatus{}, fmt.Errorf("bedrock status response has too few fields: %d", len(fields))
	}
	if fields[0] != "MCPE" {
		return bedrockStatus{}, fmt.Errorf("unexpected bedrock status brand %q", fields[0])
	}

	protocol, err := strconv.Atoi(fields[2])
	if err != nil {
		return bedrockStatus{}, fmt.Errorf("invalid bedrock protocol version %q: %w", fields[2], err)
	}
	online, err := strconv.Atoi(fields[4])
	if err != nil {
		return bedrockStatus{}, fmt.Errorf("invalid bedrock online player count %q: %w", fields[4], err)
	}
	maxPlayers, err := strconv.Atoi(fields[5])
	if err != nil {
		return bedrockStatus{}, fmt.Errorf("invalid bedrock max player count %q: %w", fields[5], err)
	}

	status := bedrockStatus{
		Brand:         fields[0],
		MOTD:          fields[1],
		Protocol:      protocol,
		Version:       fields[3],
		PlayersOnline: online,
		PlayersMax:    maxPlayers,
	}
	if len(fields) > 6 {
		status.ServerID = fields[6]
	}
	if len(fields) > 7 {
		status.MapName = fields[7]
	}
	if len(fields) > 8 {
		status.GameMode = fields[8]
	}
	return status, nil
}

func ioErrUnexpectedEOF(context string) error {
	return fmt.Errorf("%s: unexpected EOF", context)
}
