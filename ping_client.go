package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	defaultMinecraftPort     = 25565
	statusProtocolVersion    = 109
	maxPacketLength          = 2 * 1024 * 1024
	maxStatusJSONLength      = 1 * 1024 * 1024
	maxHandshakeHostByteSize = 255
	maxServerAddressLength   = 253
	maxAllowedTimeout        = 30 * time.Second

	// Minecraft Java Edition protocol packet IDs (context-dependent by state).
	packetIDHandshake      int32 = 0x00
	nextStateStatus        int32 = 0x01
	packetIDStatusRequest  byte  = 0x00
	packetIDStatusResponse int32 = 0x00
	packetIDPing           int32 = 0x01
	packetIDPong           int32 = 0x01
)

var errVarIntTooLong = errors.New("varint is too long")

var randomRead = rand.Read

type pingOptions struct {
	allowPrivateAddresses bool
}

type endpoint struct {
	Host string
	Port int
}

type endpointRoute struct {
	Dial      endpoint
	Handshake endpoint
}

type dnsResolver interface {
	LookupSRV(ctx context.Context, service, proto, name string) (string, []*net.SRV, error)
	LookupIP(ctx context.Context, network, host string) ([]net.IP, error)
}

type dialContextFunc func(ctx context.Context, network, address string) (net.Conn, error)

type pingClient struct {
	resolver    dnsResolver
	dialContext dialContextFunc
	tokenSource func() (uint64, error)
	now         func() time.Time
}

var defaultPingClient = pingClient{}

var nonPublicIPPrefixes = []netip.Prefix{
	mustParsePrefix("0.0.0.0/8"),
	mustParsePrefix("10.0.0.0/8"),
	mustParsePrefix("100.64.0.0/10"),
	mustParsePrefix("127.0.0.0/8"),
	mustParsePrefix("169.254.0.0/16"),
	mustParsePrefix("172.16.0.0/12"),
	mustParsePrefix("192.0.0.0/24"),
	mustParsePrefix("192.0.2.0/24"),
	mustParsePrefix("192.168.0.0/16"),
	mustParsePrefix("198.18.0.0/15"),
	mustParsePrefix("198.51.100.0/24"),
	mustParsePrefix("203.0.113.0/24"),
	mustParsePrefix("224.0.0.0/4"),
	mustParsePrefix("240.0.0.0/4"),
	mustParsePrefix("::/128"),
	mustParsePrefix("::1/128"),
	mustParsePrefix("100::/64"),
	mustParsePrefix("2001:db8::/32"),
	mustParsePrefix("fc00::/7"),
	mustParsePrefix("fe80::/10"),
	mustParsePrefix("ff00::/8"),
}

func newEndpoint(host string, port int) endpoint {
	return endpoint{
		Host: strings.TrimSpace(host),
		Port: port,
	}
}

func (e endpoint) String() string {
	return net.JoinHostPort(e.Host, strconv.Itoa(e.Port))
}

func (e endpoint) address() string {
	return net.JoinHostPort(e.Host, strconv.Itoa(e.Port))
}

func (e endpoint) uint16Port() (uint16, error) {
	return toUint16(e.Port)
}

func (e endpoint) validate() error {
	if e.Host == "" {
		return errors.New("server must not be empty")
	}
	if err := validateServerAddress(e.Host); err != nil {
		return err
	}
	if e.Port < 1 || e.Port > 65535 {
		return fmt.Errorf("invalid port: %d. port must be between 1 and 65535", e.Port)
	}
	return nil
}

func pingServer(server string, port int, timeout time.Duration) (int, error) {
	return pingEndpointWithOptions(newEndpoint(server, port), timeout, pingOptions{
		allowPrivateAddresses: false,
	})
}

func pingServerWithOptions(server string, port int, timeout time.Duration, options pingOptions) (int, error) {
	return pingEndpointWithOptions(newEndpoint(server, port), timeout, options)
}

func pingEndpointWithOptions(target endpoint, timeout time.Duration, options pingOptions) (int, error) {
	return defaultPingClient.withDefaults().ping(target, timeout, options)
}

