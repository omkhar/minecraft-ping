package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

func parseReleaseConfigForTest(t *testing.T, args []string) (config, error) {
	t.Helper()

	cfg := config{containerName: "test-container"}
	fs := flag.NewFlagSet("release-integration-test", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	bindFlags(fs, &cfg)
	return cfg, fs.Parse(args)
}

func validReleaseConfig() config {
	return config{
		binaryPath:      "/tmp/minecraft-ping",
		backend:         "binary",
		containerCLI:    "docker",
		containerName:   "test-container",
		javaIPv4Port:    45565,
		javaIPv6Port:    45566,
		bedrockIPv4Port: 49132,
		bedrockIPv6Port: 49133,
		probeTimeout:    12 * time.Second,
		serverBinary:    "/tmp/minecraft-staging-server",
	}
}

func restoreReleaseTimeHooks() func() {
	previousNow := releaseTimeNow
	previousAfter := releaseTimeAfter
	previousJavaProbe := javaListenerProbe
	previousBedrockProbe := bedrockListenerProbe

	return func() {
		releaseTimeNow = previousNow
		releaseTimeAfter = previousAfter
		javaListenerProbe = previousJavaProbe
		bedrockListenerProbe = previousBedrockProbe
	}
}

func restoreReleaseHooks() func() {
	restoreTime := restoreReleaseTimeHooks()
	previousValidateConfig := releaseValidateConfig
	previousRun := releaseRun
	previousNotifyContext := releaseNotifyContext
	previousResolveSingleFile := releaseResolveSingleFile
	previousVersionFromArchive := releaseVersionFromArchive
	previousExtractBinary := releaseExtractBinary
	previousAssertVersion := releaseAssertVersion
	previousStartBackend := releaseStartBackend
	previousRunProbe := releaseRunProbe
	previousWaitForJava := releaseWaitForJava
	previousWaitForBedrock := releaseWaitForBedrock
	previousLoadImage := releaseLoadImage
	previousRemoveContainer := releaseRemoveContainer
	previousStartContainer := releaseStartContainer
	previousNewIPv6Relay := releaseNewIPv6Relay
	previousNewUDPIPv6Relay := releaseNewUDPIPv6Relay
	previousMkdirTemp := releaseMkdirTemp
	previousZipOpenReader := releaseZipOpenReader
	previousOpenRoot := releaseOpenRoot
	previousOpenReadOnlyArchive := releaseOpenReadOnlyArchive
	previousGzipNewReader := releaseGzipNewReader
	previousZipFileOpen := releaseZipFileOpen
	previousRootOpenFile := releaseRootOpenFile
	previousRootChmod := releaseRootChmod
	previousCopyWithLimit := releaseCopyWithLimit
	previousCopyN := releaseCopyN
	previousFileClose := releaseFileClose
	previousRootOpen := releaseRootOpen
	previousRootClose := releaseRootClose
	previousSetIPv6Only := releaseSetIPv6Only
	previousListen := releaseListen
	previousListenPacket := releaseListenPacket
	previousResolveUDPAddr := releaseResolveUDPAddr
	previousDialTCP := releaseDialTCP
	previousDialUDP := releaseDialUDP
	previousExecCommandContext := execCommandContext

	return func() {
		restoreTime()
		releaseValidateConfig = previousValidateConfig
		releaseRun = previousRun
		releaseNotifyContext = previousNotifyContext
		releaseResolveSingleFile = previousResolveSingleFile
		releaseVersionFromArchive = previousVersionFromArchive
		releaseExtractBinary = previousExtractBinary
		releaseAssertVersion = previousAssertVersion
		releaseStartBackend = previousStartBackend
		releaseRunProbe = previousRunProbe
		releaseWaitForJava = previousWaitForJava
		releaseWaitForBedrock = previousWaitForBedrock
		releaseLoadImage = previousLoadImage
		releaseRemoveContainer = previousRemoveContainer
		releaseStartContainer = previousStartContainer
		releaseNewIPv6Relay = previousNewIPv6Relay
		releaseNewUDPIPv6Relay = previousNewUDPIPv6Relay
		releaseMkdirTemp = previousMkdirTemp
		releaseZipOpenReader = previousZipOpenReader
		releaseOpenRoot = previousOpenRoot
		releaseOpenReadOnlyArchive = previousOpenReadOnlyArchive
		releaseGzipNewReader = previousGzipNewReader
		releaseZipFileOpen = previousZipFileOpen
		releaseRootOpenFile = previousRootOpenFile
		releaseRootChmod = previousRootChmod
		releaseCopyWithLimit = previousCopyWithLimit
		releaseCopyN = previousCopyN
		releaseFileClose = previousFileClose
		releaseRootOpen = previousRootOpen
		releaseRootClose = previousRootClose
		releaseSetIPv6Only = previousSetIPv6Only
		releaseListen = previousListen
		releaseListenPacket = previousListenPacket
		releaseResolveUDPAddr = previousResolveUDPAddr
		releaseDialTCP = previousDialTCP
		releaseDialUDP = previousDialUDP
		execCommandContext = previousExecCommandContext
	}
}

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func captureLogOutput(t *testing.T) *lockedBuffer {
	t.Helper()

	buf := &lockedBuffer{}
	previousWriter := log.Writer()
	log.SetOutput(buf)
	t.Cleanup(func() {
		log.SetOutput(previousWriter)
	})
	return buf
}

func captureStdoutStderr(t *testing.T, fn func()) (string, string) {
	t.Helper()

	previousStdout := os.Stdout
	previousStderr := os.Stderr
	stdoutReader, stdoutWriter, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() stdout error = %v", err)
	}
	stderrReader, stderrWriter, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() stderr error = %v", err)
	}

	os.Stdout = stdoutWriter
	os.Stderr = stderrWriter

	stdoutCh := make(chan string, 1)
	stderrCh := make(chan string, 1)
	go func() {
		data, _ := io.ReadAll(stdoutReader)
		stdoutCh <- string(data)
	}()
	go func() {
		data, _ := io.ReadAll(stderrReader)
		stderrCh <- string(data)
	}()

	fn()

	_ = stdoutWriter.Close()
	_ = stderrWriter.Close()
	os.Stdout = previousStdout
	os.Stderr = previousStderr

	return <-stdoutCh, <-stderrCh
}

func helperExecCommandContext(behavior string) func(context.Context, string, ...string) *exec.Cmd {
	return func(ctx context.Context, command string, args ...string) *exec.Cmd {
		helperArgs := []string{"-test.run=TestHelperProcess", "--", behavior, command}
		helperArgs = append(helperArgs, args...)

		cmd := exec.CommandContext(ctx, os.Args[0], helperArgs...)
		cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
		return cmd
	}
}

func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}

	var sep int
	for i, arg := range os.Args {
		if arg == "--" {
			sep = i
			break
		}
	}
	if sep == 0 || sep+2 >= len(os.Args) {
		fmt.Fprint(os.Stderr, "missing helper arguments")
		os.Exit(2)
	}

	behavior := os.Args[sep+1]
	forwarded := os.Args[sep+3:]

	switch behavior {
	case "version-ok":
		if len(forwarded) != 1 || forwarded[0] != "-V" {
			fmt.Fprintf(os.Stderr, "unexpected version args: %v", forwarded)
			os.Exit(2)
		}
		fmt.Println("minecraft-ping 2.0.3")
	case "version-bad":
		fmt.Println("minecraft-ping 9.9.9")
	case "version-fail":
		if len(forwarded) != 1 || forwarded[0] != "-V" {
			fmt.Fprintf(os.Stderr, "unexpected version args: %v", forwarded)
			os.Exit(2)
		}
		fmt.Fprint(os.Stderr, "version exploded")
		os.Exit(1)
	case "probe-ok":
		want := []string{"-j", "--allow-private", "-W", "1.5", "--bedrock", "-6", "[::1]:19133"}
		if strings.Join(forwarded, "\x00") != strings.Join(want, "\x00") {
			fmt.Fprintf(os.Stderr, "unexpected probe args: %v", forwarded)
			os.Exit(2)
		}
		fmt.Print(`{"server":"::1","latency_ms":12}`)
	case "probe-bad-json":
		fmt.Print("not-json")
	case "probe-json":
		fmt.Print(`{"server":"127.0.0.1","latency_ms":7}`)
	case "probe-fail":
		want := []string{"-j", "--allow-private", "-W", "1", "-4", "127.0.0.1:25565"}
		if strings.Join(forwarded, "\x00") != strings.Join(want, "\x00") {
			fmt.Fprintf(os.Stderr, "unexpected probe args: %v", forwarded)
			os.Exit(2)
		}
		fmt.Fprint(os.Stderr, "probe exploded")
		os.Exit(1)
	case "container-not-found":
		fmt.Fprint(os.Stderr, "Error: No such container")
		os.Exit(1)
	case "container-rm-fail":
		if len(forwarded) > 0 && forwarded[0] == "rm" {
			want := []string{"rm", "-f", "test-container"}
			if strings.Join(forwarded, "\x00") != strings.Join(want, "\x00") {
				fmt.Fprintf(os.Stderr, "unexpected rm args: %v", forwarded)
				os.Exit(2)
			}
			fmt.Fprint(os.Stderr, "rm exploded")
			os.Exit(1)
		}
		fallthrough
	case "container-start":
		want := []string{
			"run", "-d",
			"--name", "test-container",
			"-p", "127.0.0.1:45565:25565/tcp",
			"-p", "127.0.0.1:49132:19132/udp",
			"minecraft-staging-image:ci",
		}
		if strings.Join(forwarded, "\x00") != strings.Join(want, "\x00") {
			fmt.Fprintf(os.Stderr, "unexpected container args: %v", forwarded)
			os.Exit(2)
		}
	case "load-image":
		if len(forwarded) != 1 || forwarded[0] != "load" {
			fmt.Fprintf(os.Stderr, "unexpected load args: %v", forwarded)
			os.Exit(2)
		}
		n, err := io.Copy(io.Discard, os.Stdin)
		if err != nil {
			fmt.Fprint(os.Stderr, err)
			os.Exit(2)
		}
		if n == 0 {
			fmt.Fprint(os.Stderr, "missing image bytes")
			os.Exit(2)
		}
	case "load-image-fail":
		if len(forwarded) != 1 || forwarded[0] != "load" {
			fmt.Fprintf(os.Stderr, "unexpected load args: %v", forwarded)
			os.Exit(2)
		}
		fmt.Fprint(os.Stderr, "load exploded")
		os.Exit(1)
	case "exit-ok":
		os.Exit(0)
	case "hold":
		time.Sleep(30 * time.Second)
	default:
		fmt.Fprintf(os.Stderr, "unknown helper behavior %q", behavior)
		os.Exit(2)
	}

	os.Exit(0)
}

func TestVersionFromArchiveName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		archive string
		want    string
		wantErr string
	}{
		{
			name:    "darwin tarball",
			archive: "dist/minecraft-ping_2.0.3_Darwin_arm64.tar.gz",
			want:    "2.0.3",
		},
		{
			name:    "snapshot windows zip",
			archive: "dist/minecraft-ping_2.0.3-SNAPSHOT-d526d04_Windows_amd64.zip",
			want:    "2.0.3-SNAPSHOT-d526d04",
		},
		{
			name:    "unexpected prefix",
			archive: "dist/other_2.0.3_Linux_amd64.tar.gz",
			wantErr: `derive version from archive "dist/other_2.0.3_Linux_amd64.tar.gz": unexpected name`,
		},
		{
			name:    "unexpected suffix",
			archive: "dist/minecraft-ping_2.0.3_linux_amd64.deb",
			wantErr: `derive version from archive "dist/minecraft-ping_2.0.3_linux_amd64.deb": unsupported name`,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, err := versionFromArchiveName(test.archive)
			if test.wantErr != "" {
				if err == nil {
					t.Fatalf("versionFromArchiveName(%q) returned %q, want error %q", test.archive, got, test.wantErr)
				}
				if err.Error() != test.wantErr {
					t.Fatalf("versionFromArchiveName(%q) error = %q, want %q", test.archive, err, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("versionFromArchiveName(%q) error: %v", test.archive, err)
			}
			if got != test.want {
				t.Fatalf("versionFromArchiveName(%q) = %q, want %q", test.archive, got, test.want)
			}
		})
	}
}

func TestVersionLine(t *testing.T) {
	t.Parallel()

	if got, want := versionLine("2.0.3"), "minecraft-ping 2.0.3"; got != want {
		t.Fatalf("versionLine() = %q, want %q", got, want)
	}
}

func TestBindFlagsDefaults(t *testing.T) {
	t.Parallel()

	cfg, err := parseReleaseConfigForTest(t, nil)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if cfg.backend != "binary" {
		t.Fatalf("backend = %q", cfg.backend)
	}
	if cfg.containerCLI != "docker" {
		t.Fatalf("containerCLI = %q", cfg.containerCLI)
	}
	if cfg.imageTag != "minecraft-staging-image:ci" {
		t.Fatalf("imageTag = %q", cfg.imageTag)
	}
	if cfg.ipv4Host != "127.0.0.1" || cfg.ipv6Host != "::1" {
		t.Fatalf("hosts = %q %q", cfg.ipv4Host, cfg.ipv6Host)
	}
	if cfg.probeTimeout != 12*time.Second {
		t.Fatalf("probeTimeout = %s", cfg.probeTimeout)
	}
}

