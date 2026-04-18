package main

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"strings"
	"testing"
	"time"
)

func TestPrepareBedrockProbeWrapsResolutionError(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("lookup exploded")
	_, err := prepareBedrockProbe(context.Background(), pingClient{
		resolver: stubBedrockResolver{err: sentinel},
	}, targetSpec{Host: "example.com"}, pingOptions{})
	if !errors.Is(err, sentinel) {
		t.Fatalf("prepareBedrockProbe() error = %v, want %v", err, sentinel)
	}
	if !strings.Contains(err.Error(), "failed to resolve bedrock server example.com") {
		t.Fatalf("prepareBedrockProbe() error = %q, want wrapped host context", err.Error())
	}
}

func TestBedrockPreparedProbeBannerUsesLiteralDisplayAddress(t *testing.T) {
	t.Parallel()

	prepared, err := prepareBedrockProbe(context.Background(), pingClient{}, targetSpec{
		Host: "8.8.8.8",
	}, pingOptions{
		allowPrivateAddresses: true,
	})
	if err != nil {
		t.Fatalf("prepareBedrockProbe() error = %v", err)
	}
	if got := prepared.banner(false); got != "PING 8.8.8.8 port 19132 [bedrock]:" {
		t.Fatalf("banner(false) = %q", got)
	}
}

func TestPrepareBedrockProbeNumericBannerUsesResolvedAddress(t *testing.T) {
	t.Parallel()

	prepared, err := prepareBedrockProbe(context.Background(), pingClient{
		resolver: stubBedrockResolver{
			addrs: []netip.Addr{mustParseAddr(t, "8.8.8.8")},
		},
	}, targetSpec{Host: "example.com"}, pingOptions{})
	if err != nil {
		t.Fatalf("prepareBedrockProbe() error = %v", err)
	}
	if got := prepared.banner(true); got != "PING 8.8.8.8 port 19132 [bedrock]:" {
		t.Fatalf("banner(true) = %q", got)
	}
}

func TestBedrockPreparedProbeSummaryLabelRespectsNumericMode(t *testing.T) {
	t.Parallel()

	prepared, err := prepareBedrockProbe(context.Background(), pingClient{
		resolver: stubBedrockResolver{
			addrs: []netip.Addr{mustParseAddr(t, "8.8.8.8")},
		},
	}, targetSpec{Host: "example.com"}, pingOptions{})
	if err != nil {
		t.Fatalf("prepareBedrockProbe() error = %v", err)
	}
	if got := prepared.summaryLabel(false); got != "example.com" {
		t.Fatalf("summaryLabel(false) before observation = %q", got)
	}

	prepared.observeSample(probeSample{remote: mustAddrPort(t, "8.8.8.8:19132")})
	if got := prepared.summaryLabel(false); got != "example.com" {
		t.Fatalf("summaryLabel(false) after observation = %q", got)
	}
	if got := prepared.summaryLabel(true); got != "8.8.8.8" {
		t.Fatalf("summaryLabel(true) after observation = %q", got)
	}
}

func TestPingBedrockReturnsZeroLatencyOnError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		target endpoint
		server *fakeBedrockServer
		setup  func(t *testing.T) *fakeBedrockServer
		timeo  time.Duration
		opts   pingOptions
	}{
		{
			name:   "invalid timeout",
			target: newEndpoint("8.8.8.8", defaultBedrockPort),
			timeo:  0,
			opts: pingOptions{
				allowPrivateAddresses: true,
				edition:               editionBedrock,
			},
		},
		{
			name:   "loopback rejected",
			target: newEndpoint("127.0.0.1", defaultBedrockPort),
			timeo:  time.Second,
			opts: pingOptions{
				edition: editionBedrock,
			},
		},
		{
			name: "malformed pong",
			setup: func(t *testing.T) *fakeBedrockServer {
				return startFakeBedrockServer(t, func(packet []byte) ([]byte, error) {
					pingTime := mustPingTime(t, packet)
					reply := encodeFakeBedrockPong(pingTime, "MCPE;Broken;924;1.26.3;0;10;1;World;Survival;1;19132;19133;0;")
					reply[0] = 0xff
					return reply, nil
				})
			},
			timeo: time.Second,
			opts: pingOptions{
				allowPrivateAddresses: true,
				edition:               editionBedrock,
			},
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			target := test.target
			if test.setup != nil {
				server := test.setup(t)
				defer server.Close(t)
				target = newEndpoint(server.addr.Addr().String(), int(server.addr.Port()))
			}

			latency, err := ping(target, test.timeo, test.opts)
			if err == nil {
				t.Fatal("ping() succeeded, want error")
			}
			if latency != 0 {
				t.Fatalf("ping() latency = %d, want 0 on error", latency)
			}
		})
	}
}

