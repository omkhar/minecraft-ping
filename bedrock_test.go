package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"strings"
	"testing"
	"time"
)

// Captured from a live itzg/minecraft-bedrock-server 1.26.3.1 instance.
const liveBedrockPongHex = "1c0000019d16bfe1bc8319679b9b695d9a00ffff00fefefefefdfdfdfd1234567800614d4350453b446564696361746564205365727665723b3932343b312e32362e333b303b31303b393434363639353631313431313239313534363b426564726f636b206c6576656c3b537572766976616c3b313b31393133323b31393133333b303b"

type stubBedrockResolver struct {
	addrs []netip.Addr
	err   error
}

func (r stubBedrockResolver) LookupSRV(context.Context, string, string, string) (string, []*net.SRV, error) {
	return "", nil, nil
}

func (r stubBedrockResolver) LookupNetIP(context.Context, string, string) ([]netip.Addr, error) {
	if r.err != nil {
		return nil, r.err
	}
	return append([]netip.Addr(nil), r.addrs...), nil
}

func TestBuildBedrockStatusRequest(t *testing.T) {
	now := time.Unix(1774203167, 164*int64(time.Millisecond))
	packet, pingTime, err := buildBedrockStatusRequest(now)
	if err != nil {
		t.Fatalf("buildBedrockStatusRequest() error = %v", err)
	}
	if len(packet) != 33 {
		t.Fatalf("len(packet) = %d, want 33", len(packet))
	}
	if packet[0] != bedrockUnconnectedPingPacketID {
		t.Fatalf("packet id = 0x%02x", packet[0])
	}
	if got := binary.BigEndian.Uint64(packet[1:9]); got != pingTime {
		t.Fatalf("packet timestamp = %d, want %d", got, pingTime)
	}
	if got := packet[9:25]; string(got) != string(bedrockMagic[:]) {
		t.Fatalf("magic = %x", got)
	}
}

func TestParseBedrockStatusResponseLiveCapture(t *testing.T) {
	payload := mustDecodeHex(t, liveBedrockPongHex)
	expectedPingTime := binary.BigEndian.Uint64(payload[1:9])

	status, err := parseBedrockStatusResponse(payload, expectedPingTime)
	if err != nil {
		t.Fatalf("parseBedrockStatusResponse() error = %v", err)
	}
	if status.Brand != "MCPE" {
		t.Fatalf("Brand = %q, want MCPE", status.Brand)
	}
	if status.MOTD != "Dedicated Server" {
		t.Fatalf("MOTD = %q", status.MOTD)
	}
	if status.Protocol != 924 {
		t.Fatalf("Protocol = %d", status.Protocol)
	}
	if status.Version != "1.26.3" {
		t.Fatalf("Version = %q", status.Version)
	}
	if status.PlayersOnline != 0 || status.PlayersMax != 10 {
		t.Fatalf("players = %d/%d", status.PlayersOnline, status.PlayersMax)
	}
	if status.ServerID != "9446695611411291546" {
		t.Fatalf("ServerID = %q", status.ServerID)
	}
	if status.MapName != "Bedrock level" {
		t.Fatalf("MapName = %q", status.MapName)
	}
	if status.GameMode != "Survival" {
		t.Fatalf("GameMode = %q", status.GameMode)
	}
}

func TestParseBedrockStatusResponseRejectsTimestampMismatch(t *testing.T) {
	payload := mustDecodeHex(t, liveBedrockPongHex)
	_, err := parseBedrockStatusResponse(payload, 1)
	if err == nil || err.Error() != "bedrock pong ping time mismatch" {
		t.Fatalf("parseBedrockStatusResponse() error = %v", err)
	}
}

func TestParseBedrockStatusResponseRejectsInvalidBrand(t *testing.T) {
	payload := mustDecodeHex(t, liveBedrockPongHex)
	copy(payload[35:39], []byte("XCPE"))
	_, err := parseBedrockStatusResponse(payload, binary.BigEndian.Uint64(payload[1:9]))
	if err == nil || err.Error() != `unexpected bedrock status brand "XCPE"` {
		t.Fatalf("parseBedrockStatusResponse() error = %v", err)
	}
}