func TestBindFlagsOverrides(t *testing.T) {
	t.Parallel()

	cfg, err := parseReleaseConfigForTest(t, []string{
		"-backend", "container",
		"-binary", "/tmp/minecraft-ping",
		"-container-cli", "podman",
		"-container-name", "custom",
		"-expected-version", "2.0.3",
		"-image-archive", "/tmp/image.tar.gz",
		"-image-tag", "custom-image:tag",
		"-ipv4-host", "198.51.100.10",
		"-ipv6-host", "2001:db8::10",
		"-java-ipv4-port", "35565",
		"-java-ipv6-port", "35566",
		"-bedrock-ipv4-port", "39132",
		"-bedrock-ipv6-port", "39133",
		"-probe-timeout", "3s",
	})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if cfg.backend != "container" || cfg.binaryPath != "/tmp/minecraft-ping" {
		t.Fatalf("backend/binaryPath = %q %q", cfg.backend, cfg.binaryPath)
	}
	if cfg.containerCLI != "podman" || cfg.containerName != "custom" {
		t.Fatalf("container settings = %q %q", cfg.containerCLI, cfg.containerName)
	}
	if cfg.expectedVersion != "2.0.3" || cfg.imageArchive != "/tmp/image.tar.gz" || cfg.imageTag != "custom-image:tag" {
		t.Fatalf("version/image settings = %+v", cfg)
	}
	if cfg.ipv4Host != "198.51.100.10" || cfg.ipv6Host != "2001:db8::10" {
		t.Fatalf("hosts = %q %q", cfg.ipv4Host, cfg.ipv6Host)
	}
	if cfg.javaIPv4Port != 35565 || cfg.javaIPv6Port != 35566 || cfg.bedrockIPv4Port != 39132 || cfg.bedrockIPv6Port != 39133 {
		t.Fatalf("ports = %+v", cfg)
	}
	if cfg.probeTimeout != 3*time.Second {
		t.Fatalf("probeTimeout = %s", cfg.probeTimeout)
	}
}

func TestValidateConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cfg  config
	}{
		{
			name: "binary path with server binary",
			cfg:  validReleaseConfig(),
		},
		{
			name: "archive config with container backend",
			cfg: func() config {
				cfg := validReleaseConfig()
				cfg.binaryPath = ""
				cfg.archiveGlob = "dist/*.tar.gz"
				cfg.binaryName = "minecraft-ping"
				cfg.backend = "container"
				cfg.serverBinary = ""
				return cfg
			}(),
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if err := validateConfig(test.cfg); err != nil {
				t.Fatalf("validateConfig() error = %v", err)
			}
		})
	}

	errorCases := []struct {
		name    string
		cfg     config
		wantErr string
	}{
		{
			name: "missing binary input",
			cfg: func() config {
				cfg := validReleaseConfig()
				cfg.binaryPath = ""
				cfg.serverBinary = ""
				cfg.backend = "container"
				return cfg
			}(),
			wantErr: "either -binary or both -binary-archive-glob and -binary-name are required",
		},
		{
			name: "missing server binary",
			cfg: func() config {
				cfg := validReleaseConfig()
				cfg.serverBinary = ""
				return cfg
			}(),
			wantErr: "missing -server-binary",
		},
		{
			name: "unsupported backend",
			cfg: func() config {
				cfg := validReleaseConfig()
				cfg.backend = "vm"
				return cfg
			}(),
			wantErr: `unsupported -backend "vm"`,
		},
		{
			name: "conflicting binary inputs",
			cfg: func() config {
				cfg := validReleaseConfig()
				cfg.archiveGlob = "dist/*.tar.gz"
				cfg.binaryName = "minecraft-ping"
				return cfg
			}(),
			wantErr: "choose either -binary or -binary-archive-glob/-binary-name, not both",
		},
		{
			name: "missing archive binary name",
			cfg: func() config {
				cfg := validReleaseConfig()
				cfg.binaryPath = ""
				cfg.archiveGlob = "dist/*.tar.gz"
				return cfg
			}(),
			wantErr: "both -binary-archive-glob and -binary-name are required when -binary is not set",
		},
		{
			name: "invalid probe timeout",
			cfg: func() config {
				cfg := validReleaseConfig()
				cfg.probeTimeout = 0
				return cfg
			}(),
			wantErr: "invalid -probe-timeout",
		},
		{
			name: "invalid java port",
			cfg: func() config {
				cfg := validReleaseConfig()
				cfg.javaIPv4Port = 70000
				return cfg
			}(),
			wantErr: "invalid -java-ipv4-port",
		},
		{
			name: "invalid bedrock port",
			cfg: func() config {
				cfg := validReleaseConfig()
				cfg.bedrockIPv6Port = 0
				return cfg
			}(),
			wantErr: "invalid -bedrock-ipv6-port",
		},
		{
			name: "missing container cli",
			cfg: func() config {
				cfg := validReleaseConfig()
				cfg.backend = "container"
				cfg.serverBinary = ""
				cfg.containerCLI = ""
				cfg.binaryPath = ""
				cfg.archiveGlob = "dist/*.tar.gz"
				cfg.binaryName = "minecraft-ping"
				return cfg
			}(),
			wantErr: "missing -container-cli",
		},
	}

	for _, test := range errorCases {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := validateConfig(test.cfg)
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("validateConfig() error = %v, want substring %q", err, test.wantErr)
			}
		})
	}
}

func TestRunCLIExitCodes(t *testing.T) {
	restore := restoreReleaseHooks()
	defer restore()

	releaseNotifyContext = func(ctx context.Context, _ ...os.Signal) (context.Context, context.CancelFunc) {
		return ctx, func() {}
	}

	t.Run("validation failure", func(t *testing.T) {
		releaseValidateConfig = func(config) error {
			return errors.New("bad config")
		}
		releaseRun = func(context.Context, config) error {
			t.Fatal("run should not be called when validation fails")
			return nil
		}

		if got := runCLI([]string{"-binary", "/tmp/minecraft-ping", "-server-binary", "/tmp/staging"}); got != 2 {
			t.Fatalf("runCLI() = %d, want 2", got)
		}
	})

	t.Run("parse failure", func(t *testing.T) {
		releaseValidateConfig = func(config) error {
			t.Fatal("validateConfig should not run after parse failure")
			return nil
		}
		releaseRun = func(context.Context, config) error {
			t.Fatal("run should not run after parse failure")
			return nil
		}

		if got := runCLI([]string{"-probe-timeout", "not-a-duration"}); got != 2 {
			t.Fatalf("runCLI() = %d, want 2", got)
		}
	})

	t.Run("run failure", func(t *testing.T) {
		releaseValidateConfig = validateConfig
		releaseRun = func(context.Context, config) error {
			return errors.New("run exploded")
		}

		if got := runCLI([]string{"-binary", "/tmp/minecraft-ping", "-server-binary", "/tmp/staging"}); got != 1 {
			t.Fatalf("runCLI() = %d, want 1", got)
		}
	})

	t.Run("success", func(t *testing.T) {
		releaseValidateConfig = validateConfig
		releaseRun = func(context.Context, config) error {
			return nil
		}

		if got := runCLI([]string{"-binary", "/tmp/minecraft-ping", "-server-binary", "/tmp/staging"}); got != 0 {
			t.Fatalf("runCLI() = %d, want 0", got)
		}
	})
}

func TestMainWithArgsUsesProgramNameSeparately(t *testing.T) {
	restore := restoreReleaseHooks()
	defer restore()

	releaseNotifyContext = func(ctx context.Context, _ ...os.Signal) (context.Context, context.CancelFunc) {
		return ctx, func() {}
	}

	var gotCfg config
	releaseValidateConfig = func(cfg config) error {
		gotCfg = cfg
		return nil
	}
	releaseRun = func(context.Context, config) error { return nil }

	rawArgs := []string{
		"release-integration",
		"-probe-timeout", "1250ms",
		"-binary", "/tmp/minecraft-ping",
		"-server-binary", "/tmp/staging",
	}
	if got := mainWithArgs(rawArgs); got != 0 {
		t.Fatalf("mainWithArgs() = %d, want 0", got)
	}
	if gotCfg.probeTimeout != 1250*time.Millisecond {
		t.Fatalf("probeTimeout = %s, want 1250ms", gotCfg.probeTimeout)
	}
	if gotCfg.binaryPath != "/tmp/minecraft-ping" || gotCfg.serverBinary != "/tmp/staging" {
		t.Fatalf("parsed config = %+v", gotCfg)
	}
}

func TestRunCLILogsErrorsAndSuppressesFlagOutput(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		setup    func()
		wantExit int
		wantLog  string
	}{
		{
			name: "parse failure",
			args: []string{"-probe-timeout", "not-a-duration"},
			setup: func() {
				releaseValidateConfig = func(config) error {
					t.Fatal("validateConfig should not run after parse failure")
					return nil
				}
				releaseRun = func(context.Context, config) error {
					t.Fatal("run should not run after parse failure")
					return nil
				}
			},
			wantExit: 2,
			wantLog:  `invalid value "not-a-duration" for flag -probe-timeout`,
		},
		{
			name: "validation failure",
			args: []string{"-binary", "/tmp/minecraft-ping", "-server-binary", "/tmp/staging"},
			setup: func() {
				releaseValidateConfig = func(config) error { return errors.New("bad config") }
				releaseRun = func(context.Context, config) error {
					t.Fatal("run should not run after validation failure")
					return nil
				}
			},
			wantExit: 2,
			wantLog:  "bad config",
		},
		{
			name: "run failure",
			args: []string{"-binary", "/tmp/minecraft-ping", "-server-binary", "/tmp/staging"},
			setup: func() {
				releaseValidateConfig = validateConfig
				releaseRun = func(context.Context, config) error { return errors.New("run exploded") }
			},
			wantExit: 1,
			wantLog:  "run exploded",
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			restore := restoreReleaseHooks()
			defer restore()

			releaseNotifyContext = func(ctx context.Context, _ ...os.Signal) (context.Context, context.CancelFunc) {
				return ctx, func() {}
			}
			logBuf := captureLogOutput(t)
			test.setup()

			var gotExit int
			stdout, stderr := captureStdoutStderr(t, func() {
				gotExit = runCLI(test.args)
			})

			if gotExit != test.wantExit {
				t.Fatalf("runCLI() = %d, want %d", gotExit, test.wantExit)
			}
			if stdout != "" {
				t.Fatalf("stdout = %q, want empty", stdout)
			}
			if stderr != "" {
				t.Fatalf("stderr = %q, want empty", stderr)
			}
			if !strings.Contains(logBuf.String(), test.wantLog) {
				t.Fatalf("log output = %q, want substring %q", logBuf.String(), test.wantLog)
			}
		})
	}
}

func TestRunPropagatesStepErrors(t *testing.T) {
	baseArchiveCfg := func() config {
		cfg := validReleaseConfig()
		cfg.binaryPath = ""
		cfg.archiveGlob = "dist/*.tar.gz"
		cfg.binaryName = "minecraft-ping"
		cfg.probeTimeout = time.Second
		return cfg
	}

	tests := []struct {
		name        string
		cfg         config
		setup       func(t *testing.T, cleanupCalled *bool)
		wantErr     string
		wantCleanup bool
	}{
		{
			name:    "resolve archive",
			cfg:     baseArchiveCfg(),
			wantErr: "resolve exploded",
			setup: func(t *testing.T, cleanupCalled *bool) {
				releaseResolveSingleFile = func(string) (string, error) {
					return "", errors.New("resolve exploded")
				}
			},
		},
		{
			name:    "derive version",
			cfg:     baseArchiveCfg(),
			wantErr: "version exploded",
			setup: func(t *testing.T, cleanupCalled *bool) {
				releaseResolveSingleFile = func(string) (string, error) { return "dist/minecraft-ping_2.0.3_Linux_amd64.tar.gz", nil }
				releaseVersionFromArchive = func(string) (string, error) {
					return "", errors.New("version exploded")
				}
			},
		},
		{
			name:    "extract binary",
			cfg:     baseArchiveCfg(),
			wantErr: "extract exploded",
			setup: func(t *testing.T, cleanupCalled *bool) {
				releaseResolveSingleFile = func(string) (string, error) { return "dist/minecraft-ping_2.0.3_Linux_amd64.tar.gz", nil }
				releaseVersionFromArchive = func(string) (string, error) { return "2.0.3", nil }
				releaseExtractBinary = func(string, string) (string, func(), error) {
					return "", nil, errors.New("extract exploded")
				}
			},
		},
		{
			name: "assert version",
			cfg: func() config {
				cfg := validReleaseConfig()
				cfg.expectedVersion = "2.0.3"
				return cfg
			}(),
			wantErr:     "version check exploded",
			wantCleanup: false,
			setup: func(t *testing.T, cleanupCalled *bool) {
				releaseAssertVersion = func(context.Context, string, string) error {
					return errors.New("version check exploded")
				}
			},
		},
		{
			name:        "start backend",
			cfg:         validReleaseConfig(),
			wantErr:     "backend exploded",
			wantCleanup: false,
			setup: func(t *testing.T, cleanupCalled *bool) {
				releaseStartBackend = func(context.Context, config, *[]func()) error {
					return errors.New("backend exploded")
				}
			},
		},
		{
			name:        "probe",
			cfg:         validReleaseConfig(),
			wantErr:     "java-ipv4 probe failed: probe exploded",
			wantCleanup: false,
			setup: func(t *testing.T, cleanupCalled *bool) {
				releaseStartBackend = func(context.Context, config, *[]func()) error { return nil }
				releaseRunProbe = func(context.Context, string, time.Duration, probeSpec) (pingResult, error) {
					return pingResult{}, errors.New("probe exploded")
				}
			},
		},
		{
			name:        "archive cleanup on later failure",
			cfg:         baseArchiveCfg(),
			wantErr:     "backend exploded",
			wantCleanup: true,
			setup: func(t *testing.T, cleanupCalled *bool) {
				releaseResolveSingleFile = func(string) (string, error) { return "dist/minecraft-ping_2.0.3_Linux_amd64.tar.gz", nil }
				releaseVersionFromArchive = func(string) (string, error) { return "2.0.3", nil }
				releaseExtractBinary = func(string, string) (string, func(), error) {
					return "/tmp/minecraft-ping", func() { *cleanupCalled = true }, nil
				}
				releaseAssertVersion = func(context.Context, string, string) error { return nil }
				releaseStartBackend = func(context.Context, config, *[]func()) error {
					return errors.New("backend exploded")
				}
			},
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			restore := restoreReleaseHooks()
			defer restore()

			cleanupCalled := false
			releaseResolveSingleFile = resolveSingleFile
			releaseVersionFromArchive = versionFromArchiveName
			releaseExtractBinary = extractBinary
			releaseAssertVersion = assertVersion
			releaseStartBackend = func(context.Context, config, *[]func()) error { return nil }
			releaseRunProbe = func(_ context.Context, _ string, _ time.Duration, spec probeSpec) (pingResult, error) {
				return pingResult{Server: spec.host, LatencyMS: 1}, nil
			}

			test.setup(t, &cleanupCalled)

			err := run(context.Background(), test.cfg)
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("run() error = %v, want substring %q", err, test.wantErr)
			}
			if cleanupCalled != test.wantCleanup {
				t.Fatalf("cleanupCalled = %t, want %t", cleanupCalled, test.wantCleanup)
			}
		})
	}
}