func TestPingBedrockRejectsInvalidTimeout(t *testing.T) {
	t.Parallel()

	_, err := ping(newEndpoint("8.8.8.8", defaultBedrockPort), 0, pingOptions{
		allowPrivateAddresses: true,
		edition:               editionBedrock,
	})
	if err == nil || !strings.Contains(err.Error(), "invalid timeout") {
		t.Fatalf("ping() error = %v, want invalid-timeout rejection", err)
	}
}

func TestPingBedrockRejectsLoopbackAddressByDefault(t *testing.T) {
	t.Parallel()

	_, err := ping(newEndpoint("127.0.0.1", defaultBedrockPort), time.Second, pingOptions{
		edition: editionBedrock,
	})
	if err == nil || !strings.Contains(err.Error(), "non-public address") {
		t.Fatalf("ping() error = %v, want non-public address rejection", err)
	}
}

func TestPingBedrockWrapsProbeError(t *testing.T) {
	t.Parallel()

	server := startFakeBedrockServer(t, func(packet []byte) ([]byte, error) {
		pingTime := mustPingTime(t, packet)
		reply := encodeFakeBedrockPong(pingTime, "MCPE;Broken;924;1.26.3;0;10;1;World;Survival;1;19132;19133;0;")
		reply[0] = 0xff
		return reply, nil
	})
	defer server.Close(t)

	_, err := ping(newEndpoint(server.addr.Addr().String(), int(server.addr.Port())), time.Second, pingOptions{
		allowPrivateAddresses: true,
		edition:               editionBedrock,
	})
	if err == nil || !strings.Contains(err.Error(), "failed to ping server") {
		t.Fatalf("ping() error = %v, want wrapped probe failure", err)
	}
}

func TestPrepareBedrockProbeUsesDefaultClientStateForProbe(t *testing.T) {
	t.Parallel()

	server := startFakeBedrockServer(t, func(packet []byte) ([]byte, error) {
		return encodeFakeBedrockPong(mustPingTime(t, packet), "MCPE;Test Server;924;1.26.3;1;10;983;World;Survival;1;19132;19133;0;"), nil
	})
	defer server.Close(t)

	prepared, err := prepareBedrockProbe(context.Background(), pingClient{
		resolver: stubBedrockResolver{
			addrs: []netip.Addr{server.addr.Addr()},
		},
	}, targetSpec{
		Host:         "example.com",
		Port:         int(server.addr.Port()),
		PortExplicit: true,
	}, pingOptions{
		allowPrivateAddresses: true,
	})
	if err != nil {
		t.Fatalf("prepareBedrockProbe() error = %v", err)
	}
	typed, ok := prepared.(*bedrockPreparedProbe)
	if !ok {
		t.Fatalf("prepareBedrockProbe() type = %T, want *bedrockPreparedProbe", prepared)
	}
	if typed.client.resolver == nil {
		t.Fatal("prepared client resolver is nil, want defaulted resolver")
	}
	if typed.client.dialContext == nil {
		t.Fatal("prepared client dialContext is nil, want defaulted dialer")
	}
	if typed.client.tokenSource == nil {
		t.Fatal("prepared client tokenSource is nil, want defaulted token source")
	}
	if typed.client.now == nil {
		t.Fatal("prepared client now is nil, want defaulted clock")
	}
	if typed.client.dialFallbackDelay != defaultDialFallbackDelay {
		t.Fatalf("prepared client dialFallbackDelay = %s, want %s", typed.client.dialFallbackDelay, defaultDialFallbackDelay)
	}

	sample, err := prepared.probe(context.Background(), time.Second)
	if err != nil {
		t.Fatalf("prepared.probe() error = %v", err)
	}
	if sample.remote != server.addr {
		t.Fatalf("remote = %s, want %s", sample.remote, server.addr)
	}
}

