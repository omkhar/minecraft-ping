package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
	case "probe-ok":
		want := []string{"-j", "--allow-private", "-W", "1.5", "--bedrock", "-6", "[::1]:19133"}
		if strings.Join(forwarded, "\x00") != strings.Join(want, "\x00") {
			fmt.Fprintf(os.Stderr, "unexpected probe args: %v", forwarded)
			os.Exit(2)
		}
		fmt.Print(`{"server":"::1","latency_ms":12}`)
	case "probe-bad-json":
		fmt.Print("not-json")
	case "container-not-found":
		fmt.Fprint(os.Stderr, "Error: No such container")
		os.Exit(1)
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
		if _, err := io.Copy(io.Discard, os.Stdin); err != nil {
			fmt.Fprint(os.Stderr, err)
			os.Exit(2)
		}
	default:
		fmt.Fprintf(os.Stderr, "unknown helper behavior %q", behavior)
		os.Exit(2)
	}

	os.Exit(0)
}

func TestVersionFromArchiveName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		archive   string
		want      string
		expectErr bool
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
			name:      "unexpected prefix",
			archive:   "dist/other_2.0.3_Linux_amd64.tar.gz",
			expectErr: true,
		},
		{
			name:      "unexpected suffix",
			archive:   "dist/minecraft-ping_2.0.3_linux_amd64.deb",
			expectErr: true,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, err := versionFromArchiveName(test.archive)
			if test.expectErr {
				if err == nil {
					t.Fatalf("versionFromArchiveName(%q) returned %q, want error", test.archive, got)
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
			cfg: config{
				binaryPath:   "/tmp/minecraft-ping",
				backend:      "binary",
				serverBinary: "/tmp/minecraft-staging-server",
			},
		},
		{
			name: "archive config with container backend",
			cfg: config{
				archiveGlob: "dist/*.tar.gz",
				binaryName:  "minecraft-ping",
				backend:     "container",
			},
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
			name:    "missing binary input",
			cfg:     config{backend: "container"},
			wantErr: "either -binary or both -binary-archive-glob and -binary-name are required",
		},
		{
			name: "missing server binary",
			cfg: config{
				binaryPath: "/tmp/minecraft-ping",
				backend:    "binary",
			},
			wantErr: "missing -server-binary",
		},
		{
			name: "unsupported backend",
			cfg: config{
				binaryPath: "/tmp/minecraft-ping",
				backend:    "vm",
			},
			wantErr: `unsupported -backend "vm"`,
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

func createZipArchive(t *testing.T, path string, entries map[string][]byte) {
	t.Helper()

	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("Create(%q) error = %v", path, err)
	}
	defer file.Close()

	writer := zip.NewWriter(file)
	for name, contents := range entries {
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
	for name, contents := range entries {
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
