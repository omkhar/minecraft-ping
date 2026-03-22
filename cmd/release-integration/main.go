package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

type config struct {
	archiveGlob   string
	backend       string
	binaryName    string
	binaryPath    string
	containerCLI  string
	containerName string
	imageArchive  string
	imageTag      string
	ipv4Host      string
	ipv4Port      int
	ipv6Host      string
	ipv6Port      int
	probeTimeout  time.Duration
	serverBinary  string
}

type pingResult struct {
	Server    string `json:"server"`
	LatencyMS int64  `json:"latency_ms"`
}

const maxExtractedBinarySize int64 = 64 << 20

func main() {
	cfg := config{}

	flag.StringVar(&cfg.backend, "backend", "binary", "Integration backend: binary or container")
	flag.StringVar(&cfg.archiveGlob, "binary-archive-glob", "", "Glob that resolves to a release archive containing the binary to execute")
	flag.StringVar(&cfg.binaryName, "binary-name", "", "Binary name inside the release archive")
	flag.StringVar(&cfg.binaryPath, "binary", "", "Path to an already extracted binary to execute")
	flag.StringVar(&cfg.containerCLI, "container-cli", "docker", "Container CLI used to run the Minecraft container")
	flag.StringVar(&cfg.containerName, "container-name", "minecraft-ping-release-integration", "Container name used for the integration target")
	flag.StringVar(&cfg.imageArchive, "image-archive", "", "Optional path to a compressed docker/podman image archive (.tar.gz)")
	flag.StringVar(&cfg.imageTag, "image-tag", "minecraft-staging-image:ci", "Tag of the staging image to run after it is loaded")
	flag.StringVar(&cfg.ipv4Host, "ipv4-host", "127.0.0.1", "IPv4 hostname used by the released binary")
	flag.IntVar(&cfg.ipv4Port, "ipv4-port", 25565, "IPv4 host port used by the released binary")
	flag.StringVar(&cfg.ipv6Host, "ipv6-host", "::1", "IPv6 hostname used by the released binary")
	flag.IntVar(&cfg.ipv6Port, "ipv6-port", 25566, "IPv6 host port used by the released binary")
	flag.DurationVar(&cfg.probeTimeout, "probe-timeout", 12*time.Second, "Timeout passed through to the ping binary")
	flag.StringVar(&cfg.serverBinary, "server-binary", "", "Path to the staging backend binary when -backend=binary")
	flag.Parse()

	if cfg.binaryPath == "" && (cfg.archiveGlob == "" || cfg.binaryName == "") {
		log.Fatal("either -binary or both -binary-archive-glob and -binary-name are required")
	}

	switch cfg.backend {
	case "binary":
		if cfg.serverBinary == "" {
			log.Fatal("missing -server-binary for -backend=binary")
		}
	case "container":
	default:
		log.Fatalf("unsupported -backend %q", cfg.backend)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	if err := run(ctx, cfg); err != nil {
		stop()
		log.Print(err)
		os.Exit(1)
	}
	stop()
}

func run(ctx context.Context, cfg config) error {
	binaryPath := cfg.binaryPath
	cleanup := make([]func(), 0, 4)

	if cfg.archiveGlob != "" {
		archivePath, err := resolveSingleFile(cfg.archiveGlob)
		if err != nil {
			return err
		}

		extractedBinary, removeTempDir, err := extractBinary(archivePath, cfg.binaryName)
		if err != nil {
			return err
		}
		binaryPath = extractedBinary
		cleanup = append(cleanup, removeTempDir)
	}

	defer func() {
		for i := len(cleanup) - 1; i >= 0; i-- {
			cleanup[i]()
		}
	}()

	if err := startBackend(ctx, cfg, &cleanup); err != nil {
		return err
	}

	ipv4Result, err := runProbe(ctx, binaryPath, cfg.ipv4Host, cfg.ipv4Port, cfg.probeTimeout, "-4")
	if err != nil {
		return fmt.Errorf("ipv4 probe failed: %w", err)
	}
	ipv6Result, err := runProbe(ctx, binaryPath, cfg.ipv6Host, cfg.ipv6Port, cfg.probeTimeout, "-6")
	if err != nil {
		return fmt.Errorf("ipv6 probe failed: %w", err)
	}

	log.Printf("release integration succeeded: ipv4_latency_ms=%d ipv6_latency_ms=%d", ipv4Result.LatencyMS, ipv6Result.LatencyMS)
	return nil
}

func startBackend(ctx context.Context, cfg config, cleanup *[]func()) error {
	switch cfg.backend {
	case "binary":
		return startBinaryBackend(ctx, cfg, cleanup)
	case "container":
		return startContainerBackend(ctx, cfg, cleanup)
	default:
		return fmt.Errorf("unsupported backend %q", cfg.backend)
	}
}

func startBinaryBackend(ctx context.Context, cfg config, cleanup *[]func()) error {
	log.Printf("starting staging backend %s", cfg.serverBinary)

	// #nosec G204 -- staging server binary path is produced by trusted CI/local integration setup.
	cmd := exec.CommandContext(
		ctx,
		cfg.serverBinary,
		"-listen4", net.JoinHostPort(cfg.ipv4Host, fmt.Sprint(cfg.ipv4Port)),
		"-listen6", net.JoinHostPort(cfg.ipv6Host, fmt.Sprint(cfg.ipv6Port)),
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start staging backend: %w", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	*cleanup = append(*cleanup, func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		select {
		case <-done:
		case <-time.After(5 * time.Second):
		}
	})

	if err := waitForTCP(ctx, "tcp4", net.JoinHostPort(cfg.ipv4Host, fmt.Sprint(cfg.ipv4Port))); err != nil {
		return err
	}
	if err := waitForTCP(ctx, "tcp6", net.JoinHostPort(cfg.ipv6Host, fmt.Sprint(cfg.ipv6Port))); err != nil {
		return err
	}
	return nil
}

