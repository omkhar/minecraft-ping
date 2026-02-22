package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestPingServer(t *testing.T) {
	host, port, wait := startTCPTestServer(
		t,
		mockStatusPongHandler(`{"version":{"name":"1.20.6","protocol":766},"players":{"max":1000,"online":42},"description":"ok"}`, 15*time.Millisecond),
	)
	defer wait()

	tests := []struct {
		name    string
		server  string
		port    int
		timeout time.Duration
		wantErr bool
	}{
		{
			name:    "Valid server and port",
			server:  host,
			port:    port,
			timeout: 5 * time.Second,
			wantErr: false,
		},
		{
			name:    "Invalid port - too low",
			server:  host,
			port:    0,
			timeout: 5 * time.Second,
			wantErr: true,
		},
		{
			name:    "Invalid port - too high",
			server:  host,
			port:    65536,
			timeout: 5 * time.Second,
			wantErr: true,
		},
		{
			name:    "Invalid server",
			server:  "203.0.113.1",
			port:    25565,
			timeout: 250 * time.Millisecond,
			wantErr: true,
		},
		{
			name:    "Invalid timeout",
			server:  host,
			port:    port,
			timeout: -1 * time.Second,
			wantErr: true,
		},
		{
			name:    "Invalid timeout zero",
			server:  host,
			port:    port,
			timeout: 0,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			latency, err := pingServer(tt.server, tt.port, tt.timeout)

			if tt.wantErr {
				if err == nil {
					t.Errorf("pingServer() expected error but got none")
				}
				return
			}

			if err != nil {
				t.Fatalf("pingServer() error = %v, wantErr %v", err, tt.wantErr)
			}
			if latency <= 0 {
				t.Fatalf("pingServer() got invalid latency: %d", latency)
			}
		})
	}
}

func TestPingServerMalformedStatusPacket(t *testing.T) {
	host, port, wait := startTCPTestServer(t, func(conn net.Conn) error {
		if err := conn.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
			return err
		}

		if err := readHandshakeAndStatusRequest(conn); err != nil {
			return err
		}

		var malformed bytes.Buffer
		writeVarInt(&malformed, 0x02)
		if err := writeString(&malformed, "{}", maxStatusJSONLength); err != nil {
			return err
		}

		return writePacket(conn, malformed.Bytes())
	})
	defer wait()

	_, err := pingServer(host, port, 2*time.Second)
	if err == nil {
		t.Fatal("pingServer() expected malformed status packet error but got nil")
	}
}

func TestPingServerPongMismatch(t *testing.T) {
	host, port, wait := startTCPTestServer(t, func(conn net.Conn) error {
		if err := conn.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
			return err
		}

		if err := readHandshakeAndStatusRequest(conn); err != nil {
			return err
		}

		var status bytes.Buffer
		writeVarInt(&status, 0x00)
		if err := writeString(&status, `{"version":{"name":"1.20.6","protocol":766},"players":{"max":1,"online":1},"description":"ok"}`, maxStatusJSONLength); err != nil {
			return err
		}
		if err := writePacket(conn, status.Bytes()); err != nil {
			return err
		}

		pingPacket, err := readPacket(conn, maxPacketLength)
		if err != nil {
			return err
		}
		packetID, consumed, err := readVarIntFromBytes(pingPacket)
		if err != nil {
			return err
		}
		if packetID != 0x01 {
			return fmt.Errorf("unexpected ping packet id: %d", packetID)
		}
		if len(pingPacket[consumed:]) != 8 {
			return fmt.Errorf("ping payload size = %d, want 8", len(pingPacket[consumed:]))
		}

		payload := int64(binary.BigEndian.Uint64(pingPacket[consumed:])) + 1
		var pong bytes.Buffer
		writeVarInt(&pong, 0x01)
		var pongPayload [8]byte
		binary.BigEndian.PutUint64(pongPayload[:], uint64(payload))
		pong.Write(pongPayload[:])

		return writePacket(conn, pong.Bytes())
	})
	defer wait()

	_, err := pingServer(host, port, 2*time.Second)
	if err == nil {
		t.Fatal("pingServer() expected pong mismatch error but got nil")
	}
}

func TestVarIntRoundTrip(t *testing.T) {
	values := []int32{0, 1, 2, 127, 128, 255, 2147483647, -1}

	for _, value := range values {
		var buf bytes.Buffer
		writeVarInt(&buf, value)

		got, err := readVarInt(&buf)
		if err != nil {
			t.Fatalf("readVarInt() error for value %d: %v", value, err)
		}
		if got != value {
			t.Fatalf("varint roundtrip mismatch: got %d, want %d", got, value)
		}
		if buf.Len() != 0 {
			t.Fatalf("varint reader left unread bytes: %d", buf.Len())
		}
	}
}

func TestReadVarIntTooLong(t *testing.T) {
	data := bytes.NewReader([]byte{0x80, 0x80, 0x80, 0x80, 0x80, 0x01})

	_, err := readVarInt(data)
	if !errors.Is(err, errVarIntTooLong) {
		t.Fatalf("readVarInt() error = %v, want %v", err, errVarIntTooLong)
	}
}

func TestReadStringFromBytesRejectsOversizedPayload(t *testing.T) {
	raw := []byte("hello")

	var payload bytes.Buffer
	writeVarInt(&payload, int32(len(raw)))
	payload.Write(raw)

	_, _, err := readStringFromBytes(payload.Bytes(), len(raw)-1)
	if err == nil {
		t.Fatal("readStringFromBytes() expected oversized payload error but got nil")
	}
}