func (c pingClient) withDefaults() pingClient {
	if c.resolver == nil {
		c.resolver = net.DefaultResolver
	}
	if c.dialContext == nil {
		c.dialContext = defaultDialContext
	}
	if c.tokenSource == nil {
		c.tokenSource = generatePingToken
	}
	if c.now == nil {
		c.now = time.Now
	}
	return c
}

func defaultDialContext(ctx context.Context, network, address string) (net.Conn, error) {
	var dialer net.Dialer
	return dialer.DialContext(ctx, network, address)
}

func (c pingClient) ping(target endpoint, timeout time.Duration, options pingOptions) (int, error) {
	target = newEndpoint(target.Host, target.Port)
	if err := target.validate(); err != nil {
		return 0, err
	}
	if timeout <= 0 {
		return 0, fmt.Errorf("invalid timeout: %s. timeout must be greater than 0", timeout)
	}
	if timeout > maxAllowedTimeout {
		return 0, fmt.Errorf("invalid timeout: %s. timeout must be less than or equal to %s", timeout, maxAllowedTimeout)
	}

	route := c.resolveEndpoint(target, timeout)
	latency, err := c.pingEndpoint(route, timeout, options.allowPrivateAddresses)
	if err != nil {
		if route.Dial != target {
			return 0, fmt.Errorf("failed to ping server %s (resolved to %s): %w", target, route.Dial, err)
		}
		return 0, fmt.Errorf("failed to ping server %s: %w", target, err)
	}

	return latency, nil
}

func (c pingClient) resolveEndpoint(target endpoint, timeout time.Duration) endpointRoute {
	route := endpointRoute{
		Dial:      target,
		Handshake: target,
	}

	if target.Port != defaultMinecraftPort || net.ParseIP(target.Host) != nil {
		return route
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	_, records, err := c.resolver.LookupSRV(ctx, "minecraft", "tcp", target.Host)
	if err != nil || len(records) == 0 {
		return route
	}

	srvTarget := strings.TrimSuffix(records[0].Target, ".")
	if srvTarget == "" || records[0].Port == 0 {
		return route
	}

	resolvedPort := int(records[0].Port)
	route.Dial = endpoint{Host: srvTarget, Port: resolvedPort}
	route.Handshake = endpoint{Host: target.Host, Port: resolvedPort}
	return route
}

func (c pingClient) pingEndpoint(route endpointRoute, timeout time.Duration, allowPrivateAddresses bool) (int, error) {
	conn, err := c.dialMinecraftTCP(route.Dial, timeout, allowPrivateAddresses)
	if err != nil {
		return 0, err
	}
	defer conn.Close()

	if err := conn.SetDeadline(time.Now().Add(timeout)); err != nil {
		return 0, err
	}

	if err := sendHandshakePacket(conn, route.Handshake); err != nil {
		return 0, err
	}
	if err := sendStatusRequestPacket(conn); err != nil {
		return 0, err
	}
	if err := readStatusResponse(conn); err != nil {
		return 0, err
	}

	token, err := c.tokenSource()
	if err != nil {
		return 0, err
	}
	start := c.now()

	if err := sendPingPacket(conn, token); err != nil {
		return 0, err
	}
	if err := readPongPacket(conn, token); err != nil {
		return 0, err
	}

	latencyMs := int(c.now().Sub(start) / time.Millisecond)
	if latencyMs < 1 {
		latencyMs = 1
	}

	return latencyMs, nil
}

func validateServerAddress(server string) error {
	if len(server) > maxServerAddressLength {
		return fmt.Errorf("server must not exceed %d bytes", maxServerAddressLength)
	}

	for _, r := range server {
		if r <= 0x1F || r == 0x7F {
			return errors.New("server contains control characters")
		}
	}

	return nil
}

func (c pingClient) dialMinecraftTCP(target endpoint, timeout time.Duration, allowPrivateAddresses bool) (net.Conn, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	if parsedIP := net.ParseIP(target.Host); parsedIP != nil {
		if !allowPrivateAddresses && isNonPublicIPAddress(parsedIP) {
			return nil, fmt.Errorf("refusing to connect to non-public address %s", parsedIP.String())
		}
		return c.dialContext(ctx, "tcp", endpoint{Host: parsedIP.String(), Port: target.Port}.address())
	}

	ips, err := c.resolver.LookupIP(ctx, "ip", target.Host)
	if err != nil {
		return nil, err
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("no addresses resolved for %s", target.Host)
	}

	candidates := make([]endpoint, 0, len(ips))
	for _, ip := range ips {
		ipString := ip.String()
		if !allowPrivateAddresses && isNonPublicIPAddress(ip) {
			continue
		}
		candidates = append(candidates, endpoint{Host: ipString, Port: target.Port})
	}
	if len(candidates) == 0 {
		return nil, fmt.Errorf("resolved only to non-public addresses for %s", target.Host)
	}

	var lastErr error
	for _, candidate := range candidates {
		conn, dialErr := c.dialContext(ctx, "tcp", candidate.address())
		if dialErr == nil {
			return conn, nil
		}
		lastErr = dialErr
		if ctx.Err() != nil {
			break
		}
	}
	return nil, finalizeDialError(lastErr)
}

func finalizeDialError(lastErr error) error {
	if lastErr != nil {
		return lastErr
	}
	return errors.New("failed to dial any resolved address")
}

func isNonPublicIPAddress(ip net.IP) bool {
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return true
	}

	addr = addr.Unmap()
	for _, prefix := range nonPublicIPPrefixes {
		if prefix.Contains(addr) {
			return true
		}
	}

	return false
}

