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

func TestPrepareJavaProbeProbesFakeServer(t *testing.T) {
	server := startFakeMinecraftServer(t, statusPongScript(validStatusJSON, 15*time.Millisecond))
	defer server.Close()

	target := newTargetSpec(server.Endpoint().Host, server.Endpoint().Port, true)
	prepared, err := prepareJavaProbe(context.Background(), newPingClient(), target, pingOptions{
		addressFamily:         addressFamily4,
		allowPrivateAddresses: true,
	})
	if err != nil {
		t.Fatalf("prepareJavaProbe() error = %v", err)
	}

	sample, err := prepared.probe(context.Background(), 5*time.Second)
	if err != nil {
		t.Fatalf("probe() error = %v", err)
	}
	if sample.latency <= 0 {
		t.Fatalf("probe() got invalid latency: %s", sample.latency)
	}
}

func TestPrepareJavaProbeMalformedStatusPacket(t *testing.T) {
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

	target := newTargetSpec(server.Endpoint().Host, server.Endpoint().Port, true)
	prepared, err := prepareJavaProbe(context.Background(), newPingClient(), target, pingOptions{allowPrivateAddresses: true})
	if err != nil {
		t.Fatalf("prepareJavaProbe() error = %v", err)
	}

	if _, err := prepared.probe(context.Background(), 2*time.Second); err == nil {
		t.Fatal("probe() expected malformed status packet error but got nil")
	}
}

func TestPrepareJavaProbePongMismatch(t *testing.T) {
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

	target := newTargetSpec(server.Endpoint().Host, server.Endpoint().Port, true)
	prepared, err := prepareJavaProbe(context.Background(), newPingClient(), target, pingOptions{allowPrivateAddresses: true})
	if err != nil {
		t.Fatalf("prepareJavaProbe() error = %v", err)
	}

	if _, err := prepared.probe(context.Background(), 2*time.Second); err == nil {
		t.Fatal("probe() expected pong mismatch error but got nil")
	}
}

func TestPrepareJavaProbeRejectsLoopbackAddressByDefault(t *testing.T) {
	server := startFakeMinecraftServer(t, statusPongScript(validStatusJSON, 0))
	defer server.Close()

	target := newTargetSpec(server.Endpoint().Host, server.Endpoint().Port, true)
	_, err := prepareJavaProbe(context.Background(), newPingClient(), target, pingOptions{})
	if err == nil {
		t.Fatal("prepareJavaProbe() expected non-public address rejection but got nil")
	}
	if !strings.Contains(err.Error(), "non-public address") {
		t.Fatalf("prepareJavaProbe() error = %q, want non-public address rejection", err.Error())
	}
}