func TestParseBedrockStatusResponseRejectsInvalidUTF8(t *testing.T) {
	payload := mustDecodeHex(t, liveBedrockPongHex)
	payload[35] = 0xff
	_, err := parseBedrockStatusResponse(payload, binary.BigEndian.Uint64(payload[1:9]))
	if err == nil || err.Error() != "bedrock status payload is not valid UTF-8" {
		t.Fatalf("parseBedrockStatusResponse() error = %v", err)
	}
}

func TestParseBedrockStatusTextReportsOnlinePlayerToken(t *testing.T) {
	_, err := parseBedrockStatusText("MCPE;Test Server;924;1.26.3;bogus;10;983;World;Survival;1;19132;19133;0;")
	if err == nil {
		t.Fatal("parseBedrockStatusText() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), `invalid bedrock online player count "bogus"`) {
		t.Fatalf("parseBedrockStatusText() error = %q", err)
	}
}

func TestPrepareBedrockProbeUsesResolvedAddress(t *testing.T) {
	client := pingClient{
		resolver: stubBedrockResolver{
			addrs: []netip.Addr{mustParseAddr(t, "8.8.8.8")},
		},
	}
	prepared, err := prepareBedrockProbe(context.Background(), client, targetSpec{
		Host: "example.com",
	}, pingOptions{})
	if err != nil {
		t.Fatalf("prepareBedrockProbe() error = %v", err)
	}
	if prepared.banner(false) != "PING example.com (8.8.8.8) port 19132 [bedrock]:" {
		t.Fatalf("banner = %q", prepared.banner(false))
	}
}

func TestPrepareBedrockProbeUsesIPv6DefaultPort(t *testing.T) {
	client := pingClient{
		resolver: stubBedrockResolver{
			addrs: []netip.Addr{mustParseAddr(t, "2606:4700:4700::1111")},
		},
	}
	prepared, err := prepareBedrockProbe(context.Background(), client, targetSpec{
		Host: "example.com",
	}, pingOptions{
		addressFamily: addressFamily6,
	})
	if err != nil {
		t.Fatalf("prepareBedrockProbe() error = %v", err)
	}
	if prepared.banner(false) != "PING example.com (2606:4700:4700::1111) port 19133 [bedrock]:" {
		t.Fatalf("banner = %q", prepared.banner(false))
	}
}

func TestPrepareBedrockProbeWrapsResolverError(t *testing.T) {
	sentinel := errors.New("lookup failed")
	client := pingClient{
		resolver: stubBedrockResolver{err: sentinel},
	}

	_, err := prepareBedrockProbe(context.Background(), client, targetSpec{
		Host: "example.com",
	}, pingOptions{})
	if !errors.Is(err, sentinel) {
		t.Fatalf("prepareBedrockProbe() error = %v, want %v", err, sentinel)
	}
	if !strings.Contains(err.Error(), "failed to resolve bedrock server example.com") {
		t.Fatalf("prepareBedrockProbe() error = %q, want wrapped resolver context", err)
	}
}

func TestBedrockTargetSpecFromEndpointUsesImplicitIPv6Port(t *testing.T) {
	target := bedrockTargetSpecFromEndpoint(endpoint{
		Host: "2606:4700:4700::1111",
		Port: 19133,
	}, addressFamily6)
	if target.PortExplicit {
		t.Fatal("PortExplicit = true, want false for implicit IPv6 bedrock port")
	}
}

func TestPingBedrockCandidateAgainstFakeServer(t *testing.T) {
	server := startFakeBedrockServer(t, func(packet []byte) ([]byte, error) {
		if packet[0] != bedrockUnconnectedPingPacketID {
			return nil, fmt.Errorf("packet id = 0x%02x", packet[0])
		}
		pingTime := binary.BigEndian.Uint64(packet[1:9])
		return encodeFakeBedrockPong(pingTime, "MCPE;Test Server;924;1.26.3;1;10;983;World;Survival;1;19132;19133;0;"), nil
	})
	defer server.Close(t)

	client := pingClient{
		now: time.Now,
	}
	candidate := dialCandidate{address: server.addr}
	sample, err := pingBedrockCandidate(context.Background(), client, candidate, 2*time.Second)
	if err != nil {
		t.Fatalf("pingBedrockCandidate() error = %v", err)
	}
	if sample.remote != server.addr {
		t.Fatalf("remote = %s, want %s", sample.remote, server.addr)
	}
}

func TestPingBedrockCandidateUsesInjectedClockForDeadlineFallback(t *testing.T) {
	deadlineBase := time.Unix(1_700_000_000, 500_000_000)
	nowValues := []time.Time{
		deadlineBase,
		time.Unix(1_700_000_001, 0),
		time.Unix(1_700_000_001, 0),
		deadlineBase,
	}
	nowIndex := 0
	conn := &fakeBedrockConn{
		remoteAddr: net.UDPAddrFromAddrPort(netip.MustParseAddrPort("203.0.113.10:19132")),
		onWrite: func(packet []byte) []byte {
			pingTime := binary.BigEndian.Uint64(packet[1:9])
			return encodeFakeBedrockPong(pingTime, "MCPE;Test Server;924;1.26.3;1;10;983;World;Survival;1;19132;19133;0;")
		},
	}
	client := pingClient{
		dialContext: func(context.Context, string, string) (net.Conn, error) {
			return conn, nil
		},
		now: func() time.Time {
			value := nowValues[nowIndex]
			if nowIndex < len(nowValues)-1 {
				nowIndex++
			}
			return value
		},
	}

	sample, err := pingBedrockCandidate(context.Background(), client, dialCandidate{
		address: netip.MustParseAddrPort("203.0.113.10:19132"),
	}, 2*time.Second)
	if err != nil {
		t.Fatalf("pingBedrockCandidate() error = %v", err)
	}
	if sample.remote != netip.MustParseAddrPort("203.0.113.10:19132") {
		t.Fatalf("remote = %s", sample.remote)
	}
	if want := deadlineBase.Add(2 * time.Second); !conn.deadline.Equal(want) {
		t.Fatalf("deadline = %s, want %s", conn.deadline, want)
	}
	if sample.latency != 0 {
		t.Fatalf("latency = %s, want 0", sample.latency)
	}
}

func TestPingBedrockCandidatesStopsAtExpiredDeadline(t *testing.T) {
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()

	calls := 0
	client := pingClient{
		dialContext: func(context.Context, string, string) (net.Conn, error) {
			calls++
			return nil, errors.New("unexpected dial")
		},
	}

	sample, err := pingBedrockCandidates(ctx, client, []dialCandidate{
		{address: mustAddrPort(t, "203.0.113.10:19132")},
	}, 2*time.Second)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("pingBedrockCandidates() error = %v, want %v", err, context.DeadlineExceeded)
	}
	if calls != 0 {
		t.Fatalf("dialContext calls = %d, want 0", calls)
	}
	if sample != (probeSample{}) {
		t.Fatalf("sample = %+v, want zero value", sample)
	}
}

