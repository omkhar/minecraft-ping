package main

import (
	"fmt"
	"net/netip"
)

type dialCandidate struct {
	address netip.AddrPort
}

func (c dialCandidate) Network() string {
	if c.address.Addr().Is4() {
		return "tcp4"
	}

	return "tcp6"
}

func (c dialCandidate) UDPNetwork() string {
	if c.address.Addr().Is4() {
		return "udp4"
	}

	return "udp6"
}

func (c dialCandidate) String() string {
	return c.address.String()
}

func (c dialCandidate) endpoint() endpoint {
	return endpoint{
		Host: c.address.Addr().String(),
		Port: int(c.address.Port()),
	}
}

func dialCandidateForLiteralIP(target endpoint, options pingOptions) ([]dialCandidate, error) {
	addr, ok := target.literalIP()
	if !ok {
		return nil, fmt.Errorf("invalid IP literal %q", target.Host)
	}
	if !options.addressFamily.matches(addr) {
		return nil, fmt.Errorf("%s is an %s address but %s was requested", target.Host, addressFamilyForAddr(addr), options.addressFamily.forcedFlag())
	}

	port, err := target.uint16Port()
	if err != nil {
		return nil, err
	}

	return []dialCandidate{{
		address: netip.AddrPortFrom(addr, port),
	}}, nil
}

func dialCandidatesForResolvedIPs(host string, port uint16, addrs []netip.Addr, options pingOptions) ([]dialCandidate, error) {
	return dialCandidatesForResolvedIPsByAddr(host, addrs, options.addressFamily, func(netip.Addr) uint16 {
		return port
	})
}

func dialCandidatesForResolvedIPsByAddr(host string, addrs []netip.Addr, family addressFamily, portForAddr func(netip.Addr) uint16) ([]dialCandidate, error) {
	if len(addrs) == 0 {
		return nil, fmt.Errorf("no addresses resolved for %s", host)
	}

	filtered := make([]netip.Addr, 0, len(addrs))
	for _, addr := range addrs {
		addr = addr.Unmap()
		if !addr.IsValid() {
			continue
		}
		if !family.matches(addr) {
			continue
		}
		filtered = append(filtered, addr)
	}

	candidates := buildDialCandidatesWithPortFunc(filtered, portForAddr)
	if len(candidates) != 0 {
		return candidates, nil
	}

	return nil, fmt.Errorf("no dialable addresses resolved for %s", host)
}

func buildDialCandidates(addrs []netip.Addr, port uint16) []dialCandidate {
	return buildDialCandidatesWithPortFunc(addrs, func(netip.Addr) uint16 {
		return port
	})
}

func buildDialCandidatesWithPortFunc(addrs []netip.Addr, portForAddr func(netip.Addr) uint16) []dialCandidate {
	seen := make(map[netip.Addr]struct{}, len(addrs))

	var (
		ipv4              []dialCandidate
		ipv6              []dialCandidate
		primaryFamilyIsV6 bool
		primaryFamilySet  bool
	)

	for _, addr := range addrs {
		addr = addr.Unmap()
		if !addr.IsValid() {
			continue
		}
		if _, ok := seen[addr]; ok {
			continue
		}
		seen[addr] = struct{}{}

		candidate := dialCandidate{address: netip.AddrPortFrom(addr, portForAddr(addr))}
		if !primaryFamilySet {
			primaryFamilyIsV6 = addr.Is6()
			primaryFamilySet = true
		}

		if addr.Is6() {
			ipv6 = append(ipv6, candidate)
			continue
		}

		ipv4 = append(ipv4, candidate)
	}

	switch {
	case len(ipv4) == 0:
		return ipv6
	case len(ipv6) == 0:
		return ipv4
	case primaryFamilyIsV6:
		return interleaveDialCandidates(ipv6, ipv4)
	default:
		return interleaveDialCandidates(ipv4, ipv6)
	}
}

func interleaveDialCandidates(primary []dialCandidate, secondary []dialCandidate) []dialCandidate {
	ordered := make([]dialCandidate, 0, len(primary)+len(secondary))

	limit := len(primary)
	if len(secondary) > limit {
		limit = len(secondary)
	}

	for i := 0; i < limit; i++ {
		if i < len(primary) {
			ordered = append(ordered, primary[i])
		}
		if i < len(secondary) {
			ordered = append(ordered, secondary[i])
		}
	}

	return ordered
}
