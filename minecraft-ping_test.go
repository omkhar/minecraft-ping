package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"
)

const validStatusJSON = `{"version":{"name":"1.20.6","protocol":766},"players":{"max":1000,"online":42},"description":"ok"}`

func TestPingServer(t *testing.T) {
	server := startFakeMinecraftServer(t, statusPongScript(validStatusJSON, 15*time.Millisecond))
	defer server.Close()

	tests := []struct {
		name    string
		target  endpoint
		timeout time.Duration
		options pingOptions
		wantErr bool
	}{
		{
			name:    "Valid server and port",
			target:  server.Endpoint(),
			timeout: 5 * time.Second,
			options: pingOptions{allowPrivateAddresses: true},
			wantErr: false,
		},
		{
			name:    "Invalid port - too low",
			target:  newEndpoint(server.Endpoint().Host, 0),
			timeout: 5 * time.Second,
			wantErr: true,
		},
		{
			name:    "Invalid port - too high",
			target:  newEndpoint(server.Endpoint().Host, 65536),
			timeout: 5 * time.Second,
			wantErr: true,
		},
		{
			name:    "Invalid server",
			target:  newEndpoint("203.0.113.1", defaultMinecraftPort),
			timeout: 250 * time.Millisecond,
			wantErr: true,
		},
		{
			name:    "Invalid timeout",
			target:  server.Endpoint(),
			timeout: -1 * time.Second,
			options: pingOptions{allowPrivateAddresses: true},
			wantErr: true,
		},
		{
			name:    "Invalid timeout zero",
			target:  server.Endpoint(),
			timeout: 0,
			options: pingOptions{allowPrivateAddresses: true},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			latency, err := ping(tt.target, tt.timeout, tt.options)

			if tt.wantErr {
				if err == nil {
					t.Errorf("ping() expected error but got none")
				}
				return
			}

			if err != nil {
				t.Fatalf("ping() error = %v, wantErr %v", err, tt.wantErr)
			}
			if latency <= 0 {
				t.Fatalf("ping() got invalid latency: %d", latency)
			}
		})
	}
}

func TestPingServerMalformedStatusPacket(t *testing.T) {
	server := startFakeMinecraftServer(t, func(conn *fakeMinecraftConn) error {
		if err := conn.SetDeadline(2 * time.Second); err != nil {
			return err
		}

		if _, err := conn.ExpectStatusHandshake(); err != nil {
			return err
		}

		var malformed bytes.Buffer
		writeVarInt(&malformed, 0x02)
		if err := writeString(&malformed, "{}", maxStatusJSONLength); err != nil {
			return err
		}

		return conn.SendPacket(malformed.Bytes())
	})
	defer server.Close()

	_, err := ping(server.Endpoint(), 2*time.Second, pingOptions{allowPrivateAddresses: true})
	if err == nil {
		t.Fatal("ping() expected malformed status packet error but got nil")
	}
}

func TestPingServerPongMismatch(t *testing.T) {
	server := startFakeMinecraftServer(t, func(conn *fakeMinecraftConn) error {
		if err := conn.SetDeadline(2 * time.Second); err != nil {
			return err
		}

		if _, err := conn.ExpectStatusHandshake(); err != nil {
			return err
		}
		if err := conn.SendStatusJSON(validStatusJSON); err != nil {
			return err
		}

		token, err := conn.ExpectPingToken()
		if err != nil {
			return err
		}

		return conn.SendPong(token + 1)
	})
	defer server.Close()

	_, err := ping(server.Endpoint(), 2*time.Second, pingOptions{allowPrivateAddresses: true})
	if err == nil {
		t.Fatal("ping() expected pong mismatch error but got nil")
	}
}

