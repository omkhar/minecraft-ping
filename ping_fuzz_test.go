package main

import (
	"bytes"
	"testing"
	"unicode/utf8"
)

func FuzzReadVarIntFromBytes(f *testing.F) {
	for _, seed := range [][]byte{
		nil,
		{0x00},
		{0x01},
		{0x7f},
		{0x80, 0x01},
		{0xff, 0xff, 0xff, 0xff, 0x07},
		{0x80, 0x80, 0x80, 0x80, 0x80, 0x01},
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		value, consumed, err := readVarIntFromBytes(data)
		if err != nil {
			return
		}

		if consumed <= 0 || consumed > len(data) {
			t.Fatalf("consumed out of range: consumed=%d len=%d", consumed, len(data))
		}

		var buf bytes.Buffer
		writeVarInt(&buf, value)

		roundTrip, roundConsumed, roundErr := readVarIntFromBytes(buf.Bytes())
		if roundErr != nil {
			t.Fatalf("roundtrip parse failed: %v", roundErr)
		}
		if roundTrip != value {
			t.Fatalf("roundtrip value mismatch: got=%d want=%d", roundTrip, value)
		}
		if roundConsumed != buf.Len() {
			t.Fatalf("roundtrip consumed mismatch: got=%d want=%d", roundConsumed, buf.Len())
		}
	})
}

func FuzzReadStringFromBytes(f *testing.F) {
	f.Add([]byte{0x00}, uint16(0))
	f.Add([]byte{0x01, 'a'}, uint16(1))
	f.Add([]byte{0x02, 0xc3, 0x28}, uint16(8))
	f.Add([]byte{0x03, 'f', 'o', 'o'}, uint16(3))

	f.Fuzz(func(t *testing.T, data []byte, max uint16) {
		maxBytes := int(max)
		value, consumed, err := readStringFromBytes(data, maxBytes)
		if err != nil {
			return
		}

		if consumed <= 0 || consumed > len(data) {
			t.Fatalf("consumed out of range: consumed=%d len=%d", consumed, len(data))
		}
		if len([]byte(value)) > maxBytes {
			t.Fatalf("decoded string exceeds max: len=%d max=%d", len([]byte(value)), maxBytes)
		}
		if !utf8.ValidString(value) {
			t.Fatal("decoded string is not valid UTF-8")
		}
	})
}

func FuzzReadPacket(f *testing.F) {
	f.Add([]byte{0x01, 0x00}, uint16(1))
	f.Add([]byte{0x02, 0x00, 0x01}, uint16(2))
	f.Add([]byte{0x80, 0x01, 0x00}, uint16(128))
	f.Add([]byte{0x00}, uint16(1))

	f.Fuzz(func(t *testing.T, data []byte, max uint16) {
		maxLength := int(max)
		reader := bytes.NewReader(data)

		payload, err := readPacket(reader, maxLength)
		if err != nil {
			return
		}

		if len(payload) <= 0 {
			t.Fatalf("payload length must be positive: %d", len(payload))
		}
		if len(payload) > maxLength {
			t.Fatalf("payload length exceeds max: len=%d max=%d", len(payload), maxLength)
		}

		declaredLen, consumed, varIntErr := readVarIntFromBytes(data)
		if varIntErr != nil {
			t.Fatalf("declared length varint invalid after successful parse: %v", varIntErr)
		}
		if int(declaredLen) != len(payload) {
			t.Fatalf("declared length mismatch: declared=%d actual=%d", declaredLen, len(payload))
		}

		expectedRemaining := len(data) - consumed - len(payload)
		if expectedRemaining < 0 {
			t.Fatalf("remaining byte count underflow: %d", expectedRemaining)
		}
		if reader.Len() != expectedRemaining {
			t.Fatalf("reader remaining mismatch: got=%d want=%d", reader.Len(), expectedRemaining)
		}
	})
}
