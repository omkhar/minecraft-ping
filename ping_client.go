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

type pingOptions struct {
	allowPrivateAddresses bool
}

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

func pingServer(server string, port int, timeout time.Duration) (int, error) {
	return pingServerWithOptions(server, port, timeout, pingOptions{
		allowPrivateAddresses: true,
	})
}

func pingServerWithOptions(server string, port int, timeout time.Duration, options pingOptions) (int, error) {
	server = strings.TrimSpace(server)
	if server == "" {
		return 0, errors.New("server must not be empty")
	}
	if err := validateServerAddress(server); err != nil {
		return 0, err
	}
	if port < 1 || port > 65535 {
		return 0, fmt.Errorf("invalid port: %d. port must be between 1 and 65535", port)
	}
	if timeout <= 0 {
		return 0, fmt.Errorf("invalid timeout: %s. timeout must be greater than 0", timeout)
	}
	if timeout > maxAllowedTimeout {
		return 0, fmt.Errorf("invalid timeout: %s. timeout must be less than or equal to %s", timeout, maxAllowedTimeout)
	}

	resolvedHost, resolvedPort := resolveMinecraftEndpoint(server, port, timeout)

	latency, err := pingMinecraftServer(resolvedHost, resolvedPort, server, timeout, options.allowPrivateAddresses)
	if err != nil {
		if resolvedHost != server || resolvedPort != port {
			return 0, fmt.Errorf(
				"failed to ping server %s:%d (resolved to %s:%d): %w",
				server,
				port,
				resolvedHost,
				resolvedPort,
				err,
			)
		}
		return 0, fmt.Errorf("failed to ping server %s:%d: %w", server, port, err)
	}

	return latency, nil
}

func resolveMinecraftEndpoint(server string, port int, timeout time.Duration) (string, int) {
	if port != defaultMinecraftPort || net.ParseIP(server) != nil {
		return server, port
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	_, records, err := net.DefaultResolver.LookupSRV(ctx, "minecraft", "tcp", server)
	if err != nil || len(records) == 0 {
		return server, port
	}

	target := strings.TrimSuffix(records[0].Target, ".")
	if target == "" || records[0].Port == 0 {
		return server, port
	}

	return target, int(records[0].Port)
}

func pingMinecraftServer(server string, port int, handshakeHost string, timeout time.Duration, allowPrivateAddresses bool) (int, error) {
	conn, err := dialMinecraftTCP(server, port, timeout, allowPrivateAddresses)
	if err != nil {
		return 0, err
	}
	defer conn.Close()

	if err := conn.SetDeadline(time.Now().Add(timeout)); err != nil {
		return 0, err
	}

	handshakePort, err := toUint16(port)
	if err != nil {
		return 0, err
	}

	if err := sendHandshakePacket(conn, handshakeHost, handshakePort); err != nil {
		return 0, err
	}
	if err := sendStatusRequestPacket(conn); err != nil {
		return 0, err
	}
	if err := readStatusResponse(conn); err != nil {
		return 0, err
	}

	token, err := generatePingToken()
	if err != nil {
		return 0, err
	}
	start := time.Now()

	if err := sendPingPacket(conn, token); err != nil {
		return 0, err
	}
	if err := readPongPacket(conn, token); err != nil {
		return 0, err
	}

	latencyMs := int(time.Since(start) / time.Millisecond)
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

func dialMinecraftTCP(server string, port int, timeout time.Duration, allowPrivateAddresses bool) (net.Conn, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	join := func(host string) string {
		return net.JoinHostPort(host, strconv.Itoa(port))
	}

	dialer := &net.Dialer{}

	if parsedIP := net.ParseIP(server); parsedIP != nil {
		if !allowPrivateAddresses && isNonPublicIPAddress(parsedIP) {
			return nil, fmt.Errorf("refusing to connect to non-public address %s", parsedIP.String())
		}
		return dialer.DialContext(ctx, "tcp", join(parsedIP.String()))
	}

	ips, err := net.DefaultResolver.LookupIP(ctx, "ip", server)
	if err != nil {
		return nil, err
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("no addresses resolved for %s", server)
	}

	type candidate struct {
		addr string
	}
	candidates := make([]candidate, 0, len(ips))
	for _, ip := range ips {
		ipString := ip.String()
		if ipString == "" {
			continue
		}
		if !allowPrivateAddresses && isNonPublicIPAddress(ip) {
			continue
		}
		candidates = append(candidates, candidate{addr: join(ipString)})
	}
	if len(candidates) == 0 {
		return nil, fmt.Errorf("resolved only to non-public addresses for %s", server)
	}

	var lastErr error
	for _, candidate := range candidates {
		conn, dialErr := dialer.DialContext(ctx, "tcp", candidate.addr)
		if dialErr == nil {
			return conn, nil
		}
		lastErr = dialErr
		if ctx.Err() != nil {
			break
		}
	}

	if lastErr != nil {
		return nil, lastErr
	}

	return nil, errors.New("failed to dial any resolved address")
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
	if _, err := rand.Read(payload[:]); err != nil {
		return 0, fmt.Errorf("failed to generate ping token: %w", err)
	}
	return binary.BigEndian.Uint64(payload[:]), nil
}

func sendHandshakePacket(w io.Writer, host string, port uint16) error {
	var payload bytes.Buffer

	writeVarInt(&payload, packetIDHandshake)
	writeVarInt(&payload, statusProtocolVersion)
	if err := writeString(&payload, host, maxHandshakeHostByteSize); err != nil {
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
	if len(payload) > math.MaxInt32 {
		return fmt.Errorf("packet payload exceeds int32 max: %d", len(payload))
	}

	var packet bytes.Buffer
	writeVarInt(&packet, int32(len(payload))) // #nosec G115 -- bounded by MaxInt32 check above
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
	raw := []byte(value)
	if len(raw) > maxBytes {
		return fmt.Errorf("string size %d exceeds max of %d bytes", len(raw), maxBytes)
	}
	if len(raw) > math.MaxInt32 {
		return fmt.Errorf("string size %d exceeds int32 max", len(raw))
	}

	writeVarInt(buf, int32(len(raw))) // #nosec G115 -- bounded by MaxInt32 check above
	_, err := buf.Write(raw)
	return err
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
