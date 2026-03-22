package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/netip"
	"strings"
	"testing"
	"time"
)

type stubPreparedProbe struct {
	bannerText  string
	summaryText string
	samples     []probeSample
	errs        []error
	timeouts    []time.Duration
}

func (p *stubPreparedProbe) banner(bool) string {
	return p.bannerText
}

func (p *stubPreparedProbe) summaryLabel(bool) string {
	return p.summaryText
}

func (p *stubPreparedProbe) probe(_ context.Context, timeout time.Duration) (probeSample, error) {
	if len(p.timeouts) < len(p.samples)+len(p.errs)+1 {
		p.timeouts = append(p.timeouts, 0)
	}
	index := len(p.timeouts) - 1
	p.timeouts[index] = timeout
	if len(p.errs) > 0 {
		err := p.errs[0]
		p.errs = p.errs[1:]
		return probeSample{}, err
	}
	if len(p.samples) == 0 {
		return probeSample{}, errors.New("no sample queued")
	}
	sample := p.samples[0]
	p.samples = p.samples[1:]
	return sample, nil
}

func TestParseCLIConfigDefaultJava(t *testing.T) {
	cfg, status := parseCLIConfig([]string{"mc.example.com"})
	if status != parseStatusOK {
		t.Fatalf("parseCLIConfig() status = %v", status)
	}
	if cfg.Edition != editionJava {
		t.Fatalf("Edition = %v, want java", cfg.Edition)
	}
	if cfg.Target.Host != "mc.example.com" {
		t.Fatalf("Host = %q", cfg.Target.Host)
	}
	if cfg.Target.PortExplicit {
		t.Fatal("PortExplicit = true, want false")
	}
	if cfg.Interval != time.Second {
		t.Fatalf("Interval = %s, want 1s", cfg.Interval)
	}
	if cfg.Timeout != 5*time.Second {
		t.Fatalf("Timeout = %s, want 5s", cfg.Timeout)
	}
}

func TestParseCLIConfigBedrockFlags(t *testing.T) {
	cfg, status := parseCLIConfig([]string{"--bedrock", "-6", "-c", "3", "-i", "0.5", "-w", "2", "-W", "1.5", "[2001:db8::10]:19133"})
	if status != parseStatusOK {
		t.Fatalf("parseCLIConfig() status = %v", status)
	}
	if cfg.Edition != editionBedrock {
		t.Fatalf("Edition = %v, want bedrock", cfg.Edition)
	}
	if cfg.Options.addressFamily != addressFamily6 {
		t.Fatalf("addressFamily = %v, want IPv6", cfg.Options.addressFamily)
	}
	if cfg.Count != 3 {
		t.Fatalf("Count = %d, want 3", cfg.Count)
	}
	if cfg.Interval != 500*time.Millisecond {
		t.Fatalf("Interval = %s, want 500ms", cfg.Interval)
	}
	if cfg.Deadline != 2*time.Second {
		t.Fatalf("Deadline = %s, want 2s", cfg.Deadline)
	}
	if cfg.Timeout != 1500*time.Millisecond {
		t.Fatalf("Timeout = %s, want 1.5s", cfg.Timeout)
	}
	if cfg.Target.Host != "2001:db8::10" || cfg.Target.Port != 19133 || !cfg.Target.PortExplicit {
		t.Fatalf("Target = %+v", cfg.Target)
	}
}

func TestParseCLIConfigRejectsInvalidInputs(t *testing.T) {
	tests := [][]string{
		nil,
		{"-4", "-6", "example.com"},
		{"--java", "--bedrock", "example.com"},
		{"-j", "-c", "2", "example.com"},
		{"--edition", "pocket", "example.com"},
		{"--edition", "", "example.com"},
		{"--edition=", "example.com"},
		{"--java=bedrock", "example.com"},
		{"--bedrock=java", "example.com"},
		{"--help=wat"},
		{"--version=wat"},
		{"-i", "NaN", "example.com"},
		{"-i", "Inf", "example.com"},
		{"-i", "1e20", "example.com"},
		{"-i", "9223372036.854777", "example.com"},
		{"-w", "NaN", "example.com"},
		{"-W", "NaN", "example.com"},
		{"-Z", "example.com"},
	}

	for _, args := range tests {
		if _, status := parseCLIConfig(args); status != parseStatusInvalid {
			t.Fatalf("parseCLIConfig(%v) status = %v, want invalid", args, status)
		}
	}
}

