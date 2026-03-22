package main

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"
)

const (
	defaultMinecraftPort   = defaultJavaPort
	maxServerAddressLength = 253
)

type addressFamily uint8

const (
	addressFamilyAny addressFamily = iota
	addressFamily4
	addressFamily6
)

type pingOptions struct {
	addressFamily addressFamily
	edition       edition
}

type endpoint struct {
	Host string
	Port int
}

type endpointRoute struct {
	Dial      endpoint
	Handshake endpoint
}

func (f addressFamily) validate() error {
	switch f {
	case addressFamilyAny, addressFamily4, addressFamily6:
		return nil
	default:
		return fmt.Errorf("invalid address family: %d", f)
	}
}

func (f addressFamily) resolverNetwork() string {
	switch f {
	case addressFamily4:
		return "ip4"
	case addressFamily6:
		return "ip6"
	default:
		return "ip"
	}
}

func (f addressFamily) matches(addr netip.Addr) bool {
	addr = addr.Unmap()

	switch f {
	case addressFamily4:
		return addr.Is4()
	case addressFamily6:
		return addr.Is6()
	default:
		return addr.IsValid()
	}
}

func (f addressFamily) forcedFlag() string {
	switch f {
	case addressFamily4:
		return "-4"
	case addressFamily6:
		return "-6"
	default:
		return ""
	}
}

func (f addressFamily) String() string {
	switch f {
	case addressFamily4:
		return "IPv4"
	case addressFamily6:
		return "IPv6"
	default:
		return "IP"
	}
}

func addressFamilyForAddr(addr netip.Addr) addressFamily {
	if addr.Unmap().Is6() {
		return addressFamily6
	}

	return addressFamily4
}

func newEndpoint(host string, port int) endpoint {
	return endpoint{
		Host: normalizeHost(host),
		Port: port,
	}
}

func normalizeHost(host string) string {
	host = strings.TrimSpace(host)

	if unbracketed, ok := unbracketIPv6Literal(host); ok {
		return unbracketed
	}

	return host
}

func unbracketIPv6Literal(host string) (string, bool) {
	if len(host) < 2 || host[0] != '[' || host[len(host)-1] != ']' {
		return "", false
	}

	literal := host[1 : len(host)-1]
	if _, err := netip.ParseAddr(literal); err != nil {
		return "", false
	}

	return literal, true
}

func (e endpoint) String() string {
	return net.JoinHostPort(e.Host, strconv.Itoa(e.Port))
}

func (e endpoint) uint16Port() (uint16, error) {
	return toUint16(e.Port)
}

func (e endpoint) literalIP() (netip.Addr, bool) {
	addr, err := netip.ParseAddr(e.Host)
	if err != nil {
		return netip.Addr{}, false
	}

	return addr.Unmap(), true
}

func (e endpoint) validate() error {
	if e.Host == "" {
		return errors.New("server must not be empty")
	}
	if err := validateServerAddress(e.Host); err != nil {
		return err
	}
	if e.Port < 1 || e.Port > 65535 {
		return fmt.Errorf("invalid port: %d. port must be between 1 and 65535", e.Port)
	}
	return nil
}

func validateServerAddress(server string) error {
	if len(server) > maxServerAddressLength {
		return fmt.Errorf("server must not exceed %d bytes", maxServerAddressLength)
	}
	if strings.ContainsAny(server, "[]") {
		return errors.New("server must not use brackets")
	}

	for _, r := range server {
		if r <= 0x1F || r == 0x7F {
			return errors.New("server contains control characters")
		}
	}
	if strings.Contains(server, ":") {
		if _, err := netip.ParseAddr(server); err != nil {
			return errors.New("server must not include a port")
		}
	}

	return nil
}

func toUint16(value int) (uint16, error) {
	if value < 0 || value > 65535 {
		return 0, fmt.Errorf("value %d is out of uint16 range", value)
	}
	return uint16(value), nil // #nosec G115 -- explicit bounds check above
}