func TestPrepareProbeSelectsEditionImplementation(t *testing.T) {
	server := startFakeMinecraftServer(t, statusPongScript(validStatusJSON, 0))
	defer server.Close()

	javaProbe, err := prepareProbe(context.Background(), cliConfig{
		Edition: editionJava,
		Target:  newTargetSpec(server.Endpoint().Host, server.Endpoint().Port, true),
		Options: pingOptions{allowPrivateAddresses: true},
	})
	if err != nil {
		t.Fatalf("prepareProbe() java error = %v", err)
	}
	if _, ok := javaProbe.(*javaPreparedProbe); !ok {
		t.Fatalf("prepareProbe() java type = %T, want *javaPreparedProbe", javaProbe)
	}

	bedrockProbe, err := prepareProbe(context.Background(), cliConfig{
		Edition: editionBedrock,
		Target:  targetSpec{Host: "8.8.8.8"},
		Options: pingOptions{allowPrivateAddresses: true},
	})
	if err != nil {
		t.Fatalf("prepareProbe() bedrock error = %v", err)
	}
	if _, ok := bedrockProbe.(*bedrockPreparedProbe); !ok {
		t.Fatalf("prepareProbe() bedrock type = %T, want *bedrockPreparedProbe", bedrockProbe)
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

func TestResolveJavaRouteUsesSRV(t *testing.T) {
	resolver := &stubResolver{
		srvRecords: []*net.SRV{{Target: "srv.example.net.", Port: 25570}},
	}
	client := pingClient{resolver: resolver}

	route, err := client.withDefaults().resolveJavaRouteContext(context.Background(), newTargetSpec("mc.example.com", defaultMinecraftPort, false))
	if err != nil {
		t.Fatalf("resolveJavaRouteContext() error = %v", err)
	}

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

func TestResolveJavaRouteSkipsSRVForIPAndCustomPort(t *testing.T) {
	resolver := &stubResolver{}
	client := pingClient{resolver: resolver}

	ipRoute, err := client.withDefaults().resolveJavaRouteContext(context.Background(), newTargetSpec("127.0.0.1", defaultMinecraftPort, false))
	if err != nil {
		t.Fatalf("resolveJavaRouteContext() error = %v", err)
	}
	if ipRoute.Dial != (endpoint{Host: "127.0.0.1", Port: defaultMinecraftPort}) {
		t.Fatalf("ip route = %+v", ipRoute)
	}

	customRoute, err := client.withDefaults().resolveJavaRouteContext(context.Background(), newTargetSpec("mc.example.com", 25570, true))
	if err != nil {
		t.Fatalf("resolveJavaRouteContext() error = %v", err)
	}
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

func TestResolveJavaRouteFallsBackOnInvalidSRVRecord(t *testing.T) {
	client := pingClient{
		resolver: &stubResolver{
			srvRecords: []*net.SRV{{Target: "", Port: 25570}},
		},
	}

	route, err := client.withDefaults().resolveJavaRouteContext(context.Background(), newTargetSpec("mc.example.com", defaultMinecraftPort, false))
	if err != nil {
		t.Fatalf("resolveJavaRouteContext() error = %v", err)
	}
	if route.Dial != (endpoint{Host: "mc.example.com", Port: defaultMinecraftPort}) {
		t.Fatalf("route = %+v, want unresolved target", route)
	}
}

func TestResolveJavaRouteFallsBackWhenSRVUnavailable(t *testing.T) {
	target := newTargetSpec("mc.example.com", defaultMinecraftPort, false)
	wantRoute := endpoint{Host: "mc.example.com", Port: defaultMinecraftPort}

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
			route, err := pingClient{resolver: tt.resolver}.withDefaults().resolveJavaRouteContext(context.Background(), target)
			if err != nil {
				t.Fatalf("resolveJavaRouteContext() error = %v", err)
			}
			if route.Dial != wantRoute || route.Handshake != wantRoute {
				t.Fatalf("route = %+v, want unresolved target %+v", route, wantRoute)
			}
			if tt.resolver.srvCalls != 1 {
				t.Fatalf("LookupSRV calls = %d, want 1", tt.resolver.srvCalls)
			}
		})
	}
}

func TestDialCandidatesSkipsNonPublicCandidatesByDefault(t *testing.T) {
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
	}.withDefaults()

	candidates, err := client.resolveDialCandidates(context.Background(), newEndpoint("mc.example.com", defaultMinecraftPort), pingOptions{})
	if err != nil {
		t.Fatalf("resolveDialCandidates() error = %v", err)
	}
	conn, err := client.dialCandidates(context.Background(), candidates)
	if err != nil {
		t.Fatalf("dialCandidates() error: %v", err)
	}
	_ = conn.Close()

	if strings.Join(dialer.attempts, ",") != "8.8.8.8:25565" {
		t.Fatalf("dial attempts = %v, want only public candidates", dialer.attempts)
	}
}

func TestDialCandidatesAllowsNonPublicCandidatesWithOptIn(t *testing.T) {
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
	}.withDefaults()

	candidates, err := client.resolveDialCandidates(context.Background(), newEndpoint("mc.example.com", defaultMinecraftPort), pingOptions{allowPrivateAddresses: true})
	if err != nil {
		t.Fatalf("resolveDialCandidates() error = %v", err)
	}
	conn, err := client.dialCandidates(context.Background(), candidates)
	if err != nil {
		t.Fatalf("dialCandidates() error: %v", err)
	}
	_ = conn.Close()

	if strings.Join(dialer.attempts, ",") != "127.0.0.1:25565,10.0.0.8:25565,8.8.8.8:25565" {
		t.Fatalf("dial attempts = %v, want private and public candidates", dialer.attempts)
	}
}

func TestResolveDialCandidatesRejectsHostsThatResolveOnlyToNonPublicAddresses(t *testing.T) {
	client := pingClient{
		resolver: &stubResolver{
			ipAddrs: []netip.Addr{
				mustAddr("127.0.0.1"),
				mustAddr("10.0.0.8"),
			},
		},
	}.withDefaults()

	_, err := client.resolveDialCandidates(context.Background(), newEndpoint("mc.example.com", defaultMinecraftPort), pingOptions{})
	if err == nil {
		t.Fatal("resolveDialCandidates() expected error")
	}
	if !strings.Contains(err.Error(), "resolved only to non-public addresses") {
		t.Fatalf("resolveDialCandidates() error = %v", err)
	}
}

