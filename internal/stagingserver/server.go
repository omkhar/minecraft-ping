package stagingserver

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"slices"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	statusProtocolVersion    = -1
	maxPacketLength          = 2 * 1024 * 1024
	maxStatusJSONLength      = 1 * 1024 * 1024
	maxHandshakeHostByteSize = 255

	packetIDHandshake      int32 = 0x00
	nextStateStatus        int32 = 0x01
	packetIDStatusRequest  int32 = 0x00
	packetIDStatusResponse int32 = 0x00
	packetIDPing           int32 = 0x01
	packetIDPong           int32 = 0x01
)

var errVarIntTooLong = errors.New("varint is too long")

type Config struct {
	ListenIPv4         string
	ListenIPv6         string
	BedrockListenIPv4  string
	BedrockListenIPv6  string
	StatusJSON         string
	BedrockStatus      string
	ConnectionDeadline time.Duration
}

func DefaultStatusJSON() string {
	return `{"version":{"name":"staging","protocol":109},"players":{"max":0,"online":0,"sample":[]},"description":{"text":"minecraft-ping staging"}}`
}

func Serve(ctx context.Context, cfg Config) error {
	cfg = cfg.withDefaults()

	javaEnabled := cfg.ListenIPv4 != "" || cfg.ListenIPv6 != ""
	bedrockEnabled := cfg.BedrockListenIPv4 != "" || cfg.BedrockListenIPv6 != ""
	if javaEnabled {
		if err := validateStatusJSON(cfg.StatusJSON); err != nil {
			return err
		}
	}
	if !javaEnabled && !bedrockEnabled {
		return errors.New("at least one listen address is required")
	}

	listeners := make([]net.Listener, 0, 2)
	packetListeners := make([]net.PacketConn, 0, 2)
	closeAll := func() {
		closeListeners(listeners)
		closePacketListeners(packetListeners)
	}

	if cfg.ListenIPv4 != "" {
		listener, err := net.Listen("tcp4", cfg.ListenIPv4)
		if err != nil {
			return fmt.Errorf("listen on %s: %w", cfg.ListenIPv4, err)
		}
		listeners = append(listeners, listener)
	}
	if cfg.ListenIPv6 != "" {
		listener, err := net.Listen("tcp6", cfg.ListenIPv6)
		if err != nil {
			closeAll()
			return fmt.Errorf("listen on %s: %w", cfg.ListenIPv6, err)
		}
		listeners = append(listeners, listener)
	}
	if cfg.BedrockListenIPv4 != "" {
		packetListener, err := net.ListenPacket("udp4", cfg.BedrockListenIPv4)
		if err != nil {
			closeAll()
			return fmt.Errorf("listen on %s: %w", cfg.BedrockListenIPv4, err)
		}
		packetListeners = append(packetListeners, packetListener)
	}
	if cfg.BedrockListenIPv6 != "" {
		packetListener, err := net.ListenPacket("udp6", cfg.BedrockListenIPv6)
		if err != nil {
			closeAll()
			return fmt.Errorf("listen on %s: %w", cfg.BedrockListenIPv6, err)
		}
		packetListeners = append(packetListeners, packetListener)
	}
	if bedrockEnabled {
		if cfg.BedrockStatus == "" {
			cfg.BedrockStatus = defaultBedrockStatusForListeners(packetListeners)
		}
		if err := validateBedrockStatus(cfg.BedrockStatus); err != nil {
			closeAll()
			return err
		}
	}

	errCh := make(chan error, len(listeners)+len(packetListeners))
	var acceptWG sync.WaitGroup
	for _, listener := range listeners {
		acceptWG.Add(1)
		go func(listener net.Listener) {
			defer acceptWG.Done()
			serveListener(ctx, listener, cfg.StatusJSON, cfg.ConnectionDeadline, errCh)
		}(listener)
	}
	for _, packetListener := range packetListeners {
		acceptWG.Add(1)
		go func(packetListener net.PacketConn) {
			defer acceptWG.Done()
			serveBedrockListener(ctx, packetListener, cfg.BedrockStatus, errCh)
		}(packetListener)
	}

	defer closeAll()
	defer acceptWG.Wait()

	select {
	case <-ctx.Done():
		return nil
	case err := <-errCh:
		return err
	}
}

func (cfg Config) withDefaults() Config {
	if cfg.StatusJSON == "" {
		cfg.StatusJSON = DefaultStatusJSON()
	}
	if cfg.ConnectionDeadline <= 0 {
		cfg.ConnectionDeadline = 10 * time.Second
	}
	return cfg
}

func validateStatusJSON(statusJSON string) error {
	if len(statusJSON) > maxStatusJSONLength {
		return fmt.Errorf("status json exceeds maximum size: %d", len(statusJSON))
	}
	if !json.Valid([]byte(statusJSON)) {
		return errors.New("status json must be valid JSON")
	}
	return nil
}

func closeListeners(listeners []net.Listener) {
	for _, listener := range listeners {
		_ = listener.Close()
	}
}

