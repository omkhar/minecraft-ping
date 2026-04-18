package stagingserver

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
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

func TestConfigWithDefaultsUsesExpectedPublicDefaults(t *testing.T) {
	t.Parallel()

	if statusProtocolVersion != -1 {
		t.Fatalf("statusProtocolVersion = %d, want -1", statusProtocolVersion)
	}
	if maxPacketLength != 2*1024*1024 {
		t.Fatalf("maxPacketLength = %d, want %d", maxPacketLength, 2*1024*1024)
	}
	if maxStatusJSONLength != 1*1024*1024 {
		t.Fatalf("maxStatusJSONLength = %d, want %d", maxStatusJSONLength, 1*1024*1024)
	}

	cfg := (Config{}).withDefaults()
	if cfg.StatusJSON != DefaultStatusJSON() {
		t.Fatalf("StatusJSON = %q, want default status json", cfg.StatusJSON)
	}
	if cfg.ConnectionDeadline != 10*time.Second {
		t.Fatalf("ConnectionDeadline = %v, want 10s", cfg.ConnectionDeadline)
	}

	cfg = (Config{
		StatusJSON:         `{"ok":true}`,
		ConnectionDeadline: time.Second,
	}).withDefaults()
	if cfg.StatusJSON != `{"ok":true}` {
		t.Fatalf("StatusJSON = %q, want preserved custom value", cfg.StatusJSON)
	}
	if cfg.ConnectionDeadline != time.Second {
		t.Fatalf("ConnectionDeadline = %v, want preserved custom deadline", cfg.ConnectionDeadline)
	}

	cfg = (Config{ConnectionDeadline: 1}).withDefaults()
	if cfg.ConnectionDeadline != 1 {
		t.Fatalf("ConnectionDeadline = %v, want preserved 1ns deadline", cfg.ConnectionDeadline)
	}
}

func TestServeClosesSetupResourcesOnFailure(t *testing.T) {
	t.Parallel()

	t.Run("java ipv6 failure closes tcp4 listener", func(t *testing.T) {
		t.Parallel()

		address := fmt.Sprintf("127.0.0.1:%d", freePort(t, "tcp4", "127.0.0.1:0"))
		err := Serve(context.Background(), Config{
			ListenIPv4: address,
			ListenIPv6: "[::1]:not-a-port",
		})
		if err == nil || !strings.Contains(err.Error(), "listen on [::1]:not-a-port:") {
			t.Fatalf("Serve() error = %v, want IPv6 listen failure", err)
		}

		assertCanListenTCP(t, "tcp4", address)
	})

	t.Run("bedrock ipv4 failure closes tcp4 listener", func(t *testing.T) {
		t.Parallel()

		address := fmt.Sprintf("127.0.0.1:%d", freePort(t, "tcp4", "127.0.0.1:0"))
		err := Serve(context.Background(), Config{
			ListenIPv4:        address,
			BedrockListenIPv4: "127.0.0.1:not-a-port",
		})
		if err == nil || !strings.Contains(err.Error(), "listen on 127.0.0.1:not-a-port:") {
			t.Fatalf("Serve() error = %v, want Bedrock IPv4 listen failure", err)
		}

		assertCanListenTCP(t, "tcp4", address)
	})

	t.Run("bedrock ipv6 failure closes tcp4 and udp4 listeners", func(t *testing.T) {
		t.Parallel()

		tcpAddress := fmt.Sprintf("127.0.0.1:%d", freePort(t, "tcp4", "127.0.0.1:0"))
		udpAddress := fmt.Sprintf("127.0.0.1:%d", freePacketPort(t, "udp4", "127.0.0.1:0"))
		err := Serve(context.Background(), Config{
			ListenIPv4:        tcpAddress,
			BedrockListenIPv4: udpAddress,
			BedrockListenIPv6: "[::1]:not-a-port",
		})
		if err == nil || !strings.Contains(err.Error(), "listen on [::1]:not-a-port:") {
			t.Fatalf("Serve() error = %v, want Bedrock IPv6 listen failure", err)
		}

		assertCanListenTCP(t, "tcp4", tcpAddress)
		assertCanListenPacket(t, "udp4", udpAddress)
	})

	t.Run("invalid bedrock status closes opened listeners", func(t *testing.T) {
		t.Parallel()

		tcpAddress := fmt.Sprintf("127.0.0.1:%d", freePort(t, "tcp4", "127.0.0.1:0"))
		udpAddress := fmt.Sprintf("127.0.0.1:%d", freePacketPort(t, "udp4", "127.0.0.1:0"))
		err := Serve(context.Background(), Config{
			ListenIPv4:        tcpAddress,
			BedrockListenIPv4: udpAddress,
			BedrockStatus:     "invalid",
		})
		if err == nil || err.Error() != "bedrock status must start with MCPE;" {
			t.Fatalf("Serve() error = %v, want invalid bedrock status error", err)
		}

		assertCanListenTCP(t, "tcp4", tcpAddress)
		assertCanListenPacket(t, "udp4", udpAddress)
	})
}

func TestCloseListenersClosesAll(t *testing.T) {
	t.Parallel()

	first := &acceptErrListener{}
	second := &acceptErrListener{}
	closeListeners([]net.Listener{first, second})
	if first.closeCalls != 1 || second.closeCalls != 1 {
		t.Fatalf("closeListeners() close counts = (%d, %d), want (1, 1)", first.closeCalls, second.closeCalls)
	}
}

func TestClosePacketListenersClosesAll(t *testing.T) {
	t.Parallel()

	first := &stubPacketConn{}
	second := &stubPacketConn{}
	closePacketListeners([]net.PacketConn{first, second})
	if first.closeCalls != 1 || second.closeCalls != 1 {
		t.Fatalf("closePacketListeners() close counts = (%d, %d), want (1, 1)", first.closeCalls, second.closeCalls)
	}
}