func TestPingServerRejectsLoopbackAddressByDefault(t *testing.T) {
	server := startFakeMinecraftServer(t, statusPongScript(validStatusJSON, 0))
	defer server.Close()

	_, err := ping(server.Endpoint(), 2*time.Second, pingOptions{})
	if err == nil {
		t.Fatal("ping() expected non-public address rejection but got nil")
	}
	if !strings.Contains(err.Error(), "non-public address") {
		t.Fatalf("ping() error = %q, want non-public address rejection", err.Error())
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

func TestPingServerWithOptionsAllowsLoopbackAddress(t *testing.T) {
	server := startFakeMinecraftServer(t, statusPongScript(validStatusJSON, 0))
	defer server.Close()

	latency, err := ping(server.Endpoint(), 2*time.Second, pingOptions{
		addressFamily:         addressFamily4,
		allowPrivateAddresses: true,
	})
	if err != nil {
		t.Fatalf("ping() unexpected error: %v", err)
	}
	if latency <= 0 {
		t.Fatalf("ping() got invalid latency: %d", latency)
	}
}

func TestPingEndpointWithOptionsAllowsLoopbackAddress(t *testing.T) {
	server := startFakeMinecraftServer(t, statusPongScript(validStatusJSON, 10*time.Millisecond))
	defer server.Close()

	latency, err := ping(server.Endpoint(), 2*time.Second, pingOptions{
		allowPrivateAddresses: true,
	})
	if err != nil {
		t.Fatalf("ping() unexpected error: %v", err)
	}
	if latency <= 0 {
		t.Fatalf("ping() got invalid latency: %d", latency)
	}
}

func TestPingServerRejectsExcessiveTimeout(t *testing.T) {
	_, err := ping(newEndpoint("127.0.0.1", defaultMinecraftPort), maxAllowedTimeout+time.Second, pingOptions{})
	if err == nil {
		t.Fatal("ping() expected timeout bounds error but got nil")
	}
	if !strings.Contains(err.Error(), "less than or equal") {
		t.Fatalf("ping() error = %q, expected timeout bounds message", err.Error())
	}
}

func TestPingServerRejectsControlCharacterInHost(t *testing.T) {
	_, err := ping(newEndpoint("exa\nmple.com", defaultMinecraftPort), 2*time.Second, pingOptions{})
	if err == nil {
		t.Fatal("ping() expected host validation error but got nil")
	}
	if !strings.Contains(err.Error(), "control characters") {
		t.Fatalf("ping() error = %q, expected control-character message", err.Error())
	}
}

func TestEndpointValidate(t *testing.T) {
	tests := []struct {
		name    string
		target  endpoint
		wantErr string
	}{
		{
			name:    "empty host",
			target:  newEndpoint("   ", defaultMinecraftPort),
			wantErr: "must not be empty",
		},
		{
			name:    "invalid port",
			target:  newEndpoint("mc.example.com", 70000),
			wantErr: "invalid port",
		},
		{
			name:    "valid endpoint",
			target:  newEndpoint("mc.example.com", defaultMinecraftPort),
			wantErr: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.target.validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("validate() error = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("validate() error = %v, want substring %q", err, tt.wantErr)
			}
		})
	}
}

func TestResolveEndpointUsesSRV(t *testing.T) {
	resolver := &stubResolver{
		srvRecords: []*net.SRV{{Target: "srv.example.net.", Port: 25570}},
	}
	client := pingClient{resolver: resolver}

	route := client.withDefaults().resolveEndpoint(newEndpoint("mc.example.com", defaultMinecraftPort), 2*time.Second)

	if route.Dial != (endpoint{Host: "srv.example.net", Port: 25570}) {
		t.Fatalf("dial endpoint = %+v", route.Dial)
	}
	if route.Handshake != (endpoint{Host: "mc.example.com", Port: 25570}) {
		t.Fatalf("handshake endpoint = %+v", route.Handshake)
	}
	if resolver.srvCalls != 1 {
		t.Fatalf("LookupSRV calls = %d, want 1", resolver.srvCalls)
	}
}

func TestResolveEndpointSkipsSRVForIPAndCustomPort(t *testing.T) {
	resolver := &stubResolver{}
	client := pingClient{resolver: resolver}

	ipRoute := client.withDefaults().resolveEndpoint(newEndpoint("127.0.0.1", defaultMinecraftPort), 2*time.Second)
	if ipRoute.Dial != (endpoint{Host: "127.0.0.1", Port: defaultMinecraftPort}) {
		t.Fatalf("ip route = %+v", ipRoute)
	}

	customRoute := client.withDefaults().resolveEndpoint(newEndpoint("mc.example.com", 25570), 2*time.Second)
	if customRoute.Dial != (endpoint{Host: "mc.example.com", Port: 25570}) {
		t.Fatalf("custom route = %+v", customRoute)
	}
	if resolver.srvCalls != 0 {
		t.Fatalf("LookupSRV calls = %d, want 0", resolver.srvCalls)
	}
}

func TestResolveJavaRouteSkipsSRVForExplicitDefaultPort(t *testing.T) {
	resolver := &stubResolver{
		srvRecords: []*net.SRV{{Target: "srv.example.net.", Port: 25570}},
	}
	client := pingClient{resolver: resolver}

	route, err := client.withDefaults().resolveJavaRouteContext(context.Background(), newTargetSpec("mc.example.com", defaultMinecraftPort, true))
	if err != nil {
		t.Fatalf("resolveJavaRouteContext() error = %v", err)
	}
	if route.Dial != (endpoint{Host: "mc.example.com", Port: defaultMinecraftPort}) {
		t.Fatalf("dial endpoint = %+v", route.Dial)
	}
	if route.Handshake != (endpoint{Host: "mc.example.com", Port: defaultMinecraftPort}) {
		t.Fatalf("handshake endpoint = %+v", route.Handshake)
	}
	if resolver.srvCalls != 0 {
		t.Fatalf("LookupSRV calls = %d, want 0", resolver.srvCalls)
	}
}

func TestResolveEndpointFallsBackOnInvalidSRVRecord(t *testing.T) {
	client := pingClient{
		resolver: &stubResolver{
			srvRecords: []*net.SRV{{Target: "", Port: 25570}},
		},
	}

	route := client.withDefaults().resolveEndpoint(newEndpoint("mc.example.com", defaultMinecraftPort), 2*time.Second)
	if route.Dial != (endpoint{Host: "mc.example.com", Port: defaultMinecraftPort}) {
		t.Fatalf("route = %+v, want unresolved target", route)
	}
}

func TestResolveEndpointFallsBackWhenSRVUnavailable(t *testing.T) {
	target := newEndpoint("mc.example.com", defaultMinecraftPort)

	tests := []struct {
		name     string
		resolver *stubResolver
	}{
		{
			name:     "lookup error",
			resolver: &stubResolver{srvErr: errors.New("srv failed")},
		},
		{
			name:     "no records",
			resolver: &stubResolver{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			route := pingClient{resolver: tt.resolver}.withDefaults().resolveEndpoint(target, 2*time.Second)
			if route.Dial != target || route.Handshake != target {
				t.Fatalf("route = %+v, want unresolved target %+v", route, target)
			}
			if tt.resolver.srvCalls != 1 {
				t.Fatalf("LookupSRV calls = %d, want 1", tt.resolver.srvCalls)
			}
		})
	}
}

func TestDialMinecraftTCPSkipsNonPublicCandidatesByDefault(t *testing.T) {
	successConn, peer := net.Pipe()
	defer peer.Close()

	resolver := &stubResolver{
		ipAddrs: []netip.Addr{
			mustAddr("127.0.0.1"),
			mustAddr("8.8.8.8"),
		},
	}
	dialer := &stubDialer{
		results: map[string]dialResult{
			"8.8.8.8:25565": {conn: successConn},
		},
		defaultErr: errors.New("unexpected dial"),
	}
	client := pingClient{
		resolver:    resolver,
		dialContext: dialer.DialContext,
	}

	conn, err := client.withDefaults().dialMinecraftTCP(newEndpoint("mc.example.com", defaultMinecraftPort), 2*time.Second, false)
	if err != nil {
		t.Fatalf("dialMinecraftTCP() error: %v", err)
	}
	_ = conn.Close()

	if strings.Join(dialer.attempts, ",") != "8.8.8.8:25565" {
		t.Fatalf("dial attempts = %v, want only public candidates", dialer.attempts)
	}
}

func TestDialMinecraftTCPAllowsNonPublicCandidatesWithOptIn(t *testing.T) {
	successConn, peer := net.Pipe()
	defer peer.Close()

	dialer := &stubDialer{
		results: map[string]dialResult{
			"8.8.8.8:25565": {conn: successConn},
		},
		defaultErr: errors.New("unexpected dial"),
	}
	client := pingClient{
		resolver: &stubResolver{
			ipAddrs: []netip.Addr{
				mustAddr("127.0.0.1"),
				mustAddr("10.0.0.8"),
				mustAddr("8.8.8.8"),
			},
		},
		dialContext: dialer.DialContext,
	}

	conn, err := client.withDefaults().dialMinecraftTCP(newEndpoint("mc.example.com", defaultMinecraftPort), 2*time.Second, true)
	if err != nil {
		t.Fatalf("dialMinecraftTCP() error: %v", err)
	}
	_ = conn.Close()

	if strings.Join(dialer.attempts, ",") != "127.0.0.1:25565,10.0.0.8:25565,8.8.8.8:25565" {
		t.Fatalf("dial attempts = %v, want private and public candidates", dialer.attempts)
	}
}

func TestDialMinecraftTCPRejectsHostsThatResolveOnlyToNonPublicAddresses(t *testing.T) {
	client := pingClient{
		resolver: &stubResolver{
			ipAddrs: []netip.Addr{
				mustAddr("127.0.0.1"),
				mustAddr("10.0.0.8"),
			},
		},
	}

	_, err := client.withDefaults().dialMinecraftTCP(newEndpoint("mc.example.com", defaultMinecraftPort), 2*time.Second, false)
	if err == nil {
		t.Fatal("dialMinecraftTCP() expected error")
	}
	if !strings.Contains(err.Error(), "resolved only to non-public addresses") {
		t.Fatalf("dialMinecraftTCP() error = %v", err)
	}
}

func TestDialMinecraftTCPTriesCandidatesUntilSuccess(t *testing.T) {
	successConn, peer := net.Pipe()
	defer peer.Close()

	dialer := &stubDialer{
		results: map[string]dialResult{
			"8.8.8.8:25565": {err: errors.New("first attempt failed")},
			"1.1.1.1:25565": {conn: successConn},
		},
	}
	client := pingClient{
		resolver: &stubResolver{
			ipAddrs: []netip.Addr{
				mustAddr("8.8.8.8"),
				mustAddr("1.1.1.1"),
			},
		},
		dialContext: dialer.DialContext,
	}

	conn, err := client.withDefaults().dialMinecraftTCP(newEndpoint("mc.example.com", defaultMinecraftPort), 2*time.Second, false)
	if err != nil {
		t.Fatalf("dialMinecraftTCP() error: %v", err)
	}
	_ = conn.Close()

	if strings.Join(dialer.attempts, ",") != "8.8.8.8:25565,1.1.1.1:25565" {
		t.Fatalf("dial attempts = %v", dialer.attempts)
	}
}

func TestDialMinecraftTCPDirectIPAllowsPublicAddresses(t *testing.T) {
	successConn, peer := net.Pipe()
	defer peer.Close()

	dialer := &stubDialer{
		results: map[string]dialResult{
			"8.8.8.8:25565": {conn: successConn},
		},
	}
	client := pingClient{dialContext: dialer.DialContext}

	conn, err := client.withDefaults().dialMinecraftTCP(newEndpoint("8.8.8.8", defaultMinecraftPort), 2*time.Second, false)
	if err != nil {
		t.Fatalf("dialMinecraftTCP() error: %v", err)
	}
	_ = conn.Close()

	if len(dialer.attempts) != 1 || dialer.attempts[0] != "8.8.8.8:25565" {
		t.Fatalf("dial attempts = %v", dialer.attempts)
	}
}

func TestDialMinecraftTCPPropagatesLookupAndDialErrors(t *testing.T) {
	lookupErr := errors.New("lookup failed")
	client := pingClient{
		resolver: &stubResolver{ipErr: lookupErr},
	}

	_, err := client.withDefaults().dialMinecraftTCP(newEndpoint("mc.example.com", defaultMinecraftPort), 2*time.Second, false)
	if !errors.Is(err, lookupErr) {
		t.Fatalf("dialMinecraftTCP() error = %v, want %v", err, lookupErr)
	}

	dialErr := errors.New("all dials failed")
	client = pingClient{
		resolver: &stubResolver{
			ipAddrs: []netip.Addr{mustAddr("8.8.8.8")},
		},
		dialContext: func(context.Context, string, string) (net.Conn, error) {
			return nil, dialErr
		},
	}

	_, err = client.withDefaults().dialMinecraftTCP(newEndpoint("mc.example.com", defaultMinecraftPort), 2*time.Second, false)
	if !errors.Is(err, dialErr) {
		t.Fatalf("dialMinecraftTCP() error = %v, want %v", err, dialErr)
	}
}

func TestDialMinecraftTCPResolverEdgeCases(t *testing.T) {
	t.Run("no resolved addresses", func(t *testing.T) {
		client := pingClient{
			resolver: &stubResolver{ipAddrs: []netip.Addr{}},
		}

		_, err := client.withDefaults().dialMinecraftTCP(newEndpoint("mc.example.com", defaultMinecraftPort), 2*time.Second, false)
		if err == nil || !strings.Contains(err.Error(), "no addresses resolved") {
			t.Fatalf("dialMinecraftTCP() error = %v, want no-addresses error", err)
		}
	})

	t.Run("context expiry stops additional attempts", func(t *testing.T) {
		dialer := &stubDialer{
			results: map[string]dialResult{
				"8.8.8.8:25565": {err: context.DeadlineExceeded},
				"1.1.1.1:25565": {err: errors.New("should not be attempted")},
			},
		}
		client := pingClient{
			resolver: &stubResolver{
				ipAddrs: []netip.Addr{
					mustAddr("8.8.8.8"),
					mustAddr("1.1.1.1"),
				},
			},
			dialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
				<-ctx.Done()
				return dialer.DialContext(ctx, network, address)
			},
		}

		_, err := client.withDefaults().dialMinecraftTCP(newEndpoint("mc.example.com", defaultMinecraftPort), time.Millisecond, false)
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("dialMinecraftTCP() error = %v, want %v", err, context.DeadlineExceeded)
		}
		if len(dialer.attempts) != 1 || dialer.attempts[0] != "8.8.8.8:25565" {
			t.Fatalf("dial attempts = %v, want only the first candidate", dialer.attempts)
		}
	})
}