func closePacketListeners(listeners []net.PacketConn) {
	for _, listener := range listeners {
		_ = listener.Close()
	}
}

func serveListener(ctx context.Context, listener net.Listener, statusJSON string, deadline time.Duration, errCh chan<- error) {
	var connWG sync.WaitGroup
	defer connWG.Wait()

	go func() {
		<-ctx.Done()
		_ = listener.Close()
	}()

	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return
			}
			select {
			case errCh <- err:
			default:
			}
			return
		}

		connWG.Add(1)
		go func(conn net.Conn) {
			defer connWG.Done()
			defer conn.Close()
			_ = handleConn(conn, statusJSON, deadline)
		}(conn)
	}
}

func handleConn(conn net.Conn, statusJSON string, deadline time.Duration) error {
	if err := conn.SetDeadline(time.Now().Add(deadline)); err != nil {
		return err
	}
	if err := expectStatusHandshake(conn); err != nil {
		return err
	}
	if err := sendStatusJSON(conn, statusJSON); err != nil {
		return err
	}
	token, err := expectPingToken(conn)
	if err != nil {
		return err
	}
	return sendPong(conn, token)
}

func expectStatusHandshake(r io.Reader) error {
	handshake, err := readPacket(r, maxPacketLength)
	if err != nil {
		return err
	}

	packetID, consumed, err := readVarIntFromBytes(handshake)
	if err != nil {
		return fmt.Errorf("read handshake packet id: %w", err)
	}
	if packetID != packetIDHandshake {
		return fmt.Errorf("unexpected handshake packet id: %d", packetID)
	}

	_, protocolBytes, err := readVarIntFromBytes(handshake[consumed:])
	if err != nil {
		return fmt.Errorf("read handshake protocol version: %w", err)
	}
	consumed += protocolBytes

	_, hostBytes, err := readStringFromBytes(handshake[consumed:], maxHandshakeHostByteSize)
	if err != nil {
		return fmt.Errorf("read handshake host: %w", err)
	}
	consumed += hostBytes

	if len(handshake[consumed:]) < 2 {
		return errors.New("missing handshake port bytes")
	}
	consumed += 2

	nextState, stateBytes, err := readVarIntFromBytes(handshake[consumed:])
	if err != nil {
		return fmt.Errorf("read handshake next state: %w", err)
	}
	consumed += stateBytes
	if nextState != nextStateStatus {
		return fmt.Errorf("unexpected next state: %d", nextState)
	}
	if consumed != len(handshake) {
		return fmt.Errorf("unexpected trailing handshake bytes: %d", len(handshake)-consumed)
	}

	statusRequest, err := readPacket(r, maxPacketLength)
	if err != nil {
		return fmt.Errorf("read status request: %w", err)
	}

	requestID, requestBytes, err := readVarIntFromBytes(statusRequest)
	if err != nil {
		return fmt.Errorf("read status request packet id: %w", err)
	}
	if requestID != packetIDStatusRequest {
		return fmt.Errorf("unexpected status request packet id: %d", requestID)
	}
	if requestBytes != len(statusRequest) {
		return fmt.Errorf("unexpected status request payload size: %d", len(statusRequest)-requestBytes)
	}

	return nil
}

func sendStatusJSON(w io.Writer, statusJSON string) error {
	var status bytes.Buffer
	writeVarInt(&status, packetIDStatusResponse)
	if err := writeString(&status, statusJSON, maxStatusJSONLength); err != nil {
		return err
	}
	return writePacket(w, status.Bytes())
}

func expectPingToken(r io.Reader) (uint64, error) {
	pingPacket, err := readPacket(r, maxPacketLength)
	if err != nil {
		return 0, err
	}

	packetID, consumed, err := readVarIntFromBytes(pingPacket)
	if err != nil {
		return 0, err
	}
	if packetID != packetIDPing {
		return 0, fmt.Errorf("unexpected ping packet id: %d", packetID)
	}
	if len(pingPacket[consumed:]) != 8 {
		return 0, fmt.Errorf("unexpected ping payload size: %d", len(pingPacket[consumed:]))
	}
	return binary.BigEndian.Uint64(pingPacket[consumed:]), nil
}

