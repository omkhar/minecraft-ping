package main

import (
	"fmt"
	"net/netip"
)

var nonPublicIPPrefixes = []netip.Prefix{
	mustParsePrefix("0.0.0.0/8"),
	mustParsePrefix("10.0.0.0/8"),
	mustParsePrefix("100.64.0.0/10"),
	mustParsePrefix("127.0.0.0/8"),
	mustParsePrefix("169.254.0.0/16"),
	mustParsePrefix("172.16.0.0/12"),
	mustParsePrefix("192.0.0.0/24"),
	mustParsePrefix("192.0.2.0/24"),
	mustParsePrefix("192.168.0.0/16"),
	mustParsePrefix("198.18.0.0/15"),
	mustParsePrefix("198.51.100.0/24"),
	mustParsePrefix("203.0.113.0/24"),
	mustParsePrefix("224.0.0.0/4"),
	mustParsePrefix("240.0.0.0/4"),
	mustParsePrefix("::/128"),
	mustParsePrefix("::1/128"),
	mustParsePrefix("100::/64"),
	mustParsePrefix("2001:db8::/32"),
	mustParsePrefix("fc00::/7"),
	mustParsePrefix("fe80::/10"),
	mustParsePrefix("ff00::/8"),
}

type dialCandidate struct {
	address netip.AddrPort
}

func (c dialCandidate) Network() string {
	if c.address.Addr().Is4() {
		return "tcp4"
	}

	return "tcp6"
}

func (c dialCandidate) String() string {
	return c.address.String()
}

func isNonPublicIPAddress(addr netip.Addr) bool {
	if !addr.IsValid() {
		return true
	}

	addr = addr.Unmap()
	for _, prefix := range nonPublicIPPrefixes {
		if prefix.Contains(addr) {
			return true
		}
	}

	return false
}

func dialCandidateForLiteralIP(target endpoint, options pingOptions) ([]dialCandidate, error) {
	addr, ok := target.literalIP()
	if !ok {
		return nil, fmt.Errorf("invalid IP literal %q", target.Host)
	}
	if !options.addressFamily.matches(addr) {
		return nil, fmt.Errorf("%s is an %s address but %s was requested", target.Host, addressFamilyForAddr(addr), options.addressFamily.forcedFlag())
	}
	if !options.allowPrivateAddresses && isNonPublicIPAddress(addr) {
		return nil, fmt.Errorf("refusing to connect to non-public address %s", addr)
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
	if len(addrs) == 0 {
		return nil, fmt.Errorf("no addresses resolved for %s", host)
	}

	filtered := make([]netip.Addr, 0, len(addrs))
	for _, addr := range addrs {
		addr = addr.Unmap()
		if !addr.IsValid() {
			continue
		}
		if !options.addressFamily.matches(addr) {
			continue
		}
		if !options.allowPrivateAddresses && isNonPublicIPAddress(addr) {
			continue
		}
		filtered = append(filtered, addr)
	}

	candidates := buildDialCandidates(filtered, port)
	if len(candidates) != 0 {
		return candidates, nil
	}

	if !options.allowPrivateAddresses {
		return nil, fmt.Errorf("resolved only to non-public addresses for %s", host)
	}

	return nil, fmt.Errorf("no dialable addresses resolved for %s", host)
}

func buildDialCandidates(addrs []netip.Addr, port uint16) []dialCandidate {
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

		candidate := dialCandidate{address: netip.AddrPortFrom(addr, port)}
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
