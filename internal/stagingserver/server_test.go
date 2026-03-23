package stagingserver

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"
)

func TestServeIPv4AndIPv6(t *testing.T) {
	t.Parallel()

	server := startServerWithRetry(t, func() Config {
		ipv4Port := freePort(t, "tcp4", "127.0.0.1:0")
		ipv6Port := freePort(t, "tcp6", "[::1]:0")
		bedrockIPv4Port := freePacketPort(t, "udp4", "127.0.0.1:0")
		bedrockIPv6Port := freePacketPort(t, "udp6", "[::1]:0")
		return Config{
			ListenIPv4:        fmt.Sprintf("127.0.0.1:%d", ipv4Port),
			ListenIPv6:        fmt.Sprintf("[::1]:%d", ipv6Port),
			BedrockListenIPv4: fmt.Sprintf("127.0.0.1:%d", bedrockIPv4Port),
			BedrockListenIPv6: fmt.Sprintf("[::1]:%d", bedrockIPv6Port),
		}
	}, func(cfg Config) error {
		if err := Probe("tcp4", "127.0.0.1", configuredPort(cfg.ListenIPv4, 0), 200*time.Millisecond); err != nil {
			return err
		}
		if err := Probe("tcp6", "::1", configuredPort(cfg.ListenIPv6, 0), 200*time.Millisecond); err != nil {
			return err
		}
		if err := ProbeBedrock("udp4", "127.0.0.1", configuredPort(cfg.BedrockListenIPv4, 0), 200*time.Millisecond); err != nil {
			return err
		}
		return ProbeBedrock("udp6", "::1", configuredPort(cfg.BedrockListenIPv6, 0), 200*time.Millisecond)
	})
	defer server.stop(t)
}

func TestServeRejectsInvalidStatusJSON(t *testing.T) {
	t.Parallel()

	err := Serve(context.Background(), Config{
		ListenIPv4: "127.0.0.1:0",
		StatusJSON: "{",
	})
	if err == nil || err.Error() != "status json must be valid JSON" {
		t.Fatalf("Serve() error = %v, want invalid JSON error", err)
	}
}

func TestServeRejectsInvalidBedrockStatus(t *testing.T) {
	t.Parallel()

	err := Serve(context.Background(), Config{
		BedrockListenIPv4: "127.0.0.1:0",
		BedrockStatus:     "invalid",
	})
	if err == nil || err.Error() != "bedrock status must start with MCPE;" {
		t.Fatalf("Serve() error = %v, want invalid bedrock status error", err)
	}
}

func TestServeIgnoresMalformedBedrockPackets(t *testing.T) {
	t.Parallel()

	server := startServerWithRetry(t, func() Config {
		bedrockIPv4Port := freePacketPort(t, "udp4", "127.0.0.1:0")
		return Config{
			BedrockListenIPv4: fmt.Sprintf("127.0.0.1:%d", bedrockIPv4Port),
		}
	}, func(cfg Config) error {
		return ProbeBedrock("udp4", "127.0.0.1", configuredPort(cfg.BedrockListenIPv4, 0), 200*time.Millisecond)
	})
	defer server.stop(t)

	conn, err := net.Dial("udp4", server.cfg.BedrockListenIPv4)
	if err != nil {
		t.Fatalf("net.Dial() error = %v", err)
	}
	defer conn.Close()

	if err := conn.SetDeadline(time.Now().Add(500 * time.Millisecond)); err != nil {
		t.Fatalf("SetDeadline() error = %v", err)
	}
	if _, err := conn.Write([]byte{0xff, 0x00, 0x01}); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	waitForBedrockProbe(t, "udp4", "127.0.0.1", configuredPort(server.cfg.BedrockListenIPv4, 0))
}

func TestServeIgnoresMalformedBedrockPacketsIPv6(t *testing.T) {
	t.Parallel()

	server := startServerWithRetry(t, func() Config {
		bedrockIPv6Port := freePacketPort(t, "udp6", "[::1]:0")
		return Config{
			BedrockListenIPv6: fmt.Sprintf("[::1]:%d", bedrockIPv6Port),
		}
	}, func(cfg Config) error {
		return ProbeBedrock("udp6", "::1", configuredPort(cfg.BedrockListenIPv6, 0), 200*time.Millisecond)
	})
	defer server.stop(t)

	conn, err := net.Dial("udp6", server.cfg.BedrockListenIPv6)
	if err != nil {
		t.Fatalf("net.Dial() error = %v", err)
	}
	defer conn.Close()

	if err := conn.SetDeadline(time.Now().Add(500 * time.Millisecond)); err != nil {
		t.Fatalf("SetDeadline() error = %v", err)
	}
	if _, err := conn.Write([]byte{0xff, 0x00, 0x01}); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	waitForBedrockProbe(t, "udp6", "::1", configuredPort(server.cfg.BedrockListenIPv6, 0))
}