func TestServeListenerHandlesAcceptErrors(t *testing.T) {
	t.Parallel()

	t.Run("reports runtime accept error", func(t *testing.T) {
		t.Parallel()

		sentinel := errors.New("accept failed")
		listener := &acceptErrListener{acceptErr: sentinel}
		errCh := make(chan error, 1)
		serveListener(context.Background(), listener, DefaultStatusJSON(), time.Second, errCh)

		select {
		case err := <-errCh:
			if !errors.Is(err, sentinel) {
				t.Fatalf("serveListener() error = %v, want %v", err, sentinel)
			}
		default:
			t.Fatal("serveListener() did not report the accept error")
		}
	})

	t.Run("ignores closed listener", func(t *testing.T) {
		t.Parallel()

		listener := &acceptErrListener{acceptErr: net.ErrClosed}
		errCh := make(chan error, 1)
		serveListener(context.Background(), listener, DefaultStatusJSON(), time.Second, errCh)

		select {
		case err := <-errCh:
			t.Fatalf("serveListener() error = %v, want no error for net.ErrClosed", err)
		default:
		}
	})

	t.Run("ignores canceled context", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		listener := &acceptErrListener{acceptErr: errors.New("accept failed")}
		errCh := make(chan error, 1)
		serveListener(ctx, listener, DefaultStatusJSON(), time.Second, errCh)

		select {
		case err := <-errCh:
			t.Fatalf("serveListener() error = %v, want no error after context cancellation", err)
		default:
		}
	})
}

func TestServeListenerClosesListenerOnContextCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	listener := newBlockingListener()
	errCh := make(chan error, 1)
	done := make(chan struct{})

	go func() {
		serveListener(ctx, listener, DefaultStatusJSON(), time.Second, errCh)
		close(done)
	}()

	<-listener.acceptStarted
	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("serveListener() did not return after context cancellation")
	}

	if closeCalls := listener.closeCalls.Load(); closeCalls != 1 {
		t.Fatalf("listener.Close() calls = %d, want 1", closeCalls)
	}
	select {
	case err := <-errCh:
		t.Fatalf("serveListener() error = %v, want no error after context cancellation", err)
	default:
	}
}

func TestServeRequiresAtLeastOneListenAddress(t *testing.T) {
	t.Parallel()

	err := Serve(context.Background(), Config{})
	if err == nil || err.Error() != "at least one listen address is required" {
		t.Fatalf("Serve() error = %v, want missing-listen-address error", err)
	}
}

func TestServeReportsListenSetupErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cfg     Config
		wantErr string
	}{
		{
			name: "java ipv4",
			cfg: Config{
				ListenIPv4: "127.0.0.1:not-a-port",
			},
			wantErr: "listen on 127.0.0.1:not-a-port:",
		},
		{
			name: "java ipv6",
			cfg: Config{
				ListenIPv6: "[::1]:not-a-port",
			},
			wantErr: "listen on [::1]:not-a-port:",
		},
		{
			name: "bedrock ipv4",
			cfg: Config{
				BedrockListenIPv4: "127.0.0.1:not-a-port",
			},
			wantErr: "listen on 127.0.0.1:not-a-port:",
		},
		{
			name: "bedrock ipv6",
			cfg: Config{
				BedrockListenIPv6: "[::1]:not-a-port",
			},
			wantErr: "listen on [::1]:not-a-port:",
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			err := Serve(context.Background(), test.cfg)
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("Serve() error = %v, want substring %q", err, test.wantErr)
			}
		})
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

func TestWaitForServeExitClosesBeforeWaiting(t *testing.T) {
	t.Parallel()

	listener := newBlockingListener()
	errCh := make(chan error, 1)
	sentinel := errors.New("serve failed")
	errCh <- sentinel

	var acceptWG sync.WaitGroup
	acceptWG.Add(1)
	go func() {
		defer acceptWG.Done()
		_, _ = listener.Accept()
	}()

	<-listener.acceptStarted

	done := make(chan error, 1)
	go func() {
		done <- waitForServeExit(context.Background(), errCh, func() {
			_ = listener.Close()
		}, &acceptWG)
	}()

	select {
	case err := <-done:
		if !errors.Is(err, sentinel) {
			t.Fatalf("waitForServeExit() error = %v, want %v", err, sentinel)
		}
	case <-time.After(time.Second):
		t.Fatal("waitForServeExit() did not close listeners before waiting")
	}

	if closeCalls := listener.closeCalls.Load(); closeCalls != 1 {
		t.Fatalf("listener.Close() calls = %d, want 1", closeCalls)
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
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			err := expectStatusHandshake(bytes.NewReader(test.stream))
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("expectStatusHandshake() error = %v, want substring %q", err, test.wantErr)
			}
		})
	}
}

func TestExpectStatusHandshakeWrapsVarIntReadErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		stream  []byte
		wantErr string
	}{
		{
			name:    "packet id",
			stream:  packetBytesForTest(t, []byte{0x80}),
			wantErr: "read handshake packet id: unexpected EOF",
		},
		{
			name: "protocol version",
			stream: packetBytesForTest(t, []byte{
				byte(packetIDHandshake),
				0x80,
			}),
			wantErr: "read handshake protocol version: unexpected EOF",
		},
		{
			name: "next state",
			stream: func() []byte {
				var handshake bytes.Buffer
				writeVarInt(&handshake, packetIDHandshake)
				writeVarInt(&handshake, statusProtocolVersion)
				if err := writeString(&handshake, "example.com", maxHandshakeHostByteSize); err != nil {
					t.Fatalf("writeString(host) error = %v", err)
				}
				handshake.Write([]byte{0x63, 0xdd, 0x80})
				return packetBytesForTest(t, handshake.Bytes())
			}(),
			wantErr: "read handshake next state: unexpected EOF",
		},
		{
			name: "status request packet id",
			stream: func() []byte {
				var handshake bytes.Buffer
				writeVarInt(&handshake, packetIDHandshake)
				writeVarInt(&handshake, statusProtocolVersion)
				if err := writeString(&handshake, "example.com", maxHandshakeHostByteSize); err != nil {
					t.Fatalf("writeString(host) error = %v", err)
				}
				handshake.Write([]byte{0x63, 0xdd})
				writeVarInt(&handshake, nextStateStatus)
				return append(packetBytesForTest(t, handshake.Bytes()), packetBytesForTest(t, []byte{0x80})...)
			}(),
			wantErr: "read status request packet id: unexpected EOF",
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			err := expectStatusHandshake(bytes.NewReader(test.stream))
			if err == nil || err.Error() != test.wantErr {
				t.Fatalf("expectStatusHandshake() error = %v, want %q", err, test.wantErr)
			}
		})
	}
}