func TestRunUsesDerivedVersionAndExtractedBinary(t *testing.T) {
	restore := restoreReleaseHooks()
	defer restore()

	cfg := validReleaseConfig()
	cfg.binaryPath = ""
	cfg.archiveGlob = "dist/*.tar.gz"
	cfg.binaryName = "minecraft-ping"
	cfg.probeTimeout = 1250 * time.Millisecond

	releaseResolveSingleFile = func(string) (string, error) {
		return "dist/minecraft-ping_2.0.3_Linux_amd64.tar.gz", nil
	}
	releaseVersionFromArchive = func(string) (string, error) {
		return "2.0.3", nil
	}
	releaseExtractBinary = func(string, string) (string, func(), error) {
		return "/tmp/extracted-minecraft-ping", func() {}, nil
	}
	assertVersionCalled := false
	releaseAssertVersion = func(_ context.Context, binaryPath, expectedVersion string) error {
		assertVersionCalled = true
		if binaryPath != "/tmp/extracted-minecraft-ping" {
			t.Fatalf("assertVersion() binaryPath = %q", binaryPath)
		}
		if expectedVersion != "2.0.3" {
			t.Fatalf("assertVersion() expectedVersion = %q", expectedVersion)
		}
		return nil
	}
	releaseStartBackend = func(context.Context, config, *[]func()) error { return nil }
	probeCalls := 0
	releaseRunProbe = func(_ context.Context, binaryPath string, timeout time.Duration, spec probeSpec) (pingResult, error) {
		probeCalls++
		if binaryPath != "/tmp/extracted-minecraft-ping" {
			t.Fatalf("runProbe() binaryPath = %q", binaryPath)
		}
		if timeout != 1250*time.Millisecond {
			t.Fatalf("runProbe() timeout = %s", timeout)
		}
		return pingResult{LatencyMS: 7, Server: spec.host}, nil
	}

	if err := run(context.Background(), cfg); err != nil {
		t.Fatalf("run() error = %v", err)
	}
	if !assertVersionCalled {
		t.Fatal("assertVersion() was not called")
	}
	if want := len(probeSpecs(cfg)); probeCalls != want {
		t.Fatalf("probeCalls = %d, want %d", probeCalls, want)
	}
}

func TestResolveSingleFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	one := filepath.Join(dir, "one.tar.gz")
	two := filepath.Join(dir, "two.tar.gz")
	if err := os.WriteFile(one, []byte("one"), 0o600); err != nil {
		t.Fatalf("WriteFile(one) error = %v", err)
	}
	if err := os.WriteFile(two, []byte("two"), 0o600); err != nil {
		t.Fatalf("WriteFile(two) error = %v", err)
	}

	got, err := resolveSingleFile(filepath.Join(dir, "one*.tar.gz"))
	if err != nil {
		t.Fatalf("resolveSingleFile() error = %v", err)
	}
	if got != one {
		t.Fatalf("resolveSingleFile() = %q, want %q", got, one)
	}

	if _, err := resolveSingleFile(filepath.Join(dir, "none*.tar.gz")); err == nil {
		t.Fatal("resolveSingleFile() succeeded for empty glob")
	}
	if _, err := resolveSingleFile(filepath.Join(dir, "*.tar.gz")); err == nil {
		t.Fatal("resolveSingleFile() succeeded for multi-match glob")
	}
}

func TestResolveSingleFileRejectsInvalidGlob(t *testing.T) {
	t.Parallel()

	if _, err := resolveSingleFile("["); err == nil || !strings.Contains(err.Error(), `invalid glob "["`) {
		t.Fatalf("resolveSingleFile() error = %v, want invalid-glob error", err)
	}
}

func TestExtractZipBinary(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	archivePath := filepath.Join(dir, "minecraft-ping.zip")
	createZipArchive(t, archivePath, map[string][]byte{
		"docs/readme.txt":  []byte("ignore"),
		"minecraft-ping":   []byte("zip-binary"),
		"minecraft-ping.1": []byte("ignore"),
	})

	extracted, err := extractZipBinary(archivePath, dir, "minecraft-ping")
	if err != nil {
		t.Fatalf("extractZipBinary() error = %v", err)
	}
	if extracted != filepath.Join(dir, "minecraft-ping") {
		t.Fatalf("extractZipBinary() path = %q", extracted)
	}

	data, err := os.ReadFile(extracted)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(data) != "zip-binary" {
		t.Fatalf("extracted data = %q", data)
	}
}

func TestExtractTarGzBinary(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	archivePath := filepath.Join(dir, "minecraft-ping.tar.gz")
	createTarGzArchive(t, archivePath, map[string][]byte{
		"README.md":        []byte("ignore"),
		"minecraft-ping":   []byte("tar-binary"),
		"minecraft-ping.1": []byte("ignore"),
	})

	extracted, err := extractTarGzBinary(archivePath, dir, "minecraft-ping")
	if err != nil {
		t.Fatalf("extractTarGzBinary() error = %v", err)
	}
	data, err := os.ReadFile(extracted)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(data) != "tar-binary" {
		t.Fatalf("extracted data = %q", data)
	}
}

func TestExtractZipBinaryRejectsNestedBinaryPath(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	archivePath := filepath.Join(dir, "minecraft-ping.zip")
	createZipArchive(t, archivePath, map[string][]byte{
		"nested/minecraft-ping": []byte("zip-binary"),
	})

	if _, err := extractZipBinary(archivePath, dir, "minecraft-ping"); err == nil {
		t.Fatal("extractZipBinary() succeeded, want nested-path rejection")
	}
}

func TestExtractTarGzBinaryRejectsNestedBinaryPath(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	archivePath := filepath.Join(dir, "minecraft-ping.tar.gz")
	createTarGzArchive(t, archivePath, map[string][]byte{
		"bin/minecraft-ping": []byte("tar-binary"),
	})

	if _, err := extractTarGzBinary(archivePath, dir, "minecraft-ping"); err == nil {
		t.Fatal("extractTarGzBinary() succeeded, want nested-path rejection")
	}
}

func TestOpenReadOnlyFileAndCopyWithLimit(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "payload.txt")
	if err := os.WriteFile(path, []byte("payload"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	file, err := openReadOnlyFile(path)
	if err != nil {
		t.Fatalf("openReadOnlyFile() error = %v", err)
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if string(data) != "payload" {
		t.Fatalf("data = %q", data)
	}

	var buf bytes.Buffer
	if err := copyWithLimit(&buf, strings.NewReader("abc"), 3); err != nil {
		t.Fatalf("copyWithLimit() error = %v", err)
	}
	if buf.String() != "abc" {
		t.Fatalf("copied = %q", buf.String())
	}
	if err := copyWithLimit(io.Discard, strings.NewReader("abcd"), 3); err == nil {
		t.Fatal("copyWithLimit() succeeded, want limit error")
	}
}

func TestRunCleanupExecutesInReverseOrder(t *testing.T) {
	var order []string
	runCleanup([]func(){
		func() { order = append(order, "first") },
		func() { order = append(order, "second") },
		func() { order = append(order, "third") },
	})
	if got, want := strings.Join(order, ","), "third,second,first"; got != want {
		t.Fatalf("cleanup order = %q, want %q", got, want)
	}
}

func TestWaitForListenerUsesConfiguredTimeouts(t *testing.T) {
	restore := restoreReleaseTimeHooks()
	defer restore()

	start := time.Unix(1_700_000_000, 0)
	const (
		wantReadyTimeout  = 2 * time.Minute
		wantProbeTimeout  = 2 * time.Second
		wantRetryInterval = 500 * time.Millisecond
	)
	times := []time.Time{
		start,
		start.Add(30 * time.Second),
		start.Add(90 * time.Second),
		start.Add(wantReadyTimeout + time.Second),
	}
	releaseTimeNow = func() time.Time {
		if len(times) == 0 {
			return start.Add(wantReadyTimeout + 2*time.Second)
		}
		now := times[0]
		times = times[1:]
		return now
	}

	var sleepDuration time.Duration
	releaseTimeAfter = func(d time.Duration) <-chan time.Time {
		sleepDuration = d
		ch := make(chan time.Time)
		close(ch)
		return ch
	}

	var (
		calls      int
		gotNetwork string
		gotHost    string
		gotPort    int
		gotTimeout time.Duration
	)
	err := waitForListener(context.Background(), "java", func(network, host string, port int, timeout time.Duration) error {
		calls++
		gotNetwork = network
		gotHost = host
		gotPort = port
		gotTimeout = timeout
		return errors.New("still starting")
	}, "tcp4", "127.0.0.1", 25565)
	if err == nil || !strings.Contains(err.Error(), "timed out waiting for java tcp4 listener on 127.0.0.1:25565") {
		t.Fatalf("waitForListener() error = %v, want timeout", err)
	}
	if calls != 2 {
		t.Fatalf("probe calls = %d, want 2", calls)
	}
	if gotNetwork != "tcp4" || gotHost != "127.0.0.1" || gotPort != 25565 {
		t.Fatalf("probe args = %q %q %d", gotNetwork, gotHost, gotPort)
	}
	if gotTimeout != wantProbeTimeout {
		t.Fatalf("probe timeout = %s, want %s", gotTimeout, wantProbeTimeout)
	}
	if sleepDuration != wantRetryInterval {
		t.Fatalf("retry interval = %s, want %s", sleepDuration, wantRetryInterval)
	}
}

func TestWaitForListenerHonorsContextCancel(t *testing.T) {
	restore := restoreReleaseTimeHooks()
	defer restore()

	releaseTimeNow = func() time.Time { return time.Unix(1_700_000_000, 0) }
	releaseTimeAfter = func(time.Duration) <-chan time.Time {
		return make(chan time.Time)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	calls := 0
	err := waitForListener(ctx, "java", func(network, host string, port int, timeout time.Duration) error {
		calls++
		return errors.New("still starting")
	}, "tcp4", "127.0.0.1", 25565)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("waitForListener() error = %v, want context cancellation", err)
	}
	if calls != 1 {
		t.Fatalf("probe calls = %d, want 1", calls)
	}
}

func TestSetUDPRelayDeadlineUsesConfiguredTimeout(t *testing.T) {
	restore := restoreReleaseTimeHooks()
	defer restore()

	base := time.Unix(1_700_000_000, 250_000_000)
	releaseTimeNow = func() time.Time { return base }

	setter := &stubDeadlineSetter{}
	if err := setUDPRelayDeadline(setter); err != nil {
		t.Fatalf("setUDPRelayDeadline() error = %v", err)
	}
	if want := base.Add(5 * time.Second); !setter.deadline.Equal(want) {
		t.Fatalf("deadline = %s, want %s", setter.deadline, want)
	}
}

func TestAssertVersion(t *testing.T) {
	previous := execCommandContext
	defer func() {
		execCommandContext = previous
	}()

	execCommandContext = helperExecCommandContext("version-ok")
	if err := assertVersion(context.Background(), "/tmp/minecraft-ping", "2.0.3"); err != nil {
		t.Fatalf("assertVersion() error = %v", err)
	}

	execCommandContext = helperExecCommandContext("version-bad")
	if err := assertVersion(context.Background(), "/tmp/minecraft-ping", "2.0.3"); err == nil {
		t.Fatal("assertVersion() succeeded, want mismatch error")
	}

	execCommandContext = helperExecCommandContext("version-fail")
	if err := assertVersion(context.Background(), "/tmp/minecraft-ping", "2.0.3"); err == nil || !strings.Contains(err.Error(), "version exploded") {
		t.Fatalf("assertVersion() error = %v, want wrapped command failure", err)
	}
}

func TestAssertVersionUsesThirtySecondTimeout(t *testing.T) {
	restore := restoreReleaseHooks()
	defer restore()

	var remaining time.Duration
	execCommandContext = func(ctx context.Context, command string, args ...string) *exec.Cmd {
		deadline, ok := ctx.Deadline()
		if !ok {
			t.Fatal("assertVersion() command context is missing a deadline")
		}
		remaining = time.Until(deadline)
		return helperExecCommandContext("version-ok")(ctx, command, args...)
	}

	if err := assertVersion(context.Background(), "/tmp/minecraft-ping", "2.0.3"); err != nil {
		t.Fatalf("assertVersion() error = %v", err)
	}
	if remaining < 29*time.Second+500*time.Millisecond || remaining > 30*time.Second+500*time.Millisecond {
		t.Fatalf("remaining timeout = %s, want about 30s", remaining)
	}
}

func TestStartBinaryBackendErrorPaths(t *testing.T) {
	t.Run("start failure", func(t *testing.T) {
		restore := restoreReleaseHooks()
		defer restore()

		execCommandContext = func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
			return exec.CommandContext(ctx, "/path/that/does/not/exist")
		}

		err := startBinaryBackend(context.Background(), validReleaseConfig(), &[]func(){})
		if err == nil || !strings.Contains(err.Error(), "start staging backend") {
			t.Fatalf("startBinaryBackend() error = %v, want wrapped start failure", err)
		}
	})

	t.Run("listener failures", func(t *testing.T) {
		tests := []struct {
			name    string
			setup   func()
			wantErr string
		}{
			{
				name: "java ipv4",
				setup: func() {
					var calls int
					releaseWaitForJava = func(context.Context, string, string, int) error {
						calls++
						if calls == 1 {
							return errors.New("java ipv4 exploded")
						}
						return nil
					}
				},
				wantErr: "java ipv4 exploded",
			},
			{
				name: "java ipv6",
				setup: func() {
					var calls int
					releaseWaitForJava = func(context.Context, string, string, int) error {
						calls++
						if calls == 2 {
							return errors.New("java ipv6 exploded")
						}
						return nil
					}
				},
				wantErr: "java ipv6 exploded",
			},
			{
				name: "bedrock ipv4",
				setup: func() {
					releaseWaitForJava = func(context.Context, string, string, int) error { return nil }
					var calls int
					releaseWaitForBedrock = func(context.Context, string, string, int) error {
						calls++
						if calls == 1 {
							return errors.New("bedrock ipv4 exploded")
						}
						return nil
					}
				},
				wantErr: "bedrock ipv4 exploded",
			},
			{
				name: "bedrock ipv6",
				setup: func() {
					releaseWaitForJava = func(context.Context, string, string, int) error { return nil }
					var calls int
					releaseWaitForBedrock = func(context.Context, string, string, int) error {
						calls++
						if calls == 2 {
							return errors.New("bedrock ipv6 exploded")
						}
						return nil
					}
				},
				wantErr: "bedrock ipv6 exploded",
			},
		}

		for _, test := range tests {
			test := test
			t.Run(test.name, func(t *testing.T) {
				restore := restoreReleaseHooks()
				defer restore()

				releaseWaitForJava = func(context.Context, string, string, int) error { return nil }
				releaseWaitForBedrock = func(context.Context, string, string, int) error { return nil }
				execCommandContext = helperExecCommandContext("exit-ok")
				releaseTimeAfter = func(time.Duration) <-chan time.Time {
					ch := make(chan time.Time)
					close(ch)
					return ch
				}

				test.setup()

				cleanup := make([]func(), 0, 1)
				err := startBinaryBackend(context.Background(), validReleaseConfig(), &cleanup)
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("startBinaryBackend() error = %v, want substring %q", err, test.wantErr)
				}
				runCleanup(cleanup)
			})
		}
	})
}