func TestResolveDialCandidatesUsesForcedAddressFamily(t *testing.T) {
	resolver := &stubResolver{
		ipAddrs: []netip.Addr{
			mustAddr("8.8.8.8"),
			mustAddr("2606:4700:4700::1111"),
		},
	}
	client := pingClient{resolver: resolver}

	candidates, err := client.withDefaults().resolveDialCandidates(context.Background(), newEndpoint("mc.example.com", defaultMinecraftPort), pingOptions{
		addressFamily: addressFamily6,
	})
	if err != nil {
		t.Fatalf("resolveDialCandidates() error = %v", err)
	}
	if resolver.lookupNetworks[0] != "ip6" {
		t.Fatalf("LookupNetIP network = %q, want ip6", resolver.lookupNetworks[0])
	}
	if len(candidates) != 1 || candidates[0].String() != "[2606:4700:4700::1111]:25565" {
		t.Fatalf("candidates = %v", candidates)
	}
}

func TestDialMinecraftTCPRejectsForcedAddressFamilyMismatch(t *testing.T) {
	client := pingClient{}

	_, err := client.withDefaults().dialMinecraftTCPContext(context.Background(), newEndpoint("8.8.8.8", defaultMinecraftPort), pingOptions{
		addressFamily: addressFamily6,
	})
	if err == nil || !strings.Contains(err.Error(), "-6") {
		t.Fatalf("dialMinecraftTCPContext() error = %v, want forced-family mismatch", err)
	}
}

