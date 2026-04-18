package main

import (
	"errors"
	"flag"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

type config struct {
	archiveGlob     string
	backend         string
	binaryName      string
	binaryPath      string
	containerCLI    string
	containerName   string
	expectedVersion string
	imageArchive    string
	imageTag        string
	ipv4Host        string
	ipv6Host        string
	javaIPv4Port    int
	javaIPv6Port    int
	bedrockIPv4Port int
	bedrockIPv6Port int
	probeTimeout    time.Duration
	serverBinary    string
}

type pingResult struct {
	Server    string `json:"server"`
	LatencyMS int64  `json:"latency_ms"`
}

type probeSpec struct {
	label      string
	editionArg string
	familyFlag string
	host       string
	port       int
}

const maxExtractedBinarySize int64 = 64 << 20

var execCommandContext = exec.CommandContext

func probeSpecs(cfg config) []probeSpec {
	return []probeSpec{
		{
			label:      "java-ipv4",
			familyFlag: "-4",
			host:       cfg.ipv4Host,
			port:       cfg.javaIPv4Port,
		},
		{
			label:      "java-ipv6",
			familyFlag: "-6",
			host:       cfg.ipv6Host,
			port:       cfg.javaIPv6Port,
		},
		{
			label:      "bedrock-ipv4",
			editionArg: "--bedrock",
			familyFlag: "-4",
			host:       cfg.ipv4Host,
			port:       cfg.bedrockIPv4Port,
		},
		{
			label:      "bedrock-ipv6",
			editionArg: "--bedrock",
			familyFlag: "-6",
			host:       cfg.ipv6Host,
			port:       cfg.bedrockIPv6Port,
		},
	}
}

func bindFlags(fs *flag.FlagSet, cfg *config) {
	fs.StringVar(&cfg.backend, "backend", "binary", "Integration backend: binary or container")
	fs.StringVar(&cfg.archiveGlob, "binary-archive-glob", "", "Glob that resolves to a release archive containing the binary to execute")
	fs.StringVar(&cfg.binaryName, "binary-name", "", "Binary name inside the release archive")
	fs.StringVar(&cfg.binaryPath, "binary", "", "Path to an already extracted binary to execute")
	fs.StringVar(&cfg.containerCLI, "container-cli", "docker", "Container CLI used to run the Minecraft container")
	fs.StringVar(&cfg.containerName, "container-name", cfg.containerName, "Container name used for the integration target")
	fs.StringVar(&cfg.expectedVersion, "expected-version", "", "Expected output of the released binary version string, without the leading program name")
	fs.StringVar(&cfg.imageArchive, "image-archive", "", "Optional path to a compressed docker/podman image archive (.tar.gz)")
	fs.StringVar(&cfg.imageTag, "image-tag", "minecraft-staging-image:ci", "Tag of the staging image to run after it is loaded")
	fs.StringVar(&cfg.ipv4Host, "ipv4-host", "127.0.0.1", "IPv4 hostname used by the released binary")
	fs.StringVar(&cfg.ipv6Host, "ipv6-host", "::1", "IPv6 hostname used by the released binary")
	fs.IntVar(&cfg.javaIPv4Port, "java-ipv4-port", 45565, "Java IPv4 host port used by the released binary")
	fs.IntVar(&cfg.javaIPv6Port, "java-ipv6-port", 45566, "Java IPv6 host port used by the released binary")
	fs.IntVar(&cfg.bedrockIPv4Port, "bedrock-ipv4-port", 49132, "Bedrock IPv4 host port used by the released binary")
	fs.IntVar(&cfg.bedrockIPv6Port, "bedrock-ipv6-port", 49133, "Bedrock IPv6 host port used by the released binary")
	fs.DurationVar(&cfg.probeTimeout, "probe-timeout", 12*time.Second, "Timeout passed through to the ping binary")
	fs.StringVar(&cfg.serverBinary, "server-binary", "", "Path to the staging backend binary when -backend=binary")
}

func validatePort(flagName string, port int) error {
	if port < 1 || port > 65535 {
		return fmt.Errorf("invalid %s: %d (must be between 1 and 65535)", flagName, port)
	}
	return nil
}

func validateConfig(cfg config) error {
	hasBinaryPath := strings.TrimSpace(cfg.binaryPath) != ""
	hasArchiveGlob := strings.TrimSpace(cfg.archiveGlob) != ""
	hasBinaryName := strings.TrimSpace(cfg.binaryName) != ""

	switch {
	case hasBinaryPath && (hasArchiveGlob || hasBinaryName):
		return errors.New("choose either -binary or -binary-archive-glob/-binary-name, not both")
	case !hasBinaryPath && !hasArchiveGlob && !hasBinaryName:
		return errors.New("either -binary or both -binary-archive-glob and -binary-name are required")
	case !hasBinaryPath && (!hasArchiveGlob || !hasBinaryName):
		return errors.New("both -binary-archive-glob and -binary-name are required when -binary is not set")
	}

	if cfg.probeTimeout <= 0 {
		return errors.New("invalid -probe-timeout: must be greater than 0")
	}

	for _, portCheck := range []struct {
		flagName string
		value    int
	}{
		{flagName: "-java-ipv4-port", value: cfg.javaIPv4Port},
		{flagName: "-java-ipv6-port", value: cfg.javaIPv6Port},
		{flagName: "-bedrock-ipv4-port", value: cfg.bedrockIPv4Port},
		{flagName: "-bedrock-ipv6-port", value: cfg.bedrockIPv6Port},
	} {
		if err := validatePort(portCheck.flagName, portCheck.value); err != nil {
			return err
		}
	}

	switch cfg.backend {
	case "binary":
		if strings.TrimSpace(cfg.serverBinary) == "" {
			return errors.New("missing -server-binary for -backend=binary")
		}
	case "container":
		if strings.TrimSpace(cfg.containerCLI) == "" {
			return errors.New("missing -container-cli for -backend=container")
		}
		if strings.TrimSpace(cfg.containerName) == "" {
			return errors.New("missing -container-name for -backend=container")
		}
	default:
		return fmt.Errorf("unsupported -backend %q", cfg.backend)
	}

	return nil
}