func TestPrepareBedrockProbeNumericModeUsesResolvedAddress(t *testing.T) {
	client := pingClient{
		resolver: stubBedrockResolver{
			addrs: []netip.Addr{mustParseAddr(t, "8.8.8.8")},
		},
	}

	prepared, err := prepareBedrockProbe(context.Background(), client, targetSpec{
		Host: "example.com",
	}, pingOptions{
		allowPrivateAddresses: true,
	})
	if err != nil {
		t.Fatalf("prepareBedrockProbe() error = %v", err)
	}
	if got := prepared.banner(true); got != "PING 8.8.8.8 port 19132 [bedrock]:" {
		t.Fatalf("banner(true) = %q", got)
	}
	if got := prepared.summaryLabel(true); got != "8.8.8.8" {
		t.Fatalf("summaryLabel(true) = %q", got)
	}
}

func TestPingBedrockServer(t *testing.T) {
	server := startFakeBedrockServer(t, func(packet []byte) ([]byte, error) {
		pingTime := binary.BigEndian.Uint64(packet[1:9])
		return encodeFakeBedrockPong(pingTime, "MCPE;Test Server;924;1.26.3;1;10;983;World;Survival;1;19132;19133;0;"), nil
	})
	defer server.Close(t)

	latency, err := ping(newEndpoint(server.addr.Addr().String(), int(server.addr.Port())), 2*time.Second, pingOptions{
		allowPrivateAddresses: true,
		edition:               editionBedrock,
	})
	if err != nil {
		t.Fatalf("ping() error = %v", err)
	}
	if latency <= 0 {
		t.Fatalf("ping() latency = %d, want positive", latency)
	}
}

