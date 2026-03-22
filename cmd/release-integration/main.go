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

	"github.com/omkhar/minecraft-ping/internal/stagingserver"
)

type config struct {
	archiveGlob     string
	backend         string
	binaryName      string
	binaryPath      string
	containerCLI    string
	containerName   string
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

func main() {
	cfg := config{
		containerName: fmt.Sprintf("minecraft-ping-release-integration-%d", os.Getpid()),
	}

	flag.StringVar(&cfg.backend, "backend", "binary", "Integration backend: binary or container")
	flag.StringVar(&cfg.archiveGlob, "binary-archive-glob", "", "Glob that resolves to a release archive containing the binary to execute")
	flag.StringVar(&cfg.binaryName, "binary-name", "", "Binary name inside the release archive")
	flag.StringVar(&cfg.binaryPath, "binary", "", "Path to an already extracted binary to execute")
	flag.StringVar(&cfg.containerCLI, "container-cli", "docker", "Container CLI used to run the Minecraft container")
	flag.StringVar(&cfg.containerName, "container-name", cfg.containerName, "Container name used for the integration target")
	flag.StringVar(&cfg.imageArchive, "image-archive", "", "Optional path to a compressed docker/podman image archive (.tar.gz)")
	flag.StringVar(&cfg.imageTag, "image-tag", "minecraft-staging-image:ci", "Tag of the staging image to run after it is loaded")
	flag.StringVar(&cfg.ipv4Host, "ipv4-host", "127.0.0.1", "IPv4 hostname used by the released binary")
	flag.StringVar(&cfg.ipv6Host, "ipv6-host", "::1", "IPv6 hostname used by the released binary")
	flag.IntVar(&cfg.javaIPv4Port, "java-ipv4-port", 45565, "Java IPv4 host port used by the released binary")
	flag.IntVar(&cfg.javaIPv6Port, "java-ipv6-port", 45566, "Java IPv6 host port used by the released binary")
	flag.IntVar(&cfg.bedrockIPv4Port, "bedrock-ipv4-port", 49132, "Bedrock IPv4 host port used by the released binary")
	flag.IntVar(&cfg.bedrockIPv6Port, "bedrock-ipv6-port", 49133, "Bedrock IPv6 host port used by the released binary")
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

	results := make([]string, 0, len(probeSpecs(cfg)))
	for _, spec := range probeSpecs(cfg) {
		result, err := runProbe(ctx, binaryPath, cfg.probeTimeout, spec)
		if err != nil {
			return fmt.Errorf("%s probe failed: %w", spec.label, err)
		}
		results = append(results, fmt.Sprintf("%s_latency_ms=%d", spec.label, result.LatencyMS))
	}

	log.Printf("release integration succeeded: %s", strings.Join(results, " "))
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
		"-listen4", net.JoinHostPort(cfg.ipv4Host, fmt.Sprint(cfg.javaIPv4Port)),
		"-listen6", net.JoinHostPort(cfg.ipv6Host, fmt.Sprint(cfg.javaIPv6Port)),
		"-bedrock-listen4", net.JoinHostPort(cfg.ipv4Host, fmt.Sprint(cfg.bedrockIPv4Port)),
		"-bedrock-listen6", net.JoinHostPort(cfg.ipv6Host, fmt.Sprint(cfg.bedrockIPv6Port)),
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

	if err := waitForJava(ctx, "tcp4", cfg.ipv4Host, cfg.javaIPv4Port); err != nil {
		return err
	}
	if err := waitForJava(ctx, "tcp6", cfg.ipv6Host, cfg.javaIPv6Port); err != nil {
		return err
	}
	if err := waitForBedrock(ctx, "udp4", cfg.ipv4Host, cfg.bedrockIPv4Port); err != nil {
		return err
	}
	if err := waitForBedrock(ctx, "udp6", cfg.ipv6Host, cfg.bedrockIPv6Port); err != nil {
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

	relay, err := newIPv6Relay(cfg.ipv6Host, cfg.javaIPv6Port, cfg.ipv4Host, cfg.javaIPv4Port)
	if err != nil {
		return err
	}
	*cleanup = append(*cleanup, relay.close)

	bedrockRelay, err := newUDPIPv6Relay(cfg.ipv6Host, cfg.bedrockIPv6Port, cfg.ipv4Host, cfg.bedrockIPv4Port)
	if err != nil {
		return err
	}
	*cleanup = append(*cleanup, bedrockRelay.close)

	if err := waitForJava(ctx, "tcp4", cfg.ipv4Host, cfg.javaIPv4Port); err != nil {
		return err
	}
	if err := waitForJava(ctx, "tcp6", cfg.ipv6Host, cfg.javaIPv6Port); err != nil {
		return err
	}
	if err := waitForBedrock(ctx, "udp4", cfg.ipv4Host, cfg.bedrockIPv4Port); err != nil {
		return err
	}
	if err := waitForBedrock(ctx, "udp6", cfg.ipv6Host, cfg.bedrockIPv6Port); err != nil {
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
	output, err := cmd.CombinedOutput()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			message := strings.TrimSpace(string(output))
			if isContainerNotFoundError(message) {
				return nil
			}
			return fmt.Errorf("%s rm -f %s: %w: %s", containerCLI, containerName, err, message)
		}
		var pathErr *exec.Error
		if errors.As(err, &pathErr) {
			return err
		}
		return err
	}
	return nil
}

func isContainerNotFoundError(message string) bool {
	lower := strings.ToLower(message)
	return strings.Contains(lower, "no such container") ||
		strings.Contains(lower, "no container with name or id")
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
		"-p", fmt.Sprintf("127.0.0.1:%d:25565/tcp", cfg.javaIPv4Port),
		"-p", fmt.Sprintf("127.0.0.1:%d:19132/udp", cfg.bedrockIPv4Port),
		cfg.imageTag,
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s run: %w", cfg.containerCLI, err)
	}
	return nil
}