func sendPong(w io.Writer, token uint64) error {
	var payload bytes.Buffer
	writeVarInt(&payload, packetIDPong)

	var tokenBytes [8]byte
	binary.BigEndian.PutUint64(tokenBytes[:], token)
	payload.Write(tokenBytes[:])

	return writePacket(w, payload.Bytes())
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

func readVarIntFromBytes(payload []byte) (int32, int, error) {
	var (
		numRead int
		result  int32
	)

	for {
		if numRead >= 5 {
			return 0, 0, errVarIntTooLong
		}
		if numRead >= len(payload) {
			return 0, 0, io.ErrUnexpectedEOF
		}

		b := payload[numRead]
		value := int32(b & 0x7F)
		result |= value << (7 * numRead)
		numRead++

		if b&0x80 == 0 {
			return result, numRead, nil
		}
	}
}

func readStringFromBytes(payload []byte, maxBytes int) (string, int, error) {
	stringLength, consumed, err := readVarIntFromBytes(payload)
	if err != nil {
		return "", 0, err
	}
	if stringLength < 0 {
		return "", 0, fmt.Errorf("invalid string length: %d", stringLength)
	}
	if int64(stringLength) > int64(maxBytes) {
		return "", 0, fmt.Errorf("string length %d exceeds limit %d", stringLength, maxBytes)
	}

	end := consumed + int(stringLength)
	if end > len(payload) {
		return "", 0, io.ErrUnexpectedEOF
	}

	value := payload[consumed:end]
	if !utf8.Valid(value) {
		return "", 0, errors.New("string payload is not valid UTF-8")
	}
	return string(value), end, nil
}

func writeVarInt(w io.Writer, value int32) {
	var buf [5]byte
	n := 0
	// #nosec G115 -- callers only pass non-negative protocol constants and checked payload lengths.
	v := uint32(value)
	for {
		temp := byte(v & 0x7F)
		v >>= 7
		if v != 0 {
			temp |= 0x80
		}
		buf[n] = temp
		n++
		if v == 0 {
			break
		}
	}
	_, _ = w.Write(buf[:n])
}

func writeString(w io.Writer, value string, maxBytes int) error {
	if !utf8.ValidString(value) {
		return errors.New("string payload is not valid UTF-8")
	}
	if len(value) > maxBytes {
		return fmt.Errorf("string length %d exceeds limit %d", len(value), maxBytes)
	}

	// #nosec G115 -- this package only calls writeString with protocol limits well below math.MaxInt32.
	writeVarInt(w, int32(len(value)))
	_, err := io.WriteString(w, value)
	return err
}

func Probe(network, host string, port int, timeout time.Duration) error {
	if port < 0 || port > math.MaxUint16 {
		return fmt.Errorf("invalid port %d", port)
	}

	address := net.JoinHostPort(host, fmt.Sprint(port))
	dialer := net.Dialer{Timeout: timeout}
	conn, err := dialer.Dial(network, address)
	if err != nil {
		return err
	}
	defer conn.Close()

	return probeConn(conn, host, port, timeout)
}

func probeConn(conn net.Conn, host string, port int, timeout time.Duration) error {
	if err := conn.SetDeadline(time.Now().Add(timeout)); err != nil {
		return err
	}

	var handshake bytes.Buffer
	writeVarInt(&handshake, packetIDHandshake)
	writeVarInt(&handshake, statusProtocolVersion)
	if err := writeString(&handshake, host, maxHandshakeHostByteSize); err != nil {
		return err
	}

	var portBytes [2]byte
	// #nosec G115 -- port range is validated above.
	binary.BigEndian.PutUint16(portBytes[:], uint16(port))
	handshake.Write(portBytes[:])
	writeVarInt(&handshake, nextStateStatus)
	if err := writePacket(conn, handshake.Bytes()); err != nil {
		return err
	}

	if err := writePacket(conn, []byte{byte(packetIDStatusRequest)}); err != nil {
		return err
	}

	statusResponse, err := readPacket(conn, maxPacketLength)
	if err != nil {
		return fmt.Errorf("read status response: %w", err)
	}
	packetID, consumed, err := readVarIntFromBytes(statusResponse)
	if err != nil {
		return fmt.Errorf("read status response packet id: %w", err)
	}
	if packetID != packetIDStatusResponse {
		return fmt.Errorf("unexpected status response packet id: %d", packetID)
	}
	if _, _, err := readStringFromBytes(statusResponse[consumed:], maxStatusJSONLength); err != nil {
		return fmt.Errorf("read status response payload: %w", err)
	}

	const pingToken uint64 = 0x0102030405060708
	var pingPayload bytes.Buffer
	writeVarInt(&pingPayload, packetIDPing)
	var tokenBytes [8]byte
	binary.BigEndian.PutUint64(tokenBytes[:], pingToken)
	pingPayload.Write(tokenBytes[:])
	if err := writePacket(conn, pingPayload.Bytes()); err != nil {
		return err
	}

	pongPacket, err := readPacket(conn, maxPacketLength)
	if err != nil {
		return fmt.Errorf("read pong: %w", err)
	}
	packetID, consumed, err = readVarIntFromBytes(pongPacket)
	if err != nil {
		return fmt.Errorf("read pong packet id: %w", err)
	}
	if packetID != packetIDPong {
		return fmt.Errorf("unexpected pong packet id: %d", packetID)
	}
	if len(pongPacket[consumed:]) != 8 {
		return fmt.Errorf("unexpected pong payload size: %d", len(pongPacket[consumed:]))
	}
	if !slices.Equal(pongPacket[consumed:], tokenBytes[:]) {
		return errors.New("pong token mismatch")
	}

	return nil
}