func TestPingServerWithOptionsRejectsPrivateAddressByDefault(t *testing.T) {
	_, err := pingServerWithOptions("127.0.0.1", 25565, 2*time.Second, pingOptions{
		allowPrivateAddresses: false,
	})
	if err == nil {
		t.Fatal("pingServerWithOptions() expected private address rejection but got nil")
	}
	if !strings.Contains(err.Error(), "non-public address") {
		t.Fatalf("pingServerWithOptions() error = %q, expected non-public address rejection", err.Error())
	}
}

func TestPingServerWithOptionsAllowsPrivateAddressWhenEnabled(t *testing.T) {
	host, port, wait := startTCPTestServer(
		t,
		mockStatusPongHandler(`{"version":{"name":"1.20.6","protocol":766},"players":{"max":1000,"online":42},"description":"ok"}`, 10*time.Millisecond),
	)
	defer wait()

	latency, err := pingServerWithOptions(host, port, 2*time.Second, pingOptions{
		allowPrivateAddresses: true,
	})
	if err != nil {
		t.Fatalf("pingServerWithOptions() unexpected error: %v", err)
	}
	if latency <= 0 {
		t.Fatalf("pingServerWithOptions() got invalid latency: %d", latency)
	}
}

func TestPingServerRejectsExcessiveTimeout(t *testing.T) {
	_, err := pingServer("127.0.0.1", 25565, maxAllowedTimeout+time.Second)
	if err == nil {
		t.Fatal("pingServer() expected timeout bounds error but got nil")
	}
	if !strings.Contains(err.Error(), "less than or equal") {
		t.Fatalf("pingServer() error = %q, expected timeout bounds message", err.Error())
	}
}

func TestPingServerRejectsControlCharacterInHost(t *testing.T) {
	_, err := pingServer("exa\nmple.com", 25565, 2*time.Second)
	if err == nil {
		t.Fatal("pingServer() expected host validation error but got nil")
	}
	if !strings.Contains(err.Error(), "control characters") {
		t.Fatalf("pingServer() error = %q, expected control-character message", err.Error())
	}
}

func startTCPTestServer(t *testing.T, handler func(net.Conn) error) (string, int, func()) {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start test server: %v", err)
	}

	errCh := make(chan error, 1)
	go func() {
		defer close(errCh)

		conn, err := listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			errCh <- err
			return
		}
		defer conn.Close()

		errCh <- handler(conn)
	}()

	host, portText, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		listener.Close()
		t.Fatalf("failed to parse test server addr: %v", err)
	}

	port, err := strconv.Atoi(portText)
	if err != nil {
		listener.Close()
		t.Fatalf("failed to parse test server port: %v", err)
	}

	wait := func() {
		t.Helper()
		_ = listener.Close()
		if err, ok := <-errCh; ok && err != nil {
			t.Fatalf("mock server error: %v", err)
		}
	}

	return host, port, wait
}

func mockStatusPongHandler(statusJSON string, pongDelay time.Duration) func(net.Conn) error {
	return func(conn net.Conn) error {
		if err := conn.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
			return err
		}

		if err := readHandshakeAndStatusRequest(conn); err != nil {
			return err
		}

		var status bytes.Buffer
		writeVarInt(&status, 0x00)
		if err := writeString(&status, statusJSON, maxStatusJSONLength); err != nil {
			return err
		}
		if err := writePacket(conn, status.Bytes()); err != nil {
			return err
		}

		pingPacket, err := readPacket(conn, maxPacketLength)
		if err != nil {
			return err
		}
		packetID, consumed, err := readVarIntFromBytes(pingPacket)
		if err != nil {
			return err
		}
		if packetID != 0x01 {
			return fmt.Errorf("unexpected ping packet id: %d", packetID)
		}
		if len(pingPacket[consumed:]) != 8 {
			return fmt.Errorf("ping payload size = %d, want 8", len(pingPacket[consumed:]))
		}

		if pongDelay > 0 {
			time.Sleep(pongDelay)
		}

		var pong bytes.Buffer
		writeVarInt(&pong, 0x01)
		pong.Write(pingPacket[consumed:])
		return writePacket(conn, pong.Bytes())
	}
}

func readHandshakeAndStatusRequest(conn net.Conn) error {
	handshake, err := readPacket(conn, maxPacketLength)
	if err != nil {
		return err
	}

	packetID, consumed, err := readVarIntFromBytes(handshake)
	if err != nil {
		return err
	}
	if packetID != 0x00 {
		return fmt.Errorf("unexpected handshake packet id: %d", packetID)
	}

	_, protocolBytes, err := readVarIntFromBytes(handshake[consumed:])
	if err != nil {
		return err
	}
	consumed += protocolBytes

	_, hostBytes, err := readStringFromBytes(handshake[consumed:], maxHandshakeHostByteSize)
	if err != nil {
		return err
	}
	consumed += hostBytes

	if len(handshake[consumed:]) < 2 {
		return errors.New("missing handshake port bytes")
	}
	consumed += 2

	nextState, stateBytes, err := readVarIntFromBytes(handshake[consumed:])
	if err != nil {
		return err
	}
	consumed += stateBytes

	if nextState != 0x01 {
		return fmt.Errorf("unexpected next state: %d", nextState)
	}
	if consumed != len(handshake) {
		return fmt.Errorf("unexpected trailing handshake bytes: %d", len(handshake)-consumed)
	}

	statusRequest, err := readPacket(conn, maxPacketLength)
	if err != nil {
		return err
	}

	requestID, requestBytes, err := readVarIntFromBytes(statusRequest)
	if err != nil {
		return err
	}
	if requestID != 0x00 {
		return fmt.Errorf("unexpected status request packet id: %d", requestID)
	}
	if requestBytes != len(statusRequest) {
		return fmt.Errorf("unexpected status request payload size: %d", len(statusRequest)-requestBytes)
	}

	return nil
}
