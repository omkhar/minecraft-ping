package stagingserver

import (
	"context"
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