func mustParsePrefix(raw string) netip.Prefix {
	prefix, err := netip.ParsePrefix(raw)
	if err != nil {
		panic(fmt.Sprintf("invalid IP prefix %q: %v", raw, err))
	}
	return prefix
}

func toUint16(value int) (uint16, error) {
	if value < 0 || value > math.MaxUint16 {
		return 0, fmt.Errorf("value %d is out of uint16 range", value)
	}
	return uint16(value), nil // #nosec G115 -- guarded by explicit bounds check above
}

func generatePingToken() (uint64, error) {
	var payload [8]byte
	if _, err := randomRead(payload[:]); err != nil {
		return 0, fmt.Errorf("failed to generate ping token: %w", err)
	}
	return binary.BigEndian.Uint64(payload[:]), nil
}

func sendHandshakePacket(w io.Writer, target endpoint) error {
	port, err := target.uint16Port()
	if err != nil {
		return err
	}

	var payload bytes.Buffer

	writeVarInt(&payload, packetIDHandshake)
	writeVarInt(&payload, statusProtocolVersion)
	if err := writeString(&payload, target.Host, maxHandshakeHostByteSize); err != nil {
		return err
	}

	var portBytes [2]byte
	binary.BigEndian.PutUint16(portBytes[:], port)
	payload.Write(portBytes[:])

	writeVarInt(&payload, nextStateStatus)
	return writePacket(w, payload.Bytes())
}

func sendStatusRequestPacket(w io.Writer) error {
	return writePacket(w, []byte{packetIDStatusRequest})
}

func sendPingPacket(w io.Writer, payloadValue uint64) error {
	var payload bytes.Buffer

	writeVarInt(&payload, packetIDPing)

	var token [8]byte
	binary.BigEndian.PutUint64(token[:], payloadValue)
	payload.Write(token[:])

	return writePacket(w, payload.Bytes())
}

func readStatusResponse(r io.Reader) error {
	payload, err := readPacket(r, maxPacketLength)
	if err != nil {
		return err
	}

	packetID, consumed, err := readVarIntFromBytes(payload)
	if err != nil {
		return err
	}
	if packetID != packetIDStatusResponse {
		return fmt.Errorf("unexpected status packet id: %d", packetID)
	}

	jsonPayload, jsonConsumed, err := readStringFromBytes(payload[consumed:], maxStatusJSONLength)
	if err != nil {
		return err
	}
	if consumed+jsonConsumed != len(payload) {
		return errors.New("invalid status response payload framing")
	}
	if !json.Valid([]byte(jsonPayload)) {
		return errors.New("invalid status response JSON")
	}

	return nil
}