func startContainerBackend(ctx context.Context, cfg config, cleanup *[]func()) error {
	*cleanup = append(*cleanup, func() {
		_ = removeContainer(context.Background(), cfg.containerCLI, cfg.containerName)
	})

	if err := loadImage(ctx, cfg.containerCLI, cfg.imageArchive); err != nil {
		return err
	}
	if err := removeContainer(ctx, cfg.containerCLI, cfg.containerName); err != nil {
		return err
	}
	if err := startContainer(ctx, cfg); err != nil {
		return err
	}

	relay, err := newIPv6Relay(cfg.ipv6Host, cfg.ipv6Port, cfg.ipv4Host, cfg.ipv4Port)
	if err != nil {
		return err
	}
	*cleanup = append(*cleanup, relay.close)

	if err := waitForTCP(ctx, "tcp4", net.JoinHostPort(cfg.ipv4Host, fmt.Sprint(cfg.ipv4Port))); err != nil {
		return err
	}
	if err := waitForTCP(ctx, "tcp6", net.JoinHostPort(cfg.ipv6Host, fmt.Sprint(cfg.ipv6Port))); err != nil {
		return err
	}
	return nil
}

func resolveSingleFile(glob string) (string, error) {
	matches, err := filepath.Glob(glob)
	if err != nil {
		return "", fmt.Errorf("invalid glob %q: %w", glob, err)
	}
	if len(matches) == 0 {
		return "", fmt.Errorf("no files matched %q", glob)
	}
	if len(matches) > 1 {
		return "", fmt.Errorf("glob %q matched multiple files: %v", glob, matches)
	}
	return matches[0], nil
}

func extractBinary(archivePath, binaryName string) (string, func(), error) {
	tempDir, err := os.MkdirTemp("", "minecraft-ping-release-integration-*")
	if err != nil {
		return "", nil, fmt.Errorf("create temp dir: %w", err)
	}

	cleanup := func() {
		_ = os.RemoveAll(tempDir)
	}

	var extracted string
	switch {
	case strings.HasSuffix(archivePath, ".zip"):
		extracted, err = extractZipBinary(archivePath, tempDir, binaryName)
	case strings.HasSuffix(archivePath, ".tar.gz"):
		extracted, err = extractTarGzBinary(archivePath, tempDir, binaryName)
	default:
		err = fmt.Errorf("unsupported archive format: %s", archivePath)
	}
	if err != nil {
		cleanup()
		return "", nil, err
	}
	return extracted, cleanup, nil
}