func TestExpectStatusHandshakeReportsExactTrailingByteCount(t *testing.T) {
	t.Parallel()

	stream := handshakeStreamForTest(t, handshakeOptions{
		trailingHandshake: []byte{0x00},
	})
	err := expectStatusHandshake(bytes.NewReader(stream))
	if err == nil || err.Error() != "unexpected trailing handshake bytes: 1" {
		t.Fatalf("expectStatusHandshake() error = %v, want exact trailing-byte error", err)
	}
}

func TestExpectStatusHandshakeReportsExactStatusRequestPayloadSize(t *testing.T) {
	t.Parallel()

	stream := handshakeStreamForTest(t, handshakeOptions{
		statusRequestPayload: []byte{0x00, 0x01},
	})
	err := expectStatusHandshake(bytes.NewReader(stream))
	if err == nil || err.Error() != "unexpected status request payload size: 1" {
		t.Fatalf("expectStatusHandshake() error = %v, want exact status-request-size error", err)
	}
}

func TestExpectStatusHandshakeRejectsSinglePortByte(t *testing.T) {
	t.Parallel()

	var handshake bytes.Buffer
	writeVarInt(&handshake, packetIDHandshake)
	writeVarInt(&handshake, statusProtocolVersion)
	if err := writeString(&handshake, "example.com", maxHandshakeHostByteSize); err != nil {
		t.Fatalf("writeString(host) error = %v", err)
	}
	handshake.WriteByte(0x63)

	err := expectStatusHandshake(bytes.NewReader(append(packetBytesForTest(t, handshake.Bytes()), packetBytesForTest(t, []byte{byte(packetIDStatusRequest)})...)))
	if err == nil || err.Error() != "missing handshake port bytes" {
		t.Fatalf("expectStatusHandshake() error = %v, want missing-port error", err)
	}
}

func TestExpectStatusHandshakeRejectsMissingNextStateAfterTwoPortBytes(t *testing.T) {
	t.Parallel()

	var handshake bytes.Buffer
	writeVarInt(&handshake, packetIDHandshake)
	writeVarInt(&handshake, statusProtocolVersion)
	if err := writeString(&handshake, "example.com", maxHandshakeHostByteSize); err != nil {
		t.Fatalf("writeString(host) error = %v", err)
	}
	handshake.Write([]byte{0x63, 0xdd})

	err := expectStatusHandshake(bytes.NewReader(append(packetBytesForTest(t, handshake.Bytes()), packetBytesForTest(t, []byte{byte(packetIDStatusRequest)})...)))
	if err == nil || err.Error() != "read handshake next state: unexpected EOF" {
		t.Fatalf("expectStatusHandshake() error = %v, want wrapped unexpected EOF", err)
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
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			packet := packetBytesForTest(t, test.payload)
			token, err := expectPingToken(bytes.NewReader(packet))
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("expectPingToken() error = %v, want substring %q", err, test.wantErr)
			}
			if token != 0 {
				t.Fatalf("expectPingToken() token = %d, want 0 on error", token)
			}
		})
	}
}

func TestReadVarIntFromBytesUsesAllContinuationBytes(t *testing.T) {
	t.Parallel()

	value, consumed, err := readVarIntFromBytes([]byte{0x80, 0x01})
	if err != nil {
		t.Fatalf("readVarIntFromBytes() error = %v", err)
	}
	if value != 128 {
		t.Fatalf("value = %d, want 128", value)
	}
	if consumed != 2 {
		t.Fatalf("consumed = %d, want 2", consumed)
	}
}

func TestValidateStatusJSONRejectsOversizedPayload(t *testing.T) {
	t.Parallel()

	statusJSON := strings.Repeat("a", maxStatusJSONLength+1)
	err := validateStatusJSON(statusJSON)
	if err == nil || err.Error() != fmt.Sprintf("status json exceeds maximum size: %d", len(statusJSON)) {
		t.Fatalf("validateStatusJSON() error = %v, want exact oversize error", err)
	}
}

func TestValidateStatusJSONAllowsExactConfiguredLimit(t *testing.T) {
	t.Parallel()

	statusJSON := exactJSONSizeForTest(t, 1*1024*1024)
	if err := validateStatusJSON(statusJSON); err != nil {
		t.Fatalf("validateStatusJSON() error = %v, want nil", err)
	}
}

func TestWritePacketRejectsInvalidPayloadSizes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		payload []byte
		wantErr string
	}{
		{
			name:    "empty",
			payload: nil,
			wantErr: "packet payload must not be empty",
		},
		{
			name:    "oversized",
			payload: bytes.Repeat([]byte{0x01}, maxPacketLength+1),
			wantErr: fmt.Sprintf("packet payload exceeds maximum size: %d", maxPacketLength+1),
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			err := writePacket(io.Discard, test.payload)
			if err == nil || err.Error() != test.wantErr {
				t.Fatalf("writePacket() error = %v, want %q", err, test.wantErr)
			}
		})
	}
}

func TestWritePacketAllowsExactConfiguredLimit(t *testing.T) {
	t.Parallel()

	payload := bytes.Repeat([]byte{0x01}, 2*1024*1024)
	if err := writePacket(io.Discard, payload); err != nil {
		t.Fatalf("writePacket() error = %v, want nil", err)
	}
}

func TestReadPacketRejectsInvalidLengthsAndShortReads(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		reader    io.Reader
		maxLength int
		wantErr   string
	}{
		{
			name:      "zero length",
			reader:    bytes.NewReader([]byte{0x00}),
			maxLength: maxPacketLength,
			wantErr:   "invalid packet length: 0",
		},
		{
			name:      "oversized length",
			reader:    func() io.Reader { var buf bytes.Buffer; writeVarInt(&buf, maxPacketLength+1); return &buf }(),
			maxLength: maxPacketLength,
			wantErr:   fmt.Sprintf("packet length %d exceeds limit %d", maxPacketLength+1, maxPacketLength),
		},
		{
			name:      "short read",
			reader:    bytes.NewReader([]byte{0x02, 0x01}),
			maxLength: maxPacketLength,
			wantErr:   io.ErrUnexpectedEOF.Error(),
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := readPacket(test.reader, test.maxLength)
			if err == nil || err.Error() != test.wantErr {
				t.Fatalf("readPacket() error = %v, want %q", err, test.wantErr)
			}
		})
	}
}