func TestPingBedrockWrapsMalformedPongError(t *testing.T) {
	server := startFakeBedrockServer(t, func(packet []byte) ([]byte, error) {
		pingTime := binary.BigEndian.Uint64(packet[1:9])
		reply := encodeFakeBedrockPong(pingTime, "MCPE;Broken;924;1.26.3;0;10;1;World;Survival;1;19132;19133;0;")
		reply[0] = 0xff
		return reply, nil
	})
	defer server.Close(t)

	_, err := pingBedrock(newEndpoint(server.addr.Addr().String(), int(server.addr.Port())), 2*time.Second, pingOptions{
		allowPrivateAddresses: true,
	})
	if err == nil {
		t.Fatal("pingBedrock() expected error")
	}
	if !strings.Contains(err.Error(), "failed to ping server") {
		t.Fatalf("pingBedrock() error = %q, want wrapped ping context", err)
	}
	if !strings.Contains(err.Error(), "unexpected bedrock pong packet id") {
		t.Fatalf("pingBedrock() error = %q, want malformed pong context", err)
	}
}

func TestPingBedrockServerIPv6(t *testing.T) {
	server := startFakeBedrockServerOn(t, "udp6", "[::1]:0", func(packet []byte) ([]byte, error) {
		pingTime := binary.BigEndian.Uint64(packet[1:9])
		return encodeFakeBedrockPong(pingTime, "MCPE;Test Server;924;1.26.3;1;10;983;World;Survival;1;19132;19133;0;"), nil
	})
	defer server.Close(t)

	latency, err := ping(newEndpoint(server.addr.Addr().String(), int(server.addr.Port())), 2*time.Second, pingOptions{
		addressFamily:         addressFamily6,
		allowPrivateAddresses: true,
		edition:               editionBedrock,
	})
	if err != nil {
		t.Fatalf("ping() error = %v", err)
	}
	if latency <= 0 {
		t.Fatalf("ping() latency = %d, want positive", latency)
	}
}

func TestPrepareBedrockProbeProbesResolvedAddress(t *testing.T) {
	server := startFakeBedrockServer(t, func(packet []byte) ([]byte, error) {
		pingTime := binary.BigEndian.Uint64(packet[1:9])
		return encodeFakeBedrockPong(pingTime, "MCPE;Test Server;924;1.26.3;1;10;983;World;Survival;1;19132;19133;0;"), nil
	})
	defer server.Close(t)

	client := pingClient{
		resolver: stubBedrockResolver{
			addrs: []netip.Addr{server.addr.Addr()},
		},
	}
	prepared, err := prepareBedrockProbe(context.Background(), client, targetSpec{
		Host:         "example.com",
		Port:         int(server.addr.Port()),
		PortExplicit: true,
	}, pingOptions{
		allowPrivateAddresses: true,
	})
	if err != nil {
		t.Fatalf("prepareBedrockProbe() error = %v", err)
	}

	sample, err := prepared.probe(context.Background(), 2*time.Second)
	if err != nil {
		t.Fatalf("prepared.probe() error = %v", err)
	}
	if sample.remote != server.addr {
		t.Fatalf("remote = %s, want %s", sample.remote, server.addr)
	}
}