func TestStartBinaryBackendCleanupKillsProcess(t *testing.T) {
	restore := restoreReleaseHooks()
	defer restore()

	releaseWaitForJava = func(context.Context, string, string, int) error { return nil }
	releaseWaitForBedrock = func(context.Context, string, string, int) error { return nil }
	releaseTimeAfter = func(time.Duration) <-chan time.Time {
		ch := make(chan time.Time)
		close(ch)
		return ch
	}

	var startedCmd *exec.Cmd
	execCommandContext = func(ctx context.Context, command string, args ...string) *exec.Cmd {
		startedCmd = helperExecCommandContext("hold")(ctx, command, args...)
		return startedCmd
	}

	cleanup := make([]func(), 0, 1)
	if err := startBinaryBackend(context.Background(), validReleaseConfig(), &cleanup); err != nil {
		t.Fatalf("startBinaryBackend() error = %v", err)
	}
	if len(cleanup) == 0 {
		t.Fatal("cleanup list is empty, want process cleanup")
	}

	runCleanup(cleanup)

	if startedCmd == nil || startedCmd.Process == nil {
		t.Fatal("started process is nil")
	}
	waitDone := make(chan error, 1)
	go func() {
		_, err := startedCmd.Process.Wait()
		waitDone <- err
	}()
	select {
	case err := <-waitDone:
		if err == nil || !strings.Contains(err.Error(), "no child processes") {
			t.Fatalf("Process.Wait() error = %v, want no child processes after cleanup reaps the backend", err)
		}
	case <-time.After(2 * time.Second):
		_ = startedCmd.Process.Kill()
		t.Fatal("cleanup left backend process running")
	}
}

func TestRunProbe(t *testing.T) {
	previous := execCommandContext
	defer func() {
		execCommandContext = previous
	}()

	execCommandContext = helperExecCommandContext("probe-ok")
	result, err := runProbe(context.Background(), "/tmp/minecraft-ping", 1500*time.Millisecond, probeSpec{
		label:      "bedrock-ipv6",
		editionArg: "--bedrock",
		familyFlag: "-6",
		host:       "::1",
		port:       19133,
	})
	if err != nil {
		t.Fatalf("runProbe() error = %v", err)
	}
	if result.Server != "::1" || result.LatencyMS != 12 {
		t.Fatalf("result = %+v", result)
	}

	execCommandContext = helperExecCommandContext("probe-bad-json")
	if _, err := runProbe(context.Background(), "/tmp/minecraft-ping", time.Second, probeSpec{
		label:      "java-ipv4",
		familyFlag: "-4",
		host:       "127.0.0.1",
		port:       25565,
	}); err == nil {
		t.Fatal("runProbe() succeeded, want decode error")
	}

	execCommandContext = helperExecCommandContext("probe-fail")
	if _, err := runProbe(context.Background(), "/tmp/minecraft-ping", time.Second, probeSpec{
		label:      "java-ipv4",
		familyFlag: "-4",
		host:       "127.0.0.1",
		port:       25565,
	}); err == nil || !strings.Contains(err.Error(), "probe exploded") {
		t.Fatalf("runProbe() error = %v, want wrapped command failure", err)
	}
}

func TestRunProbeUsesConfiguredTimeoutAndArguments(t *testing.T) {
	restore := restoreReleaseHooks()
	defer restore()

	var (
		gotRemaining time.Duration
		gotArgs      []string
	)
	execCommandContext = func(ctx context.Context, command string, args ...string) *exec.Cmd {
		deadline, ok := ctx.Deadline()
		if !ok {
			t.Fatal("runProbe() command context is missing a deadline")
		}
		gotRemaining = time.Until(deadline)
		gotArgs = append([]string(nil), args...)
		return helperExecCommandContext("probe-json")(ctx, command, args...)
	}

	result, err := runProbe(context.Background(), "/tmp/minecraft-ping", 1250*time.Millisecond, probeSpec{
		label:      "java-ipv4",
		familyFlag: "-4",
		host:       "127.0.0.1",
		port:       25565,
	})
	if err != nil {
		t.Fatalf("runProbe() error = %v", err)
	}
	if result.Server != "127.0.0.1" || result.LatencyMS != 7 {
		t.Fatalf("result = %+v", result)
	}
	if gotRemaining < 119*time.Second || gotRemaining > 121*time.Second {
		t.Fatalf("remaining timeout = %s, want about 2m", gotRemaining)
	}
	wantArgs := []string{"-j", "--allow-private", "-W", "1.25", "-4", "127.0.0.1:25565"}
	if strings.Join(gotArgs, "\x00") != strings.Join(wantArgs, "\x00") {
		t.Fatalf("args = %v, want %v", gotArgs, wantArgs)
	}
}

func TestFormatProbeTimeout(t *testing.T) {
	t.Parallel()

	tests := []struct {
		timeout time.Duration
		want    string
	}{
		{timeout: time.Second, want: "1"},
		{timeout: 1234500 * time.Microsecond, want: "1.234"},
		{timeout: 1234 * time.Millisecond, want: "1.234"},
		{timeout: 1250 * time.Millisecond, want: "1.25"},
		{timeout: 1500 * time.Millisecond, want: "1.5"},
		{timeout: 250 * time.Millisecond, want: "0.25"},
	}

	for _, test := range tests {
		test := test
		t.Run(test.want, func(t *testing.T) {
			t.Parallel()
			if got := formatProbeTimeout(test.timeout); got != test.want {
				t.Fatalf("formatProbeTimeout(%s) = %q, want %q", test.timeout, got, test.want)
			}
		})
	}
}

func testWaitForWrapperUsesConfiguredTimeouts(
	t *testing.T,
	wait func(context.Context, string, string, int) error,
	setProbe func(listenerProbeFunc) func(),
	network, host string,
	port int,
) {
	t.Helper()

	var (
		calls       int
		gotRetry    time.Duration
		gotTimeouts []time.Duration
	)
	restoreProbe := setProbe(func(network, host string, port int, timeout time.Duration) error {
		gotTimeouts = append(gotTimeouts, timeout)
		calls++
		if calls == 1 {
			return errors.New("not ready")
		}
		return nil
	})
	defer restoreProbe()

	previousAfter := releaseTimeAfter
	defer func() {
		releaseTimeAfter = previousAfter
	}()

	releaseTimeAfter = func(d time.Duration) <-chan time.Time {
		gotRetry = d
		ch := make(chan time.Time, 1)
		ch <- time.Unix(0, 0)
		return ch
	}

	if err := wait(context.Background(), network, host, port); err != nil {
		t.Fatalf("wait() error = %v", err)
	}
	if calls != 2 {
		t.Fatalf("wait() probe calls = %d, want 2", calls)
	}
	if gotRetry != listenerRetryInterval {
		t.Fatalf("wait() retry interval = %v, want %v", gotRetry, listenerRetryInterval)
	}
	for i, got := range gotTimeouts {
		if got != listenerProbeTimeout {
			t.Fatalf("wait() probe timeout[%d] = %v, want %v", i, got, listenerProbeTimeout)
		}
	}
}

func TestWaitForJavaUsesConfiguredTimeouts(t *testing.T) {
	testWaitForWrapperUsesConfiguredTimeouts(
		t,
		waitForJava,
		func(probe listenerProbeFunc) func() {
			previous := javaListenerProbe
			javaListenerProbe = probe
			return func() {
				javaListenerProbe = previous
			}
		},
		"tcp4",
		"127.0.0.1",
		25565,
	)
}

func TestWaitForBedrockUsesConfiguredTimeouts(t *testing.T) {
	testWaitForWrapperUsesConfiguredTimeouts(
		t,
		waitForBedrock,
		func(probe listenerProbeFunc) func() {
			previous := bedrockListenerProbe
			bedrockListenerProbe = probe
			return func() {
				bedrockListenerProbe = previous
			}
		},
		"udp4",
		"127.0.0.1",
		19132,
	)
}

func TestRemoveContainerAndStartContainer(t *testing.T) {
	previous := execCommandContext
	defer func() {
		execCommandContext = previous
	}()

	execCommandContext = helperExecCommandContext("container-not-found")
	if err := removeContainer(context.Background(), "docker", "test-container"); err != nil {
		t.Fatalf("removeContainer() error = %v", err)
	}

	execCommandContext = helperExecCommandContext("container-start")
	if err := startContainer(context.Background(), config{
		containerCLI:    "docker",
		containerName:   "test-container",
		imageTag:        "minecraft-staging-image:ci",
		javaIPv4Port:    45565,
		bedrockIPv4Port: 49132,
	}); err != nil {
		t.Fatalf("startContainer() error = %v", err)
	}
}