func extractZipBinary(archivePath, tempDir, binaryName string) (string, error) {
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return "", fmt.Errorf("open zip archive: %w", err)
	}
	defer reader.Close()

	targetRoot, err := os.OpenRoot(tempDir)
	if err != nil {
		return "", fmt.Errorf("open temp dir root: %w", err)
	}
	defer targetRoot.Close()

	for _, file := range reader.File {
		if filepath.Base(file.Name) != binaryName {
			continue
		}
		if file.UncompressedSize64 == 0 || file.UncompressedSize64 > uint64(maxExtractedBinarySize) {
			return "", fmt.Errorf("binary %q in %s has invalid size %d", binaryName, archivePath, file.UncompressedSize64)
		}

		source, err := file.Open()
		if err != nil {
			return "", fmt.Errorf("open zipped file: %w", err)
		}
		defer source.Close()

		targetPath := filepath.Join(tempDir, binaryName)
		target, err := targetRoot.OpenFile(binaryName, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
		if err != nil {
			return "", fmt.Errorf("create extracted binary: %w", err)
		}
		if err := copyWithLimit(target, source, maxExtractedBinarySize); err != nil {
			_ = target.Close()
			return "", fmt.Errorf("extract zipped binary: %w", err)
		}
		if err := target.Close(); err != nil {
			return "", fmt.Errorf("close extracted binary: %w", err)
		}
		if err := targetRoot.Chmod(binaryName, 0o700); err != nil {
			return "", fmt.Errorf("chmod extracted binary: %w", err)
		}
		return targetPath, nil
	}

	return "", fmt.Errorf("binary %q not found in %s", binaryName, archivePath)
}

func extractTarGzBinary(archivePath, tempDir, binaryName string) (string, error) {
	archive, err := openReadOnlyFile(archivePath)
	if err != nil {
		return "", fmt.Errorf("open tar.gz archive: %w", err)
	}
	defer archive.Close()

	gzipReader, err := gzip.NewReader(archive)
	if err != nil {
		return "", fmt.Errorf("open gzip stream: %w", err)
	}
	defer gzipReader.Close()

	tarReader := tar.NewReader(gzipReader)
	targetRoot, err := os.OpenRoot(tempDir)
	if err != nil {
		return "", fmt.Errorf("open temp dir root: %w", err)
	}
	defer targetRoot.Close()

	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", fmt.Errorf("read tar header: %w", err)
		}
		if filepath.Base(header.Name) != binaryName {
			continue
		}
		if header.Size <= 0 || header.Size > maxExtractedBinarySize {
			return "", fmt.Errorf("binary %q in %s has invalid size %d", binaryName, archivePath, header.Size)
		}

		targetPath := filepath.Join(tempDir, binaryName)
		target, err := targetRoot.OpenFile(binaryName, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
		if err != nil {
			return "", fmt.Errorf("create extracted binary: %w", err)
		}
		if _, err := io.CopyN(target, tarReader, header.Size); err != nil {
			_ = target.Close()
			return "", fmt.Errorf("extract tarred binary: %w", err)
		}
		if err := target.Close(); err != nil {
			return "", fmt.Errorf("close extracted binary: %w", err)
		}
		if err := targetRoot.Chmod(binaryName, 0o700); err != nil {
			return "", fmt.Errorf("chmod extracted binary: %w", err)
		}
		return targetPath, nil
	}

	return "", fmt.Errorf("binary %q not found in %s", binaryName, archivePath)
}

func loadImage(ctx context.Context, containerCLI, archivePath string) error {
	if archivePath == "" {
		return nil
	}

	log.Printf("loading image archive %s with %s", archivePath, containerCLI)

	imageArchive, err := openReadOnlyFile(archivePath)
	if err != nil {
		return fmt.Errorf("open image archive: %w", err)
	}
	defer imageArchive.Close()

	gzipReader, err := gzip.NewReader(imageArchive)
	if err != nil {
		return fmt.Errorf("open image archive gzip stream: %w", err)
	}
	defer gzipReader.Close()

	// #nosec G204 -- container CLI comes from trusted CI/local test configuration for this integration harness.
	cmd := exec.CommandContext(ctx, containerCLI, "load")
	cmd.Stdin = gzipReader
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s load: %w", containerCLI, err)
	}
	return nil
}

func removeContainer(ctx context.Context, containerCLI, containerName string) error {
	// #nosec G204 -- container CLI and name are controlled by the integration harness configuration.
	cmd := exec.CommandContext(ctx, containerCLI, "rm", "-f", containerName)
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return nil
		}
		var pathErr *exec.Error
		if errors.As(err, &pathErr) {
			return err
		}
	}
	return nil
}