func TestBuildDialCandidatesInterleavesAddressFamilies(t *testing.T) {
	candidates := buildDialCandidates([]netip.Addr{
		mustAddr("2606:4700:4700::1111"),
		mustAddr("2606:4700:4700::1001"),
		mustAddr("8.8.8.8"),
		mustAddr("1.1.1.1"),
	}, defaultMinecraftPort)

	got := []string{
		candidates[0].String(),
		candidates[1].String(),
		candidates[2].String(),
		candidates[3].String(),
	}
	want := []string{
		"[2606:4700:4700::1111]:25565",
		"8.8.8.8:25565",
		"[2606:4700:4700::1001]:25565",
		"1.1.1.1:25565",
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("candidate order = %v, want %v", got, want)
	}
}

func TestDialMinecraftTCPDirectIPv6AllowsPublicAddresses(t *testing.T) {
	successConn, peer := net.Pipe()
	defer peer.Close()

	dialer := &stubDialer{
		results: map[string]dialResult{
			"[2606:4700:4700::1111]:25565": {conn: successConn},
		},
	}
	client := pingClient{dialContext: dialer.DialContext}

	conn, err := client.withDefaults().dialMinecraftTCPContext(context.Background(), newEndpoint("2606:4700:4700::1111", defaultMinecraftPort), pingOptions{
		addressFamily: addressFamily6,
	})
	if err != nil {
		t.Fatalf("dialMinecraftTCPContext() error: %v", err)
	}
	_ = conn.Close()

	if len(dialer.attempts) != 1 || dialer.attempts[0] != "[2606:4700:4700::1111]:25565" {
		t.Fatalf("dial attempts = %v", dialer.attempts)
	}
	if len(dialer.networks) != 1 || dialer.networks[0] != "tcp6" {
		t.Fatalf("dial networks = %v, want [tcp6]", dialer.networks)
	}
}

func TestPingClientUsesMinimumOneMillisecondLatency(t *testing.T) {
	server := startFakeMinecraftServer(t, statusPongScript(validStatusJSON, 0))
	defer server.Close()

	now := time.Now()
	client := pingClient{
		resolver:    net.DefaultResolver,
		dialContext: defaultDialContext,
		tokenSource: func() (uint64, error) { return 42, nil },
		now: func() time.Time {
			return now
		},
	}

	latency, err := client.withDefaults().pingEndpoint(endpointRoute{
		Dial:      server.Endpoint(),
		Handshake: server.Endpoint(),
	}, 2*time.Second, true)
	if err != nil {
		t.Fatalf("pingEndpoint() error: %v", err)
	}
	if latency != 1 {
		t.Fatalf("latency = %d, want 1", latency)
	}
}