func readPongPacket(r io.Reader, expected uint64) error {
	payload, err := readPacket(r, maxPacketLength)
	if err != nil {
		return err
	}

	packetID, consumed, err := readVarIntFromBytes(payload)
	if err != nil {
		return err
	}
	if packetID != packetIDPong {
		return fmt.Errorf("unexpected pong packet id: %d", packetID)
	}

	if len(payload[consumed:]) != 8 {
		return errors.New("invalid pong payload size")
	}

	received := binary.BigEndian.Uint64(payload[consumed:])
	if received != expected {
		return errors.New("pong payload mismatch")
	}

	return nil
}

func writePacket(w io.Writer, payload []byte) error {
	if len(payload) == 0 {
		return errors.New("packet payload must not be empty")
	}
	if len(payload) > maxPacketLength {
		return fmt.Errorf("packet payload exceeds maximum size: %d", len(payload))
	}

	var packet bytes.Buffer
	writeVarInt(&packet, int32(len(payload))) // #nosec G115 -- bounded by maxPacketLength
	packet.Write(payload)

	_, err := w.Write(packet.Bytes())
	return err
}

func readPacket(r io.Reader, maxLength int) ([]byte, error) {
	packetLength, err := readVarInt(r)
	if err != nil {
		return nil, err
	}
	if packetLength <= 0 {
		return nil, fmt.Errorf("invalid packet length: %d", packetLength)
	}
	if int64(packetLength) > int64(maxLength) {
		return nil, fmt.Errorf("packet length %d exceeds limit %d", packetLength, maxLength)
	}

	packet := make([]byte, packetLength)
	if _, err := io.ReadFull(r, packet); err != nil {
		return nil, err
	}

	return packet, nil
}

func readVarInt(r io.Reader) (int32, error) {
	var (
		numRead int
		result  int32
	)

	for {
		if numRead >= 5 {
			return 0, errVarIntTooLong
		}

		var one [1]byte
		if _, err := io.ReadFull(r, one[:]); err != nil {
			return 0, err
		}

		value := int32(one[0] & 0x7F)
		result |= value << (7 * numRead)
		numRead++

		if one[0]&0x80 == 0 {
			return result, nil
		}
	}
}

func writeVarInt(buf *bytes.Buffer, value int32) {
	unsigned := uint32(value) // #nosec G115 -- two's-complement reinterpretation required by MC VarInt encoding
	for {
		if unsigned&^uint32(0x7F) == 0 {
			buf.WriteByte(byte(unsigned)) // #nosec G115 -- value is masked to one byte by condition above
			return
		}
		buf.WriteByte(byte(unsigned&0x7F | 0x80)) // #nosec G115 -- low 8 bits are intentionally serialized
		unsigned >>= 7
	}
}

func readVarIntFromBytes(data []byte) (int32, int, error) {
	reader := bytes.NewReader(data)
	value, err := readVarInt(reader)
	if err != nil {
		return 0, 0, err
	}
	return value, len(data) - reader.Len(), nil
}

func writeString(buf *bytes.Buffer, value string, maxBytes int) error {
	if err := validateStringByteLength(len(value), maxBytes); err != nil {
		return err
	}

	raw := []byte(value)
	writeVarInt(buf, int32(len(raw))) // #nosec G115 -- bounded by validateStringByteLength
	_, err := buf.Write(raw)
	return err
}

func validateStringByteLength(length int, maxBytes int) error {
	if length > maxBytes {
		return fmt.Errorf("string size %d exceeds max of %d bytes", length, maxBytes)
	}
	if length > math.MaxInt32 {
		return fmt.Errorf("string size %d exceeds int32 max", length)
	}
	return nil
}

func readStringFromBytes(data []byte, maxBytes int) (string, int, error) {
	size, consumed, err := readVarIntFromBytes(data)
	if err != nil {
		return "", 0, err
	}
	if size < 0 {
		return "", 0, fmt.Errorf("invalid string size: %d", size)
	}
	if int(size) > maxBytes {
		return "", 0, fmt.Errorf("string size %d exceeds max of %d bytes", size, maxBytes)
	}

	total := consumed + int(size)
	if total > len(data) {
		return "", 0, io.ErrUnexpectedEOF
	}

	raw := data[consumed:total]
	if !utf8.Valid(raw) {
		return "", 0, errors.New("string payload is not valid UTF-8")
	}

	return string(raw), total, nil
}