func TestReadPacketPropagatesLengthReadError(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("read length failed")
	_, err := readPacket(errReader{err: sentinel}, maxPacketLength)
	if !errors.Is(err, sentinel) {
		t.Fatalf("readPacket() error = %v, want %v", err, sentinel)
	}
}

func TestReadPacketAllowsExactConfiguredLimit(t *testing.T) {
	t.Parallel()

	const limit = 2 * 1024 * 1024
	payload := bytes.Repeat([]byte{0x01}, limit)
	packet, err := readPacket(bytes.NewReader(packetBytesForTest(t, payload)), limit)
	if err != nil {
		t.Fatalf("readPacket() error = %v, want nil", err)
	}
	if len(packet) != limit {
		t.Fatalf("packet length = %d, want %d", len(packet), limit)
	}
}

func TestExpectStatusHandshakePropagatesPacketReadErrors(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("read handshake failed")
	if err := expectStatusHandshake(errReader{err: sentinel}); !errors.Is(err, sentinel) {
		t.Fatalf("expectStatusHandshake() error = %v, want %v", err, sentinel)
	}

	err := expectStatusHandshake(bytes.NewReader(packetBytesForTest(t, []byte{0x80})))
	if err == nil || err.Error() != "read handshake packet id: unexpected EOF" {
		t.Fatalf("expectStatusHandshake() error = %v, want wrapped unexpected EOF", err)
	}
}

func TestExpectStatusHandshakePropagatesStatusRequestReadError(t *testing.T) {
	t.Parallel()

	var handshake bytes.Buffer
	writeVarInt(&handshake, packetIDHandshake)
	writeVarInt(&handshake, statusProtocolVersion)
	if err := writeString(&handshake, "example.com", maxHandshakeHostByteSize); err != nil {
		t.Fatalf("writeString(host) error = %v", err)
	}
	handshake.Write([]byte{0x63, 0xdd})
	writeVarInt(&handshake, nextStateStatus)

	sentinel := errors.New("read status request failed")
	stream := io.MultiReader(bytes.NewReader(packetBytesForTest(t, handshake.Bytes())), errReader{err: sentinel})
	err := expectStatusHandshake(stream)
	if err == nil || !errors.Is(err, sentinel) || err.Error() != "read status request: read status request failed" {
		t.Fatalf("expectStatusHandshake() error = %v, want wrapped status-request read error", err)
	}
}

func TestExpectPingTokenPropagatesPacketReadErrors(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("read ping failed")
	if token, err := expectPingToken(errReader{err: sentinel}); !errors.Is(err, sentinel) {
		t.Fatalf("expectPingToken() error = %v, want %v", err, sentinel)
	} else if token != 0 {
		t.Fatalf("expectPingToken() token = %d, want 0 on error", token)
	}

	token, err := expectPingToken(bytes.NewReader(packetBytesForTest(t, []byte{0x80})))
	if err == nil || err.Error() != io.ErrUnexpectedEOF.Error() {
		t.Fatalf("expectPingToken() error = %v, want unexpected EOF", err)
	}
	if token != 0 {
		t.Fatalf("expectPingToken() token = %d, want 0 on error", token)
	}
}

func TestSendStatusJSONPropagatesWriteStringError(t *testing.T) {
	t.Parallel()

	statusJSON := strings.Repeat("a", maxStatusJSONLength+1)
	err := sendStatusJSON(io.Discard, statusJSON)
	if err == nil || err.Error() != fmt.Sprintf("string length %d exceeds limit %d", len(statusJSON), maxStatusJSONLength) {
		t.Fatalf("sendStatusJSON() error = %v, want exact size-limit error", err)
	}
}

func TestReadVarIntRejectsTooLongAndReadErrors(t *testing.T) {
	t.Parallel()

	tooLong := bytes.NewReader([]byte{0x80, 0x80, 0x80, 0x80, 0x80})
	if value, err := readVarInt(tooLong); !errors.Is(err, errVarIntTooLong) {
		t.Fatalf("readVarInt() error = %v, want %v", err, errVarIntTooLong)
	} else if value != 0 {
		t.Fatalf("readVarInt() value = %d, want 0 on error", value)
	}

	sentinel := errors.New("read varint failed")
	if value, err := readVarInt(errReader{err: sentinel}); !errors.Is(err, sentinel) {
		t.Fatalf("readVarInt() error = %v, want %v", err, sentinel)
	} else if value != 0 {
		t.Fatalf("readVarInt() value = %d, want 0 on error", value)
	}

	value, err := readVarInt(bytes.NewReader([]byte{0x80, 0x80, 0x80, 0x80, 0x01}))
	if err != nil {
		t.Fatalf("readVarInt() boundary error = %v, want nil", err)
	}
	if value != 268435456 {
		t.Fatalf("readVarInt() boundary value = %d, want %d", value, 268435456)
	}
}

func TestReadVarIntFromBytesRejectsTooLongAndShortPayloads(t *testing.T) {
	t.Parallel()

	if value, consumed, err := readVarIntFromBytes([]byte{0x80, 0x80, 0x80, 0x80, 0x80}); !errors.Is(err, errVarIntTooLong) {
		t.Fatalf("readVarIntFromBytes() error = %v, want %v", err, errVarIntTooLong)
	} else if value != 0 || consumed != 0 {
		t.Fatalf("readVarIntFromBytes() = (%d, %d), want (0, 0) on error", value, consumed)
	}
	if value, consumed, err := readVarIntFromBytes([]byte{0x80}); err == nil || err.Error() != io.ErrUnexpectedEOF.Error() {
		t.Fatalf("readVarIntFromBytes() error = %v, want unexpected EOF", err)
	} else if value != 0 || consumed != 0 {
		t.Fatalf("readVarIntFromBytes() = (%d, %d), want (0, 0) on error", value, consumed)
	}
}