func TestPingClientWrapsResolvedDialError(t *testing.T) {
	sentinel := errors.New("dial failed")
	client := pingClient{
		resolver: &stubResolver{
			srvRecords: []*net.SRV{{Target: "8.8.8.8.", Port: 25570}},
		},
		dialContext: func(context.Context, string, string) (net.Conn, error) {
			return nil, sentinel
		},
	}

	_, err := client.withDefaults().ping(newEndpoint("mc.example.com", defaultMinecraftPort), 2*time.Second, pingOptions{})
	if !errors.Is(err, sentinel) {
		t.Fatalf("ping() error = %v, want %v", err, sentinel)
	}
	if !strings.Contains(err.Error(), "resolved to 8.8.8.8:25570") {
		t.Fatalf("ping() error = %q, want resolved endpoint context", err.Error())
	}
}

func TestPingEndpointUsesHandshakeEndpoint(t *testing.T) {
	handshakeTarget := endpoint{Host: "mc.example.com", Port: 25570}
	server := startFakeMinecraftServer(t, func(conn *fakeMinecraftConn) error {
		if err := conn.SetDeadline(2 * time.Second); err != nil {
			return err
		}

		gotHandshake, err := conn.ExpectStatusHandshake()
		if err != nil {
			return err
		}
		if gotHandshake != handshakeTarget {
			return fmt.Errorf("handshake endpoint = %+v, want %+v", gotHandshake, handshakeTarget)
		}
		if err := conn.SendStatusJSON(validStatusJSON); err != nil {
			return err
		}

		token, err := conn.ExpectPingToken()
		if err != nil {
			return err
		}
		return conn.SendPong(token)
	})
	defer server.Close()

	client := pingClient{
		resolver:    net.DefaultResolver,
		dialContext: defaultDialContext,
		tokenSource: func() (uint64, error) { return 7, nil },
		now:         time.Now,
	}

	latency, err := client.withDefaults().pingEndpoint(endpointRoute{
		Dial:      server.Endpoint(),
		Handshake: handshakeTarget,
	}, 2*time.Second, true)
	if err != nil {
		t.Fatalf("pingEndpoint() error: %v", err)
	}
	if latency <= 0 {
		t.Fatalf("latency = %d, want positive", latency)
	}
}

func TestPingEndpointPropagatesTokenError(t *testing.T) {
	server := startFakeMinecraftServer(t, func(conn *fakeMinecraftConn) error {
		if err := conn.SetDeadline(2 * time.Second); err != nil {
			return err
		}
		if _, err := conn.ExpectStatusHandshake(); err != nil {
			return err
		}
		return conn.SendStatusJSON(validStatusJSON)
	})
	defer server.Close()

	sentinel := errors.New("token failed")
	client := pingClient{
		resolver:    net.DefaultResolver,
		dialContext: defaultDialContext,
		tokenSource: func() (uint64, error) { return 0, sentinel },
		now:         time.Now,
	}

	_, err := client.withDefaults().pingEndpoint(endpointRoute{
		Dial:      server.Endpoint(),
		Handshake: server.Endpoint(),
	}, 2*time.Second, true)
	if !errors.Is(err, sentinel) {
		t.Fatalf("pingEndpoint() error = %v, want %v", err, sentinel)
	}
}

func TestPingEndpointPropagatesConnectionSetupErrors(t *testing.T) {
	validStatusPacket := encodePacket(t, func(buf *bytes.Buffer) {
		writeVarInt(buf, packetIDStatusResponse)
		if err := writeString(buf, validStatusJSON, maxStatusJSONLength); err != nil {
			t.Fatalf("writeString() error: %v", err)
		}
	})

	tests := []struct {
		name string
		conn *scriptedConn
		err  error
	}{
		{
			name: "deadline error",
			conn: &scriptedConn{deadlineErr: errors.New("deadline failed")},
			err:  errors.New("deadline failed"),
		},
		{
			name: "handshake write error",
			conn: &scriptedConn{writeErrAt: map[int]error{1: errors.New("handshake write failed")}},
			err:  errors.New("handshake write failed"),
		},
		{
			name: "status request write error",
			conn: &scriptedConn{writeErrAt: map[int]error{2: errors.New("status request write failed")}},
			err:  errors.New("status request write failed"),
		},
		{
			name: "ping write error",
			conn: newScriptedConn(validStatusPacket, map[int]error{3: errors.New("ping write failed")}),
			err:  errors.New("ping write failed"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := pingClient{
				dialContext: func(context.Context, string, string) (net.Conn, error) {
					return tt.conn, nil
				},
				tokenSource: func() (uint64, error) { return 7, nil },
				now:         time.Now,
			}

			_, err := client.withDefaults().pingEndpoint(endpointRoute{
				Dial:      newEndpoint("8.8.8.8", defaultMinecraftPort),
				Handshake: newEndpoint("mc.example.com", defaultMinecraftPort),
			}, 2*time.Second, true)
			if err == nil || err.Error() != tt.err.Error() {
				t.Fatalf("pingEndpoint() error = %v, want %v", err, tt.err)
			}
			if !tt.conn.closed {
				t.Fatal("pingEndpoint() did not close the connection")
			}
		})
	}
}

