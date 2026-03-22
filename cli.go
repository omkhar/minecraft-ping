package main

import (
	"bytes"
	"encoding/json"
	"errors"
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

type cliParseError struct {
	err        error
	message    string
	writeUsage bool
}

type pingFunc func(target endpoint, timeout time.Duration, options pingOptions) (int, error)

func (e *cliParseError) Error() string {
	return e.err.Error()
}

func (e *cliParseError) Unwrap() error {
	return e.err
}

type cliFlagValues struct {
	server       *string
	port         *int
	timeout      *time.Duration
	allowPrivate *bool
	forceIPv4    *bool
	forceIPv6    *bool
	format       *string
}

func newCLIFlagSet(output io.Writer) (*flag.FlagSet, cliFlagValues) {
	fs := flag.NewFlagSet("minecraft-ping", flag.ContinueOnError)
	fs.SetOutput(output)
	fs.Usage = func() {
		_, _ = fmt.Fprintf(output, "Usage:\n  %s [flags]\n\nFlags:\n", fs.Name())
		fs.PrintDefaults()
	}

	values := cliFlagValues{
		server:       fs.String("server", "mc.hypixel.net", "Minecraft server to ping (e.g., mc.example.com)"),
		port:         fs.Int("port", defaultMinecraftPort, "Minecraft server port"),
		timeout:      fs.Duration("timeout", 5*time.Second, "Connection timeout (e.g., 5s, 1m)"),
		allowPrivate: fs.Bool("allow-private", false, "Allow connections to private/local network addresses"),
		forceIPv4:    fs.Bool("4", false, "Force IPv4"),
		forceIPv6:    fs.Bool("6", false, "Force IPv6"),
		format:       fs.String("format", "text", "Output format: text or json"),
	}

	return fs, values
}

func parseCLIConfig(args []string) (cliConfig, error) {
	var output bytes.Buffer
	fs, values := newCLIFlagSet(&output)

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) && output.Len() == 0 {
			fs.Usage()
		}

		message := output.String()
		if message == "" {
			message = err.Error()
		}

		return cliConfig{}, &cliParseError{
			err:        err,
			message:    message,
			writeUsage: errors.Is(err, flag.ErrHelp),
		}
	}

	outputFormat, err := normalizeOutputFormat(*values.format)
	if err != nil {
		return cliConfig{}, err
	}
	family, err := parseAddressFamily(*values.forceIPv4, *values.forceIPv6)
	if err != nil {
		return cliConfig{}, err
	}

	return cliConfig{
		Endpoint: newEndpoint(*values.server, *values.port),
		Timeout:  *values.timeout,
		Format:   outputFormat,
		Options: pingOptions{
			allowPrivateAddresses: *values.allowPrivate,
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

func writeCLIMessage(w io.Writer, message string) {
	if message == "" {
		return
	}
	if strings.HasSuffix(message, "\n") {
		_, _ = io.WriteString(w, message)
		return
	}
	_, _ = fmt.Fprintln(w, message)
}

func run(argv []string, stdout io.Writer, stderr io.Writer, ping pingFunc) int {
	if len(argv) > 1 {
		argv = argv[1:]
	} else {
		argv = nil
	}

	if err := runCLI(argv, stdout, ping); err != nil {
		var parseErr *cliParseError
		if errors.As(err, &parseErr) {
			if parseErr.writeUsage {
				writeCLIMessage(stdout, parseErr.message)
				return 0
			}

			writeCLIMessage(stderr, parseErr.message)
			return 1
		}
		_, _ = fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}
