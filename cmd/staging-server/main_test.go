package main

import (
	"flag"
	"io"
	"testing"
	"time"

	"github.com/omkhar/minecraft-ping/v2/internal/stagingserver"
)

func parseStagingConfigForTest(t *testing.T, args []string) (stagingserver.Config, error) {
	t.Helper()

	var cfg stagingserver.Config
	fs := flag.NewFlagSet("staging-server-test", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	bindFlags(fs, &cfg)
	return cfg, fs.Parse(args)
}

func TestBindFlagsDefaults(t *testing.T) {
	t.Parallel()

	cfg, err := parseStagingConfigForTest(t, nil)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if cfg.ListenIPv4 != "127.0.0.1:25565" {
		t.Fatalf("ListenIPv4 = %q", cfg.ListenIPv4)
	}
	if cfg.ListenIPv6 != "[::1]:25566" {
		t.Fatalf("ListenIPv6 = %q", cfg.ListenIPv6)
	}
	if cfg.BedrockListenIPv4 != "" || cfg.BedrockListenIPv6 != "" {
		t.Fatalf("bedrock listen defaults = %q %q", cfg.BedrockListenIPv4, cfg.BedrockListenIPv6)
	}
	if cfg.StatusJSON != stagingserver.DefaultStatusJSON() {
		t.Fatalf("StatusJSON = %q", cfg.StatusJSON)
	}
	if cfg.BedrockStatus != "" {
		t.Fatalf("BedrockStatus = %q", cfg.BedrockStatus)
	}
	if cfg.ConnectionDeadline != 10*time.Second {
		t.Fatalf("ConnectionDeadline = %s", cfg.ConnectionDeadline)
	}
}

func TestBindFlagsOverrides(t *testing.T) {
	t.Parallel()

	cfg, err := parseStagingConfigForTest(t, []string{
		"-listen4", "127.0.0.1:3000",
		"-listen6", "[::1]:3001",
		"-bedrock-listen4", "127.0.0.1:4000",
		"-bedrock-listen6", "[::1]:4001",
		"-status-json", `{"description":"ok"}`,
		"-bedrock-status", "MCPE;test;924;1.26.3;0;10;1;world;Creative;1;19132;19133;0;",
		"-deadline", "3s",
	})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if cfg.ListenIPv4 != "127.0.0.1:3000" || cfg.ListenIPv6 != "[::1]:3001" {
		t.Fatalf("listen addresses = %q %q", cfg.ListenIPv4, cfg.ListenIPv6)
	}
	if cfg.BedrockListenIPv4 != "127.0.0.1:4000" || cfg.BedrockListenIPv6 != "[::1]:4001" {
		t.Fatalf("bedrock listen addresses = %q %q", cfg.BedrockListenIPv4, cfg.BedrockListenIPv6)
	}
	if cfg.StatusJSON != `{"description":"ok"}` {
		t.Fatalf("StatusJSON = %q", cfg.StatusJSON)
	}
	if cfg.BedrockStatus != "MCPE;test;924;1.26.3;0;10;1;world;Creative;1;19132;19133;0;" {
		t.Fatalf("BedrockStatus = %q", cfg.BedrockStatus)
	}
	if cfg.ConnectionDeadline != 3*time.Second {
		t.Fatalf("ConnectionDeadline = %s", cfg.ConnectionDeadline)
	}
}

func TestBindFlagsRejectsInvalidDuration(t *testing.T) {
	t.Parallel()

	if _, err := parseStagingConfigForTest(t, []string{"-deadline", "not-a-duration"}); err == nil {
		t.Fatal("Parse() succeeded, want error")
	}
}
