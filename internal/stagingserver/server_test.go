package stagingserver

import (
	"context"
	"errors"
	"fmt"
	"net"
	"testing"
	"time"
)

func TestServeIPv4AndIPv6(t *testing.T) {
	t.Parallel()

	ipv4Port := freePort(t, "tcp4", "127.0.0.1:0")
	ipv6Port := freePort(t, "tcp6", "[::1]:0")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- Serve(ctx, Config{
			ListenIPv4: fmt.Sprintf("127.0.0.1:%d", ipv4Port),
			ListenIPv6: fmt.Sprintf("[::1]:%d", ipv6Port),
		})
	}()

	waitForProbe(t, "tcp4", "127.0.0.1", ipv4Port)
	waitForProbe(t, "tcp6", "::1", ipv6Port)

	cancel()
	if err := <-errCh; err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("Serve() error = %v", err)
	}
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

func waitForProbe(t *testing.T, network, host string, port int) {
	t.Helper()

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if err := Probe(network, host, port, 2*time.Second); err == nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("probe to %s %s:%d did not succeed", network, host, port)
}