func TestReadStringFromBytesRejectsInvalidLengthsAndShortPayloads(t *testing.T) {
	t.Parallel()

	t.Run("negative length", func(t *testing.T) {
		t.Parallel()

		var payload bytes.Buffer
		writeVarInt(&payload, -1)
		value, consumed, err := readStringFromBytes(payload.Bytes(), 8)
		if err == nil || err.Error() != "invalid string length: -1" {
			t.Fatalf("readStringFromBytes() error = %v, want negative-length error", err)
		}
		if value != "" || consumed != 0 {
			t.Fatalf("readStringFromBytes() = (%q, %d), want (\"\", 0) on error", value, consumed)
		}
	})

	t.Run("size limit", func(t *testing.T) {
		t.Parallel()

		var payload bytes.Buffer
		writeVarInt(&payload, 9)
		value, consumed, err := readStringFromBytes(payload.Bytes(), 8)
		if err == nil || err.Error() != "string length 9 exceeds limit 8" {
			t.Fatalf("readStringFromBytes() error = %v, want size-limit error", err)
		}
		if value != "" || consumed != 0 {
			t.Fatalf("readStringFromBytes() = (%q, %d), want (\"\", 0) on error", value, consumed)
		}
	})

	t.Run("short payload", func(t *testing.T) {
		t.Parallel()

		var payload bytes.Buffer
		writeVarInt(&payload, 4)
		payload.WriteString("abc")
		value, consumed, err := readStringFromBytes(payload.Bytes(), 8)
		if err == nil || err.Error() != io.ErrUnexpectedEOF.Error() {
			t.Fatalf("readStringFromBytes() error = %v, want unexpected EOF", err)
		}
		if value != "" || consumed != 0 {
			t.Fatalf("readStringFromBytes() = (%q, %d), want (\"\", 0) on error", value, consumed)
		}
	})

	t.Run("truncated length prefix", func(t *testing.T) {
		t.Parallel()

		value, consumed, err := readStringFromBytes([]byte{0x80}, 8)
		if err == nil || err.Error() != io.ErrUnexpectedEOF.Error() {
			t.Fatalf("readStringFromBytes() error = %v, want unexpected EOF", err)
		}
		if value != "" || consumed != 0 {
			t.Fatalf("readStringFromBytes() = (%q, %d), want (\"\", 0) on error", value, consumed)
		}
	})
}

func TestReadStringFromBytesAllowsEmptyAndExactLimit(t *testing.T) {
	t.Parallel()

	t.Run("empty", func(t *testing.T) {
		t.Parallel()

		value, consumed, err := readStringFromBytes([]byte{0x00}, 8)
		if err != nil {
			t.Fatalf("readStringFromBytes() error = %v, want nil", err)
		}
		if value != "" || consumed != 1 {
			t.Fatalf("readStringFromBytes() = (%q, %d), want (\"\", 1)", value, consumed)
		}
	})

	t.Run("exact limit", func(t *testing.T) {
		t.Parallel()

		var payload bytes.Buffer
		writeVarInt(&payload, 8)
		payload.WriteString("abcdefgh")
		value, consumed, err := readStringFromBytes(payload.Bytes(), 8)
		if err != nil {
			t.Fatalf("readStringFromBytes() error = %v, want nil", err)
		}
		if value != "abcdefgh" || consumed != payload.Len() {
			t.Fatalf("readStringFromBytes() = (%q, %d), want (%q, %d)", value, consumed, "abcdefgh", payload.Len())
		}
	})
}

func TestWriteStringRejectsInvalidUTF8AndOversizedPayloads(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		value   string
		max     int
		wantErr string
	}{
		{
			name:    "invalid utf8",
			value:   string([]byte{0xff}),
			max:     8,
			wantErr: "string payload is not valid UTF-8",
		},
		{
			name:    "oversized",
			value:   strings.Repeat("a", 9),
			max:     8,
			wantErr: "string length 9 exceeds limit 8",
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			err := writeString(io.Discard, test.value, test.max)
			if err == nil || err.Error() != test.wantErr {
				t.Fatalf("writeString() error = %v, want %q", err, test.wantErr)
			}
		})
	}
}

func TestReadStringFromBytesRejectsInvalidUTF8(t *testing.T) {
	t.Parallel()

	var payload bytes.Buffer
	writeVarInt(&payload, 1)
	payload.WriteByte(0xff)
	value, consumed, err := readStringFromBytes(payload.Bytes(), 8)
	if err == nil || err.Error() != "string payload is not valid UTF-8" {
		t.Fatalf("readStringFromBytes() error = %v, want invalid-utf8 error", err)
	}
	if value != "" || consumed != 0 {
		t.Fatalf("readStringFromBytes() = (%q, %d), want (\"\", 0) on error", value, consumed)
	}
}

func TestWriteStringAllowsExactLimit(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	if err := writeString(&buf, "abcdefgh", 8); err != nil {
		t.Fatalf("writeString() error = %v, want nil", err)
	}
}

