package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
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
)

var errVarIntTooLong = errors.New("varint is too long")

func pingServer(server string, port int, timeout time.Duration) (int, error) {
	server = strings.TrimSpace(server)
	if server == "" {
		return 0, errors.New("server must not be empty")
	}
	if port < 1 || port > 65535 {
		return 0, fmt.Errorf("invalid port: %d. port must be between 1 and 65535", port)
	}
	if timeout <= 0 {
		return 0, fmt.Errorf("invalid timeout: %s. timeout must be greater than 0", timeout)
	}

	resolvedHost, resolvedPort := resolveMinecraftEndpoint(server, port, timeout)

	latency, err := pingMinecraftServer(resolvedHost, resolvedPort, timeout)
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

func pingMinecraftServer(server string, port int, timeout time.Duration) (int, error) {
	dialer := &net.Dialer{Timeout: timeout}
	conn, err := dialer.Dial("tcp", net.JoinHostPort(server, strconv.Itoa(port)))
	if err != nil {
		return 0, err
	}
	defer conn.Close()

	if err := conn.SetDeadline(time.Now().Add(timeout)); err != nil {
		return 0, err
	}

	if err := sendHandshakePacket(conn, server, uint16(port)); err != nil {
		return 0, err
	}
	if err := sendStatusRequestPacket(conn); err != nil {
		return 0, err
	}
	if err := readStatusResponse(conn); err != nil {
		return 0, err
	}

	token := time.Now().UnixNano()
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

func sendHandshakePacket(w io.Writer, host string, port uint16) error {
	var payload bytes.Buffer

	writeVarInt(&payload, 0x00) // Handshake packet ID.
	writeVarInt(&payload, statusProtocolVersion)
	if err := writeString(&payload, host, maxHandshakeHostByteSize); err != nil {
		return err
	}

	var portBytes [2]byte
	binary.BigEndian.PutUint16(portBytes[:], port)
	payload.Write(portBytes[:])

	writeVarInt(&payload, 0x01) // Next state: status.
	return writePacket(w, payload.Bytes())
}

func sendStatusRequestPacket(w io.Writer) error {
	return writePacket(w, []byte{0x00})
}

func sendPingPacket(w io.Writer, payloadValue int64) error {
	var payload bytes.Buffer

	writeVarInt(&payload, 0x01)

	var token [8]byte
	binary.BigEndian.PutUint64(token[:], uint64(payloadValue))
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
	if packetID != 0x00 {
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

func readPongPacket(r io.Reader, expected int64) error {
	payload, err := readPacket(r, maxPacketLength)
	if err != nil {
		return err
	}

	packetID, consumed, err := readVarIntFromBytes(payload)
	if err != nil {
		return err
	}
	if packetID != 0x01 {
		return fmt.Errorf("unexpected pong packet id: %d", packetID)
	}

	if len(payload[consumed:]) != 8 {
		return errors.New("invalid pong payload size")
	}

	received := int64(binary.BigEndian.Uint64(payload[consumed:]))
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
	writeVarInt(&packet, int32(len(payload)))
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
	if packetLength > int32(maxLength) {
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
	unsigned := uint32(value)
	for {
		if unsigned&^uint32(0x7F) == 0 {
			buf.WriteByte(byte(unsigned))
			return
		}
		buf.WriteByte(byte(unsigned&0x7F | 0x80))
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

	writeVarInt(buf, int32(len(raw)))
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