func TestExpectStatusHandshakeRejectsMalformedPackets(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		stream  []byte
		wantErr string
	}{
		{
			name: "wrong handshake packet id",
			stream: handshakeStreamForTest(t, handshakeOptions{
				packetID: packetIDPing,
			}),
			wantErr: "unexpected handshake packet id",
		},
		{
			name: "oversized host",
			stream: handshakeStreamForTest(t, handshakeOptions{
				host: bytes.Repeat([]byte("a"), maxHandshakeHostByteSize+1),
			}),
			wantErr: "string length 256 exceeds limit 255",
		},
		{
			name: "invalid utf8 host",
			stream: handshakeStreamForTest(t, handshakeOptions{
				host: []byte{0xff},
			}),
			wantErr: "string payload is not valid UTF-8",
		},
		{
			name: "missing port bytes",
			stream: handshakeStreamForTest(t, handshakeOptions{
				omitPort: true,
			}),
			wantErr: "missing handshake port bytes",
		},
		{
			name: "unexpected next state",
			stream: handshakeStreamForTest(t, handshakeOptions{
				nextState: 2,
			}),
			wantErr: "unexpected next state",
		},
		{
			name: "trailing handshake bytes",
			stream: handshakeStreamForTest(t, handshakeOptions{
				trailingHandshake: []byte{0x00},
			}),
			wantErr: "unexpected trailing handshake bytes",
		},
		{
			name: "unexpected status request id",
			stream: handshakeStreamForTest(t, handshakeOptions{
				statusRequestPayload: []byte{0x01},
			}),
			wantErr: "unexpected status request packet id",
		},
		{
			name: "unexpected status request payload size",
			stream: handshakeStreamForTest(t, handshakeOptions{
				statusRequestPayload: []byte{0x00, 0x01},
			}),
			wantErr: "unexpected status request payload size",
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			err := expectStatusHandshake(bytes.NewReader(test.stream))
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("expectStatusHandshake() error = %v, want substring %q", err, test.wantErr)
			}
		})
	}
}

func TestExpectPingTokenRejectsMalformedPackets(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		payload []byte
		wantErr string
	}{
		{
			name:    "unexpected packet id",
			payload: []byte{0x00},
			wantErr: "unexpected ping packet id",
		},
		{
			name:    "short payload",
			payload: []byte{0x01, 0x01, 0x02},
			wantErr: "unexpected ping payload size: 2",
		},
		{
			name:    "long payload",
			payload: append([]byte{0x01}, bytes.Repeat([]byte{0x01}, 9)...),
			wantErr: "unexpected ping payload size: 9",
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			packet := packetBytesForTest(t, test.payload)
			_, err := expectPingToken(bytes.NewReader(packet))
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("expectPingToken() error = %v, want substring %q", err, test.wantErr)
			}
		})
	}
}

type handshakeOptions struct {
	packetID             int32
	host                 []byte
	omitPort             bool
	nextState            int32
	trailingHandshake    []byte
	statusRequestPayload []byte
}

func handshakeStreamForTest(t *testing.T, opts handshakeOptions) []byte {
	t.Helper()

	packetID := opts.packetID
	if packetID == 0 {
		packetID = packetIDHandshake
	}
	host := opts.host
	if host == nil {
		host = []byte("example.com")
	}
	nextState := opts.nextState
	if nextState == 0 {
		nextState = nextStateStatus
	}

	var handshake bytes.Buffer
	writeVarInt(&handshake, packetID)
	writeVarInt(&handshake, statusProtocolVersion)
	writeVarInt(&handshake, int32(len(host)))
	if _, err := handshake.Write(host); err != nil {
		t.Fatalf("Write(host) error = %v", err)
	}

	if !opts.omitPort {
		var portBytes [2]byte
		binary.BigEndian.PutUint16(portBytes[:], 25565)
		if _, err := handshake.Write(portBytes[:]); err != nil {
			t.Fatalf("Write(port) error = %v", err)
		}
		writeVarInt(&handshake, nextState)
		if len(opts.trailingHandshake) > 0 {
			if _, err := handshake.Write(opts.trailingHandshake); err != nil {
				t.Fatalf("Write(trailing) error = %v", err)
			}
		}
	}

	stream := packetBytesForTest(t, handshake.Bytes())
	statusRequestPayload := opts.statusRequestPayload
	if statusRequestPayload == nil {
		statusRequestPayload = []byte{byte(packetIDStatusRequest)}
	}
	return append(stream, packetBytesForTest(t, statusRequestPayload)...)
}

