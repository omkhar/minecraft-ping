package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

func TestRunCLITextOutput(t *testing.T) {
	var output bytes.Buffer
	var called bool

	err := runCLI(
		[]string{"-server", "mc.example.com", "-port", "25566", "-timeout", "2s"},
		&output,
		func(server string, port int, timeout time.Duration, options pingOptions) (int, error) {
			called = true
			if server != "mc.example.com" {
				t.Fatalf("server = %q, want mc.example.com", server)
			}
			if port != 25566 {
				t.Fatalf("port = %d, want 25566", port)
			}
			if timeout != 2*time.Second {
				t.Fatalf("timeout = %s, want 2s", timeout)
			}
			if options.allowPrivateAddresses {
				t.Fatalf("allowPrivateAddresses = true, want false")
			}
			return 37, nil
		},
	)
	if err != nil {
		t.Fatalf("runCLI() returned error: %v", err)
	}
	if !called {
		t.Fatal("runCLI() did not call ping function")
	}
	if got := output.String(); got != "Ping time is 37 ms\n" {
		t.Fatalf("output = %q, want %q", got, "Ping time is 37 ms\n")
	}
}

func TestRunCLIJSONOutput(t *testing.T) {
	var output bytes.Buffer

	err := runCLI(
		[]string{"-server", "json.example", "-allow-private", "-format", "JSON"},
		&output,
		func(server string, port int, timeout time.Duration, options pingOptions) (int, error) {
			if server != "json.example" {
				t.Fatalf("server = %q, want json.example", server)
			}
			if port != 25565 {
				t.Fatalf("port = %d, want default 25565", port)
			}
			if timeout != 5*time.Second {
				t.Fatalf("timeout = %s, want default 5s", timeout)
			}
			if !options.allowPrivateAddresses {
				t.Fatalf("allowPrivateAddresses = false, want true")
			}
			return 9, nil
		},
	)
	if err != nil {
		t.Fatalf("runCLI() returned error: %v", err)
	}

	var result pingResult
	if err := json.Unmarshal(output.Bytes(), &result); err != nil {
		t.Fatalf("json.Unmarshal() error: %v", err)
	}
	if result.Server != "json.example" {
		t.Fatalf("json server = %q, want json.example", result.Server)
	}
	if result.LatencyMs != 9 {
		t.Fatalf("json latency_ms = %d, want 9", result.LatencyMs)
	}
}

func TestRunCLIRejectsInvalidFormatBeforePing(t *testing.T) {
	var output bytes.Buffer
	called := false

	err := runCLI(
		[]string{"-format", "xml"},
		&output,
		func(string, int, time.Duration, pingOptions) (int, error) {
			called = true
			return 1, nil
		},
	)
	if err == nil {
		t.Fatal("runCLI() expected invalid format error but got nil")
	}
	if called {
		t.Fatal("runCLI() called ping function for invalid format")
	}
	if !strings.Contains(err.Error(), "expected text or json") {
		t.Fatalf("runCLI() error = %q, expected format validation message", err.Error())
	}
	if output.Len() != 0 {
		t.Fatalf("runCLI() wrote output for invalid format: %q", output.String())
	}
}

func TestRunCLIInvalidFlagReturnsErrorWithoutCallingPingAndNoStderr(t *testing.T) {
	var output bytes.Buffer
	called := false

	stderrCapture, err := os.CreateTemp("", "minecraft-ping-stderr-*")
	if err != nil {
		t.Fatalf("os.CreateTemp() error: %v", err)
	}
	defer os.Remove(stderrCapture.Name())

	originalStderr := os.Stderr
	os.Stderr = stderrCapture
	defer func() {
		os.Stderr = originalStderr
		_ = stderrCapture.Close()
	}()

	err = runCLI(
		[]string{"-unknown-flag"},
		&output,
		func(string, int, time.Duration, pingOptions) (int, error) {
			called = true
			return 1, nil
		},
	)
	if err == nil {
		t.Fatal("runCLI() expected flag parse error but got nil")
	}
	if called {
		t.Fatal("runCLI() called ping function when flag parsing failed")
	}
	if output.Len() != 0 {
		t.Fatalf("runCLI() wrote output for invalid flag: %q", output.String())
	}

	if _, err := stderrCapture.Seek(0, 0); err != nil {
		t.Fatalf("stderr seek failed: %v", err)
	}
	stderrBytes, err := io.ReadAll(stderrCapture)
	if err != nil {
		t.Fatalf("stderr read failed: %v", err)
	}
	if len(stderrBytes) != 0 {
		t.Fatalf("expected no stderr output, got %q", string(stderrBytes))
	}
}