func TestHandleConnPropagatesStepErrors(t *testing.T) {
	t.Parallel()

	t.Run("deadline", func(t *testing.T) {
		t.Parallel()

		sentinel := errors.New("deadline failed")
		conn := &scriptedConn{
			reader:      bytes.NewReader(nil),
			deadlineErr: sentinel,
		}
		if err := handleConn(conn, DefaultStatusJSON(), time.Second); !errors.Is(err, sentinel) {
			t.Fatalf("handleConn() error = %v, want %v", err, sentinel)
		}
	})

	t.Run("handshake", func(t *testing.T) {
		t.Parallel()

		conn := &scriptedConn{
			reader: bytes.NewReader(handshakeStreamForTest(t, handshakeOptions{
				packetID: packetIDPing,
			})),
		}
		err := handleConn(conn, DefaultStatusJSON(), time.Second)
		if err == nil || !strings.Contains(err.Error(), "unexpected handshake packet id") {
			t.Fatalf("handleConn() error = %v, want handshake failure", err)
		}
	})

	t.Run("status write", func(t *testing.T) {
		t.Parallel()

		sentinel := errors.New("write failed")
		conn := &scriptedConn{
			reader:   bytes.NewReader(handshakeStreamForTest(t, handshakeOptions{})),
			writeErr: sentinel,
		}
		if err := handleConn(conn, DefaultStatusJSON(), time.Second); !errors.Is(err, sentinel) {
			t.Fatalf("handleConn() error = %v, want %v", err, sentinel)
		}
	})

	t.Run("ping", func(t *testing.T) {
		t.Parallel()

		stream := append(handshakeStreamForTest(t, handshakeOptions{}), packetBytesForTest(t, []byte{byte(packetIDPing), 0x01})...)
		conn := &scriptedConn{
			reader: bytes.NewReader(stream),
		}
		err := handleConn(conn, DefaultStatusJSON(), time.Second)
		if err == nil || err.Error() != "unexpected ping payload size: 1" {
			t.Fatalf("handleConn() error = %v, want exact ping-size error", err)
		}
	})
}

func TestServeIgnoresConnectionHandlerErrors(t *testing.T) {
	t.Parallel()

	server := startServerWithRetry(t, func() Config {
		port := freePort(t, "tcp4", "127.0.0.1:0")
		return Config{ListenIPv4: fmt.Sprintf("127.0.0.1:%d", port)}
	}, func(cfg Config) error {
		return Probe("tcp4", "127.0.0.1", configuredPort(cfg.ListenIPv4, 0), 200*time.Millisecond)
	})
	defer server.stop(t)

	conn, err := net.Dial("tcp4", server.cfg.ListenIPv4)
	if err != nil {
		t.Fatalf("net.Dial() error = %v", err)
	}
	if _, err := conn.Write(packetBytesForTest(t, []byte{byte(packetIDPing)})); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	_ = conn.Close()

	select {
	case err := <-server.errCh:
		t.Fatalf("Serve() error = %v, want server to ignore per-connection handshake errors", err)
	case <-time.After(200 * time.Millisecond):
	}

	if err := Probe("tcp4", "127.0.0.1", configuredPort(server.cfg.ListenIPv4, 0), 200*time.Millisecond); err != nil {
		t.Fatalf("Probe() after malformed client error = %v", err)
	}
}

func TestProbeRejectsOnlyOutOfRangePorts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		port    int
		wantErr string
	}{
		{name: "negative", port: -1, wantErr: "invalid port -1"},
		{name: "too large", port: 1 + int(^uint16(0)), wantErr: "invalid port 65536"},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			err := Probe("tcp4", "127.0.0.1", test.port, 50*time.Millisecond)
			if err == nil || err.Error() != test.wantErr {
				t.Fatalf("Probe() error = %v, want %q", err, test.wantErr)
			}
		})
	}

	for _, port := range []int{0, int(^uint16(0))} {
		port := port
		t.Run(fmt.Sprintf("accepted boundary %d", port), func(t *testing.T) {
			t.Parallel()

			err := Probe("tcp4", "127.0.0.1", port, 50*time.Millisecond)
			if err == nil {
				t.Fatal("Probe() error = nil, want a network error from an unserved loopback port")
			}
			if strings.Contains(err.Error(), "invalid port") {
				t.Fatalf("Probe() error = %v, want boundary port to pass validation", err)
			}
		})
	}
}