func packetBytesForTest(t *testing.T, payload []byte) []byte {
	t.Helper()

	var packet bytes.Buffer
	if err := writePacket(&packet, payload); err != nil {
		t.Fatalf("writePacket() error = %v", err)
	}
	return packet.Bytes()
}

type runningServer struct {
	cfg    Config
	cancel context.CancelFunc
	errCh  chan error
}

func startServerWithRetry(t *testing.T, newConfig func() Config, ready func(Config) error) runningServer {
	t.Helper()

	for attempt := 0; attempt < 5; attempt++ {
		cfg := newConfig()
		ctx, cancel := context.WithCancel(context.Background())
		errCh := make(chan error, 1)
		go func() {
			errCh <- Serve(ctx, cfg)
		}()

		if err, terminal := waitForServerReady(cfg, errCh, ready); err == nil {
			return runningServer{
				cfg:    cfg,
				cancel: cancel,
				errCh:  errCh,
			}
		} else {
			cancel()
			if !terminal {
				_ = waitForStopped(errCh)
			}
			if isAddressInUseError(err) {
				continue
			}
			t.Fatalf("Serve() error = %v", err)
		}
	}

	t.Fatal("failed to start staging server after repeated port collisions")
	return runningServer{}
}

func waitForServerReady(cfg Config, errCh <-chan error, ready func(Config) error) (error, bool) {
	deadline := time.Now().Add(10 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		select {
		case err := <-errCh:
			return err, true
		default:
		}
		if err := ready(cfg); err == nil {
			return nil, false
		} else {
			lastErr = err
		}
		time.Sleep(100 * time.Millisecond)
	}

	select {
	case err := <-errCh:
		return err, true
	default:
	}
	if lastErr != nil {
		return lastErr, false
	}
	return errors.New("server did not become ready"), false
}

func waitForStopped(errCh <-chan error) error {
	select {
	case err := <-errCh:
		return err
	case <-time.After(2 * time.Second):
		return errors.New("timed out waiting for staging server to stop")
	}
}

func isAddressInUseError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "address already in use")
}

func (s runningServer) stop(t *testing.T) {
	t.Helper()

	s.cancel()
	if err := waitForStopped(s.errCh); err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("Serve() shutdown error = %v", err)
	}
}

func freePort(t *testing.T, network, address string) int {
	t.Helper()

	listener, err := net.Listen(network, address)
	if err != nil {
		if network == "tcp6" {
			t.Skipf("IPv6 unavailable on test host: %v", err)
		}
		t.Fatalf("net.Listen(%q, %q): %v", network, address, err)
	}
	defer listener.Close()

	addr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("listener addr = %T, want *net.TCPAddr", listener.Addr())
	}
	return addr.Port
}

func freePacketPort(t *testing.T, network, address string) int {
	t.Helper()

	conn, err := net.ListenPacket(network, address)
	if err != nil {
		if network == "udp6" {
			t.Skipf("IPv6 unavailable on test host: %v", err)
		}
		t.Fatalf("net.ListenPacket(%q, %q): %v", network, address, err)
	}
	defer conn.Close()

	switch addr := conn.LocalAddr().(type) {
	case *net.UDPAddr:
		return addr.Port
	default:
		t.Fatalf("listener addr = %T, want *net.UDPAddr", conn.LocalAddr())
		return 0
	}
}

func waitForBedrockProbe(t *testing.T, network, host string, port int) {
	t.Helper()

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if err := ProbeBedrock(network, host, port, 2*time.Second); err == nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("bedrock probe to %s %s:%d did not succeed", network, host, port)
}
