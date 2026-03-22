package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"
)

type pingResult struct {
	Server    string `json:"server"`
	LatencyMs int    `json:"latency_ms"`
}

type cliConfig struct {
	Endpoint endpoint
	Timeout  time.Duration
	Format   string
	Options  pingOptions
}

type pingFunc func(target endpoint, timeout time.Duration, options pingOptions) (int, error)

func parseCLIConfig(args []string) (cliConfig, error) {
	fs := flag.NewFlagSet("minecraft-ping", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	serverPtr := fs.String("server", "mc.hypixel.net", "Minecraft server to ping (e.g., mc.example.com)")
	portPtr := fs.Int("port", defaultMinecraftPort, "Minecraft server port (default: 25565)")
	timeoutPtr := fs.Duration("timeout", 5*time.Second, "Connection timeout (e.g., 5s, 1m)")
	allowPrivatePtr := fs.Bool("allow-private", false, "Allow connections to private/local network addresses")
	forceIPv4Ptr := fs.Bool("4", false, "Force IPv4")
	forceIPv6Ptr := fs.Bool("6", false, "Force IPv6")
	formatPtr := fs.String("format", "text", "Output format: text or json")

	if err := fs.Parse(args); err != nil {
		return cliConfig{}, err
	}

	outputFormat, err := normalizeOutputFormat(*formatPtr)
	if err != nil {
		return cliConfig{}, err
	}
	family, err := parseAddressFamily(*forceIPv4Ptr, *forceIPv6Ptr)
	if err != nil {
		return cliConfig{}, err
	}

	return cliConfig{
		Endpoint: newEndpoint(*serverPtr, *portPtr),
		Timeout:  *timeoutPtr,
		Format:   outputFormat,
		Options: pingOptions{
			allowPrivateAddresses: *allowPrivatePtr,
			addressFamily:         family,
		},
	}, nil
}

func parseAddressFamily(forceIPv4 bool, forceIPv6 bool) (addressFamily, error) {
	switch {
	case forceIPv4 && forceIPv6:
		return addressFamilyAny, fmt.Errorf("flags -4 and -6 are mutually exclusive")
	case forceIPv4:
		return addressFamily4, nil
	case forceIPv6:
		return addressFamily6, nil
	default:
		return addressFamilyAny, nil
	}
}

func normalizeOutputFormat(format string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "", "text":
		return "text", nil
	case "json":
		return "json", nil
	default:
		return "", fmt.Errorf("invalid format %q (expected text or json)", format)
	}
}

func renderResult(format string, target endpoint, latency int) string {
	if format == "json" {
		// pingResult is a simple string/int struct; marshaling cannot fail.
		out, _ := json.Marshal(pingResult{Server: target.Host, LatencyMs: latency})
		return string(out)
	}
	return fmt.Sprintf("Ping time is %d ms", latency)
}

func runCLI(args []string, stdout io.Writer, ping pingFunc) error {
	config, err := parseCLIConfig(args)
	if err != nil {
		return err
	}

	latency, err := ping(config.Endpoint, config.Timeout, config.Options)
	if err != nil {
		return err
	}

	_, err = fmt.Fprintln(stdout, renderResult(config.Format, config.Endpoint, latency))
	return err
}

func run(argv []string, stdout io.Writer, stderr io.Writer, ping pingFunc) int {
	if len(argv) > 1 {
		argv = argv[1:]
	} else {
		argv = nil
	}

	if err := runCLI(argv, stdout, ping); err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}