func TestReadStatusResponseErrors(t *testing.T) {
	tests := []struct {
		name    string
		payload []byte
		wantErr error
		wantMsg string
	}{
		{
			name:    "packet read error",
			payload: []byte{0x80},
			wantErr: io.EOF,
		},
		{
			name: "packet id varint error",
			payload: encodePacket(t, func(buf *bytes.Buffer) {
				buf.Write([]byte{0x80, 0x80, 0x80, 0x80, 0x80, 0x01})
			}),
			wantErr: errVarIntTooLong,
		},
		{
			name: "string decode error",
			payload: encodePacket(t, func(buf *bytes.Buffer) {
				writeVarInt(buf, packetIDStatusResponse)
				writeVarInt(buf, -1)
			}),
			wantMsg: "invalid string size",
		},
		{
			name: "unexpected packet id",
			payload: encodePacket(t, func(buf *bytes.Buffer) {
				writeVarInt(buf, 0x02)
			}),
			wantMsg: "unexpected status packet id",
		},
		{
			name: "invalid json",
			payload: encodePacket(t, func(buf *bytes.Buffer) {
				writeVarInt(buf, packetIDStatusResponse)
				if err := writeString(buf, "{invalid", maxStatusJSONLength); err != nil {
					t.Fatalf("writeString() error: %v", err)
				}
			}),
			wantMsg: "invalid status response JSON",
		},
		{
			name: "invalid framing",
			payload: encodePacket(t, func(buf *bytes.Buffer) {
				writeVarInt(buf, packetIDStatusResponse)
				if err := writeString(buf, "{}", maxStatusJSONLength); err != nil {
					t.Fatalf("writeString() error: %v", err)
				}
				buf.WriteByte(0x00)
			}),
			wantMsg: "invalid status response payload framing",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := readStatusResponse(bytes.NewReader(tt.payload))
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("readStatusResponse() error = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantMsg) {
				t.Fatalf("readStatusResponse() error = %v, want substring %q", err, tt.wantMsg)
			}
		})
	}
}

func TestReadPongPacketErrors(t *testing.T) {
	tests := []struct {
		name    string
		payload []byte
		wantErr error
		wantMsg string
	}{
		{
			name:    "packet read error",
			payload: []byte{0x80},
			wantErr: io.EOF,
		},
		{
			name: "packet id varint error",
			payload: encodePacket(t, func(buf *bytes.Buffer) {
				buf.Write([]byte{0x80, 0x80, 0x80, 0x80, 0x80, 0x01})
			}),
			wantErr: errVarIntTooLong,
		},
		{
			name: "unexpected packet id",
			payload: encodePacket(t, func(buf *bytes.Buffer) {
				writeVarInt(buf, 0x02)
			}),
			wantMsg: "unexpected pong packet id",
		},
		{
			name: "invalid payload size",
			payload: encodePacket(t, func(buf *bytes.Buffer) {
				writeVarInt(buf, packetIDPong)
				buf.Write([]byte{0x00, 0x01})
			}),
			wantMsg: "invalid pong payload size",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := readPongPacket(bytes.NewReader(tt.payload), 42)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("readPongPacket() error = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantMsg) {
				t.Fatalf("readPongPacket() error = %v, want substring %q", err, tt.wantMsg)
			}
		})
	}
}

func TestReadStringFromBytesErrors(t *testing.T) {
	tests := []struct {
		name    string
		payload []byte
		wantErr error
		wantMsg string
	}{
		{
			name:    "unexpected eof",
			payload: []byte{0x04, 'a', 'b', 'c'},
			wantErr: io.ErrUnexpectedEOF,
		},
		{
			name:    "invalid utf8",
			payload: []byte{0x02, 0xc3, 0x28},
			wantMsg: "valid UTF-8",
		},
		{
			name:    "invalid varint",
			payload: []byte{0x80, 0x80, 0x80, 0x80, 0x80, 0x01},
			wantErr: errVarIntTooLong,
		},
		{
			name:    "negative size",
			payload: []byte{0xff, 0xff, 0xff, 0xff, 0x0f},
			wantMsg: "invalid string size",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := readStringFromBytes(tt.payload, maxHandshakeHostByteSize)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("readStringFromBytes() error = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantMsg) {
				t.Fatalf("readStringFromBytes() error = %v, want substring %q", err, tt.wantMsg)
			}
		})
	}
}

func TestWritePacketRejectsInvalidPayload(t *testing.T) {
	var buf bytes.Buffer

	if err := writePacket(&buf, nil); err == nil {
		t.Fatal("writePacket() expected empty payload error")
	}

	tooLarge := make([]byte, maxPacketLength+1)
	if err := writePacket(&buf, tooLarge); err == nil {
		t.Fatal("writePacket() expected oversized payload error")
	}
}

func TestReadPacketRejectsInvalidLength(t *testing.T) {
	tests := []struct {
		name    string
		payload []byte
		wantErr string
	}{
		{
			name:    "zero length",
			payload: []byte{0x00},
			wantErr: "invalid packet length",
		},
		{
			name:    "too large",
			payload: []byte{0x02, 0x00, 0x01},
			wantErr: "exceeds limit 1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := readPacket(bytes.NewReader(tt.payload), 1)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("readPacket() error = %v, want substring %q", err, tt.wantErr)
			}
		})
	}
}

func TestReadPacketUnexpectedEOF(t *testing.T) {
	_, err := readPacket(bytes.NewReader([]byte{0x02, 0x00}), 2)
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("readPacket() error = %v, want %v", err, io.ErrUnexpectedEOF)
	}
}

func TestReadPacketPropagatesVarIntError(t *testing.T) {
	_, err := readPacket(bytes.NewReader([]byte{0x80, 0x80, 0x80, 0x80, 0x80, 0x01}), maxPacketLength)
	if !errors.Is(err, errVarIntTooLong) {
		t.Fatalf("readPacket() error = %v, want %v", err, errVarIntTooLong)
	}
}

func TestWriteStringRejectsOversizedValue(t *testing.T) {
	var buf bytes.Buffer
	if err := writeString(&buf, "toolong", 3); err == nil {
		t.Fatal("writeString() expected oversized value error")
	}
}

