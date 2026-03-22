package main

import (
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"
)

const (
	defaultJavaPort      = 25565
	defaultBedrockPort   = 19132
	defaultBedrockPortV6 = 19133
)

type edition uint8

const (
	editionJava edition = iota
	editionBedrock
)

func (e edition) String() string {
	switch e {
	case editionBedrock:
		return "bedrock"
	default:
		return "java"
	}
}

func parseEdition(raw string) (edition, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "java":
		return editionJava, nil
	case "bedrock":
		return editionBedrock, nil
	default:
		return editionJava, fmt.Errorf("unsupported edition %q", raw)
	}
}

type targetSpec struct {
	Host         string
	Port         int
	PortExplicit bool
}

func newTargetSpec(host string, port int, explicit bool) targetSpec {
	return targetSpec{
		Host:         normalizeHost(host),
		Port:         port,
		PortExplicit: explicit,
	}
}

func (t targetSpec) String() string {
	if t.PortExplicit {
		return net.JoinHostPort(t.Host, strconv.Itoa(t.Port))
	}
	return t.Host
}

func (t targetSpec) validate() error {
	if strings.TrimSpace(t.Host) == "" {
		return fmt.Errorf("missing destination")
	}
	if err := validateServerAddress(t.Host); err != nil {
		return err
	}
	if t.PortExplicit {
		if t.Port < 1 || t.Port > 65535 {
			return fmt.Errorf("invalid port: %d", t.Port)
		}
	}
	return nil
}

func (t targetSpec) literalIP() (netip.Addr, bool) {
	addr, err := netip.ParseAddr(t.Host)
	if err != nil {
		return netip.Addr{}, false
	}
	return addr.Unmap(), true
}

func (t targetSpec) defaultPort(family addressFamily, ed edition) int {
	if t.PortExplicit {
		return t.Port
	}
	if ed == editionBedrock && family == addressFamily6 {
		return defaultBedrockPortV6
	}
	if ed == editionBedrock {
		return defaultBedrockPort
	}
	return defaultJavaPort
}

func (t targetSpec) portForAddr(addr netip.Addr, ed edition) int {
	if t.PortExplicit {
		return t.Port
	}
	if ed == editionBedrock && addr.Unmap().Is6() {
		return defaultBedrockPortV6
	}
	if ed == editionBedrock {
		return defaultBedrockPort
	}
	return defaultJavaPort
}

func (t targetSpec) fallbackEndpoint(family addressFamily, ed edition) endpoint {
	return endpoint{
		Host: t.Host,
		Port: t.defaultPort(family, ed),
	}
}

func parseDestination(raw string) (targetSpec, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return targetSpec{}, fmt.Errorf("missing destination")
	}

	if literal, ok := unbracketIPv6Literal(raw); ok {
		return newTargetSpec(literal, 0, false), nil
	}
	if _, err := netip.ParseAddr(raw); err == nil {
		return newTargetSpec(raw, 0, false), nil
	}

	looksLikeHostPort := strings.HasPrefix(raw, "[") || strings.Count(raw, ":") == 1
	if looksLikeHostPort {
		host, portText, err := net.SplitHostPort(raw)
		if err == nil {
			port, err := strconv.Atoi(portText)
			if err != nil {
				return targetSpec{}, fmt.Errorf("invalid port: %s", portText)
			}
			return newTargetSpec(host, port, true), nil
		}
	}

	if strings.Count(raw, ":") > 1 {
		return targetSpec{}, fmt.Errorf("invalid destination %q", raw)
	}

	return newTargetSpec(raw, 0, false), nil
}
