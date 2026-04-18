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

	"github.com/omkhar/minecraft-ping/v2/internal/stagingserver"
)

const (
	listenerReadyTimeout    = 2 * time.Minute
	listenerProbeTimeout    = 2 * time.Second
	listenerRetryInterval   = 500 * time.Millisecond
	udpRelayDeadlineTimeout = 5 * time.Second
)

type listenerProbeFunc func(network, host string, port int, timeout time.Duration) error
type udpRelayConn interface {
	deadlineSetter
	Write([]byte) (int, error)
	ReadFromUDP([]byte) (int, *net.UDPAddr, error)
	Close() error
}

var (
	releaseTimeNow   = time.Now
	releaseTimeAfter = time.After

	javaListenerProbe    listenerProbeFunc = stagingserver.Probe
	bedrockListenerProbe listenerProbeFunc = stagingserver.ProbeBedrock

	releaseValidateConfig      = validateConfig
	releaseRun                 = run
	releaseNotifyContext       = signal.NotifyContext
	releaseResolveSingleFile   = resolveSingleFile
	releaseVersionFromArchive  = versionFromArchiveName
	releaseExtractBinary       = extractBinary
	releaseAssertVersion       = assertVersion
	releaseStartBackend        = startBackend
	releaseRunProbe            = runProbe
	releaseWaitForJava         = waitForJava
	releaseWaitForBedrock      = waitForBedrock
	releaseLoadImage           = loadImage
	releaseRemoveContainer     = removeContainer
	releaseStartContainer      = startContainer
	releaseNewIPv6Relay        = newIPv6Relay
	releaseNewUDPIPv6Relay     = newUDPIPv6Relay
	releaseMkdirTemp           = os.MkdirTemp
	releaseZipOpenReader       = zip.OpenReader
	releaseOpenRoot            = os.OpenRoot
	releaseOpenReadOnlyArchive = openReadOnlyFile
	releaseGzipNewReader       = gzip.NewReader
	releaseZipFileOpen         = func(file *zip.File) (io.ReadCloser, error) { return file.Open() }
	releaseRootOpen            = func(root *os.Root, name string) (*os.File, error) { return root.Open(name) }
	releaseRootClose           = func(root *os.Root) error { return root.Close() }
	releaseRootOpenFile        = func(root *os.Root, name string, flag int, perm os.FileMode) (*os.File, error) {
		return root.OpenFile(name, flag, perm)
	}
	releaseRootChmod = func(root *os.Root, name string, mode os.FileMode) error {
		return root.Chmod(name, mode)
	}
	releaseCopyWithLimit = copyWithLimit
	releaseCopyN         = io.CopyN
	releaseFileClose     = func(file *os.File) error { return file.Close() }
	releaseSetIPv6Only   = setIPv6Only
	releaseListen        = func(cfg net.ListenConfig, ctx context.Context, network, address string) (net.Listener, error) {
		return cfg.Listen(ctx, network, address)
	}
	releaseListenPacket = func(cfg net.ListenConfig, ctx context.Context, network, address string) (net.PacketConn, error) {
		return cfg.ListenPacket(ctx, network, address)
	}
	releaseResolveUDPAddr = net.ResolveUDPAddr
	releaseDialTCP        = net.Dial
	releaseDialUDP        = func(network string, laddr, raddr *net.UDPAddr) (udpRelayConn, error) {
		return net.DialUDP(network, laddr, raddr)
	}
)

func main() {
	os.Exit(mainWithArgs(os.Args))
}

func mainWithArgs(args []string) int {
	return runCLI(args[1:])
}