func TestParseSecondsDurationRejectsOverflowBoundary(t *testing.T) {
	if _, ok := parseSecondsDuration("9223372036.854777"); ok {
		t.Fatal("parseSecondsDuration() accepted overflowing duration")
	}
	if _, ok := parseSecondsDuration("9223372036.8547758071"); ok {
		t.Fatal("parseSecondsDuration() accepted exact-overflow duration")
	}
}

func TestParseSecondsDurationPreservesNanosecondBoundaries(t *testing.T) {
	duration, ok := parseSecondsDuration("0.000000001")
	if !ok {
		t.Fatal("parseSecondsDuration() rejected 1ns duration")
	}
	if duration != time.Nanosecond {
		t.Fatalf("duration = %s, want 1ns", duration)
	}

	duration, ok = parseSecondsDuration("9223372036.854775807")
	if !ok {
		t.Fatal("parseSecondsDuration() rejected max int64 duration")
	}
	if duration != time.Duration(math.MaxInt64) {
		t.Fatalf("duration = %d, want %d", duration, time.Duration(math.MaxInt64))
	}
}

func TestRunWithRuntimeWritesHelpAndVersion(t *testing.T) {
	tests := []struct {
		name       string
		argv       []string
		wantRC     int
		wantStdout string
	}{
		{
			name:       "short help",
			argv:       []string{"minecraft-ping", "-h"},
			wantRC:     0,
			wantStdout: "Usage: minecraft-ping [options] destination",
		},
		{
			name:       "long help short circuits trailing invalid argv",
			argv:       []string{"minecraft-ping", "--help", "--bedrock=java"},
			wantRC:     0,
			wantStdout: "Usage: minecraft-ping [options] destination",
		},
		{
			name:       "short version",
			argv:       []string{"minecraft-ping", "-V"},
			wantRC:     0,
			wantStdout: "minecraft-ping dev\n",
		},
		{
			name:       "long version short circuits trailing invalid argv",
			argv:       []string{"minecraft-ping", "--version", "--bedrock=java"},
			wantRC:     0,
			wantStdout: "minecraft-ping dev\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer

			rc := runWithRuntime(tt.argv, &stdout, &stderr, defaultCLIRuntime())
			if rc != tt.wantRC {
				t.Fatalf("rc = %d, want %d", rc, tt.wantRC)
			}
			if got := stdout.String(); got != tt.wantStdout && !strings.Contains(got, tt.wantStdout) {
				t.Fatalf("stdout = %q", got)
			}
			if stderr.Len() != 0 {
				t.Fatalf("stderr = %q, want empty", stderr.String())
			}
		})
	}
}

func TestRunWithRuntimeWritesUsageOnInvalidArgv(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	tests := [][]string{
		{"minecraft-ping", "-j", "-c", "2", "example.com"},
		{"minecraft-ping", "-W", "NaN", "example.com"},
		{"minecraft-ping", "--edition", "", "example.com"},
		{"minecraft-ping", "--help=wat"},
		{"minecraft-ping", "--edition=", "example.com"},
	}

	for _, argv := range tests {
		stdout.Reset()
		stderr.Reset()

		rc := runWithRuntime(argv, &stdout, &stderr, defaultCLIRuntime())
		if rc != 2 {
			t.Fatalf("runWithRuntime(%v) rc = %d, want 2", argv, rc)
		}
		if !strings.Contains(stdout.String(), "Usage: minecraft-ping [options] destination") {
			t.Fatalf("runWithRuntime(%v) stdout = %q", argv, stdout.String())
		}
		if stderr.Len() != 0 {
			t.Fatalf("runWithRuntime(%v) stderr = %q, want empty", argv, stderr.String())
		}
	}
}

func TestRunWithRuntimeJSONProbe(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	probe := &stubPreparedProbe{
		bannerText:  "unused",
		summaryText: "unused",
		samples: []probeSample{{
			latency: 12 * time.Millisecond,
		}},
	}

	rc := runWithRuntime(
		[]string{"minecraft-ping", "-j", "example.com"},
		&stdout,
		&stderr,
		cliRuntime{
			newContext: func() (context.Context, context.CancelFunc) { return context.WithCancel(context.Background()) },
			session:    defaultSessionRuntime(),
			prepare: func(context.Context, cliConfig) (preparedProbe, error) {
				return probe, nil
			},
		},
	)
	if rc != 0 {
		t.Fatalf("rc = %d, want 0", rc)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q", stderr.String())
	}

	var result pingResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if result.Server != "example.com" || result.LatencyMs != 12 {
		t.Fatalf("result = %+v", result)
	}
}

