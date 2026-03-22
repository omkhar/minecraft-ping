package main

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"strings"
	"time"
)

type javaPreparedProbe struct {
	client        pingClient
	target        targetSpec
	route         endpointRoute
	candidates    []dialCandidate
	displayTarget netip.AddrPort
}

func prepareJavaProbe(ctx context.Context, client pingClient, target targetSpec, options pingOptions) (preparedProbe, error) {
	client = client.withDefaults()

	route, err := client.resolveJavaRouteContext(ctx, target)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve java server %s: %w", target.Host, err)
	}

	candidates, err := client.resolveDialCandidates(ctx, route.Dial, options)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve java server %s: %w", target.Host, err)
	}

	prepared := javaPreparedProbe{
		client:     client,
		target:     target,
		route:      route,
		candidates: candidates,
	}
	if len(candidates) > 0 {
		prepared.displayTarget = candidates[0].address
	}

	return prepared, nil
}

func (p javaPreparedProbe) banner(numeric bool) string {
	label := p.summaryLabel(numeric)
	displayPort := p.route.Handshake.Port
	if p.displayTarget.IsValid() && !numeric && p.target.Host != p.displayTarget.Addr().String() {
		return fmt.Sprintf("PING %s (%s) port %d [%s]:", p.target.Host, p.displayTarget.Addr(), displayPort, editionJava)
	}
	if p.displayTarget.IsValid() {
		return fmt.Sprintf("PING %s port %d [%s]:", p.displayTarget.Addr(), displayPort, editionJava)
	}
	return fmt.Sprintf("PING %s port %d [%s]:", label, displayPort, editionJava)
}

func (p javaPreparedProbe) summaryLabel(numeric bool) string {
	if numeric && p.displayTarget.IsValid() {
		return p.displayTarget.Addr().String()
	}
	return p.target.Host
}

func (p javaPreparedProbe) probe(ctx context.Context, timeout time.Duration) (probeSample, error) {
	return p.client.pingJavaPreparedContext(ctx, p.route, p.candidates, timeout)
}

func (c pingClient) resolveJavaRouteContext(ctx context.Context, target targetSpec) (endpointRoute, error) {
	routeTarget := target.fallbackEndpoint(addressFamilyAny, editionJava)
	route := endpointRoute{
		Dial:      routeTarget,
		Handshake: routeTarget,
	}

	if target.PortExplicit || routeTarget.Port != defaultMinecraftPort {
		return route, nil
	}
	if _, ok := routeTarget.literalIP(); ok {
		return route, nil
	}

	_, records, err := c.withDefaults().resolver.LookupSRV(ctx, "minecraft", "tcp", routeTarget.Host)
	if err != nil || len(records) == 0 {
		if ctx.Err() != nil {
			return endpointRoute{}, ctx.Err()
		}
		return route, nil
	}

	srvTarget := strings.TrimSuffix(records[0].Target, ".")
	if srvTarget == "" || records[0].Port == 0 {
		return route, nil
	}

	resolvedPort := int(records[0].Port)
	route.Dial = endpoint{Host: srvTarget, Port: resolvedPort}
	route.Handshake = endpoint{Host: routeTarget.Host, Port: resolvedPort}
	return route, nil
}

func (c pingClient) pingJavaPreparedContext(ctx context.Context, route endpointRoute, candidates []dialCandidate, timeout time.Duration) (probeSample, error) {
	if len(candidates) == 0 {
		var err error
		candidates, err = c.resolveDialCandidates(ctx, route.Dial, pingOptions{})
		if err != nil {
			return probeSample{}, err
		}
	}

	conn, err := c.dialCandidates(ctx, candidates)
	if err != nil {
		return probeSample{}, err
	}
	defer conn.Close()

	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(timeout)
	}
	if err := conn.SetDeadline(deadline); err != nil {
		return probeSample{}, err
	}

	if err := sendHandshakePacket(conn, route.Handshake); err != nil {
		return probeSample{}, err
	}
	if err := sendStatusRequestPacket(conn); err != nil {
		return probeSample{}, err
	}
	if err := readStatusResponse(conn); err != nil {
		return probeSample{}, err
	}

	token, err := c.tokenSource()
	if err != nil {
		return probeSample{}, err
	}
	start := c.now()

	if err := sendPingPacket(conn, token); err != nil {
		return probeSample{}, err
	}
	if err := readPongPacket(conn, token); err != nil {
		return probeSample{}, err
	}

	latency := c.now().Sub(start)
	if latency < time.Millisecond {
		latency = time.Millisecond
	}

	return probeSample{
		latency: latency,
		remote:  remoteAddrPort(conn.RemoteAddr()),
	}, nil
}

func remoteAddrPort(addr net.Addr) netip.AddrPort {
	if addr == nil {
		return netip.AddrPort{}
	}

	addrPort, err := netip.ParseAddrPort(addr.String())
	if err == nil {
		return addrPort
	}

	host, portText, splitErr := net.SplitHostPort(addr.String())
	if splitErr != nil {
		return netip.AddrPort{}
	}

	ip, ipErr := netip.ParseAddr(host)
	if ipErr != nil {
		return netip.AddrPort{}
	}
	port, portErr := net.LookupPort("tcp", portText)
	if portErr != nil {
		return netip.AddrPort{}
	}

	return netip.AddrPortFrom(ip.Unmap(), uint16(port))
}