func waitForJava(ctx context.Context, network, host string, port int) error {
	address := net.JoinHostPort(host, fmt.Sprint(port))
	log.Printf("waiting for java %s listener on %s", network, address)

	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		if err := stagingserver.Probe(network, host, port, 2*time.Second); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	return fmt.Errorf("timed out waiting for java %s listener on %s", network, address)
}

func waitForBedrock(ctx context.Context, network, host string, port int) error {
	address := net.JoinHostPort(host, fmt.Sprint(port))
	log.Printf("waiting for bedrock %s listener on %s", network, address)

	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		if err := stagingserver.ProbeBedrock(network, host, port, 2*time.Second); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	return fmt.Errorf("timed out waiting for bedrock %s listener on %s", network, address)
}

func runProbe(ctx context.Context, binaryPath string, timeout time.Duration, spec probeSpec) (pingResult, error) {
	log.Printf("running %s probe with %s against %s:%d", spec.label, binaryPath, spec.host, spec.port)

	commandCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	args := []string{
		"-j",
		"-W", strconv.FormatFloat(timeout.Seconds(), 'f', -1, 64),
	}
	if spec.editionArg != "" {
		args = append(args, spec.editionArg)
	}
	args = append(args, spec.familyFlag, net.JoinHostPort(spec.host, fmt.Sprint(spec.port)))

	// #nosec G204 -- extracted binary path is selected by the harness from local release artifacts under test.
	cmd := exec.CommandContext(commandCtx, binaryPath, args...)

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

type udpIPv6Relay struct {
	conn       net.PacketConn
	targetAddr *net.UDPAddr
	wg         sync.WaitGroup
}

func newUDPIPv6Relay(listenHost string, listenPort int, targetHost string, targetPort int) (*udpIPv6Relay, error) {
	address := net.JoinHostPort(listenHost, fmt.Sprint(listenPort))
	listenConfig := net.ListenConfig{
		Control: func(network, address string, rawConn syscall.RawConn) error {
			var controlErr error
			if network != "udp6" {
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

	conn, err := listenConfig.ListenPacket(context.Background(), "udp6", address)
	if err != nil {
		return nil, fmt.Errorf("listen on udp ipv6 relay %s: %w", address, err)
	}

	targetAddr, err := net.ResolveUDPAddr("udp4", net.JoinHostPort(targetHost, fmt.Sprint(targetPort)))
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("resolve udp relay target %s:%d: %w", targetHost, targetPort, err)
	}

	relay := &udpIPv6Relay{
		conn:       conn,
		targetAddr: targetAddr,
	}
	relay.wg.Add(1)
	go relay.serve()
	return relay, nil
}

func (r *udpIPv6Relay) serve() {
	defer r.wg.Done()

	var buf [2048]byte
	for {
		n, clientAddr, err := r.conn.ReadFrom(buf[:])
		if err != nil {
			return
		}
		if err := r.forwardPacket(buf[:n], clientAddr); err != nil {
			log.Printf("udp ipv6 relay failed: %v", err)
		}
	}
}

func (r *udpIPv6Relay) forwardPacket(payload []byte, clientAddr net.Addr) error {
	upstream, err := net.DialUDP("udp4", nil, r.targetAddr)
	if err != nil {
		return err
	}
	defer upstream.Close()

	if err := upstream.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return err
	}
	if _, err := upstream.Write(payload); err != nil {
		return err
	}

	var reply [2048]byte
	n, _, err := upstream.ReadFromUDP(reply[:])
	if err != nil {
		return err
	}
	_, err = r.conn.WriteTo(reply[:n], clientAddr)
	return err
}

func (r *udpIPv6Relay) close() {
	if r == nil {
		return
	}
	_ = r.conn.Close()
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