func TestRunWithRuntimeTextSession(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	probe := &stubPreparedProbe{
		bannerText:  "PING example.com (203.0.113.10) port 25565 [java]:",
		summaryText: "example.com",
		samples: []probeSample{
			{latency: 12 * time.Millisecond, remote: mustAddrPort(t, "203.0.113.10:25565")},
			{latency: 15 * time.Millisecond, remote: mustAddrPort(t, "203.0.113.10:25565")},
		},
	}

	nowValues := []time.Time{
		time.Unix(100, 0),
		time.Unix(100, 0),
		time.Unix(100, 500_000_000),
		time.Unix(101, 0),
		time.Unix(102, 0),
	}
	nowIndex := 0
	now := func() time.Time {
		if nowIndex >= len(nowValues) {
			return nowValues[len(nowValues)-1]
		}
		value := nowValues[nowIndex]
		nowIndex++
		return value
	}

	rc := runWithRuntime(
		[]string{"minecraft-ping", "-c", "2", "example.com"},
		&stdout,
		&stderr,
		cliRuntime{
			newContext: func() (context.Context, context.CancelFunc) { return context.WithCancel(context.Background()) },
			session: sessionRuntime{
				now: now,
				sleep: func(context.Context, time.Duration) error {
					return nil
				},
			},
			prepare: func(context.Context, cliConfig) (preparedProbe, error) {
				return probe, nil
			},
		},
	)
	if rc != 0 {
		t.Fatalf("rc = %d, want 0", rc)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q", stderr.String())
	}

	output := stdout.String()
	for _, want := range []string{
		"PING example.com (203.0.113.10) port 25565 [java]:",
		"pong from 203.0.113.10:25565: seq=1 time=12.000 ms",
		"pong from 203.0.113.10:25565: seq=2 time=15.000 ms",
		"--- example.com ping statistics ---",
		"2 probes transmitted, 2 received, 0% packet loss",
		"rtt min/avg/max/mdev = 12.000/13.500/15.000/1.500 ms",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("output missing %q in %q", want, output)
		}
	}
}

func TestRunWithRuntimeTimestampModeOnlyStampsReplyLines(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	probe := &stubPreparedProbe{
		bannerText:  "PING example.com (203.0.113.10) port 25565 [java]:",
		summaryText: "example.com",
		samples: []probeSample{
			{latency: 12 * time.Millisecond, remote: mustAddrPort(t, "203.0.113.10:25565")},
		},
	}

	nowValues := []time.Time{
		time.Unix(100, 0),
		time.Unix(100, 0),
		time.Unix(100, 0),
		time.Unix(101, 0),
	}
	nowIndex := 0
	now := func() time.Time {
		if nowIndex >= len(nowValues) {
			return nowValues[len(nowValues)-1]
		}
		value := nowValues[nowIndex]
		nowIndex++
		return value
	}

	rc := runWithRuntime(
		[]string{"minecraft-ping", "-D", "-c", "1", "example.com"},
		&stdout,
		&stderr,
		cliRuntime{
			newContext: func() (context.Context, context.CancelFunc) { return context.WithCancel(context.Background()) },
			session: sessionRuntime{
				now: now,
				sleep: func(context.Context, time.Duration) error {
					return nil
				},
			},
			prepare: func(context.Context, cliConfig) (preparedProbe, error) {
				return probe, nil
			},
		},
	)
	if rc != 0 {
		t.Fatalf("rc = %d, want 0", rc)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q", stderr.String())
	}

	lines := strings.Split(strings.TrimRight(stdout.String(), "\n"), "\n")
	if len(lines) < 5 {
		t.Fatalf("unexpected output: %q", stdout.String())
	}
	if strings.HasPrefix(lines[0], "[") {
		t.Fatalf("banner line should not be timestamped: %q", lines[0])
	}
	if !strings.HasPrefix(lines[1], "[100.000000] pong from 203.0.113.10:25565: seq=1 time=12.000 ms") {
		t.Fatalf("reply line = %q", lines[1])
	}
	if strings.HasPrefix(lines[3], "[") || strings.HasPrefix(lines[4], "[") {
		t.Fatalf("summary lines should not be timestamped: %q", stdout.String())
	}
}

func mustAddrPort(t *testing.T, raw string) netip.AddrPort {
	t.Helper()

	addr, err := netip.ParseAddrPort(raw)
	if err != nil {
		t.Fatalf("ParseAddrPort(%q): %v", raw, err)
	}
	return addr
}