func TestStartContainerBackendErrorPaths(t *testing.T) {
	tests := []struct {
		name    string
		setup   func()
		wantErr string
	}{
		{
			name: "load image",
			setup: func() {
				releaseLoadImage = func(context.Context, string, string) error {
					return errors.New("load exploded")
				}
			},
			wantErr: "load exploded",
		},
		{
			name: "remove container",
			setup: func() {
				releaseLoadImage = func(context.Context, string, string) error { return nil }
				releaseRemoveContainer = func(context.Context, string, string) error {
					return errors.New("remove exploded")
				}
			},
			wantErr: "remove exploded",
		},
		{
			name: "start container",
			setup: func() {
				releaseLoadImage = func(context.Context, string, string) error { return nil }
				releaseRemoveContainer = func(context.Context, string, string) error { return nil }
				releaseStartContainer = func(context.Context, config) error {
					return errors.New("start exploded")
				}
			},
			wantErr: "start exploded",
		},
		{
			name: "java relay",
			setup: func() {
				releaseLoadImage = func(context.Context, string, string) error { return nil }
				releaseRemoveContainer = func(context.Context, string, string) error { return nil }
				releaseStartContainer = func(context.Context, config) error { return nil }
				releaseNewIPv6Relay = func(string, int, string, int) (*ipv6Relay, error) {
					return nil, errors.New("java relay exploded")
				}
			},
			wantErr: "java relay exploded",
		},
		{
			name: "bedrock relay",
			setup: func() {
				releaseLoadImage = func(context.Context, string, string) error { return nil }
				releaseRemoveContainer = func(context.Context, string, string) error { return nil }
				releaseStartContainer = func(context.Context, config) error { return nil }
				releaseNewIPv6Relay = func(string, int, string, int) (*ipv6Relay, error) {
					return &ipv6Relay{listener: stubListener{}}, nil
				}
				releaseNewUDPIPv6Relay = func(string, int, string, int) (*udpIPv6Relay, error) {
					return nil, errors.New("bedrock relay exploded")
				}
			},
			wantErr: "bedrock relay exploded",
		},
		{
			name: "java wait ipv4",
			setup: func() {
				releaseLoadImage = func(context.Context, string, string) error { return nil }
				releaseRemoveContainer = func(context.Context, string, string) error { return nil }
				releaseStartContainer = func(context.Context, config) error { return nil }
				releaseNewIPv6Relay = func(string, int, string, int) (*ipv6Relay, error) {
					return &ipv6Relay{listener: stubListener{}}, nil
				}
				releaseNewUDPIPv6Relay = func(string, int, string, int) (*udpIPv6Relay, error) {
					return &udpIPv6Relay{conn: stubPacketConn{}}, nil
				}
				var calls int
				releaseWaitForJava = func(context.Context, string, string, int) error {
					calls++
					if calls == 1 {
						return errors.New("java ipv4 wait exploded")
					}
					return nil
				}
			},
			wantErr: "java ipv4 wait exploded",
		},
		{
			name: "java wait ipv6",
			setup: func() {
				releaseLoadImage = func(context.Context, string, string) error { return nil }
				releaseRemoveContainer = func(context.Context, string, string) error { return nil }
				releaseStartContainer = func(context.Context, config) error { return nil }
				releaseNewIPv6Relay = func(string, int, string, int) (*ipv6Relay, error) {
					return &ipv6Relay{listener: stubListener{}}, nil
				}
				releaseNewUDPIPv6Relay = func(string, int, string, int) (*udpIPv6Relay, error) {
					return &udpIPv6Relay{conn: stubPacketConn{}}, nil
				}
				var calls int
				releaseWaitForJava = func(context.Context, string, string, int) error {
					calls++
					if calls == 2 {
						return errors.New("java ipv6 wait exploded")
					}
					return nil
				}
			},
			wantErr: "java ipv6 wait exploded",
		},
		{
			name: "bedrock wait ipv4",
			setup: func() {
				releaseLoadImage = func(context.Context, string, string) error { return nil }
				releaseRemoveContainer = func(context.Context, string, string) error { return nil }
				releaseStartContainer = func(context.Context, config) error { return nil }
				releaseNewIPv6Relay = func(string, int, string, int) (*ipv6Relay, error) {
					return &ipv6Relay{listener: stubListener{}}, nil
				}
				releaseNewUDPIPv6Relay = func(string, int, string, int) (*udpIPv6Relay, error) {
					return &udpIPv6Relay{conn: stubPacketConn{}}, nil
				}
				releaseWaitForJava = func(context.Context, string, string, int) error { return nil }
				var calls int
				releaseWaitForBedrock = func(context.Context, string, string, int) error {
					calls++
					if calls == 1 {
						return errors.New("bedrock ipv4 wait exploded")
					}
					return nil
				}
			},
			wantErr: "bedrock ipv4 wait exploded",
		},
		{
			name: "bedrock wait ipv6",
			setup: func() {
				releaseLoadImage = func(context.Context, string, string) error { return nil }
				releaseRemoveContainer = func(context.Context, string, string) error { return nil }
				releaseStartContainer = func(context.Context, config) error { return nil }
				releaseNewIPv6Relay = func(string, int, string, int) (*ipv6Relay, error) {
					return &ipv6Relay{listener: stubListener{}}, nil
				}
				releaseNewUDPIPv6Relay = func(string, int, string, int) (*udpIPv6Relay, error) {
					return &udpIPv6Relay{conn: stubPacketConn{}}, nil
				}
				releaseWaitForJava = func(context.Context, string, string, int) error { return nil }
				var calls int
				releaseWaitForBedrock = func(context.Context, string, string, int) error {
					calls++
					if calls == 2 {
						return errors.New("bedrock ipv6 wait exploded")
					}
					return nil
				}
			},
			wantErr: "bedrock ipv6 wait exploded",
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			restore := restoreReleaseHooks()
			defer restore()

			releaseLoadImage = func(context.Context, string, string) error { return nil }
			releaseRemoveContainer = func(context.Context, string, string) error { return nil }
			releaseStartContainer = func(context.Context, config) error { return nil }
			releaseNewIPv6Relay = func(string, int, string, int) (*ipv6Relay, error) {
				return &ipv6Relay{listener: stubListener{}}, nil
			}
			releaseNewUDPIPv6Relay = func(string, int, string, int) (*udpIPv6Relay, error) {
				return &udpIPv6Relay{conn: stubPacketConn{}}, nil
			}
			releaseWaitForJava = func(context.Context, string, string, int) error { return nil }
			releaseWaitForBedrock = func(context.Context, string, string, int) error { return nil }

			test.setup()

			cleanup := make([]func(), 0, 4)
			err := startContainerBackend(context.Background(), validReleaseConfig(), &cleanup)
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("startContainerBackend() error = %v, want substring %q", err, test.wantErr)
			}
			runCleanup(cleanup)
		})
	}
}

func TestStartContainerBackendRegistersCleanup(t *testing.T) {
	restore := restoreReleaseHooks()
	defer restore()

	listener := &scriptedListener{}
	packetConn := &scriptedPacketConn{}
	removeCalls := 0
	releaseLoadImage = func(context.Context, string, string) error { return nil }
	releaseRemoveContainer = func(context.Context, string, string) error {
		removeCalls++
		return nil
	}
	releaseStartContainer = func(context.Context, config) error { return nil }
	releaseNewIPv6Relay = func(string, int, string, int) (*ipv6Relay, error) {
		return &ipv6Relay{listener: listener}, nil
	}
	releaseNewUDPIPv6Relay = func(string, int, string, int) (*udpIPv6Relay, error) {
		return &udpIPv6Relay{conn: packetConn}, nil
	}
	releaseWaitForJava = func(context.Context, string, string, int) error { return nil }
	releaseWaitForBedrock = func(context.Context, string, string, int) error { return nil }

	cleanup := make([]func(), 0, 4)
	if err := startContainerBackend(context.Background(), validReleaseConfig(), &cleanup); err != nil {
		t.Fatalf("startContainerBackend() error = %v", err)
	}
	if len(cleanup) != 3 {
		t.Fatalf("cleanup length = %d, want 3", len(cleanup))
	}

	runCleanup(cleanup)

	if removeCalls != 2 {
		t.Fatalf("removeContainer calls = %d, want 2", removeCalls)
	}
	if listener.closeCalls != 1 {
		t.Fatalf("listener close calls = %d, want 1", listener.closeCalls)
	}
	if packetConn.closeCalls != 1 {
		t.Fatalf("packet close calls = %d, want 1", packetConn.closeCalls)
	}
}

func TestLoadImage(t *testing.T) {
	previous := execCommandContext
	defer func() {
		execCommandContext = previous
	}()
	execCommandContext = helperExecCommandContext("load-image")

	dir := t.TempDir()
	archivePath := filepath.Join(dir, "image.tar.gz")
	file, err := os.Create(archivePath)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	gzipWriter := gzip.NewWriter(file)
	if _, err := gzipWriter.Write([]byte("image-bytes")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatalf("Close() gzip error = %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("Close() file error = %v", err)
	}

	if err := loadImage(context.Background(), "docker", archivePath); err != nil {
		t.Fatalf("loadImage() error = %v", err)
	}
}

func TestLoadImageErrorPaths(t *testing.T) {
	t.Run("empty archive path", func(t *testing.T) {
		restore := restoreReleaseHooks()
		defer restore()

		openedArchive := false
		releaseOpenReadOnlyArchive = func(string) (*os.File, error) {
			openedArchive = true
			return nil, errors.New("open archive exploded")
		}

		startedCommand := false
		execCommandContext = func(ctx context.Context, command string, args ...string) *exec.Cmd {
			startedCommand = true
			return helperExecCommandContext("load-image")(ctx, command, args...)
		}

		if err := loadImage(context.Background(), "docker", ""); err != nil {
			t.Fatalf("loadImage() error = %v", err)
		}
		if openedArchive {
			t.Fatal("loadImage() opened archive for empty path")
		}
		if startedCommand {
			t.Fatal("loadImage() started container CLI for empty path")
		}
	})

	t.Run("open archive", func(t *testing.T) {
		restore := restoreReleaseHooks()
		defer restore()

		releaseOpenReadOnlyArchive = func(string) (*os.File, error) {
			return nil, errors.New("open archive exploded")
		}

		if err := loadImage(context.Background(), "docker", "ignored.tar.gz"); err == nil || err.Error() != "open image archive: open archive exploded" {
			t.Fatalf("loadImage() error = %v, want exact archive-open failure", err)
		}
	})

	t.Run("gzip reader", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "image.tar.gz")
		if err := os.WriteFile(path, []byte("not-gzip"), 0o600); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}
		if err := loadImage(context.Background(), "docker", path); err == nil || !strings.Contains(err.Error(), "open image archive gzip stream") {
			t.Fatalf("loadImage() error = %v, want gzip failure", err)
		}
	})

	t.Run("command failure", func(t *testing.T) {
		restore := restoreReleaseHooks()
		defer restore()

		execCommandContext = helperExecCommandContext("load-image-fail")

		dir := t.TempDir()
		path := filepath.Join(dir, "image.tar.gz")
		file, err := os.Create(path)
		if err != nil {
			t.Fatalf("Create() error = %v", err)
		}
		gzipWriter := gzip.NewWriter(file)
		if _, err := gzipWriter.Write([]byte("image-bytes")); err != nil {
			t.Fatalf("Write() error = %v", err)
		}
		if err := gzipWriter.Close(); err != nil {
			t.Fatalf("Close() gzip error = %v", err)
		}
		if err := file.Close(); err != nil {
			t.Fatalf("Close() file error = %v", err)
		}

		if err := loadImage(context.Background(), "docker", path); err == nil || !strings.Contains(err.Error(), "docker load") {
			t.Fatalf("loadImage() error = %v, want wrapped command failure", err)
		}
	})
}

func TestRemoveContainerReturnsExecPathError(t *testing.T) {
	restore := restoreReleaseHooks()
	defer restore()

	if err := removeContainer(context.Background(), "definitely-not-on-path", "test-container"); err == nil {
		t.Fatal("removeContainer() succeeded, want exec path error")
	}
}

func TestIsContainerNotFoundError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		message string
		want    bool
	}{
		{message: "Error: No such container", want: true},
		{message: "Error: No container with name or ID found", want: true},
		{message: "permission denied", want: false},
	}

	for _, test := range tests {
		test := test
		t.Run(test.message, func(t *testing.T) {
			t.Parallel()
			if got := isContainerNotFoundError(test.message); got != test.want {
				t.Fatalf("isContainerNotFoundError(%q) = %t, want %t", test.message, got, test.want)
			}
		})
	}
}

func TestStartContainerReturnsRunError(t *testing.T) {
	restore := restoreReleaseHooks()
	defer restore()

	execCommandContext = func(ctx context.Context, command string, args ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "/path/that/does/not/exist", args...)
	}

	if err := startContainer(context.Background(), config{
		containerCLI:    "docker",
		containerName:   "test-container",
		imageTag:        "minecraft-staging-image:ci",
		javaIPv4Port:    45565,
		bedrockIPv4Port: 49132,
	}); err == nil || !strings.Contains(err.Error(), "docker run") {
		t.Fatalf("startContainer() error = %v, want wrapped run failure", err)
	}
}

func TestExtractZipBinaryMissingBinary(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	archivePath := filepath.Join(dir, "minecraft-ping.zip")
	createZipArchive(t, archivePath, map[string][]byte{
		"docs/readme.txt": []byte("ignore"),
	})

	if _, err := extractZipBinary(archivePath, dir, "minecraft-ping"); err == nil || !strings.Contains(err.Error(), `binary "minecraft-ping" not found`) {
		t.Fatalf("extractZipBinary() error = %v, want missing-binary error", err)
	}
}

func TestExtractTarGzBinaryMissingBinary(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	archivePath := filepath.Join(dir, "minecraft-ping.tar.gz")
	createTarGzArchive(t, archivePath, map[string][]byte{
		"README.md": []byte("ignore"),
	})

	if _, err := extractTarGzBinary(archivePath, dir, "minecraft-ping"); err == nil || !strings.Contains(err.Error(), `binary "minecraft-ping" not found`) {
		t.Fatalf("extractTarGzBinary() error = %v, want missing-binary error", err)
	}
}

func TestExtractBinaryDispatch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		archiveExt string
		create     func(t *testing.T, path string)
		wantBinary string
	}{
		{
			name:       "zip archive",
			archiveExt: ".zip",
			create: func(t *testing.T, path string) {
				createZipArchive(t, path, map[string][]byte{
					"docs/readme.txt": []byte("ignore"),
					"minecraft-ping":  []byte("zip-binary"),
				})
			},
			wantBinary: "zip-binary",
		},
		{
			name:       "tar.gz archive",
			archiveExt: ".tar.gz",
			create: func(t *testing.T, path string) {
				createTarGzArchive(t, path, map[string][]byte{
					"docs/readme.txt": []byte("ignore"),
					"minecraft-ping":  []byte("tar-binary"),
				})
			},
			wantBinary: "tar-binary",
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			archivePath := filepath.Join(dir, "minecraft-ping"+test.archiveExt)
			test.create(t, archivePath)

			extracted, cleanup, err := extractBinary(archivePath, "minecraft-ping")
			if err != nil {
				t.Fatalf("extractBinary() error = %v", err)
			}
			defer cleanup()

			data, err := os.ReadFile(extracted)
			if err != nil {
				t.Fatalf("ReadFile() error = %v", err)
			}
			if string(data) != test.wantBinary {
				t.Fatalf("extracted data = %q, want %q", data, test.wantBinary)
			}
		})
	}

	t.Run("unsupported format", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		archivePath := filepath.Join(dir, "minecraft-ping.deb")
		if err := os.WriteFile(archivePath, []byte("not-an-archive"), 0o600); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}

		if _, _, err := extractBinary(archivePath, "minecraft-ping"); err == nil || !strings.Contains(err.Error(), "unsupported archive format") {
			t.Fatalf("extractBinary() error = %v, want unsupported-format error", err)
		}
	})
}

func TestExtractBinaryErrorPaths(t *testing.T) {
	t.Run("create temp dir", func(t *testing.T) {
		restore := restoreReleaseHooks()
		defer restore()

		releaseMkdirTemp = func(string, string) (string, error) {
			return "", errors.New("mkdir temp exploded")
		}

		if _, _, err := extractBinary("release.tar.gz", "minecraft-ping"); err == nil || !strings.Contains(err.Error(), "mkdir temp exploded") {
			t.Fatalf("extractBinary() error = %v, want temp-dir failure", err)
		}
	})

	t.Run("cleanup on extraction failure", func(t *testing.T) {
		restore := restoreReleaseHooks()
		defer restore()

		root := t.TempDir()
		var tempDir string
		releaseMkdirTemp = func(dir, pattern string) (string, error) {
			var err error
			tempDir, err = os.MkdirTemp(root, pattern)
			return tempDir, err
		}
		releaseZipOpenReader = func(string) (*zip.ReadCloser, error) {
			return nil, errors.New("zip exploded")
		}

		if _, _, err := extractBinary("release.zip", "minecraft-ping"); err == nil || !strings.Contains(err.Error(), "zip exploded") {
			t.Fatalf("extractBinary() error = %v, want wrapped zip failure", err)
		}
		if _, err := os.Stat(tempDir); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("temp dir stat error = %v, want removed temp dir", err)
		}
	})
}