func TestPrepareBedrockProbeProbesResolvedIPv6ImplicitPort(t *testing.T) {
	var (
		dialedNetwork string
		dialedAddress string
	)
	client := pingClient{
		resolver: stubBedrockResolver{
			addrs: []netip.Addr{mustParseAddr(t, "::1")},
		},
	}
	client.dialContext = func(_ context.Context, network, address string) (net.Conn, error) {
		dialedNetwork = network
		dialedAddress = address
		return &fakeBedrockConn{
			remoteAddr: net.UDPAddrFromAddrPort(netip.MustParseAddrPort("[::1]:19133")),
			onWrite: func(packet []byte) []byte {
				pingTime := binary.BigEndian.Uint64(packet[1:9])
				return encodeFakeBedrockPong(pingTime, "MCPE;Test Server;924;1.26.3;1;10;983;World;Survival;1;19132;19133;0;")
			},
		}, nil
	}
	prepared, err := prepareBedrockProbe(context.Background(), client, targetSpec{
		Host: "example.com",
	}, pingOptions{
		addressFamily:         addressFamily6,
		allowPrivateAddresses: true,
	})
	if err != nil {
		t.Fatalf("prepareBedrockProbe() error = %v", err)
	}

	sample, err := prepared.probe(context.Background(), 2*time.Second)
	if err != nil {
		t.Fatalf("prepared.probe() error = %v", err)
	}
	if sample.remote != netip.MustParseAddrPort("[::1]:19133") {
		t.Fatalf("remote = %s, want [::1]:19133", sample.remote)
	}
	if dialedNetwork != "udp6" {
		t.Fatalf("network = %q, want udp6", dialedNetwork)
	}
	if dialedAddress != "[::1]:19133" {
		t.Fatalf("address = %q, want [::1]:19133", dialedAddress)
	}
}

func TestPingBedrockCandidateRejectsMalformedPong(t *testing.T) {
	server := startFakeBedrockServer(t, func(packet []byte) ([]byte, error) {
		pingTime := binary.BigEndian.Uint64(packet[1:9])
		reply := encodeFakeBedrockPong(pingTime, "MCPE;Broken;924;1.26.3;0;10;1;World;Survival;1;19132;19133;0;")
		reply[0] = 0xff
		return reply, nil
	})
	defer server.Close(t)

	client := pingClient{}
	candidate := dialCandidate{address: server.addr}
	if _, err := pingBedrockCandidate(context.Background(), client, candidate, time.Second); err == nil {
		t.Fatal("pingBedrockCandidate() expected malformed pong error")
	}
}

func TestParseBedrockStatusResponseRejectsTrailingBytes(t *testing.T) {
	payload := append(mustDecodeHex(t, liveBedrockPongHex), 0x00)
	_, err := parseBedrockStatusResponse(payload, binary.BigEndian.Uint64(payload[1:9]))
	if err == nil || err.Error() != "bedrock pong length mismatch: got 133 want 132" {
		t.Fatalf("parseBedrockStatusResponse() error = %v", err)
	}
}

func mustDecodeHex(t *testing.T, raw string) []byte {
	t.Helper()

	buf := make([]byte, len(raw)/2)
	for i := 0; i < len(raw); i += 2 {
		var value uint64
		_, err := fmt.Sscanf(raw[i:i+2], "%02x", &value)
		if err != nil {
			t.Fatalf("decode hex: %v", err)
		}
		buf[i/2] = byte(value)
	}
	return buf
}

func mustParseAddr(t *testing.T, raw string) netip.Addr {
	t.Helper()

	addr, err := netip.ParseAddr(raw)
	if err != nil {
		t.Fatalf("ParseAddr(%q): %v", raw, err)
	}
	return addr
}