func TestDialCandidatesTriesCandidatesUntilSuccess(t *testing.T) {
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
	}.withDefaults()

	candidates, err := client.resolveDialCandidates(context.Background(), newEndpoint("mc.example.com", defaultMinecraftPort), pingOptions{})
	if err != nil {
		t.Fatalf("resolveDialCandidates() error = %v", err)
	}
	conn, err := client.dialCandidates(context.Background(), candidates)
	if err != nil {
		t.Fatalf("dialCandidates() error: %v", err)
	}
	_ = conn.Close()

	if strings.Join(dialer.attempts, ",") != "8.8.8.8:25565,1.1.1.1:25565" {
		t.Fatalf("dial attempts = %v", dialer.attempts)
	}
}

func TestDialCandidatesDirectIPAllowsPublicAddresses(t *testing.T) {
	successConn, peer := net.Pipe()
	defer peer.Close()

	dialer := &stubDialer{
		results: map[string]dialResult{
			"8.8.8.8:25565": {conn: successConn},
		},
	}
	client := pingClient{dialContext: dialer.DialContext}.withDefaults()

	candidates, err := client.resolveDialCandidates(context.Background(), newEndpoint("8.8.8.8", defaultMinecraftPort), pingOptions{})
	if err != nil {
		t.Fatalf("resolveDialCandidates() error = %v", err)
	}
	conn, err := client.dialCandidates(context.Background(), candidates)
	if err != nil {
		t.Fatalf("dialCandidates() error: %v", err)
	}
	_ = conn.Close()

	if len(dialer.attempts) != 1 || dialer.attempts[0] != "8.8.8.8:25565" {
		t.Fatalf("dial attempts = %v", dialer.attempts)
	}
}

func TestDialCandidatesPropagatesLookupAndDialErrors(t *testing.T) {
	lookupErr := errors.New("lookup failed")
	client := pingClient{
		resolver: &stubResolver{ipErr: lookupErr},
	}.withDefaults()

	_, err := client.resolveDialCandidates(context.Background(), newEndpoint("mc.example.com", defaultMinecraftPort), pingOptions{})
	if !errors.Is(err, lookupErr) {
		t.Fatalf("resolveDialCandidates() error = %v, want %v", err, lookupErr)
	}

	dialErr := errors.New("all dials failed")
	client = pingClient{
		resolver: &stubResolver{
			ipAddrs: []netip.Addr{mustAddr("8.8.8.8")},
		},
		dialContext: func(context.Context, string, string) (net.Conn, error) {
			return nil, dialErr
		},
	}.withDefaults()

	candidates, err := client.resolveDialCandidates(context.Background(), newEndpoint("mc.example.com", defaultMinecraftPort), pingOptions{})
	if err != nil {
		t.Fatalf("resolveDialCandidates() error = %v", err)
	}
	_, err = client.dialCandidates(context.Background(), candidates)
	if !errors.Is(err, dialErr) {
		t.Fatalf("dialCandidates() error = %v, want %v", err, dialErr)
	}
}

