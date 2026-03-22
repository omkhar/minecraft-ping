package main

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"unicode/utf8"
)

const (
	statusProtocolVersion    = 109
	maxPacketLength          = 2 * 1024 * 1024
	maxStatusJSONLength      = 1 * 1024 * 1024
	maxHandshakeHostByteSize = 255

	// Minecraft Java Edition protocol packet IDs (context-dependent by state).
	packetIDHandshake      int32 = 0x00
	nextStateStatus        int32 = 0x01
	packetIDStatusRequest  byte  = 0x00
	packetIDStatusResponse int32 = 0x00
	packetIDPing           int32 = 0x01
	packetIDPong           int32 = 0x01
)

var errVarIntTooLong = errors.New("varint is too long")

func generatePingToken() (uint64, error) {
	var payload [8]byte
	if _, err := rand.Read(payload[:]); err != nil {
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