func TestBedrockTargetSpecFromEndpointDefaults(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		target       endpoint
		family       addressFamily
		wantExplicit bool
	}{
		{
			name:         "hostname ipv4 default port",
			target:       newEndpoint("example.com", defaultBedrockPort),
			family:       addressFamilyAny,
			wantExplicit: false,
		},
		{
			name:         "hostname ipv6 default port",
			target:       newEndpoint("example.com", defaultBedrockPortV6),
			family:       addressFamily6,
			wantExplicit: false,
		},
		{
			name:         "hostname custom port",
			target:       newEndpoint("example.com", 20000),
			family:       addressFamily6,
			wantExplicit: true,
		},
		{
			name:         "literal default port",
			target:       newEndpoint("8.8.8.8", defaultBedrockPort),
			family:       addressFamilyAny,
			wantExplicit: false,
		},
		{
			name:         "literal custom port",
			target:       newEndpoint("8.8.8.8", 20000),
			family:       addressFamilyAny,
			wantExplicit: true,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			spec := bedrockTargetSpecFromEndpoint(test.target, test.family)
			if spec.PortExplicit != test.wantExplicit {
				t.Fatalf("PortExplicit = %t, want %t", spec.PortExplicit, test.wantExplicit)
			}
		})
	}
}

func TestResolveBedrockCandidatesRejectsInvalidInputs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		target  targetSpec
		options pingOptions
		wantErr string
	}{
		{
			name:    "invalid target",
			target:  targetSpec{Host: "exa\nmple.com"},
			options: pingOptions{},
			wantErr: "control characters",
		},
		{
			name:   "invalid address family",
			target: targetSpec{Host: "example.com"},
			options: pingOptions{
				addressFamily: addressFamily(99),
			},
			wantErr: "invalid address family",
		},
		{
			name:   "forced family mismatch",
			target: targetSpec{Host: "8.8.8.8"},
			options: pingOptions{
				addressFamily:         addressFamily6,
				allowPrivateAddresses: true,
			},
			wantErr: "-6",
		},
		{
			name:    "loopback rejected",
			target:  targetSpec{Host: "127.0.0.1"},
			options: pingOptions{},
			wantErr: "non-public address",
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := pingClient{}.resolveBedrockCandidates(context.Background(), test.target, test.options)
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("resolveBedrockCandidates() error = %v, want substring %q", err, test.wantErr)
			}
		})
	}
}

func TestResolveBedrockCandidatesPropagatesLookupError(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("lookup failed")
	_, err := pingClient{
		resolver: stubBedrockResolver{err: sentinel},
	}.resolveBedrockCandidates(context.Background(), targetSpec{Host: "example.com"}, pingOptions{})
	if !errors.Is(err, sentinel) {
		t.Fatalf("resolveBedrockCandidates() error = %v, want %v", err, sentinel)
	}
}

func TestResolveBedrockCandidatesAllowsPublicLiteralByDefault(t *testing.T) {
	t.Parallel()

	candidates, err := pingClient{}.resolveBedrockCandidates(context.Background(), targetSpec{
		Host: "8.8.8.8",
	}, pingOptions{})
	if err != nil {
		t.Fatalf("resolveBedrockCandidates() error = %v", err)
	}
	if len(candidates) != 1 || candidates[0].String() != "8.8.8.8:19132" {
		t.Fatalf("candidates = %v", candidates)
	}
}

func TestPingBedrockCandidatesRejectsEmptyCandidateList(t *testing.T) {
	t.Parallel()

	_, err := pingBedrockCandidates(context.Background(), pingClient{}, nil, time.Second)
	if err == nil || err.Error() != "no dial candidates available" {
		t.Fatalf("pingBedrockCandidates() error = %v", err)
	}
}

func TestPingBedrockCandidatesReturnsContextErrorBeforeDial(t *testing.T) {
	t.Parallel()

	attempts := 0
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()

	_, err := pingBedrockCandidates(ctx, pingClient{
		dialContext: func(context.Context, string, string) (net.Conn, error) {
			attempts++
			return nil, errors.New("unexpected dial")
		},
	}, []dialCandidate{{address: mustAddrPort(t, "8.8.8.8:19132")}}, time.Second)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("pingBedrockCandidates() error = %v, want %v", err, context.DeadlineExceeded)
	}
	if attempts != 0 {
		t.Fatalf("dial attempts = %d, want 0", attempts)
	}
}

