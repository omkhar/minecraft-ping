package main

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"net/netip"
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

func TestPrepareBedrockProbeUsesResolvedAddress(t *testing.T) {
	client := pingClient{
		resolver: stubBedrockResolver{
			addrs: []netip.Addr{mustParseAddr(t, "203.0.113.10")},
		},
	}
	prepared, err := prepareBedrockProbe(context.Background(), client, targetSpec{
		Host: "example.com",
	}, pingOptions{})
	if err != nil {
		t.Fatalf("prepareBedrockProbe() error = %v", err)
	}
	if prepared.banner(false) != "PING example.com (203.0.113.10) port 19132 [bedrock]:" {
		t.Fatalf("banner = %q", prepared.banner(false))
	}
}

func TestPrepareBedrockProbeUsesIPv6DefaultPort(t *testing.T) {
	client := pingClient{
		resolver: stubBedrockResolver{
			addrs: []netip.Addr{mustParseAddr(t, "2001:db8::10")},
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
	if prepared.banner(false) != "PING example.com (2001:db8::10) port 19133 [bedrock]:" {
		t.Fatalf("banner = %q", prepared.banner(false))
	}
}

func TestBedrockTargetSpecFromEndpointUsesImplicitIPv6Port(t *testing.T) {
	target := bedrockTargetSpecFromEndpoint(endpoint{
		Host: "2001:db8::10",
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
		now: func() time.Time {
			return time.Unix(0, 0)
		},
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

	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatalf("ListenUDP() error = %v", err)
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