func TestValidateStringByteLength(t *testing.T) {
	tests := []struct {
		name    string
		length  int
		max     int
		wantMsg string
	}{
		{name: "ok", length: 3, max: 3},
		{name: "max bytes", length: 4, max: 3, wantMsg: "exceeds max"},
		{name: "int32", length: math.MaxInt32 + 1, max: math.MaxInt32 + 1, wantMsg: "exceeds int32 max"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateStringByteLength(tt.length, tt.max)
			if tt.wantMsg == "" {
				if err != nil {
					t.Fatalf("validateStringByteLength() error = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantMsg) {
				t.Fatalf("validateStringByteLength() error = %v, want substring %q", err, tt.wantMsg)
			}
		})
	}
}

func TestSendHandshakePacketRejectsInvalidEndpoint(t *testing.T) {
	var buf bytes.Buffer

	if err := sendHandshakePacket(&buf, endpoint{Host: strings.Repeat("a", maxHandshakeHostByteSize+1), Port: defaultMinecraftPort}); err == nil {
		t.Fatal("sendHandshakePacket() expected oversized host error")
	}
	if err := sendHandshakePacket(&buf, endpoint{Host: "mc.example.com", Port: -1}); err == nil {
		t.Fatal("sendHandshakePacket() expected invalid port error")
	}
}

func TestToUint16Bounds(t *testing.T) {
	if _, err := toUint16(-1); err == nil {
		t.Fatal("toUint16(-1) expected error")
	}
	if _, err := toUint16(70000); err == nil {
		t.Fatal("toUint16(70000) expected error")
	}
	if value, err := toUint16(25565); err != nil || value != 25565 {
		t.Fatalf("toUint16(25565) = %d, %v", value, err)
	}
}

func TestValidateServerAddressRejectsOversizedHost(t *testing.T) {
	err := validateServerAddress(strings.Repeat("a", maxServerAddressLength+1))
	if err == nil || !strings.Contains(err.Error(), "must not exceed") {
		t.Fatalf("validateServerAddress() error = %v", err)
	}
}

func TestDefaultPingUsesEndpoint(t *testing.T) {
	server := startFakeMinecraftServer(t, statusPongScript(validStatusJSON, 0))
	defer server.Close()

	latency, err := ping(server.Endpoint(), 2*time.Second, pingOptions{
		allowPrivateAddresses: true,
	})
	if err != nil {
		t.Fatalf("ping() error: %v", err)
	}
	if latency <= 0 {
		t.Fatalf("latency = %d, want positive", latency)
	}
}

type fakeMinecraftServer struct {
	t        *testing.T
	listener net.Listener
	endpoint endpoint
	errCh    chan error
}

type scriptedConn struct {
	readBuf     *bytes.Reader
	writeErrAt  map[int]error
	writes      [][]byte
	writeCalls  int
	deadlineErr error
	closed      bool
}

type fakeMinecraftConn struct {
	conn net.Conn
}

type staticAddr string

func newScriptedConn(readPayload []byte, writeErrAt map[int]error) *scriptedConn {
	return &scriptedConn{
		readBuf:    bytes.NewReader(readPayload),
		writeErrAt: writeErrAt,
	}
}

func (c *scriptedConn) Read(p []byte) (int, error) {
	if c.readBuf == nil {
		return 0, io.EOF
	}
	return c.readBuf.Read(p)
}

func (c *scriptedConn) Write(p []byte) (int, error) {
	c.writeCalls++
	if err, ok := c.writeErrAt[c.writeCalls]; ok {
		return 0, err
	}

	written := append([]byte(nil), p...)
	c.writes = append(c.writes, written)
	return len(p), nil
}

func (c *scriptedConn) Close() error {
	c.closed = true
	return nil
}

func (c *scriptedConn) LocalAddr() net.Addr              { return staticAddr("local") }
func (c *scriptedConn) RemoteAddr() net.Addr             { return staticAddr("remote") }
func (c *scriptedConn) SetDeadline(time.Time) error      { return c.deadlineErr }
func (c *scriptedConn) SetReadDeadline(time.Time) error  { return nil }
func (c *scriptedConn) SetWriteDeadline(time.Time) error { return nil }
func (a staticAddr) Network() string                     { return "tcp" }
func (a staticAddr) String() string                      { return string(a) }

func startFakeMinecraftServer(t *testing.T, script func(*fakeMinecraftConn) error) *fakeMinecraftServer {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start test server: %v", err)
	}

	addr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		listener.Close()
		t.Fatalf("listener addr = %T, want *net.TCPAddr", listener.Addr())
	}

	server := &fakeMinecraftServer{
		t:        t,
		listener: listener,
		endpoint: endpoint{Host: addr.IP.String(), Port: addr.Port},
		errCh:    make(chan error, 1),
	}

	go func() {
		defer close(server.errCh)

		conn, err := listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			server.errCh <- err
			return
		}
		defer conn.Close()

		server.errCh <- script(&fakeMinecraftConn{conn: conn})
	}()

	return server
}

func (s *fakeMinecraftServer) Endpoint() endpoint {
	return s.endpoint
}

func (s *fakeMinecraftServer) Close() {
	s.t.Helper()
	_ = s.listener.Close()
	if err, ok := <-s.errCh; ok && err != nil {
		s.t.Fatalf("mock server error: %v", err)
	}
}

func (c *fakeMinecraftConn) SetDeadline(timeout time.Duration) error {
	return c.conn.SetDeadline(time.Now().Add(timeout))
}