func TestPingBedrockCandidatesReturnsFirstErrorAfterContextCancellation(t *testing.T) {
	t.Parallel()

	firstErr := errors.New("first dial failed")
	secondErr := errors.New("second dial failed")
	attempts := 0
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	_, err := pingBedrockCandidates(ctx, pingClient{
		dialContext: func(_ context.Context, _ string, address string) (net.Conn, error) {
			attempts++
			if address == "8.8.8.8:19132" {
				cancel()
				return nil, firstErr
			}
			return nil, secondErr
		},
	}, []dialCandidate{
		{address: mustAddrPort(t, "8.8.8.8:19132")},
		{address: mustAddrPort(t, "1.1.1.1:19132")},
	}, time.Second)
	if !errors.Is(err, firstErr) {
		t.Fatalf("pingBedrockCandidates() error = %v, want %v", err, firstErr)
	}
	if errors.Is(err, context.Canceled) {
		t.Fatalf("pingBedrockCandidates() error = %v, want joined dial error", err)
	}
	if attempts != 1 {
		t.Fatalf("dial attempts = %d, want 1", attempts)
	}
}

func TestPingBedrockCandidateUsesReadBufferSize(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("read exploded")
	conn := &recordingBedrockConn{
		remoteAddr: &net.UDPAddr{IP: net.ParseIP("203.0.113.10"), Port: 19132},
		readErr:    sentinel,
	}
	_, err := pingBedrockCandidate(context.Background(), pingClient{
		dialContext: func(context.Context, string, string) (net.Conn, error) {
			return conn, nil
		},
		now: time.Now,
	}, dialCandidate{address: mustAddrPort(t, "203.0.113.10:19132")}, time.Second)
	if !errors.Is(err, sentinel) {
		t.Fatalf("pingBedrockCandidate() error = %v, want %v", err, sentinel)
	}
	if conn.readBufferLen != 2048 {
		t.Fatalf("Read() buffer length = %d, want 2048", conn.readBufferLen)
	}
}

func TestBuildBedrockStatusRequestPropagatesRandomFailure(t *testing.T) {
	sentinel := errors.New("entropy unavailable")
	read := func(buf []byte) (int, error) {
		if len(buf) != 8 {
			t.Fatalf("random read buffer length = %d, want 8", len(buf))
		}
		return 0, sentinel
	}

	packet, pingTime, err := buildBedrockStatusRequestWith(time.Unix(1_700_000_000, 0), read)
	if !errors.Is(err, sentinel) {
		t.Fatalf("buildBedrockStatusRequest() error = %v, want %v", err, sentinel)
	}
	if packet != nil {
		t.Fatalf("packet = %x, want nil on error", packet)
	}
	if pingTime != 0 {
		t.Fatalf("pingTime = %d, want 0 on error", pingTime)
	}
}

func TestBuildBedrockStatusRequestUsesRandomClientGUID(t *testing.T) {
	packet, pingTime, err := buildBedrockStatusRequestWith(time.Unix(1_700_000_000, 0), func(buf []byte) (int, error) {
		copy(buf, []byte{1, 2, 3, 4, 5, 6, 7, 8})
		return len(buf), nil
	})
	if err != nil {
		t.Fatalf("buildBedrockStatusRequest() error = %v", err)
	}
	if pingTime != 1_700_000_000_000 {
		t.Fatalf("pingTime = %d", pingTime)
	}
	if got := mustBigEndianUint64(t, packet[25:33]); got != 0x0102030405060708 {
		t.Fatalf("client GUID = 0x%x, want 0x0102030405060708", got)
	}
}