func TestDialCandidatesResolverEdgeCases(t *testing.T) {
	t.Run("no resolved addresses", func(t *testing.T) {
		client := pingClient{
			resolver: &stubResolver{ipAddrs: []netip.Addr{}},
		}.withDefaults()

		_, err := client.resolveDialCandidates(context.Background(), newEndpoint("mc.example.com", defaultMinecraftPort), pingOptions{})
		if err == nil || !strings.Contains(err.Error(), "no addresses resolved") {
			t.Fatalf("resolveDialCandidates() error = %v, want no-addresses error", err)
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
		}.withDefaults()

		candidates, err := client.resolveDialCandidates(context.Background(), newEndpoint("mc.example.com", defaultMinecraftPort), pingOptions{})
		if err != nil {
			t.Fatalf("resolveDialCandidates() error = %v", err)
		}

		ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
		defer cancel()

		_, err = client.dialCandidates(ctx, candidates)
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("dialCandidates() error = %v, want %v", err, context.DeadlineExceeded)
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

func TestResolveDialCandidatesRejectsForcedAddressFamilyMismatch(t *testing.T) {
	client := pingClient{}.withDefaults()

	_, err := client.resolveDialCandidates(context.Background(), newEndpoint("8.8.8.8", defaultMinecraftPort), pingOptions{
		addressFamily: addressFamily6,
	})
	if err == nil || !strings.Contains(err.Error(), "-6") {
		t.Fatalf("resolveDialCandidates() error = %v, want forced-family mismatch", err)
	}
}

func TestBuildDialCandidatesInterleavesAddressFamilies(t *testing.T) {
	candidates := buildDialCandidatesWithPortFunc([]netip.Addr{
		mustAddr("2606:4700:4700::1111"),
		mustAddr("2606:4700:4700::1001"),
		mustAddr("8.8.8.8"),
		mustAddr("1.1.1.1"),
	}, func(netip.Addr) uint16 {
		return defaultMinecraftPort
	})

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

func TestDialCandidatesDirectIPv6AllowsPublicAddresses(t *testing.T) {
	successConn, peer := net.Pipe()
	defer peer.Close()

	dialer := &stubDialer{
		results: map[string]dialResult{
			"[2606:4700:4700::1111]:25565": {conn: successConn},
		},
	}
	client := pingClient{dialContext: dialer.DialContext}.withDefaults()

	candidates, err := client.resolveDialCandidates(context.Background(), newEndpoint("2606:4700:4700::1111", defaultMinecraftPort), pingOptions{
		addressFamily: addressFamily6,
	})
	if err != nil {
		t.Fatalf("resolveDialCandidates() error = %v", err)
	}
	conn, err := client.dialCandidates(context.Background(), candidates)
	if err != nil {
		t.Fatalf("dialCandidates() error: %v", err)
	}
	_ = conn.Close()

	if len(dialer.attempts) != 1 || dialer.attempts[0] != "[2606:4700:4700::1111]:25565" {
		t.Fatalf("dial attempts = %v", dialer.attempts)
	}
	if len(dialer.networks) != 1 || dialer.networks[0] != "tcp6" {
		t.Fatalf("dial networks = %v, want [tcp6]", dialer.networks)
	}
}

func TestPingJavaPreparedContextUsesMinimumOneMillisecondLatency(t *testing.T) {
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
	}.withDefaults()

	route := endpointRoute{Dial: server.Endpoint(), Handshake: server.Endpoint()}
	candidates, err := client.resolveDialCandidates(context.Background(), route.Dial, pingOptions{allowPrivateAddresses: true})
	if err != nil {
		t.Fatalf("resolveDialCandidates() error = %v", err)
	}

	sample, err := client.pingJavaPreparedContext(context.Background(), route, candidates, 2*time.Second)
	if err != nil {
		t.Fatalf("pingJavaPreparedContext() error: %v", err)
	}
	if sample.latency != time.Millisecond {
		t.Fatalf("latency = %s, want 1ms", sample.latency)
	}
}

func TestPingJavaPreparedContextPropagatesDialError(t *testing.T) {
	sentinel := errors.New("dial failed")
	client := pingClient{
		resolver: &stubResolver{
			srvRecords: []*net.SRV{{Target: "8.8.8.8.", Port: 25570}},
		},
		dialContext: func(context.Context, string, string) (net.Conn, error) {
			return nil, sentinel
		},
	}.withDefaults()

	route, err := client.resolveJavaRouteContext(context.Background(), newTargetSpec("mc.example.com", defaultMinecraftPort, false))
	if err != nil {
		t.Fatalf("resolveJavaRouteContext() error = %v", err)
	}

	candidates, err := client.resolveDialCandidates(context.Background(), route.Dial, pingOptions{})
	if err != nil {
		t.Fatalf("resolveDialCandidates() error = %v", err)
	}

	_, err = client.pingJavaPreparedContext(context.Background(), route, candidates, 2*time.Second)
	if !errors.Is(err, sentinel) {
		t.Fatalf("pingJavaPreparedContext() error = %v, want %v", err, sentinel)
	}
}

func TestPingJavaPreparedContextUsesHandshakeEndpoint(t *testing.T) {
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
	}.withDefaults()

	route := endpointRoute{Dial: server.Endpoint(), Handshake: handshakeTarget}
	candidates, err := client.resolveDialCandidates(context.Background(), route.Dial, pingOptions{allowPrivateAddresses: true})
	if err != nil {
		t.Fatalf("resolveDialCandidates() error = %v", err)
	}

	sample, err := client.pingJavaPreparedContext(context.Background(), route, candidates, 2*time.Second)
	if err != nil {
		t.Fatalf("pingJavaPreparedContext() error: %v", err)
	}
	if sample.latency <= 0 {
		t.Fatalf("latency = %s, want positive", sample.latency)
	}
}

func TestPingJavaPreparedContextPropagatesTokenError(t *testing.T) {
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
	}.withDefaults()

	route := endpointRoute{Dial: server.Endpoint(), Handshake: server.Endpoint()}
	candidates, err := client.resolveDialCandidates(context.Background(), route.Dial, pingOptions{allowPrivateAddresses: true})
	if err != nil {
		t.Fatalf("resolveDialCandidates() error = %v", err)
	}

	_, err = client.pingJavaPreparedContext(context.Background(), route, candidates, 2*time.Second)
	if !errors.Is(err, sentinel) {
		t.Fatalf("pingJavaPreparedContext() error = %v, want %v", err, sentinel)
	}
}

func TestPingJavaPreparedContextPropagatesConnectionSetupErrors(t *testing.T) {
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
			}.withDefaults()

			route := endpointRoute{
				Dial:      newEndpoint("8.8.8.8", defaultMinecraftPort),
				Handshake: newEndpoint("mc.example.com", defaultMinecraftPort),
			}
			candidates, err := client.resolveDialCandidates(context.Background(), route.Dial, pingOptions{allowPrivateAddresses: true})
			if err != nil {
				t.Fatalf("resolveDialCandidates() error = %v", err)
			}

			_, err = client.pingJavaPreparedContext(context.Background(), route, candidates, 2*time.Second)
			if err == nil || err.Error() != tt.err.Error() {
				t.Fatalf("pingJavaPreparedContext() error = %v, want %v", err, tt.err)
			}
			if !tt.conn.closed {
				t.Fatal("pingJavaPreparedContext() did not close the connection")
			}
		})
	}
}

