package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

type pingResult struct {
	Server    string `json:"server"`
	LatencyMs int    `json:"latency_ms"`
}

type pingFunc func(server string, port int, timeout time.Duration, options pingOptions) (int, error)

func defaultPing(server string, port int, timeout time.Duration, options pingOptions) (int, error) {
	return pingServerWithOptions(server, port, timeout, options)
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

func renderResult(format, server string, latency int) string {
	switch format {
	case "json":
		// pingResult is a simple string/int struct; marshaling cannot fail.
		out, _ := json.Marshal(pingResult{Server: server, LatencyMs: latency})
		return string(out)
	case "text":
		return fmt.Sprintf("Ping time is %d ms", latency)
	default:
		return fmt.Sprintf("Ping time is %d ms", latency)
	}
}

func runCLI(args []string, stdout io.Writer, ping pingFunc) error {
	fs := flag.NewFlagSet("minecraft-ping", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	serverPtr := fs.String("server", "mc.hypixel.net", "Minecraft server to ping (e.g., mc.example.com)")
	portPtr := fs.Int("port", 25565, "Minecraft server port (default: 25565)")
	timePtr := fs.Duration("timeout", 5*time.Second, "Connection timeout (e.g., 5s, 1m)")
	allowPrivatePtr := fs.Bool("allow-private", false, "Allow connections to private/local network addresses")
	formatPtr := fs.String("format", "text", "Output format: text or json")

	if err := fs.Parse(args); err != nil {
		return err
	}

	outputFormat, err := normalizeOutputFormat(*formatPtr)
	if err != nil {
		return err
	}

	latency, err := ping(*serverPtr, *portPtr, *timePtr, pingOptions{
		allowPrivateAddresses: *allowPrivatePtr,
	})
	if err != nil {
		return err
	}

	output := renderResult(outputFormat, *serverPtr, latency)
	_, err = fmt.Fprintln(stdout, output)
	return err
}

func argsWithoutProgram(argv []string) []string {
	if len(argv) <= 1 {
		return nil
	}
	return argv[1:]
}

func execute(argv []string, stdout io.Writer, stderr io.Writer, ping pingFunc) int {
	if err := runCLI(argsWithoutProgram(argv), stdout, ping); err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

func main() {
	os.Exit(execute(os.Args, os.Stdout, os.Stderr, defaultPing))
}