func runCLI(args []string) int {
	cfg := config{
		containerName: fmt.Sprintf("minecraft-ping-release-integration-%d", os.Getpid()),
	}
	fs := flag.NewFlagSet("release-integration", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	bindFlags(fs, &cfg)
	if err := fs.Parse(args); err != nil {
		log.Print(err)
		return 2
	}

	if err := releaseValidateConfig(cfg); err != nil {
		log.Print(err)
		return 2
	}

	ctx, stop := releaseNotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := releaseRun(ctx, cfg); err != nil {
		log.Print(err)
		return 1
	}
	return 0
}

func run(ctx context.Context, cfg config) error {
	binaryPath := cfg.binaryPath
	cleanup := make([]func(), 0, 4)

	if cfg.archiveGlob != "" {
		archivePath, err := releaseResolveSingleFile(cfg.archiveGlob)
		if err != nil {
			return err
		}
		if cfg.expectedVersion == "" {
			expectedVersion, err := releaseVersionFromArchive(archivePath)
			if err != nil {
				return err
			}
			cfg.expectedVersion = expectedVersion
		}

		extractedBinary, removeTempDir, err := releaseExtractBinary(archivePath, cfg.binaryName)
		if err != nil {
			return err
		}
		binaryPath = extractedBinary
		cleanup = append(cleanup, removeTempDir)
	}

	defer func() {
		runCleanup(cleanup)
	}()

	if cfg.expectedVersion != "" {
		if err := releaseAssertVersion(ctx, binaryPath, cfg.expectedVersion); err != nil {
			return err
		}
	}

	if err := releaseStartBackend(ctx, cfg, &cleanup); err != nil {
		return err
	}

	for _, spec := range probeSpecs(cfg) {
		if _, err := releaseRunProbe(ctx, binaryPath, cfg.probeTimeout, spec); err != nil {
			return fmt.Errorf("%s probe failed: %w", spec.label, err)
		}
	}
	return nil
}

func versionFromArchiveName(archivePath string) (string, error) {
	base := filepath.Base(archivePath)

	const prefix = "minecraft-ping_"
	if !strings.HasPrefix(base, prefix) {
		return "", fmt.Errorf("derive version from archive %q: unexpected name", archivePath)
	}

	for _, marker := range []string{"_Darwin_", "_Linux_", "_Windows_"} {
		if before, _, ok := strings.Cut(base, marker); ok {
			return strings.TrimPrefix(before, prefix), nil
		}
	}

	return "", fmt.Errorf("derive version from archive %q: unsupported name", archivePath)
}

func assertVersion(ctx context.Context, binaryPath, expectedVersion string) error {
	commandCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// #nosec G204 -- binary path is selected by the integration harness from local release artifacts under test.
	cmd := execCommandContext(commandCtx, binaryPath, "-V")
	stdout, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return fmt.Errorf("version check failed: %w: %s", err, strings.TrimSpace(string(exitErr.Stderr)))
		}
		return fmt.Errorf("run version check: %w", err)
	}

	got := strings.TrimSpace(string(stdout))
	want := versionLine(expectedVersion)
	if got != want {
		return fmt.Errorf("version check failed: got %q, want %q", got, want)
	}
	return nil
}

func versionLine(version string) string {
	return fmt.Sprintf("minecraft-ping %s", version)
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
	// #nosec G204 -- staging server binary path is produced by trusted CI/local integration setup.
	cmd := execCommandContext(
		ctx,
		cfg.serverBinary,
		"-listen4", net.JoinHostPort(cfg.ipv4Host, fmt.Sprint(cfg.javaIPv4Port)),
		"-listen6", net.JoinHostPort(cfg.ipv6Host, fmt.Sprint(cfg.javaIPv6Port)),
		"-bedrock-listen4", net.JoinHostPort(cfg.ipv4Host, fmt.Sprint(cfg.bedrockIPv4Port)),
		"-bedrock-listen6", net.JoinHostPort(cfg.ipv6Host, fmt.Sprint(cfg.bedrockIPv6Port)),
	)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start staging backend: %w", err)
	}

	*cleanup = append(*cleanup, func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
	})

	if err := releaseWaitForJava(ctx, "tcp4", cfg.ipv4Host, cfg.javaIPv4Port); err != nil {
		return err
	}
	if err := releaseWaitForJava(ctx, "tcp6", cfg.ipv6Host, cfg.javaIPv6Port); err != nil {
		return err
	}
	if err := releaseWaitForBedrock(ctx, "udp4", cfg.ipv4Host, cfg.bedrockIPv4Port); err != nil {
		return err
	}
	if err := releaseWaitForBedrock(ctx, "udp6", cfg.ipv6Host, cfg.bedrockIPv6Port); err != nil {
		return err
	}
	return nil
}