func TestPingEndpointUsesInjectedClockForDeadlineFallback(t *testing.T) {
	validReadPayload := append(
		encodePacket(t, func(buf *bytes.Buffer) {
			writeVarInt(buf, packetIDStatusResponse)
			if err := writeString(buf, validStatusJSON, maxStatusJSONLength); err != nil {
				t.Fatalf("writeString() error: %v", err)
			}
		}),
		encodePacket(t, func(buf *bytes.Buffer) {
			writeVarInt(buf, packetIDPong)
			var token [8]byte
			binary.BigEndian.PutUint64(token[:], 7)
			buf.Write(token[:])
		})...,
	)
	conn := newScriptedConn(validReadPayload, nil)
	now := time.Unix(1_700_000_000, 250_000_000)
	client := pingClient{
		dialContext: func(context.Context, string, string) (net.Conn, error) {
			return conn, nil
		},
		tokenSource: func() (uint64, error) { return 7, nil },
		now:         func() time.Time { return now },
	}

	sample, err := client.pingJavaPreparedContext(context.Background(), endpointRoute{
		Dial:      newEndpoint("8.8.8.8", defaultMinecraftPort),
		Handshake: newEndpoint("mc.example.com", defaultMinecraftPort),
	}, []dialCandidate{{address: netip.MustParseAddrPort("8.8.8.8:25565")}}, 2*time.Second)
	if err != nil {
		t.Fatalf("pingJavaPreparedContext() error = %v", err)
	}

	if want := now.Add(2 * time.Second); !conn.deadline.Equal(want) {
		t.Fatalf("deadline = %s, want %s", conn.deadline, want)
	}
	if sample.latency != time.Millisecond {
		t.Fatalf("latency = %s, want 1ms", sample.latency)
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
		{
			name: "top level array",
			payload: encodePacket(t, func(buf *bytes.Buffer) {
				writeVarInt(buf, packetIDStatusResponse)
				if err := writeString(buf, "[]", maxStatusJSONLength); err != nil {
					t.Fatalf("writeString() error: %v", err)
				}
			}),
			wantMsg: "expected top-level object",
		},
		{
			name: "top level string",
			payload: encodePacket(t, func(buf *bytes.Buffer) {
				writeVarInt(buf, packetIDStatusResponse)
				if err := writeString(buf, `"ok"`, maxStatusJSONLength); err != nil {
					t.Fatalf("writeString() error: %v", err)
				}
			}),
			wantMsg: "expected top-level object",
		},
		{
			name: "top level null",
			payload: encodePacket(t, func(buf *bytes.Buffer) {
				writeVarInt(buf, packetIDStatusResponse)
				if err := writeString(buf, "null", maxStatusJSONLength); err != nil {
					t.Fatalf("writeString() error: %v", err)
				}
			}),
			wantMsg: "expected top-level object",
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
	deadline    time.Time
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

func (c *scriptedConn) LocalAddr() net.Addr  { return staticAddr("local") }
func (c *scriptedConn) RemoteAddr() net.Addr { return staticAddr("remote") }
func (c *scriptedConn) SetDeadline(deadline time.Time) error {
	c.deadline = deadline
	return c.deadlineErr
}
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