func (c *fakeMinecraftConn) ExpectHandshake() (endpoint, error) {
	handshake, err := readPacket(c.conn, maxPacketLength)
	if err != nil {
		return endpoint{}, err
	}

	packetID, consumed, err := readVarIntFromBytes(handshake)
	if err != nil {
		return endpoint{}, err
	}
	if packetID != packetIDHandshake {
		return endpoint{}, fmt.Errorf("unexpected handshake packet id: %d", packetID)
	}

	protocolVersion, protocolBytes, err := readVarIntFromBytes(handshake[consumed:])
	if err != nil {
		return endpoint{}, err
	}
	consumed += protocolBytes
	if protocolVersion != statusProtocolVersion {
		return endpoint{}, fmt.Errorf("unexpected protocol version: %d", protocolVersion)
	}

	host, hostBytes, err := readStringFromBytes(handshake[consumed:], maxHandshakeHostByteSize)
	if err != nil {
		return endpoint{}, err
	}
	consumed += hostBytes

	if len(handshake[consumed:]) < 2 {
		return endpoint{}, errors.New("missing handshake port bytes")
	}
	port := int(binary.BigEndian.Uint16(handshake[consumed:]))
	consumed += 2

	nextState, stateBytes, err := readVarIntFromBytes(handshake[consumed:])
	if err != nil {
		return endpoint{}, err
	}
	consumed += stateBytes

	if nextState != nextStateStatus {
		return endpoint{}, fmt.Errorf("unexpected next state: %d", nextState)
	}
	if consumed != len(handshake) {
		return endpoint{}, fmt.Errorf("unexpected trailing handshake bytes: %d", len(handshake)-consumed)
	}

	return endpoint{Host: host, Port: port}, nil
}

func (c *fakeMinecraftConn) ExpectStatusRequest() error {
	statusRequest, err := readPacket(c.conn, maxPacketLength)
	if err != nil {
		return err
	}

	requestID, requestBytes, err := readVarIntFromBytes(statusRequest)
	if err != nil {
		return err
	}
	if requestID != packetIDStatusResponse {
		return fmt.Errorf("unexpected status request packet id: %d", requestID)
	}
	if requestBytes != len(statusRequest) {
		return fmt.Errorf("unexpected status request payload size: %d", len(statusRequest)-requestBytes)
	}

	return nil
}

func (c *fakeMinecraftConn) ExpectStatusHandshake() (endpoint, error) {
	handshakeTarget, err := c.ExpectHandshake()
	if err != nil {
		return endpoint{}, err
	}
	if err := c.ExpectStatusRequest(); err != nil {
		return endpoint{}, err
	}
	return handshakeTarget, nil
}

func (c *fakeMinecraftConn) SendStatusJSON(statusJSON string) error {
	var status bytes.Buffer
	writeVarInt(&status, packetIDStatusResponse)
	if err := writeString(&status, statusJSON, maxStatusJSONLength); err != nil {
		return err
	}
	return c.SendPacket(status.Bytes())
}

func (c *fakeMinecraftConn) ExpectPingToken() (uint64, error) {
	pingPacket, err := readPacket(c.conn, maxPacketLength)
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
		return 0, fmt.Errorf("ping payload size = %d, want 8", len(pingPacket[consumed:]))
	}

	return binary.BigEndian.Uint64(pingPacket[consumed:]), nil
}

func (c *fakeMinecraftConn) SendPong(token uint64) error {
	var pong bytes.Buffer
	writeVarInt(&pong, packetIDPong)

	var payload [8]byte
	binary.BigEndian.PutUint64(payload[:], token)
	pong.Write(payload[:])

	return c.SendPacket(pong.Bytes())
}

func (c *fakeMinecraftConn) SendPacket(payload []byte) error {
	return writePacket(c.conn, payload)
}

func statusPongScript(statusJSON string, pongDelay time.Duration) func(*fakeMinecraftConn) error {
	return func(conn *fakeMinecraftConn) error {
		if err := conn.SetDeadline(3 * time.Second); err != nil {
			return err
		}

		if _, err := conn.ExpectStatusHandshake(); err != nil {
			return err
		}
		if err := conn.SendStatusJSON(statusJSON); err != nil {
			return err
		}

		token, err := conn.ExpectPingToken()
		if err != nil {
			return err
		}

		if pongDelay > 0 {
			time.Sleep(pongDelay)
		}

		return conn.SendPong(token)
	}
}

type stubResolver struct {
	mu             sync.Mutex
	srvRecords     []*net.SRV
	srvErr         error
	ipAddrs        []netip.Addr
	ipErr          error
	srvCalls       int
	ipCalls        int
	lookupNetworks []string
}

func (r *stubResolver) LookupSRV(context.Context, string, string, string) (string, []*net.SRV, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.srvCalls++
	return "", r.srvRecords, r.srvErr
}

func (r *stubResolver) LookupNetIP(_ context.Context, network string, _ string) ([]netip.Addr, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.ipCalls++
	r.lookupNetworks = append(r.lookupNetworks, network)
	return r.ipAddrs, r.ipErr
}

type dialResult struct {
	conn net.Conn
	err  error
}

type stubDialer struct {
	mu         sync.Mutex
	attempts   []string
	networks   []string
	results    map[string]dialResult
	defaultErr error
}

func (d *stubDialer) DialContext(_ context.Context, network string, address string) (net.Conn, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.attempts = append(d.attempts, address)
	d.networks = append(d.networks, network)

	if result, ok := d.results[address]; ok {
		if result.conn != nil {
			return result.conn, nil
		}
		return nil, result.err
	}
	if d.defaultErr != nil {
		return nil, d.defaultErr
	}
	return nil, fmt.Errorf("unexpected dial target %s", address)
}

func encodePacket(t *testing.T, build func(*bytes.Buffer)) []byte {
	t.Helper()

	var payload bytes.Buffer
	build(&payload)

	var packet bytes.Buffer
	writeVarInt(&packet, int32(payload.Len()))
	if _, err := io.Copy(&packet, &payload); err != nil {
		t.Fatalf("io.Copy() error: %v", err)
	}

	return packet.Bytes()
}

func mustAddr(raw string) netip.Addr {
	addr, err := netip.ParseAddr(raw)
	if err != nil {
		panic(err)
	}

	return addr
}