func TestExtractZipBinaryErrorPaths(t *testing.T) {
	t.Run("open archive", func(t *testing.T) {
		if _, err := extractZipBinary(filepath.Join(t.TempDir(), "missing.zip"), t.TempDir(), "minecraft-ping"); err == nil || !strings.Contains(err.Error(), "open zip archive") {
			t.Fatalf("extractZipBinary() error = %v, want archive-open failure", err)
		}
	})

	t.Run("open temp dir root", func(t *testing.T) {
		dir := t.TempDir()
		archivePath := filepath.Join(dir, "minecraft-ping.zip")
		createZipArchive(t, archivePath, map[string][]byte{"minecraft-ping": []byte("zip-binary")})
		if _, err := extractZipBinary(archivePath, filepath.Join(dir, "missing"), "minecraft-ping"); err == nil || !strings.Contains(err.Error(), "open temp dir root") {
			t.Fatalf("extractZipBinary() error = %v, want root-open failure", err)
		}
	})

	t.Run("invalid size", func(t *testing.T) {
		dir := t.TempDir()
		archivePath := filepath.Join(dir, "minecraft-ping.zip")
		createZipArchive(t, archivePath, map[string][]byte{"minecraft-ping": {}})
		if _, err := extractZipBinary(archivePath, dir, "minecraft-ping"); err == nil || !strings.Contains(err.Error(), "invalid size 0") {
			t.Fatalf("extractZipBinary() error = %v, want invalid-size failure", err)
		}
	})

	t.Run("zip file open", func(t *testing.T) {
		restore := restoreReleaseHooks()
		defer restore()

		dir := t.TempDir()
		archivePath := filepath.Join(dir, "minecraft-ping.zip")
		createZipArchive(t, archivePath, map[string][]byte{"minecraft-ping": []byte("zip-binary")})
		releaseZipFileOpen = func(*zip.File) (io.ReadCloser, error) {
			return nil, errors.New("zip open exploded")
		}

		if _, err := extractZipBinary(archivePath, dir, "minecraft-ping"); err == nil || !strings.Contains(err.Error(), "zip open exploded") {
			t.Fatalf("extractZipBinary() error = %v, want file-open failure", err)
		}
	})

	t.Run("create target", func(t *testing.T) {
		restore := restoreReleaseHooks()
		defer restore()

		dir := t.TempDir()
		archivePath := filepath.Join(dir, "minecraft-ping.zip")
		createZipArchive(t, archivePath, map[string][]byte{"minecraft-ping": []byte("zip-binary")})
		releaseRootOpenFile = func(*os.Root, string, int, os.FileMode) (*os.File, error) {
			return nil, errors.New("open file exploded")
		}

		if _, err := extractZipBinary(archivePath, dir, "minecraft-ping"); err == nil || !strings.Contains(err.Error(), "open file exploded") {
			t.Fatalf("extractZipBinary() error = %v, want target-create failure", err)
		}
	})

	t.Run("copy", func(t *testing.T) {
		restore := restoreReleaseHooks()
		defer restore()

		dir := t.TempDir()
		archivePath := filepath.Join(dir, "minecraft-ping.zip")
		createZipArchive(t, archivePath, map[string][]byte{"minecraft-ping": []byte("zip-binary")})
		closeCalls := 0
		releaseFileClose = func(file *os.File) error {
			closeCalls++
			return file.Close()
		}
		releaseCopyWithLimit = func(io.Writer, io.Reader, int64) error {
			return errors.New("copy exploded")
		}

		if _, err := extractZipBinary(archivePath, dir, "minecraft-ping"); err == nil || !strings.Contains(err.Error(), "copy exploded") {
			t.Fatalf("extractZipBinary() error = %v, want copy failure", err)
		}
		if closeCalls != 1 {
			t.Fatalf("releaseFileClose() calls = %d, want 1", closeCalls)
		}
	})

	t.Run("close target", func(t *testing.T) {
		restore := restoreReleaseHooks()
		defer restore()

		dir := t.TempDir()
		archivePath := filepath.Join(dir, "minecraft-ping.zip")
		createZipArchive(t, archivePath, map[string][]byte{"minecraft-ping": []byte("zip-binary")})
		var closeCalls int
		releaseFileClose = func(file *os.File) error {
			closeCalls++
			if closeCalls == 1 {
				return errors.New("close exploded")
			}
			return file.Close()
		}

		if _, err := extractZipBinary(archivePath, dir, "minecraft-ping"); err == nil || !strings.Contains(err.Error(), "close exploded") {
			t.Fatalf("extractZipBinary() error = %v, want close failure", err)
		}
	})

	t.Run("chmod", func(t *testing.T) {
		restore := restoreReleaseHooks()
		defer restore()

		dir := t.TempDir()
		archivePath := filepath.Join(dir, "minecraft-ping.zip")
		createZipArchive(t, archivePath, map[string][]byte{"minecraft-ping": []byte("zip-binary")})
		releaseRootChmod = func(*os.Root, string, os.FileMode) error {
			return errors.New("chmod exploded")
		}

		if _, err := extractZipBinary(archivePath, dir, "minecraft-ping"); err == nil || !strings.Contains(err.Error(), "chmod exploded") {
			t.Fatalf("extractZipBinary() error = %v, want chmod failure", err)
		}
	})
}

func TestExtractTarGzBinaryErrorPaths(t *testing.T) {
	t.Run("open archive", func(t *testing.T) {
		if _, err := extractTarGzBinary(filepath.Join(t.TempDir(), "missing.tar.gz"), t.TempDir(), "minecraft-ping"); err == nil || !strings.Contains(err.Error(), "open tar.gz archive") {
			t.Fatalf("extractTarGzBinary() error = %v, want archive-open failure", err)
		}
	})

	t.Run("gzip reader", func(t *testing.T) {
		dir := t.TempDir()
		archivePath := filepath.Join(dir, "minecraft-ping.tar.gz")
		if err := os.WriteFile(archivePath, []byte("not-gzip"), 0o600); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}
		if _, err := extractTarGzBinary(archivePath, dir, "minecraft-ping"); err == nil || !strings.Contains(err.Error(), "open gzip stream") {
			t.Fatalf("extractTarGzBinary() error = %v, want gzip failure", err)
		}
	})

	t.Run("open temp dir root", func(t *testing.T) {
		dir := t.TempDir()
		archivePath := filepath.Join(dir, "minecraft-ping.tar.gz")
		createTarGzArchive(t, archivePath, map[string][]byte{"minecraft-ping": []byte("tar-binary")})
		if _, err := extractTarGzBinary(archivePath, filepath.Join(dir, "missing"), "minecraft-ping"); err == nil || !strings.Contains(err.Error(), "open temp dir root") {
			t.Fatalf("extractTarGzBinary() error = %v, want root-open failure", err)
		}
	})

	t.Run("invalid tar header", func(t *testing.T) {
		dir := t.TempDir()
		archivePath := filepath.Join(dir, "minecraft-ping.tar.gz")
		file, err := os.Create(archivePath)
		if err != nil {
			t.Fatalf("Create() error = %v", err)
		}
		gzipWriter := gzip.NewWriter(file)
		if _, err := gzipWriter.Write([]byte("not-a-tar")); err != nil {
			t.Fatalf("Write() error = %v", err)
		}
		if err := gzipWriter.Close(); err != nil {
			t.Fatalf("Close() gzip error = %v", err)
		}
		if err := file.Close(); err != nil {
			t.Fatalf("Close() file error = %v", err)
		}

		if _, err := extractTarGzBinary(archivePath, dir, "minecraft-ping"); err == nil || !strings.Contains(err.Error(), "read tar header") {
			t.Fatalf("extractTarGzBinary() error = %v, want tar-header failure", err)
		}
	})

	t.Run("invalid size", func(t *testing.T) {
		dir := t.TempDir()
		archivePath := filepath.Join(dir, "minecraft-ping.tar.gz")
		createTarGzArchive(t, archivePath, map[string][]byte{"minecraft-ping": {}})
		if _, err := extractTarGzBinary(archivePath, dir, "minecraft-ping"); err == nil || !strings.Contains(err.Error(), "invalid size 0") {
			t.Fatalf("extractTarGzBinary() error = %v, want invalid-size failure", err)
		}
	})

	t.Run("create target", func(t *testing.T) {
		restore := restoreReleaseHooks()
		defer restore()

		dir := t.TempDir()
		archivePath := filepath.Join(dir, "minecraft-ping.tar.gz")
		createTarGzArchive(t, archivePath, map[string][]byte{"minecraft-ping": []byte("tar-binary")})
		releaseRootOpenFile = func(*os.Root, string, int, os.FileMode) (*os.File, error) {
			return nil, errors.New("open file exploded")
		}

		if _, err := extractTarGzBinary(archivePath, dir, "minecraft-ping"); err == nil || !strings.Contains(err.Error(), "open file exploded") {
			t.Fatalf("extractTarGzBinary() error = %v, want target-create failure", err)
		}
	})

	t.Run("copy", func(t *testing.T) {
		restore := restoreReleaseHooks()
		defer restore()

		dir := t.TempDir()
		archivePath := filepath.Join(dir, "minecraft-ping.tar.gz")
		createTarGzArchive(t, archivePath, map[string][]byte{"minecraft-ping": []byte("tar-binary")})
		closeCalls := 0
		releaseFileClose = func(file *os.File) error {
			closeCalls++
			return file.Close()
		}
		releaseCopyN = func(io.Writer, io.Reader, int64) (int64, error) {
			return 0, errors.New("copy exploded")
		}

		if _, err := extractTarGzBinary(archivePath, dir, "minecraft-ping"); err == nil || !strings.Contains(err.Error(), "copy exploded") {
			t.Fatalf("extractTarGzBinary() error = %v, want copy failure", err)
		}
		if closeCalls != 1 {
			t.Fatalf("releaseFileClose() calls = %d, want 1", closeCalls)
		}
	})

	t.Run("close target", func(t *testing.T) {
		restore := restoreReleaseHooks()
		defer restore()

		dir := t.TempDir()
		archivePath := filepath.Join(dir, "minecraft-ping.tar.gz")
		createTarGzArchive(t, archivePath, map[string][]byte{"minecraft-ping": []byte("tar-binary")})
		var closeCalls int
		releaseFileClose = func(file *os.File) error {
			closeCalls++
			if closeCalls == 1 {
				return errors.New("close exploded")
			}
			return file.Close()
		}

		if _, err := extractTarGzBinary(archivePath, dir, "minecraft-ping"); err == nil || !strings.Contains(err.Error(), "close exploded") {
			t.Fatalf("extractTarGzBinary() error = %v, want close failure", err)
		}
	})

	t.Run("chmod", func(t *testing.T) {
		restore := restoreReleaseHooks()
		defer restore()

		dir := t.TempDir()
		archivePath := filepath.Join(dir, "minecraft-ping.tar.gz")
		createTarGzArchive(t, archivePath, map[string][]byte{"minecraft-ping": []byte("tar-binary")})
		releaseRootChmod = func(*os.Root, string, os.FileMode) error {
			return errors.New("chmod exploded")
		}

		if _, err := extractTarGzBinary(archivePath, dir, "minecraft-ping"); err == nil || !strings.Contains(err.Error(), "chmod exploded") {
			t.Fatalf("extractTarGzBinary() error = %v, want chmod failure", err)
		}
	})
}

func TestExtractBinarySizeBoundaries(t *testing.T) {
	formats := []struct {
		name    string
		suffix  string
		fill    byte
		create  func(*testing.T, string, map[string][]byte)
		extract func(string, string, string) (string, error)
	}{
		{
			name:    "zip",
			suffix:  ".zip",
			fill:    'z',
			create:  createZipArchive,
			extract: extractZipBinary,
		},
		{
			name:    "tar.gz",
			suffix:  ".tar.gz",
			fill:    't',
			create:  createTarGzArchive,
			extract: extractTarGzBinary,
		},
	}
	sizes := []struct {
		name    string
		size    int
		wantErr bool
	}{
		{name: "single-byte", size: 1},
		{name: "max", size: int(maxExtractedBinarySize)},
		{name: "oversized", size: int(maxExtractedBinarySize) + 1, wantErr: true},
	}

	for _, format := range formats {
		format := format
		t.Run(format.name, func(t *testing.T) {
			for _, size := range sizes {
				size := size
				t.Run(size.name, func(t *testing.T) {
					dir := t.TempDir()
					archivePath := filepath.Join(dir, "minecraft-ping"+format.suffix)
					format.create(t, archivePath, map[string][]byte{
						"minecraft-ping": bytes.Repeat([]byte{format.fill}, size.size),
					})

					_, err := format.extract(archivePath, dir, "minecraft-ping")
					if size.wantErr {
						if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("invalid size %d", size.size)) {
							t.Fatalf("%s extract error = %v, want oversized failure", format.name, err)
						}
						return
					}
					if err != nil {
						t.Fatalf("%s extract error = %v", format.name, err)
					}
				})
			}
		})
	}
}