func startContainer(ctx context.Context, cfg config) error {
	log.Printf("starting minecraft container %s using %s", cfg.containerName, cfg.containerCLI)

	// #nosec G204 -- image tag and container CLI are chosen by CI/local integration configuration.
	cmd := exec.CommandContext(
		ctx,
		cfg.containerCLI,
		"run",
		"-d",
		"--name", cfg.containerName,
		"-p", fmt.Sprintf("127.0.0.1:%d:25565", cfg.ipv4Port),
		cfg.imageTag,
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s run: %w", cfg.containerCLI, err)
	}
	return nil
}

func waitForTCP(ctx context.Context, network, address string) error {
	log.Printf("waiting for %s listener on %s", network, address)

	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		dialer := net.Dialer{Timeout: 2 * time.Second}
		conn, err := dialer.DialContext(ctx, network, address)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	return fmt.Errorf("timed out waiting for %s listener on %s", network, address)
}

func runProbe(ctx context.Context, binaryPath, host string, port int, timeout time.Duration, familyFlag string) (pingResult, error) {
	log.Printf("running %s probe with %s against %s:%d", familyFlag, binaryPath, host, port)

	commandCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	// #nosec G204 -- extracted binary path is selected by the harness from local release artifacts under test.
	cmd := exec.CommandContext(
		commandCtx,
		binaryPath,
		"-j",
		"-W", strconv.FormatFloat(timeout.Seconds(), 'f', -1, 64),
		familyFlag,
		net.JoinHostPort(host, fmt.Sprint(port)),
	)

	stdout, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return pingResult{}, fmt.Errorf("command failed: %w: %s", err, strings.TrimSpace(string(exitErr.Stderr)))
		}
		return pingResult{}, err
	}

	var result pingResult
	if err := json.Unmarshal(stdout, &result); err != nil {
		return pingResult{}, fmt.Errorf("decode probe output %q: %w", strings.TrimSpace(string(stdout)), err)
	}
	return result, nil
}

type ipv6Relay struct {
	listener net.Listener
	wg       sync.WaitGroup
}

func newIPv6Relay(listenHost string, listenPort int, targetHost string, targetPort int) (*ipv6Relay, error) {
	address := net.JoinHostPort(listenHost, fmt.Sprint(listenPort))
	listenConfig := net.ListenConfig{
		Control: func(network, address string, rawConn syscall.RawConn) error {
			var controlErr error
			if network != "tcp6" {
				return nil
			}
			if err := rawConn.Control(func(fd uintptr) {
				controlErr = setIPv6Only(fd)
			}); err != nil {
				return err
			}
			return controlErr
		},
	}

	listener, err := listenConfig.Listen(context.Background(), "tcp6", address)
	if err != nil {
		return nil, fmt.Errorf("listen on ipv6 relay %s: %w", address, err)
	}

	relay := &ipv6Relay{listener: listener}
	targetAddress := net.JoinHostPort(targetHost, fmt.Sprint(targetPort))
	relay.wg.Add(1)
	go func() {
		defer relay.wg.Done()
		for {
			client, err := listener.Accept()
			if err != nil {
				return
			}

			relay.wg.Add(1)
			go func(client net.Conn) {
				defer relay.wg.Done()
				defer client.Close()

				upstream, err := net.Dial("tcp4", targetAddress)
				if err != nil {
					log.Printf("ipv6 relay dial failed: %v", err)
					return
				}
				defer upstream.Close()

				proxyConns(client, upstream)
			}(client)
		}
	}()

	return relay, nil
}

func proxyConns(a, b net.Conn) {
	var proxyWG sync.WaitGroup
	copyStream := func(dst, src net.Conn) {
		defer proxyWG.Done()
		_, _ = io.Copy(dst, src)
		_ = dst.SetDeadline(time.Now())
		_ = src.SetDeadline(time.Now())
	}

	proxyWG.Add(2)
	go copyStream(a, b)
	go copyStream(b, a)
	proxyWG.Wait()
}

func (r *ipv6Relay) close() {
	if r == nil {
		return
	}
	_ = r.listener.Close()
	r.wg.Wait()
}

func openReadOnlyFile(path string) (*os.File, error) {
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return nil, err
	}

	file, err := root.Open(filepath.Base(path))
	if err != nil {
		_ = root.Close()
		return nil, err
	}
	if err := root.Close(); err != nil {
		_ = file.Close()
		return nil, err
	}
	return file, nil
}

func copyWithLimit(dst io.Writer, src io.Reader, limit int64) error {
	written, err := io.Copy(dst, io.LimitReader(src, limit+1))
	if err != nil {
		return err
	}
	if written > limit {
		return fmt.Errorf("copied %d bytes, exceeds limit %d", written, limit)
	}
	return nil
}