type fakeBedrockServer struct {
	conn  *net.UDPConn
	addr  netip.AddrPort
	errCh chan error
}

func startFakeBedrockServer(t *testing.T, handler func([]byte) ([]byte, error)) *fakeBedrockServer {
	t.Helper()

	return startFakeBedrockServerOn(t, "udp4", "127.0.0.1:0", handler)
}

func startFakeBedrockServerOn(t *testing.T, network, address string, handler func([]byte) ([]byte, error)) *fakeBedrockServer {
	t.Helper()

	packetConn, err := net.ListenPacket(network, address)
	if err != nil {
		if network == "udp6" {
			t.Skipf("udp6 test listener unavailable: %v", err)
		}
		t.Fatalf("ListenUDP() error = %v", err)
	}
	conn, ok := packetConn.(*net.UDPConn)
	if !ok {
		t.Fatalf("ListenPacket() returned %T, want *net.UDPConn", packetConn)
	}

	server := &fakeBedrockServer{
		conn:  conn,
		addr:  conn.LocalAddr().(*net.UDPAddr).AddrPort(),
		errCh: make(chan error, 1),
	}
	go func() {
		defer close(server.errCh)

		var buf [2048]byte
		if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
			server.errCh <- err
			return
		}
		n, remote, err := conn.ReadFromUDPAddrPort(buf[:])
		if err != nil {
			server.errCh <- err
			return
		}
		reply, err := handler(append([]byte(nil), buf[:n]...))
		if err != nil {
			server.errCh <- err
			return
		}
		if _, err := conn.WriteToUDPAddrPort(reply, remote); err != nil {
			server.errCh <- err
			return
		}
	}()
	return server
}

func (s *fakeBedrockServer) Close(t *testing.T) {
	t.Helper()

	_ = s.conn.Close()
	if err, ok := <-s.errCh; ok && err != nil && !errors.Is(err, net.ErrClosed) {
		t.Fatalf("fake bedrock server error: %v", err)
	}
}

func encodeFakeBedrockPong(pingTime uint64, status string) []byte {
	payload := make([]byte, 0, 35+len(status))
	payload = append(payload, bedrockUnconnectedPongPacketID)

	var scratch [8]byte
	binary.BigEndian.PutUint64(scratch[:], pingTime)
	payload = append(payload, scratch[:]...)
	binary.BigEndian.PutUint64(scratch[:], 0x8877665544332211)
	payload = append(payload, scratch[:]...)
	payload = append(payload, bedrockMagic[:]...)

	var length [2]byte
	binary.BigEndian.PutUint16(length[:], uint16(len(status))) // #nosec G115 -- test fixture only
	payload = append(payload, length[:]...)
	payload = append(payload, []byte(status)...)
	return payload
}

type fakeBedrockConn struct {
	readBuf    []byte
	remoteAddr net.Addr
	onWrite    func([]byte) []byte
	deadline   time.Time
}

func (c *fakeBedrockConn) Read(p []byte) (int, error) {
	if len(c.readBuf) == 0 {
		return 0, io.EOF
	}
	n := copy(p, c.readBuf)
	c.readBuf = c.readBuf[n:]
	return n, nil
}

func (c *fakeBedrockConn) Write(p []byte) (int, error) {
	if c.onWrite != nil {
		c.readBuf = c.onWrite(bytes.Clone(p))
	}
	return len(p), nil
}

func (c *fakeBedrockConn) Close() error {
	return nil
}

func (c *fakeBedrockConn) LocalAddr() net.Addr {
	return &net.UDPAddr{}
}

func (c *fakeBedrockConn) RemoteAddr() net.Addr {
	return c.remoteAddr
}

func (c *fakeBedrockConn) SetDeadline(deadline time.Time) error {
	c.deadline = deadline
	return nil
}

func (c *fakeBedrockConn) SetReadDeadline(time.Time) error {
	return nil
}

func (c *fakeBedrockConn) SetWriteDeadline(time.Time) error {
	return nil
}