func TestRandomUint64UsesEightByteBufferAndZeroValueOnError(t *testing.T) {
	sentinel := errors.New("entropy unavailable")
	got, err := randomUint64With(func(buf []byte) (int, error) {
		if len(buf) != 8 {
			t.Fatalf("random read buffer length = %d, want 8", len(buf))
		}
		return 0, sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("randomUint64() error = %v, want %v", err, sentinel)
	}
	if got != 0 {
		t.Fatalf("randomUint64() = %d, want 0 on error", got)
	}
}

func TestParseBedrockStatusResponseRejectsShortHeaders(t *testing.T) {
	t.Parallel()

	payload := make([]byte, 34)
	if _, err := parseBedrockStatusResponse(payload, 0); err == nil || !strings.Contains(err.Error(), "bedrock pong too short") {
		t.Fatalf("parseBedrockStatusResponse() error = %v, want too-short error", err)
	}
}

func TestParseBedrockStatusResponseAllowsEmptyStatusPayloadToReachTextParser(t *testing.T) {
	t.Parallel()

	payload := make([]byte, 35)
	payload[0] = bedrockUnconnectedPongPacketID
	copy(payload[17:33], bedrockMagic[:])

	_, err := parseBedrockStatusResponse(payload, 0)
	if err == nil || !strings.Contains(err.Error(), "too few fields") {
		t.Fatalf("parseBedrockStatusResponse() error = %v, want text-parser failure", err)
	}
}

func TestParseBedrockStatusResponseRejectsMagicMismatch(t *testing.T) {
	t.Parallel()

	payload := mustDecodeHex(t, liveBedrockPongHex)
	payload[17] ^= 0xff
	_, err := parseBedrockStatusResponse(payload, mustBigEndianUint64(t, payload[1:9]))
	if err == nil || err.Error() != "bedrock pong magic mismatch" {
		t.Fatalf("parseBedrockStatusResponse() error = %v", err)
	}
}

func TestParseBedrockStatusResponseRejectsUnexpectedPacketID(t *testing.T) {
	t.Parallel()

	payload := mustDecodeHex(t, liveBedrockPongHex)
	payload[0] = 0xfe
	_, err := parseBedrockStatusResponse(payload, mustBigEndianUint64(t, payload[1:9]))
	if err == nil || err.Error() != "unexpected bedrock pong packet id: 254" {
		t.Fatalf("parseBedrockStatusResponse() error = %v", err)
	}
}

func TestParseBedrockStatusResponseRejectsTruncatedPayload(t *testing.T) {
	t.Parallel()

	payload := mustDecodeHex(t, liveBedrockPongHex)
	payload = payload[:len(payload)-1]

	_, err := parseBedrockStatusResponse(payload, mustBigEndianUint64(t, payload[1:9]))
	if err == nil || !strings.Contains(err.Error(), "unexpected EOF") {
		t.Fatalf("parseBedrockStatusResponse() error = %v, want unexpected EOF", err)
	}
}

func TestParseBedrockStatusResponsePreservesLastStatusByte(t *testing.T) {
	t.Parallel()

	statusText := "MCPE;MOTD;924;1.26.3;1;10;123;World;Survival"
	payload := encodeFakeBedrockPong(7, statusText)

	status, err := parseBedrockStatusResponse(payload, 7)
	if err != nil {
		t.Fatalf("parseBedrockStatusResponse() error = %v", err)
	}
	if status.GameMode != "Survival" {
		t.Fatalf("GameMode = %q, want Survival", status.GameMode)
	}
}

func TestParseBedrockStatusResponseUsesAllPingTimeBytes(t *testing.T) {
	t.Parallel()

	const pingTime uint64 = 0x0102030405060708
	payload := encodeFakeBedrockPong(pingTime, "MCPE;MOTD;924;1.26.3;1;10;123;World;Survival")

	status, err := parseBedrockStatusResponse(payload, pingTime)
	if err != nil {
		t.Fatalf("parseBedrockStatusResponse() error = %v", err)
	}
	if status.Protocol != 924 {
		t.Fatalf("Protocol = %d, want 924", status.Protocol)
	}
}

func TestParseBedrockStatusTextFieldBoundaries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		statusText string
		want       bedrockStatus
	}{
		{
			name:       "minimum fields",
			statusText: "MCPE;MOTD;924;1.26.3;1;10",
			want: bedrockStatus{
				Brand:         "MCPE",
				MOTD:          "MOTD",
				Protocol:      924,
				Version:       "1.26.3",
				PlayersOnline: 1,
				PlayersMax:    10,
			},
		},
		{
			name:       "includes server id",
			statusText: "MCPE;MOTD;924;1.26.3;1;10;123",
			want: bedrockStatus{
				Brand:         "MCPE",
				MOTD:          "MOTD",
				Protocol:      924,
				Version:       "1.26.3",
				PlayersOnline: 1,
				PlayersMax:    10,
				ServerID:      "123",
			},
		},
		{
			name:       "includes map name",
			statusText: "MCPE;MOTD;924;1.26.3;1;10;123;World",
			want: bedrockStatus{
				Brand:         "MCPE",
				MOTD:          "MOTD",
				Protocol:      924,
				Version:       "1.26.3",
				PlayersOnline: 1,
				PlayersMax:    10,
				ServerID:      "123",
				MapName:       "World",
			},
		},
		{
			name:       "includes game mode",
			statusText: "MCPE;MOTD;924;1.26.3;1;10;123;World;Survival",
			want: bedrockStatus{
				Brand:         "MCPE",
				MOTD:          "MOTD",
				Protocol:      924,
				Version:       "1.26.3",
				PlayersOnline: 1,
				PlayersMax:    10,
				ServerID:      "123",
				MapName:       "World",
				GameMode:      "Survival",
			},
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, err := parseBedrockStatusText(test.statusText)
			if err != nil {
				t.Fatalf("parseBedrockStatusText() error = %v", err)
			}
			if got != test.want {
				t.Fatalf("parseBedrockStatusText() = %+v, want %+v", got, test.want)
			}
		})
	}
}