func TestRunCLIPropagatesPingError(t *testing.T) {
	var output bytes.Buffer
	sentinel := errors.New("boom")

	err := runCLI(
		[]string{"-server", "mc.example.com"},
		&output,
		func(string, int, time.Duration, pingOptions) (int, error) {
			return 0, sentinel
		},
	)
	if !errors.Is(err, sentinel) {
		t.Fatalf("runCLI() error = %v, want %v", err, sentinel)
	}
	if output.Len() != 0 {
		t.Fatalf("runCLI() wrote output when ping failed: %q", output.String())
	}
}

func TestNormalizeOutputFormat(t *testing.T) {
	tests := []struct {
		name      string
		format    string
		want      string
		expectErr bool
	}{
		{name: "default empty", format: "", want: "text"},
		{name: "whitespace text", format: "  text  ", want: "text"},
		{name: "uppercase json", format: "JSON", want: "json"},
		{name: "unsupported", format: "yaml", expectErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeOutputFormat(tt.format)
			if tt.expectErr {
				if err == nil {
					t.Fatalf("normalizeOutputFormat(%q) expected error", tt.format)
				}
				return
			}
			if err != nil {
				t.Fatalf("normalizeOutputFormat(%q) error: %v", tt.format, err)
			}
			if got != tt.want {
				t.Fatalf("normalizeOutputFormat(%q) = %q, want %q", tt.format, got, tt.want)
			}
		})
	}
}

func TestArgsWithoutProgram(t *testing.T) {
	tests := []struct {
		name    string
		in      []string
		want    []string
		wantNil bool
	}{
		{name: "empty", in: nil, want: nil, wantNil: true},
		{name: "binary only", in: []string{"minecraft-ping"}, want: nil, wantNil: true},
		{name: "binary plus one arg", in: []string{"minecraft-ping", "-json"}, want: []string{"-json"}, wantNil: false},
		{name: "binary plus args", in: []string{"minecraft-ping", "-server", "mc.example.com"}, want: []string{"-server", "mc.example.com"}, wantNil: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := argsWithoutProgram(tt.in)
			if strings.Join(got, "|") != strings.Join(tt.want, "|") {
				t.Fatalf("argsWithoutProgram(%v) = %v, want %v", tt.in, got, tt.want)
			}
			if (got == nil) != tt.wantNil {
				t.Fatalf("argsWithoutProgram(%v) nil = %t, want nil = %t", tt.in, got == nil, tt.wantNil)
			}
		})
	}
}

func TestExecuteSuccess(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	rc := execute(
		[]string{"minecraft-ping", "-server", "mc.example.com"},
		&stdout,
		&stderr,
		func(string, int, time.Duration, pingOptions) (int, error) {
			return 11, nil
		},
	)
	if rc != 0 {
		t.Fatalf("execute() rc = %d, want 0", rc)
	}
	if stderr.Len() != 0 {
		t.Fatalf("execute() wrote stderr on success: %q", stderr.String())
	}
	if stdout.String() != "Ping time is 11 ms\n" {
		t.Fatalf("execute() stdout = %q, want %q", stdout.String(), "Ping time is 11 ms\n")
	}
}

func TestExecuteFailureWritesError(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	rc := execute(
		[]string{"minecraft-ping", "-format", "xml"},
		&stdout,
		&stderr,
		func(string, int, time.Duration, pingOptions) (int, error) {
			return 99, nil
		},
	)
	if rc != 1 {
		t.Fatalf("execute() rc = %d, want 1", rc)
	}
	if stdout.Len() != 0 {
		t.Fatalf("execute() wrote stdout on failure: %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "expected text or json") {
		t.Fatalf("execute() stderr = %q, expected format validation message", stderr.String())
	}
}

func TestExecuteDoesNotCreateLogFile(t *testing.T) {
	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd() error: %v", err)
	}

	workDir := t.TempDir()
	if err := os.Chdir(workDir); err != nil {
		t.Fatalf("os.Chdir(%q) error: %v", workDir, err)
	}
	defer func() {
		if chdirErr := os.Chdir(originalWD); chdirErr != nil {
			t.Fatalf("restore working directory error: %v", chdirErr)
		}
	}()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	rc := execute(
		[]string{"minecraft-ping", "-server", "mc.example.com"},
		&stdout,
		&stderr,
		func(string, int, time.Duration, pingOptions) (int, error) {
			return 5, nil
		},
	)
	if rc != 0 {
		t.Fatalf("execute() rc = %d, want 0", rc)
	}

	if _, err := os.Stat("mcping.log"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("mcping.log should not be created, got stat err=%v", err)
	}
}