func startContainerBackend(ctx context.Context, cfg config, cleanup *[]func()) error {
	*cleanup = append(*cleanup, func() {
		_ = releaseRemoveContainer(context.Background(), cfg.containerCLI, cfg.containerName)
	})

	if err := releaseLoadImage(ctx, cfg.containerCLI, cfg.imageArchive); err != nil {
		return err
	}
	if err := releaseRemoveContainer(ctx, cfg.containerCLI, cfg.containerName); err != nil {
		return err
	}
	if err := releaseStartContainer(ctx, cfg); err != nil {
		return err
	}

	relay, err := releaseNewIPv6Relay(cfg.ipv6Host, cfg.javaIPv6Port, cfg.ipv4Host, cfg.javaIPv4Port)
	if err != nil {
		return err
	}
	*cleanup = append(*cleanup, relay.close)

	bedrockRelay, err := releaseNewUDPIPv6Relay(cfg.ipv6Host, cfg.bedrockIPv6Port, cfg.ipv4Host, cfg.bedrockIPv4Port)
	if err != nil {
		return err
	}
	*cleanup = append(*cleanup, bedrockRelay.close)

	if err := releaseWaitForJava(ctx, "tcp4", cfg.ipv4Host, cfg.javaIPv4Port); err != nil {
		return err
	}
	if err := releaseWaitForJava(ctx, "tcp6", cfg.ipv6Host, cfg.javaIPv6Port); err != nil {
		return err
	}
	if err := releaseWaitForBedrock(ctx, "udp4", cfg.ipv4Host, cfg.bedrockIPv4Port); err != nil {
		return err
	}
	if err := releaseWaitForBedrock(ctx, "udp6", cfg.ipv6Host, cfg.bedrockIPv6Port); err != nil {
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
	tempDir, err := releaseMkdirTemp("", "minecraft-ping-release-integration-*")
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
	reader, err := releaseZipOpenReader(archivePath)
	if err != nil {
		return "", fmt.Errorf("open zip archive: %w", err)
	}
	defer reader.Close()

	targetRoot, err := releaseOpenRoot(tempDir)
	if err != nil {
		return "", fmt.Errorf("open temp dir root: %w", err)
	}
	defer targetRoot.Close()

	for _, file := range reader.File {
		if file.Name != binaryName {
			continue
		}
		if file.UncompressedSize64 == 0 || file.UncompressedSize64 > uint64(maxExtractedBinarySize) {
			return "", fmt.Errorf("binary %q in %s has invalid size %d", binaryName, archivePath, file.UncompressedSize64)
		}

		source, err := releaseZipFileOpen(file)
		if err != nil {
			return "", fmt.Errorf("open zipped file: %w", err)
		}
		defer source.Close()

		targetPath := filepath.Join(tempDir, binaryName)
		target, err := releaseRootOpenFile(targetRoot, binaryName, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
		if err != nil {
			return "", fmt.Errorf("create extracted binary: %w", err)
		}
		if err := releaseCopyWithLimit(target, source, maxExtractedBinarySize); err != nil {
			_ = releaseFileClose(target)
			return "", fmt.Errorf("extract zipped binary: %w", err)
		}
		if err := releaseFileClose(target); err != nil {
			return "", fmt.Errorf("close extracted binary: %w", err)
		}
		if err := releaseRootChmod(targetRoot, binaryName, 0o700); err != nil {
			return "", fmt.Errorf("chmod extracted binary: %w", err)
		}
		return targetPath, nil
	}

	return "", fmt.Errorf("binary %q not found in %s", binaryName, archivePath)
}

func extractTarGzBinary(archivePath, tempDir, binaryName string) (string, error) {
	archive, err := releaseOpenReadOnlyArchive(archivePath)
	if err != nil {
		return "", fmt.Errorf("open tar.gz archive: %w", err)
	}
	defer archive.Close()

	gzipReader, err := releaseGzipNewReader(archive)
	if err != nil {
		return "", fmt.Errorf("open gzip stream: %w", err)
	}
	defer gzipReader.Close()

	tarReader := tar.NewReader(gzipReader)
	targetRoot, err := releaseOpenRoot(tempDir)
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
		if header.Name != binaryName {
			continue
		}
		if header.Size <= 0 || header.Size > maxExtractedBinarySize {
			return "", fmt.Errorf("binary %q in %s has invalid size %d", binaryName, archivePath, header.Size)
		}

		targetPath := filepath.Join(tempDir, binaryName)
		target, err := releaseRootOpenFile(targetRoot, binaryName, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
		if err != nil {
			return "", fmt.Errorf("create extracted binary: %w", err)
		}
		if _, err := releaseCopyN(target, tarReader, header.Size); err != nil {
			_ = releaseFileClose(target)
			return "", fmt.Errorf("extract tarred binary: %w", err)
		}
		if err := releaseFileClose(target); err != nil {
			return "", fmt.Errorf("close extracted binary: %w", err)
		}
		if err := releaseRootChmod(targetRoot, binaryName, 0o700); err != nil {
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

	imageArchive, err := releaseOpenReadOnlyArchive(archivePath)
	if err != nil {
		return fmt.Errorf("open image archive: %w", err)
	}
	defer imageArchive.Close()

	gzipReader, err := releaseGzipNewReader(imageArchive)
	if err != nil {
		return fmt.Errorf("open image archive gzip stream: %w", err)
	}
	defer gzipReader.Close()

	// #nosec G204 -- container CLI comes from trusted CI/local test configuration for this integration harness.
	cmd := execCommandContext(ctx, containerCLI, "load")
	cmd.Stdin = gzipReader
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s load: %w: %s", containerCLI, err, strings.TrimSpace(string(output)))
	}
	return nil
}

func removeContainer(ctx context.Context, containerCLI, containerName string) error {
	// #nosec G204 -- container CLI and name are controlled by the integration harness configuration.
	cmd := execCommandContext(ctx, containerCLI, "rm", "-f", containerName)
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
	// #nosec G204 -- image tag and container CLI are chosen by CI/local integration configuration.
	cmd := execCommandContext(
		ctx,
		cfg.containerCLI,
		"run",
		"-d",
		"--name", cfg.containerName,
		"-p", fmt.Sprintf("127.0.0.1:%d:25565/tcp", cfg.javaIPv4Port),
		"-p", fmt.Sprintf("127.0.0.1:%d:19132/udp", cfg.bedrockIPv4Port),
		cfg.imageTag,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s run: %w: %s", cfg.containerCLI, err, strings.TrimSpace(string(output)))
	}
	return nil
}

func waitForJava(ctx context.Context, network, host string, port int) error {
	return waitForListener(ctx, "java", javaListenerProbe, network, host, port)
}

func waitForBedrock(ctx context.Context, network, host string, port int) error {
	return waitForListener(ctx, "bedrock", bedrockListenerProbe, network, host, port)
}

func waitForListener(ctx context.Context, kind string, probe listenerProbeFunc, network, host string, port int) error {
	address := net.JoinHostPort(host, fmt.Sprint(port))

	deadline := releaseTimeNow().Add(listenerReadyTimeout)
	for releaseTimeNow().Before(deadline) {
		if err := probe(network, host, port, listenerProbeTimeout); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-releaseTimeAfter(listenerRetryInterval):
		}
	}
	return fmt.Errorf("timed out waiting for %s %s listener on %s", kind, network, address)
}

func ipv6OnlyControl(expectedNetwork string) func(string, string, syscall.RawConn) error {
	return func(network, address string, rawConn syscall.RawConn) error {
		var controlErr error
		if network != expectedNetwork {
			return nil
		}
		if err := rawConn.Control(func(fd uintptr) {
			controlErr = releaseSetIPv6Only(fd)
		}); err != nil {
			return err
		}
		return controlErr
	}
}

func runProbe(ctx context.Context, binaryPath string, timeout time.Duration, spec probeSpec) (pingResult, error) {
	commandCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	args := []string{
		"-j",
		"--allow-private",
		"-W", formatProbeTimeout(timeout),
	}
	if spec.editionArg != "" {
		args = append(args, spec.editionArg)
	}
	args = append(args, spec.familyFlag, net.JoinHostPort(spec.host, fmt.Sprint(spec.port)))

	// #nosec G204 -- extracted binary path is selected by the harness from local release artifacts under test.
	cmd := execCommandContext(commandCtx, binaryPath, args...)

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

func formatProbeTimeout(timeout time.Duration) string {
	formatted := strconv.FormatFloat(timeout.Seconds(), 'f', 3, 64)
	formatted = strings.TrimRight(formatted, "0")
	return strings.TrimRight(formatted, ".")
}

type ipv6Relay struct {
	listener net.Listener
	wg       sync.WaitGroup
}

func newIPv6Relay(listenHost string, listenPort int, targetHost string, targetPort int) (*ipv6Relay, error) {
	address := net.JoinHostPort(listenHost, fmt.Sprint(listenPort))
	listenConfig := net.ListenConfig{
		Control: ipv6OnlyControl("tcp6"),
	}

	listener, err := releaseListen(listenConfig, context.Background(), "tcp6", address)
	if err != nil {
		return nil, fmt.Errorf("listen on ipv6 relay %s: %w", address, err)
	}

	relay := &ipv6Relay{listener: listener}
	targetAddress := net.JoinHostPort(targetHost, fmt.Sprint(targetPort))
	relay.wg.Go(func() {
		relay.serve(targetAddress)
	})

	return relay, nil
}

func (r *ipv6Relay) serve(targetAddress string) {
	for {
		client, err := r.listener.Accept()
		if err != nil {
			return
		}

		r.wg.Go(func() {
			defer client.Close()

			upstream, err := releaseDialTCP("tcp4", targetAddress)
			if err != nil {
				log.Printf("ipv6 relay dial failed: %v", err)
				return
			}
			defer upstream.Close()

			proxyConns(client, upstream)
		})
	}
}

func proxyConns(a, b net.Conn) {
	var proxyWG sync.WaitGroup
	copyStream := func(dst, src net.Conn) {
		_, _ = io.Copy(dst, src)
		_ = dst.SetDeadline(time.Now())
		_ = src.SetDeadline(time.Now())
	}

	proxyWG.Go(func() { copyStream(a, b) })
	proxyWG.Go(func() { copyStream(b, a) })
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
		Control: ipv6OnlyControl("udp6"),
	}

	conn, err := releaseListenPacket(listenConfig, context.Background(), "udp6", address)
	if err != nil {
		return nil, fmt.Errorf("listen on udp ipv6 relay %s: %w", address, err)
	}

	targetAddr, err := releaseResolveUDPAddr("udp4", net.JoinHostPort(targetHost, fmt.Sprint(targetPort)))
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("resolve udp relay target %s:%d: %w", targetHost, targetPort, err)
	}

	relay := &udpIPv6Relay{
		conn:       conn,
		targetAddr: targetAddr,
	}
	relay.wg.Go(relay.serve)
	return relay, nil
}

func (r *udpIPv6Relay) serve() {
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
	upstream, err := releaseDialUDP("udp4", nil, r.targetAddr)
	if err != nil {
		return err
	}
	defer upstream.Close()

	if err := setUDPRelayDeadline(upstream); err != nil {
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
	root, err := releaseOpenRoot(filepath.Dir(path))
	if err != nil {
		return nil, err
	}

	file, err := releaseRootOpen(root, filepath.Base(path))
	if err != nil {
		_ = releaseRootClose(root)
		return nil, err
	}
	if err := releaseRootClose(root); err != nil {
		_ = releaseFileClose(file)
		return nil, err
	}
	return file, nil
}

func runCleanup(cleanup []func()) {
	for i := len(cleanup) - 1; i >= 0; i-- {
		cleanup[i]()
	}
}

type deadlineSetter interface {
	SetDeadline(time.Time) error
}

func setDeadlineFromNow(conn deadlineSetter, timeout time.Duration) error {
	return conn.SetDeadline(releaseTimeNow().Add(timeout))
}

func setUDPRelayDeadline(conn deadlineSetter) error {
	return setDeadlineFromNow(conn, udpRelayDeadlineTimeout)
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