func TestIPv6OnlyControl(t *testing.T) {
	t.Run("skips non matching network", func(t *testing.T) {
		called := false
		if err := ipv6OnlyControl("tcp6")("tcp4", "", stubRawConn{
			control: func(func(uintptr)) error {
				called = true
				return nil
			},
		}); err != nil {
			t.Fatalf("ipv6OnlyControl() error = %v", err)
		}
		if called {
			t.Fatal("ipv6OnlyControl() called RawConn.Control for non-matching network")
		}
	})

	t.Run("propagates raw control error", func(t *testing.T) {
		sentinel := errors.New("raw control exploded")
		err := ipv6OnlyControl("udp6")("udp6", "", stubRawConn{
			control: func(func(uintptr)) error { return sentinel },
		})
		if !errors.Is(err, sentinel) {
			t.Fatalf("ipv6OnlyControl() error = %v, want %v", err, sentinel)
		}
	})

	t.Run("propagates ipv6 only error", func(t *testing.T) {
		restore := restoreReleaseHooks()
		defer restore()

		sentinel := errors.New("ipv6 only exploded")
		releaseSetIPv6Only = func(uintptr) error { return sentinel }

		err := ipv6OnlyControl("udp6")("udp6", "", stubRawConn{
			control: func(fn func(uintptr)) error {
				fn(123)
				return nil
			},
		})
		if !errors.Is(err, sentinel) {
			t.Fatalf("ipv6OnlyControl() error = %v, want %v", err, sentinel)
		}
	})
}

func TestNewIPv6RelayAndServe(t *testing.T) {
	t.Run("listen error", func(t *testing.T) {
		restore := restoreReleaseHooks()
		defer restore()

		releaseListen = func(net.ListenConfig, context.Context, string, string) (net.Listener, error) {
			return nil, errors.New("listen exploded")
		}
		if _, err := newIPv6Relay("::1", 45566, "127.0.0.1", 45565); err == nil || !strings.Contains(err.Error(), "listen exploded") {
			t.Fatalf("newIPv6Relay() error = %v, want listen failure", err)
		}
	})

	t.Run("accept error exits serve", func(t *testing.T) {
		relay := &ipv6Relay{listener: &scriptedListener{accepts: []acceptResult{{err: errors.New("accept exploded")}}}}
		done := make(chan struct{})
		go func() {
			relay.serve("127.0.0.1:25565")
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("serve() did not stop after accept error")
		}
	})

	t.Run("dial failure logs", func(t *testing.T) {
		restore := restoreReleaseHooks()
		defer restore()

		server, client := net.Pipe()
		defer client.Close()
		listener := &scriptedListener{accepts: []acceptResult{{conn: server}, {err: errors.New("stop")}}}
		releaseDialTCP = func(string, string) (net.Conn, error) {
			return nil, errors.New("dial exploded")
		}

		logBuf := captureLogOutput(t)

		relay := &ipv6Relay{listener: listener}
		done := make(chan struct{})
		go func() {
			relay.serve("127.0.0.1:25565")
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("serve() did not stop")
		}
		deadline := time.Now().Add(time.Second)
		for !strings.Contains(logBuf.String(), "ipv6 relay dial failed: dial exploded") && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
		if !strings.Contains(logBuf.String(), "ipv6 relay dial failed: dial exploded") {
			t.Fatalf("log output = %q, want dial failure log", logBuf.String())
		}
	})

	t.Run("nil close", func(t *testing.T) {
		var relay *ipv6Relay
		relay.close()
	})
}

func TestNewIPv6RelayStartsServeAndCloseWaits(t *testing.T) {
	restore := restoreReleaseHooks()
	defer restore()

	client, relayClient := net.Pipe()
	upstream, relayUpstream := net.Pipe()
	defer client.Close()
	defer upstream.Close()

	listener := &scriptedListener{accepts: []acceptResult{{conn: relayClient}}}
	dialed := make(chan struct{}, 1)
	releaseListen = func(net.ListenConfig, context.Context, string, string) (net.Listener, error) {
		return listener, nil
	}
	releaseDialTCP = func(network, address string) (net.Conn, error) {
		close(dialed)
		return relayUpstream, nil
	}

	relay, err := newIPv6Relay("::1", 45566, "127.0.0.1", 45565)
	if err != nil {
		t.Fatalf("newIPv6Relay() error = %v", err)
	}

	select {
	case <-dialed:
	case <-time.After(time.Second):
		t.Fatal("newIPv6Relay() did not start the serve loop")
	}

	closeDone := make(chan struct{})
	go func() {
		relay.close()
		close(closeDone)
	}()
	select {
	case <-closeDone:
		t.Fatal("relay.close() returned before active proxy streams ended")
	case <-time.After(50 * time.Millisecond):
	}

	if _, err := client.Write([]byte("ping")); err != nil {
		t.Fatalf("client.Write() error = %v", err)
	}
	buf := make([]byte, 4)
	if _, err := io.ReadFull(upstream, buf); err != nil {
		t.Fatalf("io.ReadFull() error = %v", err)
	}
	if string(buf) != "ping" {
		t.Fatalf("proxied payload = %q, want %q", buf, "ping")
	}

	_ = client.Close()
	_ = upstream.Close()
	select {
	case <-closeDone:
	case <-time.After(time.Second):
		t.Fatal("relay.close() did not wait for active streams")
	}
	if listener.closeCalls != 1 {
		t.Fatalf("listener close calls = %d, want 1", listener.closeCalls)
	}
}

func TestProxyConnsCopiesDataAndSetsDeadlines(t *testing.T) {
	aLocal, aPeer := net.Pipe()
	bLocal, bPeer := net.Pipe()
	a := &deadlineTrackingConn{Conn: aLocal}
	b := &deadlineTrackingConn{Conn: bLocal}
	defer aPeer.Close()
	defer bPeer.Close()

	done := make(chan struct{})
	go func() {
		proxyConns(a, b)
		close(done)
	}()

	select {
	case <-done:
		t.Fatal("proxyConns() returned before either side closed")
	case <-time.After(50 * time.Millisecond):
	}

	if _, err := aPeer.Write([]byte("to-b")); err != nil {
		t.Fatalf("aPeer.Write() error = %v", err)
	}
	buf := make([]byte, 4)
	if _, err := io.ReadFull(bPeer, buf); err != nil {
		t.Fatalf("io.ReadFull(bPeer) error = %v", err)
	}
	if string(buf) != "to-b" {
		t.Fatalf("bPeer payload = %q, want %q", buf, "to-b")
	}

	if _, err := bPeer.Write([]byte("to-a")); err != nil {
		t.Fatalf("bPeer.Write() error = %v", err)
	}
	if _, err := io.ReadFull(aPeer, buf); err != nil {
		t.Fatalf("io.ReadFull(aPeer) error = %v", err)
	}
	if string(buf) != "to-a" {
		t.Fatalf("aPeer payload = %q, want %q", buf, "to-a")
	}

	_ = aPeer.Close()
	_ = bPeer.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("proxyConns() did not return after both peers closed")
	}
	if a.deadlineCount() != 2 || b.deadlineCount() != 2 {
		t.Fatalf("deadline counts = %d, %d, want 2 and 2", a.deadlineCount(), b.deadlineCount())
	}
}

func TestNewUDPIPv6RelayAndServe(t *testing.T) {
	t.Run("listen error", func(t *testing.T) {
		restore := restoreReleaseHooks()
		defer restore()

		releaseListenPacket = func(net.ListenConfig, context.Context, string, string) (net.PacketConn, error) {
			return nil, errors.New("listen exploded")
		}
		if _, err := newUDPIPv6Relay("::1", 49133, "127.0.0.1", 49132); err == nil || !strings.Contains(err.Error(), "listen exploded") {
			t.Fatalf("newUDPIPv6Relay() error = %v, want listen failure", err)
		}
	})

	t.Run("resolve target error", func(t *testing.T) {
		restore := restoreReleaseHooks()
		defer restore()

		conn := &scriptedPacketConn{}
		releaseListenPacket = func(net.ListenConfig, context.Context, string, string) (net.PacketConn, error) {
			return conn, nil
		}
		releaseResolveUDPAddr = func(string, string) (*net.UDPAddr, error) {
			return nil, errors.New("resolve exploded")
		}
		if _, err := newUDPIPv6Relay("::1", 49133, "127.0.0.1", 49132); err == nil || !strings.Contains(err.Error(), "resolve exploded") {
			t.Fatalf("newUDPIPv6Relay() error = %v, want resolve failure", err)
		}
		if conn.closeCalls != 1 {
			t.Fatalf("packet conn close calls = %d, want 1", conn.closeCalls)
		}
	})

	t.Run("serve returns on read error", func(t *testing.T) {
		relay := &udpIPv6Relay{conn: &scriptedPacketConn{readErrs: []error{errors.New("read exploded")}}}
		relay.wg.Add(1)
		done := make(chan struct{})
		go func() {
			relay.serve()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("serve() did not stop after read error")
		}
	})

	t.Run("serve logs forward errors", func(t *testing.T) {
		clientAddr := &net.UDPAddr{IP: net.ParseIP("::1"), Port: 12345}
		conn := &scriptedPacketConn{
			readPayloads: [][]byte{[]byte("hello")},
			readAddrs:    []net.Addr{clientAddr},
			readErrs:     []error{nil, errors.New("stop")},
		}
		relay := &udpIPv6Relay{
			conn:       conn,
			targetAddr: &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 19132},
		}
		relay.wg.Add(1)

		restore := restoreReleaseHooks()
		defer restore()
		releaseDialUDP = func(string, *net.UDPAddr, *net.UDPAddr) (udpRelayConn, error) {
			return nil, errors.New("dial exploded")
		}

		logBuf := captureLogOutput(t)

		done := make(chan struct{})
		go func() {
			relay.serve()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("serve() did not stop")
		}
		deadline := time.Now().Add(time.Second)
		for !strings.Contains(logBuf.String(), "udp ipv6 relay failed: dial exploded") && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
		if !strings.Contains(logBuf.String(), "udp ipv6 relay failed: dial exploded") {
			t.Fatalf("log output = %q, want forward failure log", logBuf.String())
		}
	})

	t.Run("forward packet errors", func(t *testing.T) {
		restore := restoreReleaseHooks()
		defer restore()

		relay := &udpIPv6Relay{targetAddr: &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 19132}}
		clientAddr := &net.UDPAddr{IP: net.ParseIP("::1"), Port: 12345}

		releaseDialUDP = func(string, *net.UDPAddr, *net.UDPAddr) (udpRelayConn, error) {
			return nil, errors.New("dial exploded")
		}
		if err := relay.forwardPacket([]byte("hello"), clientAddr); err == nil || !strings.Contains(err.Error(), "dial exploded") {
			t.Fatalf("forwardPacket() error = %v, want dial failure", err)
		}
	})

	t.Run("nil close", func(t *testing.T) {
		var relay *udpIPv6Relay
		relay.close()
	})
}

func TestNewUDPIPv6RelayStartsServeAndCloseWaits(t *testing.T) {
	restore := restoreReleaseHooks()
	defer restore()

	conn := newBlockingPacketConn()
	releaseListenPacket = func(net.ListenConfig, context.Context, string, string) (net.PacketConn, error) {
		return conn, nil
	}
	releaseResolveUDPAddr = func(string, string) (*net.UDPAddr, error) {
		return &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 19132}, nil
	}

	relay, err := newUDPIPv6Relay("::1", 49133, "127.0.0.1", 49132)
	if err != nil {
		t.Fatalf("newUDPIPv6Relay() error = %v", err)
	}

	select {
	case <-conn.readStarted:
	case <-time.After(time.Second):
		t.Fatal("newUDPIPv6Relay() did not start the serve loop")
	}

	closeDone := make(chan struct{})
	go func() {
		relay.close()
		close(closeDone)
	}()
	select {
	case <-closeDone:
		t.Fatal("relay.close() returned before the serve goroutine exited")
	case <-time.After(50 * time.Millisecond):
	}

	close(conn.allowExit)
	select {
	case <-closeDone:
	case <-time.After(time.Second):
		t.Fatal("relay.close() did not return after closing the packet conn")
	}
	if conn.closeCalls != 1 {
		t.Fatalf("packet conn close calls = %d, want 1", conn.closeCalls)
	}
}

func TestUDPIPv6RelayForwardPacketPropagatesErrorsAndTruncatesReplies(t *testing.T) {
	restore := restoreReleaseHooks()
	defer restore()

	t.Run("deadline error", func(t *testing.T) {
		relay := &udpIPv6Relay{
			conn:       &scriptedPacketConn{},
			targetAddr: &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 19132},
		}
		releaseDialUDP = func(string, *net.UDPAddr, *net.UDPAddr) (udpRelayConn, error) {
			return &scriptedUDPConn{deadlineErr: errors.New("deadline exploded")}, nil
		}

		err := relay.forwardPacket([]byte("hello"), &net.UDPAddr{IP: net.ParseIP("::1"), Port: 12345})
		if err == nil || !strings.Contains(err.Error(), "deadline exploded") {
			t.Fatalf("forwardPacket() error = %v, want deadline failure", err)
		}
	})

	t.Run("write error", func(t *testing.T) {
		relay := &udpIPv6Relay{
			conn:       &scriptedPacketConn{},
			targetAddr: &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 19132},
		}
		releaseDialUDP = func(string, *net.UDPAddr, *net.UDPAddr) (udpRelayConn, error) {
			return &scriptedUDPConn{writeErr: errors.New("write exploded")}, nil
		}

		err := relay.forwardPacket([]byte("hello"), &net.UDPAddr{IP: net.ParseIP("::1"), Port: 12345})
		if err == nil || !strings.Contains(err.Error(), "write exploded") {
			t.Fatalf("forwardPacket() error = %v, want write failure", err)
		}
	})

	t.Run("read error", func(t *testing.T) {
		relay := &udpIPv6Relay{
			conn:       &scriptedPacketConn{},
			targetAddr: &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 19132},
		}
		releaseDialUDP = func(string, *net.UDPAddr, *net.UDPAddr) (udpRelayConn, error) {
			return &scriptedUDPConn{readErr: errors.New("read exploded")}, nil
		}

		err := relay.forwardPacket([]byte("hello"), &net.UDPAddr{IP: net.ParseIP("::1"), Port: 12345})
		if err == nil || !strings.Contains(err.Error(), "read exploded") {
			t.Fatalf("forwardPacket() error = %v, want read failure", err)
		}
	})

	t.Run("write back error", func(t *testing.T) {
		conn := &scriptedPacketConn{writeErr: errors.New("write back exploded")}
		relay := &udpIPv6Relay{
			conn:       conn,
			targetAddr: &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 19132},
		}
		releaseDialUDP = func(string, *net.UDPAddr, *net.UDPAddr) (udpRelayConn, error) {
			return &scriptedUDPConn{readPayload: []byte("reply")}, nil
		}

		err := relay.forwardPacket([]byte("hello"), &net.UDPAddr{IP: net.ParseIP("::1"), Port: 12345})
		if err == nil || !strings.Contains(err.Error(), "write back exploded") {
			t.Fatalf("forwardPacket() error = %v, want write-back failure", err)
		}
	})

	t.Run("reply buffer truncation", func(t *testing.T) {
		conn := &scriptedPacketConn{}
		relay := &udpIPv6Relay{
			conn:       conn,
			targetAddr: &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 19132},
		}
		releaseDialUDP = func(string, *net.UDPAddr, *net.UDPAddr) (udpRelayConn, error) {
			return &scriptedUDPConn{readPayload: bytes.Repeat([]byte("r"), 2049)}, nil
		}

		if err := relay.forwardPacket([]byte("hello"), &net.UDPAddr{IP: net.ParseIP("::1"), Port: 12345}); err != nil {
			t.Fatalf("forwardPacket() error = %v", err)
		}
		if len(conn.writePayloads) != 1 {
			t.Fatalf("write payload count = %d, want 1", len(conn.writePayloads))
		}
		if got := len(conn.writePayloads[0]); got != 2048 {
			t.Fatalf("reply length = %d, want 2048", got)
		}
	})
}