func TestParseBedrockStatusTextRejectsTooFewFields(t *testing.T) {
	t.Parallel()

	_, err := parseBedrockStatusText("MCPE;MOTD;924;1.26.3;1")
	if err == nil || !strings.Contains(err.Error(), "too few fields") {
		t.Fatalf("parseBedrockStatusText() error = %v, want too-few-fields error", err)
	}
}

func TestParseBedrockStatusTextReportsFieldSpecificTokens(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		statusText string
		wantErr    string
	}{
		{
			name:       "protocol token",
			statusText: "MCPE;MOTD;bad;1.26.3;1;10",
			wantErr:    `invalid bedrock protocol version "bad"`,
		},
		{
			name:       "online token",
			statusText: "MCPE;MOTD;924;1.26.3;bad;10",
			wantErr:    `invalid bedrock online player count "bad"`,
		},
		{
			name:       "max token",
			statusText: "MCPE;MOTD;924;1.26.3;1;bad",
			wantErr:    `invalid bedrock max player count "bad"`,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := parseBedrockStatusText(test.statusText)
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("parseBedrockStatusText() error = %v, want substring %q", err, test.wantErr)
			}
		})
	}
}

func mustPingTime(t *testing.T, packet []byte) uint64 {
	t.Helper()

	if len(packet) < 9 {
		t.Fatalf("packet length = %d, want at least 9", len(packet))
	}
	return mustBigEndianUint64(t, packet[1:9])
}

func mustBigEndianUint64(t *testing.T, buf []byte) uint64 {
	t.Helper()

	if len(buf) != 8 {
		t.Fatalf("buffer length = %d, want 8", len(buf))
	}
	var value uint64
	for _, b := range buf {
		value = (value << 8) | uint64(b)
	}
	return value
}

type recordingBedrockConn struct {
	readBufferLen int
	readErr       error
	remoteAddr    net.Addr
	deadline      time.Time
}

func (c *recordingBedrockConn) Read(p []byte) (int, error) {
	c.readBufferLen = len(p)
	return 0, c.readErr
}

func (c *recordingBedrockConn) Write(p []byte) (int, error) {
	return len(p), nil
}

func (c *recordingBedrockConn) Close() error {
	return nil
}

func (c *recordingBedrockConn) LocalAddr() net.Addr {
	return &net.UDPAddr{}
}

func (c *recordingBedrockConn) RemoteAddr() net.Addr {
	return c.remoteAddr
}

func (c *recordingBedrockConn) SetDeadline(deadline time.Time) error {
	c.deadline = deadline
	return nil
}

func (c *recordingBedrockConn) SetReadDeadline(time.Time) error {
	return nil
}

func (c *recordingBedrockConn) SetWriteDeadline(time.Time) error {
	return nil
}