func TestProbeConnUsesExpectedHandshakeAndValidatesResponses(t *testing.T) {
	t.Parallel()

	t.Run("successful exchange", func(t *testing.T) {
		t.Parallel()

		conn := &scriptedConn{
			reader: bytes.NewReader(append(statusResponsePacketForTest(t, `{"description":{"text":"ok"}}`), pongPacketForTest(t, 0x0102030405060708)...)),
		}
		if err := probeConn(conn, "example.com", 25565, time.Second); err != nil {
			t.Fatalf("probeConn() error = %v", err)
		}

		writes := bytes.NewReader(conn.writes.Bytes())
		handshakePacket, err := readPacket(writes, 2*1024*1024)
		if err != nil {
			t.Fatalf("readPacket(handshake) error = %v", err)
		}
		packetID, consumed, err := readVarIntFromBytes(handshakePacket)
		if err != nil {
			t.Fatalf("readVarIntFromBytes(packet id) error = %v", err)
		}
		if packetID != packetIDHandshake {
			t.Fatalf("packetID = %d, want %d", packetID, packetIDHandshake)
		}
		protocol, protocolBytes, err := readVarIntFromBytes(handshakePacket[consumed:])
		if err != nil {
			t.Fatalf("readVarIntFromBytes(protocol) error = %v", err)
		}
		if protocol != -1 {
			t.Fatalf("protocol = %d, want -1", protocol)
		}
		consumed += protocolBytes
		host, hostBytes, err := readStringFromBytes(handshakePacket[consumed:], maxHandshakeHostByteSize)
		if err != nil {
			t.Fatalf("readStringFromBytes(host) error = %v", err)
		}
		if host != "example.com" {
			t.Fatalf("host = %q, want %q", host, "example.com")
		}
		consumed += hostBytes
		if got := binary.BigEndian.Uint16(handshakePacket[consumed : consumed+2]); got != 25565 {
			t.Fatalf("port = %d, want 25565", got)
		}
	})

	t.Run("deadline", func(t *testing.T) {
		t.Parallel()

		sentinel := errors.New("deadline failed")
		conn := &scriptedConn{
			reader:      bytes.NewReader(nil),
			deadlineErr: sentinel,
		}
		if err := probeConn(conn, "example.com", 25565, time.Second); !errors.Is(err, sentinel) {
			t.Fatalf("probeConn() error = %v, want %v", err, sentinel)
		}
	})

	t.Run("host write validation", func(t *testing.T) {
		t.Parallel()

		conn := &scriptedConn{reader: bytes.NewReader(nil)}
		err := probeConn(conn, strings.Repeat("a", maxHandshakeHostByteSize+1), 25565, time.Second)
		if err == nil || err.Error() != "string length 256 exceeds limit 255" {
			t.Fatalf("probeConn() error = %v, want host-limit error", err)
		}
	})

	t.Run("handshake write", func(t *testing.T) {
		t.Parallel()

		sentinel := errors.New("handshake write failed")
		conn := &scriptedConn{
			reader:         bytes.NewReader(nil),
			writeErr:       sentinel,
			writeErrOnCall: 1,
		}
		if err := probeConn(conn, "example.com", 25565, time.Second); !errors.Is(err, sentinel) {
			t.Fatalf("probeConn() error = %v, want %v", err, sentinel)
		}
	})

	t.Run("status request write", func(t *testing.T) {
		t.Parallel()

		sentinel := errors.New("status request write failed")
		conn := &scriptedConn{
			reader:         bytes.NewReader(append(statusResponsePacketForTest(t, `{"description":{"text":"ok"}}`), pongPacketForTest(t, 0x0102030405060708)...)),
			writeErr:       sentinel,
			writeErrOnCall: 2,
		}
		if err := probeConn(conn, "example.com", 25565, time.Second); !errors.Is(err, sentinel) {
			t.Fatalf("probeConn() error = %v, want %v", err, sentinel)
		}
	})

	t.Run("pong token mismatch", func(t *testing.T) {
		t.Parallel()

		conn := &scriptedConn{
			reader: bytes.NewReader(append(statusResponsePacketForTest(t, `{"description":{"text":"ok"}}`), pongPacketForTest(t, 0x1112131415161718)...)),
		}
		err := probeConn(conn, "example.com", 25565, time.Second)
		if err == nil || err.Error() != "pong token mismatch" {
			t.Fatalf("probeConn() error = %v, want token-mismatch error", err)
		}
	})

	t.Run("invalid status response id", func(t *testing.T) {
		t.Parallel()

		conn := &scriptedConn{
			reader: bytes.NewReader(append(statusResponsePacketForTest(t, `{"description":{"text":"ok"}}`, byte(packetIDPing)), pongPacketForTest(t, 0x0102030405060708)...)),
		}
		err := probeConn(conn, "example.com", 25565, time.Second)
		if err == nil || !strings.Contains(err.Error(), "unexpected status response packet id") {
			t.Fatalf("probeConn() error = %v, want status-response-id error", err)
		}
	})

	t.Run("status response read", func(t *testing.T) {
		t.Parallel()

		sentinel := errors.New("status response read failed")
		conn := &scriptedConn{reader: errReader{err: sentinel}}
		err := probeConn(conn, "example.com", 25565, time.Second)
		if err == nil || !errors.Is(err, sentinel) || err.Error() != "read status response: status response read failed" {
			t.Fatalf("probeConn() error = %v, want wrapped status-response read error", err)
		}
	})

	t.Run("status response packet id parse", func(t *testing.T) {
		t.Parallel()

		conn := &scriptedConn{
			reader: bytes.NewReader(append(packetBytesForTest(t, []byte{0x80}), pongPacketForTest(t, 0x0102030405060708)...)),
		}
		err := probeConn(conn, "example.com", 25565, time.Second)
		if err == nil || err.Error() != "read status response packet id: unexpected EOF" {
			t.Fatalf("probeConn() error = %v, want wrapped status-response packet-id error", err)
		}
	})

	t.Run("invalid status response payload", func(t *testing.T) {
		t.Parallel()

		conn := &scriptedConn{
			reader: bytes.NewReader(append(statusResponseRawPacketForTest(t, []byte{byte(packetIDStatusResponse), 0x01, 0xff}), pongPacketForTest(t, 0x0102030405060708)...)),
		}
		err := probeConn(conn, "example.com", 25565, time.Second)
		if err == nil || err.Error() != "read status response payload: string payload is not valid UTF-8" {
			t.Fatalf("probeConn() error = %v, want wrapped status-response payload error", err)
		}
	})

	t.Run("ping write", func(t *testing.T) {
		t.Parallel()

		sentinel := errors.New("ping write failed")
		conn := &scriptedConn{
			reader:         bytes.NewReader(append(statusResponsePacketForTest(t, `{"description":{"text":"ok"}}`), pongPacketForTest(t, 0x0102030405060708)...)),
			writeErr:       sentinel,
			writeErrOnCall: 3,
		}
		if err := probeConn(conn, "example.com", 25565, time.Second); !errors.Is(err, sentinel) {
			t.Fatalf("probeConn() error = %v, want %v", err, sentinel)
		}
	})

	t.Run("pong read", func(t *testing.T) {
		t.Parallel()

		sentinel := errors.New("pong read failed")
		conn := &scriptedConn{
			reader: io.MultiReader(bytes.NewReader(statusResponsePacketForTest(t, `{"description":{"text":"ok"}}`)), errReader{err: sentinel}),
		}
		err := probeConn(conn, "example.com", 25565, time.Second)
		if err == nil || !errors.Is(err, sentinel) || err.Error() != "read pong: pong read failed" {
			t.Fatalf("probeConn() error = %v, want wrapped pong read error", err)
		}
	})

	t.Run("pong packet id parse", func(t *testing.T) {
		t.Parallel()

		conn := &scriptedConn{
			reader: bytes.NewReader(append(statusResponsePacketForTest(t, `{"description":{"text":"ok"}}`), packetBytesForTest(t, []byte{0x80})...)),
		}
		err := probeConn(conn, "example.com", 25565, time.Second)
		if err == nil || err.Error() != "read pong packet id: unexpected EOF" {
			t.Fatalf("probeConn() error = %v, want wrapped pong packet-id error", err)
		}
	})

	t.Run("invalid pong packet id", func(t *testing.T) {
		t.Parallel()

		conn := &scriptedConn{
			reader: bytes.NewReader(append(statusResponsePacketForTest(t, `{"description":{"text":"ok"}}`), pongPacketForTest(t, 0x0102030405060708, byte(packetIDStatusRequest))...)),
		}
		err := probeConn(conn, "example.com", 25565, time.Second)
		if err == nil || err.Error() != "unexpected pong packet id: 0" {
			t.Fatalf("probeConn() error = %v, want exact invalid-pong-id error", err)
		}
	})

	t.Run("invalid pong payload size", func(t *testing.T) {
		t.Parallel()

		rawPong := packetBytesForTest(t, append([]byte{byte(packetIDPong)}, bytes.Repeat([]byte{0x01}, 7)...))
		conn := &scriptedConn{
			reader: bytes.NewReader(append(statusResponsePacketForTest(t, `{"description":{"text":"ok"}}`), rawPong...)),
		}
		err := probeConn(conn, "example.com", 25565, time.Second)
		if err == nil || !strings.Contains(err.Error(), "unexpected pong payload size") {
			t.Fatalf("probeConn() error = %v, want pong-size error", err)
		}
	})
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

func exactJSONSizeForTest(t *testing.T, size int) string {
	t.Helper()

	const envelope = `{"a":""}`
	if size < len(envelope) {
		t.Fatalf("requested JSON size %d is smaller than minimum envelope %d", size, len(envelope))
	}
	return `{"a":"` + strings.Repeat("a", size-len(envelope)) + `"}`
}

func statusResponsePacketForTest(t *testing.T, statusJSON string, packetID ...byte) []byte {
	t.Helper()

	id := byte(packetIDStatusResponse)
	if len(packetID) > 0 {
		id = packetID[0]
	}

	var payload bytes.Buffer
	payload.WriteByte(id)
	if err := writeString(&payload, statusJSON, maxStatusJSONLength); err != nil {
		t.Fatalf("writeString(status response) error = %v", err)
	}
	return packetBytesForTest(t, payload.Bytes())
}

func statusResponseRawPacketForTest(t *testing.T, payload []byte) []byte {
	t.Helper()
	return packetBytesForTest(t, payload)
}

func pongPacketForTest(t *testing.T, token uint64, packetID ...byte) []byte {
	t.Helper()

	id := byte(packetIDPong)
	if len(packetID) > 0 {
		id = packetID[0]
	}

	var payload bytes.Buffer
	payload.WriteByte(id)
	var tokenBytes [8]byte
	binary.BigEndian.PutUint64(tokenBytes[:], token)
	payload.Write(tokenBytes[:])
	return packetBytesForTest(t, payload.Bytes())
}

type runningServer struct {
	cfg    Config
	cancel context.CancelFunc
	errCh  chan error
}

func startServerWithRetry(t *testing.T, newConfig func() Config, ready func(Config) error) runningServer {
	t.Helper()

	for range 5 {
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

func assertCanListenTCP(t *testing.T, network, address string) {
	t.Helper()

	listener, err := net.Listen(network, address)
	if err != nil {
		t.Fatalf("net.Listen(%q, %q): %v", network, address, err)
	}
	_ = listener.Close()
}

func assertCanListenPacket(t *testing.T, network, address string) {
	t.Helper()

	conn, err := net.ListenPacket(network, address)
	if err != nil {
		t.Fatalf("net.ListenPacket(%q, %q): %v", network, address, err)
	}
	_ = conn.Close()
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

type errReader struct {
	err error
}

func (r errReader) Read([]byte) (int, error) {
	return 0, r.err
}

type scriptedConn struct {
	reader         io.Reader
	writeErr       error
	writeErrOnCall int
	writeCalls     int
	deadlineErr    error
	writes         bytes.Buffer
}

func (c *scriptedConn) Read(p []byte) (int, error) {
	return c.reader.Read(p)
}

func (c *scriptedConn) Write(p []byte) (int, error) {
	c.writeCalls++
	if c.writeErr != nil && (c.writeErrOnCall == 0 || c.writeCalls == c.writeErrOnCall) {
		return 0, c.writeErr
	}
	return c.writes.Write(p)
}

func (*scriptedConn) Close() error { return nil }

func (*scriptedConn) LocalAddr() net.Addr  { return dummyAddr("local") }
func (*scriptedConn) RemoteAddr() net.Addr { return dummyAddr("remote") }

func (c *scriptedConn) SetDeadline(time.Time) error {
	return c.deadlineErr
}

func (*scriptedConn) SetReadDeadline(time.Time) error  { return nil }
func (*scriptedConn) SetWriteDeadline(time.Time) error { return nil }

type acceptErrListener struct {
	acceptErr  error
	closeCalls int
}

func (l *acceptErrListener) Accept() (net.Conn, error) { return nil, l.acceptErr }
func (l *acceptErrListener) Close() error {
	l.closeCalls++
	return nil
}
func (*acceptErrListener) Addr() net.Addr { return dummyAddr("listener") }

type blockingListener struct {
	acceptStarted chan struct{}
	closeCh       chan struct{}
	closeCalls    atomic.Int32
}

func newBlockingListener() *blockingListener {
	return &blockingListener{
		acceptStarted: make(chan struct{}),
		closeCh:       make(chan struct{}),
	}
}

func (l *blockingListener) Accept() (net.Conn, error) {
	close(l.acceptStarted)
	<-l.closeCh
	return nil, net.ErrClosed
}

func (l *blockingListener) Close() error {
	if l.closeCalls.CompareAndSwap(0, 1) {
		close(l.closeCh)
		return nil
	}
	l.closeCalls.Add(1)
	return nil
}

func (*blockingListener) Addr() net.Addr { return dummyAddr("listener") }

type stubPacketConn struct {
	closeCalls int
}

func (*stubPacketConn) ReadFrom([]byte) (int, net.Addr, error)    { return 0, dummyAddr("remote"), io.EOF }
func (*stubPacketConn) WriteTo(p []byte, _ net.Addr) (int, error) { return len(p), nil }
func (c *stubPacketConn) Close() error {
	c.closeCalls++
	return nil
}
func (*stubPacketConn) LocalAddr() net.Addr              { return dummyAddr("packet") }
func (*stubPacketConn) SetDeadline(time.Time) error      { return nil }
func (*stubPacketConn) SetReadDeadline(time.Time) error  { return nil }
func (*stubPacketConn) SetWriteDeadline(time.Time) error { return nil }

type dummyAddr string

func (addr dummyAddr) Network() string { return "tcp" }
func (addr dummyAddr) String() string  { return string(addr) }