func TestUDPIPv6RelayServeTruncatesIncomingPayloads(t *testing.T) {
	restore := restoreReleaseHooks()
	defer restore()

	clientAddr := &net.UDPAddr{IP: net.ParseIP("::1"), Port: 12345}
	conn := &scriptedPacketConn{
		readPayloads: [][]byte{bytes.Repeat([]byte("p"), 2049)},
		readAddrs:    []net.Addr{clientAddr},
		readErrs:     []error{nil, errors.New("stop")},
	}
	relay := &udpIPv6Relay{
		conn:       conn,
		targetAddr: &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 19132},
	}
	relay.wg.Add(1)
	var dialed *scriptedUDPConn
	releaseDialUDP = func(string, *net.UDPAddr, *net.UDPAddr) (udpRelayConn, error) {
		dialed = &scriptedUDPConn{readPayload: []byte("reply")}
		return dialed, nil
	}

	done := make(chan struct{})
	go func() {
		relay.serve()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("serve() did not stop")
	}

	if dialed == nil || len(dialed.writePayloads) != 1 {
		t.Fatal("serve() did not forward any payloads")
	}
	if got := len(dialed.writePayloads[0]); got != 2048 {
		t.Fatalf("forwarded payload length = %d, want 2048", got)
	}
}

func TestOpenReadOnlyFileErrorPaths(t *testing.T) {
	t.Run("open root", func(t *testing.T) {
		restore := restoreReleaseHooks()
		defer restore()

		releaseOpenRoot = func(string) (*os.Root, error) {
			return nil, errors.New("root exploded")
		}
		if _, err := openReadOnlyFile("/tmp/payload.txt"); err == nil || !strings.Contains(err.Error(), "root exploded") {
			t.Fatalf("openReadOnlyFile() error = %v, want root-open failure", err)
		}
	})

	t.Run("close root", func(t *testing.T) {
		restore := restoreReleaseHooks()
		defer restore()

		dir := t.TempDir()
		path := filepath.Join(dir, "payload.txt")
		if err := os.WriteFile(path, []byte("payload"), 0o600); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}
		releaseRootClose = func(*os.Root) error {
			return errors.New("close root exploded")
		}

		if _, err := openReadOnlyFile(path); err == nil || !strings.Contains(err.Error(), "close root exploded") {
			t.Fatalf("openReadOnlyFile() error = %v, want root-close failure", err)
		}
	})

	t.Run("open file closes root", func(t *testing.T) {
		restore := restoreReleaseHooks()
		defer restore()

		rootClosed := 0
		releaseRootOpen = func(*os.Root, string) (*os.File, error) {
			return nil, errors.New("open file exploded")
		}
		releaseRootClose = func(*os.Root) error {
			rootClosed++
			return nil
		}

		if _, err := openReadOnlyFile("/tmp/payload.txt"); err == nil || !strings.Contains(err.Error(), "open file exploded") {
			t.Fatalf("openReadOnlyFile() error = %v, want file-open failure", err)
		}
		if rootClosed != 1 {
			t.Fatalf("root close calls = %d, want 1", rootClosed)
		}
	})

	t.Run("close root closes file", func(t *testing.T) {
		restore := restoreReleaseHooks()
		defer restore()

		dir := t.TempDir()
		path := filepath.Join(dir, "payload.txt")
		if err := os.WriteFile(path, []byte("payload"), 0o600); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}

		fileClosed := 0
		releaseRootClose = func(*os.Root) error {
			return errors.New("close root exploded")
		}
		releaseFileClose = func(file *os.File) error {
			fileClosed++
			return file.Close()
		}

		if _, err := openReadOnlyFile(path); err == nil || !strings.Contains(err.Error(), "close root exploded") {
			t.Fatalf("openReadOnlyFile() error = %v, want root-close failure", err)
		}
		if fileClosed != 1 {
			t.Fatalf("file close calls = %d, want 1", fileClosed)
		}
	})
}

func TestCopyWithLimitReadError(t *testing.T) {
	t.Parallel()

	errReader := io.MultiReader(strings.NewReader("ok"), failingReader{err: errors.New("read exploded")})
	if err := copyWithLimit(io.Discard, errReader, 16); err == nil || !strings.Contains(err.Error(), "read exploded") {
		t.Fatalf("copyWithLimit() error = %v, want read failure", err)
	}
}

func TestCopyWithLimitRejectsOneByteOver(t *testing.T) {
	t.Parallel()

	if err := copyWithLimit(io.Discard, strings.NewReader("abcde"), 3); err == nil || err.Error() != "copied 4 bytes, exceeds limit 3" {
		t.Fatalf("copyWithLimit() error = %v, want exact one-byte-over failure", err)
	}
}

func createZipArchive(t *testing.T, path string, entries map[string][]byte) {
	t.Helper()

	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("Create(%q) error = %v", path, err)
	}
	defer file.Close()

	writer := zip.NewWriter(file)
	names := make([]string, 0, len(entries))
	for name := range entries {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		contents := entries[name]
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatalf("Create(%q) error = %v", name, err)
		}
		if _, err := entry.Write(contents); err != nil {
			t.Fatalf("Write(%q) error = %v", name, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close() zip error = %v", err)
	}
}

func createTarGzArchive(t *testing.T, path string, entries map[string][]byte) {
	t.Helper()

	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("Create(%q) error = %v", path, err)
	}
	defer file.Close()

	gzipWriter := gzip.NewWriter(file)
	tarWriter := tar.NewWriter(gzipWriter)
	names := make([]string, 0, len(entries))
	for name := range entries {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		contents := entries[name]
		header := &tar.Header{
			Name: name,
			Mode: 0o600,
			Size: int64(len(contents)),
		}
		if err := tarWriter.WriteHeader(header); err != nil {
			t.Fatalf("WriteHeader(%q) error = %v", name, err)
		}
		if _, err := tarWriter.Write(contents); err != nil {
			t.Fatalf("Write(%q) error = %v", name, err)
		}
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatalf("Close() tar error = %v", err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatalf("Close() gzip error = %v", err)
	}
}

type stubDeadlineSetter struct {
	deadline time.Time
}

func (s *stubDeadlineSetter) SetDeadline(deadline time.Time) error {
	s.deadline = deadline
	return nil
}

type stubRawConn struct {
	control func(func(uintptr)) error
}

func (s stubRawConn) Control(fn func(uintptr)) error {
	if s.control != nil {
		return s.control(fn)
	}
	fn(0)
	return nil
}

func (stubRawConn) Read(func(uintptr) bool) error  { return nil }
func (stubRawConn) Write(func(uintptr) bool) error { return nil }

type stubListener struct{}

func (stubListener) Accept() (net.Conn, error) { return nil, errors.New("stub listener accept") }
func (stubListener) Close() error              { return nil }
func (stubListener) Addr() net.Addr            { return &net.TCPAddr{} }

type stubPacketConn struct{}

func (stubPacketConn) ReadFrom([]byte) (int, net.Addr, error) {
	return 0, nil, errors.New("stub packet conn read")
}
func (stubPacketConn) WriteTo([]byte, net.Addr) (int, error) { return 0, nil }
func (stubPacketConn) Close() error                          { return nil }
func (stubPacketConn) LocalAddr() net.Addr                   { return &net.UDPAddr{} }
func (stubPacketConn) SetDeadline(time.Time) error           { return nil }
func (stubPacketConn) SetReadDeadline(time.Time) error       { return nil }
func (stubPacketConn) SetWriteDeadline(time.Time) error      { return nil }

type acceptResult struct {
	conn net.Conn
	err  error
}

type scriptedListener struct {
	accepts    []acceptResult
	closeCalls int
}

func (l *scriptedListener) Accept() (net.Conn, error) {
	if len(l.accepts) == 0 {
		return nil, errors.New("no more accepts")
	}
	result := l.accepts[0]
	l.accepts = l.accepts[1:]
	return result.conn, result.err
}

func (l *scriptedListener) Close() error {
	l.closeCalls++
	return nil
}
func (*scriptedListener) Addr() net.Addr { return &net.TCPAddr{} }

type scriptedPacketConn struct {
	readPayloads  [][]byte
	readAddrs     []net.Addr
	readErrs      []error
	writePayloads [][]byte
	writeAddrs    []net.Addr
	writeErr      error
	closeCalls    int
}

func (c *scriptedPacketConn) ReadFrom(buf []byte) (int, net.Addr, error) {
	if len(c.readErrs) == 0 {
		return 0, nil, errors.New("no more reads")
	}
	err := c.readErrs[0]
	c.readErrs = c.readErrs[1:]
	if err != nil {
		return 0, nil, err
	}
	payload := c.readPayloads[0]
	c.readPayloads = c.readPayloads[1:]
	addr := c.readAddrs[0]
	c.readAddrs = c.readAddrs[1:]
	n := copy(buf, payload)
	return n, addr, nil
}

func (c *scriptedPacketConn) WriteTo(payload []byte, addr net.Addr) (int, error) {
	c.writePayloads = append(c.writePayloads, append([]byte(nil), payload...))
	c.writeAddrs = append(c.writeAddrs, addr)
	if c.writeErr != nil {
		return 0, c.writeErr
	}
	return len(payload), nil
}
func (c *scriptedPacketConn) Close() error {
	c.closeCalls++
	return nil
}
func (*scriptedPacketConn) LocalAddr() net.Addr              { return &net.UDPAddr{} }
func (*scriptedPacketConn) SetDeadline(time.Time) error      { return nil }
func (*scriptedPacketConn) SetReadDeadline(time.Time) error  { return nil }
func (*scriptedPacketConn) SetWriteDeadline(time.Time) error { return nil }

type failingReader struct {
	err error
}

func (r failingReader) Read([]byte) (int, error) { return 0, r.err }

type scriptedUDPConn struct {
	writeErr      error
	readPayload   []byte
	readErr       error
	deadlineErr   error
	writePayloads [][]byte
	deadlineCalls int
	closeCalls    int
}

func (c *scriptedUDPConn) Write(payload []byte) (int, error) {
	c.writePayloads = append(c.writePayloads, append([]byte(nil), payload...))
	if c.writeErr != nil {
		return 0, c.writeErr
	}
	return len(payload), nil
}

func (c *scriptedUDPConn) ReadFromUDP(buf []byte) (int, *net.UDPAddr, error) {
	if c.readErr != nil {
		return 0, nil, c.readErr
	}
	n := copy(buf, c.readPayload)
	return n, &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 19132}, nil
}

func (c *scriptedUDPConn) Close() error {
	c.closeCalls++
	return nil
}

func (c *scriptedUDPConn) SetDeadline(time.Time) error {
	c.deadlineCalls++
	return c.deadlineErr
}

type blockingPacketConn struct {
	readStarted chan struct{}
	closeCh     chan struct{}
	allowExit   chan struct{}
	closeCalls  int
}

func newBlockingPacketConn() *blockingPacketConn {
	return &blockingPacketConn{
		readStarted: make(chan struct{}, 1),
		closeCh:     make(chan struct{}),
		allowExit:   make(chan struct{}),
	}
}

func (c *blockingPacketConn) ReadFrom([]byte) (int, net.Addr, error) {
	select {
	case c.readStarted <- struct{}{}:
	default:
	}
	<-c.closeCh
	<-c.allowExit
	return 0, nil, errors.New("closed")
}

func (c *blockingPacketConn) WriteTo([]byte, net.Addr) (int, error) { return 0, nil }

func (c *blockingPacketConn) Close() error {
	c.closeCalls++
	select {
	case <-c.closeCh:
	default:
		close(c.closeCh)
	}
	return nil
}

func (*blockingPacketConn) LocalAddr() net.Addr              { return &net.UDPAddr{} }
func (*blockingPacketConn) SetDeadline(time.Time) error      { return nil }
func (*blockingPacketConn) SetReadDeadline(time.Time) error  { return nil }
func (*blockingPacketConn) SetWriteDeadline(time.Time) error { return nil }

type deadlineTrackingConn struct {
	net.Conn
	mu            sync.Mutex
	deadlineCalls int
}

func (c *deadlineTrackingConn) SetDeadline(deadline time.Time) error {
	c.mu.Lock()
	c.deadlineCalls++
	c.mu.Unlock()
	return c.Conn.SetDeadline(deadline)
}

func (c *deadlineTrackingConn) deadlineCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.deadlineCalls
}
