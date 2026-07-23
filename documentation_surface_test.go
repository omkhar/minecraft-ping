package main

import (
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

var liveDocumentationPaths = []string{
	".github/ISSUE_TEMPLATE/bug_report.yml",
	".github/ISSUE_TEMPLATE/config.yml",
	".github/ISSUE_TEMPLATE/feature_request.yml",
	".github/pull_request_template.md",
	"README.md",
	"CHANGELOG.md",
	"CODE_OF_CONDUCT.md",
	"CONTRIBUTING.md",
	"GOVERNANCE.md",
	"SECURITY.md",
	"SUPPORT.md",
	"docs/FUNCTIONS.md",
	"docs/LIMITATIONS.md",
	"docs/RUNTIME.md",
	"docs/STYLE.md",
	"docs/architecture.md",
	"docs/cli-reference.md",
	"docs/development.md",
	"docs/release-verification.md",
	"docs/releasing.md",
	"man/minecraft-ping.1",
}

var documentationInventoryExclusions = map[string]string{
	"AGENTS.md": "Agent control source. The agent-surface verification checks this file.",
	"CLAUDE.md": "Generated agent mirror. The agent-surface verification checks this file.",
	"GEMINI.md": "Generated agent mirror. The agent-surface verification checks this file.",
	"LICENSE":   "Apache License legal text. The repository must preserve the exact license.",
}

var supportedUserSurfaceIDs = []string{
	"command.defaults",
	"command.destination",
	"command.exit-status",
	"command.help",
	"command.json-mode",
	"command.no-subcommands",
	"command.options",
	"command.output",
	"command.precedence",
	"command.text-mode",
	"command.version",
	"deployment.none",
	"distribution.archives",
	"distribution.linux-packages",
	"distribution.source-archive",
	"distribution.source-install",
	"distribution.verification-assets",
	"library.none",
	"network.address-selection",
	"network.bedrock",
	"network.java",
	"runtime.pinned",
}

var provedLimitationPhrases = map[string][]string{
	"Product scope": {
		"measures one Minecraft protocol exchange",
		"does not detect the Minecraft edition",
		"One process accepts one destination",
		"does not log in",
		"does not report the server description",
		"does not prove that a player can join",
		"does not supply a proxy option",
		"does not do a reverse DNS lookup",
		"does not save session history",
	},
	"Command limits": {
		"default interval is one second",
		"count must be a positive integer",
		"count must fit the Go `int` type",
		"minimum is one nanosecond",
		"accepts exponent notation",
		"largest duration is `9,223,372,036.854775807` seconds",
		"rejects a nonfinite value",
		"maximum probe timeout is 30 seconds",
		"Do not use `-4` and `-6` together",
		"Use only one of `--edition`, `--java`, and `--bedrock`",
		"`--edition` accepts only `java` or `bedrock`",
		"JSON mode makes one probe",
		"options can occur before or after the destination",
		"one destination must be the final argument",
		"Invalid arguments print help to standard error",
		"Version output does not require a destination",
	},
	"Destination and address limits": {
		"server name can contain no more than 253 bytes",
		"cannot contain control characters or square brackets",
		"destination port must be from 1 through 65,535",
		"brackets when an IPv6 destination has an explicit port",
		"Java uses port `25565`",
		"Bedrock uses port `19132` for IPv4",
		"rejects these IPv4 prefixes",
		"rejects these IPv6 prefixes",
		"`--allow-private` disables this address filter",
		"address-family option applies to literal targets",
		"resolver result order selects the primary address family",
		"resolves the destination once before a text session",
	},
	"Java limits": {
		"sends a status handshake",
		"latency measures only the ping and pong exchange",
		"uses SRV only for a host name without an explicit port",
		"only the first SRV record",
		"handshake keeps the original host name",
		"attempts start 250 milliseconds apart",
		"status response must contain one JSON object",
		"maximum Java packet is 2 MiB",
		"maximum status JSON value is 1 MiB",
		"maximum handshake host value is 255 bytes",
		"VarInt can contain no more than five bytes",
		"exact random 64-bit token",
	},
	"Bedrock limits": {
		"RakNet unconnected ping and pong over UDP",
		"does not use SRV",
		"tries resolved addresses in sequence",
		"reads at most 2,048 bytes",
		"expected packet ID, timestamp, magic value",
		"six required `MCPE` fields",
		"must be decimal integers",
		"does not display them",
	},
	"Session and output limits": {
		"Text mode continues until a count",
		"minimum time from one probe start",
		"probe timeout starts after a TCP or UDP connection succeeds",
		"probe timeout does not limit DNS",
		"session deadline does not interrupt preparation",
		"session deadline can shorten socket input",
		"signal cancels DNS and connection setup",
		"does not print one error for each failed probe",
		"Quiet mode hides successful reply lines",
		"count and a deadline are both set",
		"returns status `1` when it gets no replies",
		"JSON mode returns status `2` for a prepare error",
		"JSON output contains the destination host",
		"Numeric mode can put the host name in the first banner",
		"JSON latency is an integer number of milliseconds",
		"population standard deviation for `mdev`",
		"ignores output-stream write errors",
	},
	"Staging and integration limits": {
		"`cmd/staging-server` is a test backend",
		"needs at least one Java or Bedrock listen address",
		"default per-connection deadline is 10 seconds",
		"valid JSON of at most 1 MiB",
		"must start with `MCPE;`",
		"exact 33-byte unconnected ping",
		"waits at most 2 minutes for a listener",
		"timeout for a listener check is 2 seconds",
		"repeats every 500 milliseconds",
		"default probe timeout of 12 seconds",
		"version command 30 seconds",
		"container image-load command 2 minutes",
		"UDP relay operation is 5 seconds",
		"extracts one exact entry name",
		"maximum extracted binary size is 64 MiB",
		"publishes only the Java IPv4 and Bedrock IPv4 ports",
	},
	"Release limits": {
		"macOS Arm64, Linux AMD64 and Arm64",
		"do not include Intel macOS",
		"`.deb`, `.rpm`, and `.apk` formats",
		"archives include the license, README, and man-page source",
		"packages install the command, license, README, and man page",
		"set `CGO_ENABLED=0`",
		"GitHub-verified signed, annotated tag",
		"publishes a draft only after archive",
		"attestations are not available for a private repository",
	},
	"Documentation and runtime contract limits": {
		"machine-readable runtime inventory",
		"restricted GitHub Actions YAML form",
		"three-line `run_container_smoke` call form",
		"named functions in non-test Go files",
	},
}

type releaseConfiguration struct {
	Builds []struct {
		Goos   []string `yaml:"goos"`
		Goarch []string `yaml:"goarch"`
		Ignore []struct {
			Goos   string `yaml:"goos"`
			Goarch string `yaml:"goarch"`
		} `yaml:"ignore"`
		Env []string `yaml:"env"`
	} `yaml:"builds"`
	Archives []struct {
		Formats         []string `yaml:"formats"`
		FormatOverrides []struct {
			Goos    string   `yaml:"goos"`
			Formats []string `yaml:"formats"`
		} `yaml:"format_overrides"`
		Files []struct {
			Source string `yaml:"src"`
		} `yaml:"files"`
	} `yaml:"archives"`
	NFPMS []struct {
		Formats  []string `yaml:"formats"`
		Contents []struct {
			Source      string `yaml:"src"`
			Destination string `yaml:"dst"`
		} `yaml:"contents"`
	} `yaml:"nfpms"`
	Checksum struct {
		NameTemplate string `yaml:"name_template"`
	} `yaml:"checksum"`
	Source struct {
		Enabled      bool   `yaml:"enabled"`
		NameTemplate string `yaml:"name_template"`
		Format       string `yaml:"format"`
	} `yaml:"source"`
	Signs []struct {
		Command   string `yaml:"cmd"`
		Signature string `yaml:"signature"`
		Artifacts string `yaml:"artifacts"`
	} `yaml:"signs"`
}

func TestDocumentationInventoryMatchesRepository(t *testing.T) {
	t.Parallel()

	actual, err := repositoryDocumentationPaths(".")
	if err != nil {
		t.Fatal(err)
	}
	expected := append([]string(nil), liveDocumentationPaths...)
	for path := range documentationInventoryExclusions {
		expected = append(expected, path)
	}
	if err := compareExactSets(expected, actual); err != nil {
		t.Fatalf("documentation inventory drift: %v", err)
	}

	for _, mirror := range []string{"CLAUDE.md", "GEMINI.md"} {
		data, err := os.ReadFile(mirror)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), "GENERATED FROM AGENTS.md") {
			t.Errorf("%s does not declare its generated source", mirror)
		}
	}
}

func TestIssueTemplateYAMLParses(t *testing.T) {
	t.Parallel()

	for _, path := range liveDocumentationPaths {
		if filepath.Ext(path) != ".yml" && filepath.Ext(path) != ".yaml" {
			continue
		}
		data, err := os.ReadFile(filepath.FromSlash(path))
		if err != nil {
			t.Errorf("read %s: %v", path, err)
			continue
		}
		var document yaml.Node
		if err := yaml.Unmarshal(data, &document); err != nil {
			t.Errorf("parse %s: %v", path, err)
		}
	}
}

func TestLiveDocumentationDeclaresSimplifiedTechnicalEnglish(t *testing.T) {
	t.Parallel()

	const declaration = "This document uses ASD-STE100 Simplified Technical English."
	for _, path := range liveDocumentationPaths {
		data, err := os.ReadFile(filepath.FromSlash(path))
		if err != nil {
			t.Errorf("read %s: %v", path, err)
			continue
		}
		if !strings.Contains(string(data), declaration) {
			t.Errorf("%s does not declare the documentation language", path)
		}
	}
}

func TestLiveDocumentationUsesControlledStyle(t *testing.T) {
	t.Parallel()

	for _, path := range liveDocumentationPaths {
		data, err := os.ReadFile(filepath.FromSlash(path))
		if err != nil {
			t.Errorf("read %s: %v", path, err)
			continue
		}
		for _, finding := range controlledStyleFindings(string(data)) {
			t.Errorf("%s: %s", path, finding)
		}
	}
}

func TestControlledStyleCheckerRejectsUnsupportedProse(t *testing.T) {
	t.Parallel()

	for name, prose := range map[string]string{
		"passive voice": "Reports will be handled by the maintainer.",
		"long sentence": "This sentence has more than twenty five separate words because it deliberately adds many unnecessary words that make the instruction difficult for a reader to understand quickly and correctly.",
		"semicolon":     "Run the test; then read the result.",
		"em dash":       "Use one option — do not use two options.",
	} {
		if findings := controlledStyleFindings(prose); len(findings) == 0 {
			t.Errorf("style checker accepted %s", name)
		}
	}
}

func TestStyleGuideDocumentsAutomatedBoundaries(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(filepath.Join("docs", "STYLE.md"))
	if err != nil {
		t.Fatal(err)
	}
	guide := string(data)
	for _, required := range []string{
		"25 words",
		"passive-voice patterns",
		"semicolons",
		"en dashes",
		"em dashes",
		"fenced code blocks",
		"link targets",
		"inline code",
		"pull request template",
		"issue templates",
		"Apache License",
		"generated agent mirrors",
	} {
		if !strings.Contains(guide, required) {
			t.Errorf("docs/STYLE.md does not state automated boundary %q", required)
		}
	}
}

func TestFunctionCatalogCoversProductionFunctions(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(filepath.Join("docs", "FUNCTIONS.md"))
	if err != nil {
		t.Fatal(err)
	}
	catalog := string(data)
	functions, err := productionFunctions(".")
	if err != nil {
		t.Fatal(err)
	}
	if err := compareFunctionCatalog(functions, catalog); err != nil {
		t.Fatal(err)
	}
}

func TestFunctionCatalogComparisonRejectsDrift(t *testing.T) {
	t.Parallel()

	valid := "| Function | Purpose |\n| --- | --- |\n| `main.one` | It returns one. |\n| `main.two` | It returns two. |\n"
	if err := compareFunctionCatalog([]string{"main.one", "main.two"}, valid); err != nil {
		t.Fatal(err)
	}
	if err := compareFunctionCatalog([]string{"main.one"}, valid); err == nil {
		t.Fatal("compareFunctionCatalog accepted a stale function")
	}
	if err := compareFunctionCatalog([]string{"main.one", "main.two", "main.three"}, valid); err == nil {
		t.Fatal("compareFunctionCatalog accepted a missing function")
	}
	duplicate := valid + "| `main.one` | It returns one again. |\n"
	if err := compareFunctionCatalog([]string{"main.one", "main.two"}, duplicate); err == nil {
		t.Fatal("compareFunctionCatalog accepted a duplicate function")
	}
}

func TestSupportedUserFunctionsMatchCommandContracts(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(filepath.Join("docs", "FUNCTIONS.md"))
	if err != nil {
		t.Fatal(err)
	}
	document := string(data)
	section := between(document, "## Supported user functions", "## Implementation function catalog")
	if section == "" {
		t.Fatal("docs/FUNCTIONS.md does not contain the supported user function inventory")
	}
	if err := compareExactSets(supportedUserSurfaceIDs, supportedSurfaceIDs(section)); err != nil {
		t.Fatalf("supported user surface drift: %v", err)
	}

	source, err := os.ReadFile("cli.go")
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := acceptedCLIOptions(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := compareExactSets(accepted, optionTokens(between(section, "### Command options", "### Other supported surfaces"))); err != nil {
		t.Fatalf("supported function option drift: %v", err)
	}
	if !strings.Contains(usageText(), "print unix timestamp before each reply line") {
		t.Fatal("help text does not describe the timestamp output boundary")
	}

	cfg, status := parseCLIConfig([]string{"example.com"})
	if status != parseStatusOK {
		t.Fatalf("default command status = %v, want OK", status)
	}
	if cfg.Edition != editionJava || cfg.Options.addressFamily != addressFamilyAny {
		t.Fatalf("default edition/family = %v/%v, want Java/automatic", cfg.Edition, cfg.Options.addressFamily)
	}
	if cfg.Count != 0 || cfg.Interval != time.Second || cfg.Deadline != 0 || cfg.Timeout != 5*time.Second {
		t.Fatalf("default count/interval/deadline/timeout = %d/%s/%s/%s", cfg.Count, cfg.Interval, cfg.Deadline, cfg.Timeout)
	}
	if _, status := parseCLIConfig([]string{"first.example", "subcommand"}); status != parseStatusInvalid {
		t.Fatalf("second positional argument status = %v, want invalid", status)
	}
	const explicitPort = 24444
	if got := newTargetSpec("example.com", explicitPort, true).defaultPort(addressFamily6, editionBedrock); got != explicitPort {
		t.Fatalf("explicit port precedence = %d, want %d", got, explicitPort)
	}
	if got := newTargetSpec("example.com", 0, false).defaultPort(addressFamilyAny, editionJava); got != defaultJavaPort {
		t.Fatalf("default Java port = %d, want %d", got, defaultJavaPort)
	}
	if got := newTargetSpec("example.com", 0, false).defaultPort(addressFamily4, editionBedrock); got != defaultBedrockPort {
		t.Fatalf("default Bedrock IPv4 port = %d, want %d", got, defaultBedrockPort)
	}
	if got := newTargetSpec("example.com", 0, false).defaultPort(addressFamily6, editionBedrock); got != defaultBedrockPortV6 {
		t.Fatalf("default Bedrock IPv6 port = %d, want %d", got, defaultBedrockPortV6)
	}
	if target, err := parseDestination("[2001:db8::1]"); err != nil || target.Host != "2001:db8::1" || target.PortExplicit {
		t.Fatalf("bracketed IPv6 destination = %+v, %v", target, err)
	}

	for _, mode := range []struct {
		name        string
		args        []string
		wantEdition edition
		wantJSON    bool
	}{
		{name: "Java text", args: []string{"example.com"}, wantEdition: editionJava},
		{name: "Java JSON", args: []string{"-j", "example.com"}, wantEdition: editionJava, wantJSON: true},
		{name: "Bedrock text", args: []string{"--bedrock", "example.com"}, wantEdition: editionBedrock},
		{name: "Bedrock JSON", args: []string{"--bedrock", "-j", "example.com"}, wantEdition: editionBedrock, wantJSON: true},
	} {
		t.Run(mode.name, func(t *testing.T) {
			cfg, status := parseCLIConfig(mode.args)
			if status != parseStatusOK || cfg.Edition != mode.wantEdition || cfg.JSON != mode.wantJSON {
				t.Fatalf("parseCLIConfig(%v) = %+v, %v", mode.args, cfg, status)
			}
		})
	}

	descriptions := supportedSurfaceDescriptions(section)
	for id, required := range map[string][]string{
		"command.defaults":       {"Java", "automatic address family", "one second", "five seconds", "no count", "no deadline"},
		"command.destination":    {"host", "host and port", "bracketed IPv6 with or without a port", "bare IPv6"},
		"command.exit-status":    {"Status `0`", "JSON probe failure", "no text replies", "count and deadline", "Status `2`", "argument or preparation failure"},
		"command.no-subcommands": {"no subcommands", "one destination"},
		"command.output":         {"standard output", "standard error", "write errors"},
		"command.precedence": {
			"before or after the destination",
			"final argument",
			"Explicit ports",
			"Do not use `-4` and `-6` together",
			"Use only one of `--edition`, `--java`, and `--bedrock`",
			"Do not combine `-j` with `-c`, `-i`, `-w`, `-q`, or `-D`",
			"status `2`",
		},
		"command.text-mode":           {"banner", "final summary", "Unless quiet", "successful replies"},
		"command.json-mode":           {"one probe", "successful probe", "one object"},
		"deployment.none":             {"does not publish a production server or container", "staging"},
		"distribution.archives":       {"macOS Arm64", "Linux AMD64", "Linux Arm64", "Windows AMD64", "Windows Arm64", "tar.gz", "ZIP"},
		"distribution.linux-packages": {"DEB", "RPM", "APK"},
		"distribution.source-archive": {"source archive", "tar.gz"},
		"distribution.verification-assets": {
			"checksums.txt",
			"Sigstore",
			"SPDX SBOM",
			"Public releases",
			"provenance",
		},
		"library.none":              {"does not expose a supported Go library API", "`main` package"},
		"network.address-selection": {"resolves", "non-public", "IPv4", "IPv6"},
		"network.bedrock":           {"RakNet", "UDP", "does not use SRV", "19132", "19133"},
		"network.java":              {"status handshake", "TCP", "SRV", "25565"},
		"runtime.pinned":            {"runtime inventory", "pinned"},
	} {
		description, ok := descriptions[id]
		if !ok {
			t.Errorf("supported surface %s has no description", id)
			continue
		}
		for _, phrase := range required {
			if !strings.Contains(description, phrase) {
				t.Errorf("supported surface %s does not contain %q: %q", id, phrase, description)
			}
		}
	}
}

func TestSupportedDistributionSurfaceMatchesGoReleaser(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(".goreleaser.yml")
	if err != nil {
		t.Fatal(err)
	}
	var config releaseConfiguration
	if err := yaml.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	if len(config.Builds) != 1 || len(config.Archives) != 1 || len(config.NFPMS) != 1 {
		t.Fatalf("release config build/archive/package counts = %d/%d/%d", len(config.Builds), len(config.Archives), len(config.NFPMS))
	}

	targets := make(map[string]bool)
	for _, goos := range config.Builds[0].Goos {
		for _, goarch := range config.Builds[0].Goarch {
			targets[goos+"/"+goarch] = true
		}
	}
	for _, ignored := range config.Builds[0].Ignore {
		delete(targets, ignored.Goos+"/"+ignored.Goarch)
	}
	if err := compareExactSets(
		[]string{"darwin/arm64", "linux/amd64", "linux/arm64", "windows/amd64", "windows/arm64"},
		sortedSet(targets),
	); err != nil {
		t.Fatalf("release target drift: %v", err)
	}
	if err := compareExactSets([]string{"apk", "deb", "rpm"}, config.NFPMS[0].Formats); err != nil {
		t.Fatalf("Linux package format drift: %v", err)
	}
	if err := compareExactSets([]string{"tar.gz"}, config.Archives[0].Formats); err != nil {
		t.Fatalf("default release archive format drift: %v", err)
	}
	if len(config.Archives[0].FormatOverrides) != 1 ||
		config.Archives[0].FormatOverrides[0].Goos != "windows" {
		t.Fatalf("release archive format overrides = %+v", config.Archives[0].FormatOverrides)
	}
	if err := compareExactSets([]string{"zip"}, config.Archives[0].FormatOverrides[0].Formats); err != nil {
		t.Fatalf("Windows release archive format drift: %v", err)
	}

	archiveFiles := make(map[string]bool)
	for _, file := range config.Archives[0].Files {
		archiveFiles[file.Source] = true
	}
	if err := compareExactSets([]string{"LICENSE", "README.md", "man/minecraft-ping.1"}, sortedSet(archiveFiles)); err != nil {
		t.Fatalf("release archive file drift: %v", err)
	}

	packageDestinations := make(map[string]bool)
	for _, file := range config.NFPMS[0].Contents {
		packageDestinations[file.Destination] = true
	}
	if err := compareExactSets(
		[]string{
			"/usr/share/doc/minecraft-ping/LICENSE",
			"/usr/share/doc/minecraft-ping/README.md",
			"/usr/share/man/man1/minecraft-ping.1",
		},
		sortedSet(packageDestinations),
	); err != nil {
		t.Fatalf("Linux package content drift: %v", err)
	}
	if err := compareExactSets([]string{"CGO_ENABLED=0"}, config.Builds[0].Env); err != nil {
		t.Fatalf("release build environment drift: %v", err)
	}
	if !config.Source.Enabled || config.Source.Format != "tar.gz" ||
		!strings.Contains(config.Source.NameTemplate, "_source") {
		t.Fatalf("source archive config = %+v", config.Source)
	}
	if config.Checksum.NameTemplate != "checksums.txt" {
		t.Fatalf("release checksum name = %q, want checksums.txt", config.Checksum.NameTemplate)
	}
	if len(config.Signs) != 1 || config.Signs[0].Command != "cosign" ||
		config.Signs[0].Signature != "${artifact}.sigstore.json" ||
		config.Signs[0].Artifacts != "all" {
		t.Fatalf("release signature config = %+v", config.Signs)
	}

	readme, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(readme), "go install github.com/omkhar/minecraft-ping/v2@latest") {
		t.Fatal("README.md does not document the source installation surface")
	}

	releaseWorkflow, err := os.ReadFile(filepath.Join(".github", "workflows", "release.yml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{
		"actions/attest-build-provenance@",
		".provenance.jsonl",
		"Generate SPDX SBOMs",
		`cosign sign-blob --yes --bundle "${sbom}.sigstore.json" "${sbom}"`,
	} {
		if !strings.Contains(string(releaseWorkflow), required) {
			t.Errorf("release workflow does not contain %q", required)
		}
	}
}

func TestRepositoryDoesNotDeclarePublicLibraryPackage(t *testing.T) {
	t.Parallel()

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".go" || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), entry.Name(), nil, parser.PackageClauseOnly)
		if err != nil {
			t.Fatal(err)
		}
		if parsed.Name.Name != "main" {
			t.Errorf("%s declares package %s, want main", entry.Name(), parsed.Name.Name)
		}
	}
}

func TestProductionFunctionsIgnoreVendoredModules(t *testing.T) {
	root := t.TempDir()
	for _, directory := range []string{
		filepath.Join(root, "scripts"),
		filepath.Join(root, "vendor", "example.com", "dependency"),
	} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for path, source := range map[string]string{
		filepath.Join(root, "owned.go"):                                             "package owned\n\nfunc Owned() {}\n",
		filepath.Join(root, "vendor", "example.com", "dependency", "dependency.go"): "package dependency\n\nfunc Vendored() {}\n",
	} {
		if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	functions, err := productionFunctions(root)
	if err != nil {
		t.Fatal(err)
	}
	foundOwned := false
	for _, function := range functions {
		if strings.HasSuffix(function, ".Vendored") {
			t.Fatalf("productionFunctions included vendored function %q", function)
		}
		if strings.HasSuffix(function, ".Owned") {
			foundOwned = true
		}
	}
	if !foundOwned {
		t.Fatal("productionFunctions did not include the repository-owned function")
	}
}

func TestDocumentedLimitsCoverEnforcedConstants(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(filepath.Join("docs", "LIMITATIONS.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, required := range []string{
		"253 bytes",
		"1 through 65,535",
		"30 seconds",
		"250 milliseconds",
		"2 MiB",
		"1 MiB",
		"255 bytes",
		"2,048 bytes",
		"64 MiB",
		"five bytes",
		"2 minutes",
		"2 seconds",
		"500 milliseconds",
		"5 seconds",
	} {
		if !strings.Contains(text, required) {
			t.Errorf("docs/LIMITATIONS.md does not contain %q", required)
		}
	}
}

func TestDocumentedBehaviorCoversNonobviousBoundaries(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(filepath.Join("docs", "LIMITATIONS.md"))
	if err != nil {
		t.Fatal(err)
	}
	limits := string(data)
	for _, required := range []string{
		"The interval is the minimum time from one probe start to the next probe",
		"The probe timeout starts after a TCP or UDP connection succeeds",
		"The probe timeout does not limit DNS, SRV lookup, or TCP or UDP connection",
		"replies are fewer than the count",
		"Numeric mode can put the host name in the first banner",
		"resolves the destination once before a text session",
		"socket operation can continue until its socket deadline",
		"9,223,372,036.854775807",
		"0.0.0.0/8",
		"ff00::/8",
		"default probe timeout of 12 seconds",
	} {
		if !strings.Contains(limits, required) {
			t.Errorf("docs/LIMITATIONS.md does not state behavior boundary %q", required)
		}
	}
}

func TestEveryDocumentedLimitationHasContractCoverage(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(filepath.Join("docs", "LIMITATIONS.md"))
	if err != nil {
		t.Fatal(err)
	}
	sections := markdownSecondLevelSections(string(data))
	actualSectionNames := make(map[string]bool, len(sections))
	for name := range sections {
		actualSectionNames[name] = true
	}
	expectedSectionNames := make(map[string]bool, len(provedLimitationPhrases))
	for name := range provedLimitationPhrases {
		expectedSectionNames[name] = true
	}
	if err := compareExactSets(sortedSet(expectedSectionNames), sortedSet(actualSectionNames)); err != nil {
		t.Fatalf("limitation section drift: %v", err)
	}

	for section, phrases := range provedLimitationPhrases {
		bullets := markdownBullets(sections[section])
		if len(bullets) != len(phrases) {
			t.Errorf("%s has %d limitation bullets, want %d", section, len(bullets), len(phrases))
			continue
		}
		matched := make(map[int]bool, len(bullets))
		for _, phrase := range phrases {
			match := -1
			for index, bullet := range bullets {
				if strings.Contains(bullet, phrase) {
					if match >= 0 {
						t.Errorf("%s phrase %q matches more than one bullet", section, phrase)
					}
					match = index
				}
			}
			if match < 0 {
				t.Errorf("%s does not contain proved limitation %q", section, phrase)
				continue
			}
			if matched[match] {
				t.Errorf("%s bullet %q has more than one proof phrase", section, bullets[match])
			}
			matched[match] = true
		}
		for index, bullet := range bullets {
			if !matched[index] {
				t.Errorf("%s has an unproved limitation bullet: %q", section, bullet)
			}
		}
	}
}

func TestDurationDocumentationMatchesExponentSyntax(t *testing.T) {
	t.Parallel()

	duration, ok := parseSecondsDuration("1e1")
	if !ok || duration != 10*time.Second {
		t.Fatalf("parseSecondsDuration(1e1) = %s, %t, want 10s, true", duration, ok)
	}

	for _, path := range []string{
		"docs/FUNCTIONS.md",
		"docs/LIMITATIONS.md",
		"docs/cli-reference.md",
		"man/minecraft-ping.1",
	} {
		data, err := os.ReadFile(filepath.FromSlash(path))
		if err != nil {
			t.Errorf("read %s: %v", path, err)
			continue
		}
		document := string(data)
		if !strings.Contains(document, "accepts exponent notation") {
			t.Errorf("%s does not document accepted exponent notation", path)
		}
		if strings.Contains(document, "rejects exponent") {
			t.Errorf("%s incorrectly documents rejected exponent notation", path)
		}
	}
}

func TestArgumentOrderDocumentationMatchesParser(t *testing.T) {
	t.Parallel()

	raw, status := scanArgv([]string{"example.com", "-c", "1"})
	if status != parseStatusOK || raw.destination != "example.com" || raw.count != "1" {
		t.Fatalf("scanArgv() after-destination option = %#v, %v", raw, status)
	}
	if _, status := scanArgv([]string{"--", "example.com", "-c", "1"}); status != parseStatusInvalid {
		t.Fatalf("scanArgv() accepted an argument after the -- destination: %v", status)
	}

	for _, path := range []string{
		"docs/FUNCTIONS.md",
		"docs/LIMITATIONS.md",
		"docs/cli-reference.md",
		"man/minecraft-ping.1",
	} {
		data, err := os.ReadFile(filepath.FromSlash(path))
		if err != nil {
			t.Errorf("read %s: %v", path, err)
			continue
		}
		document := string(data)
		if !strings.Contains(document, "before or after the destination") {
			t.Errorf("%s does not document regular option order", path)
		}
		if !strings.Contains(document, "final argument") {
			t.Errorf("%s does not document the -- destination boundary", path)
		}
	}
}

func TestCLIReferenceAndManPageCoverEveryOption(t *testing.T) {
	t.Parallel()

	source, err := os.ReadFile("cli.go")
	if err != nil {
		t.Fatal(err)
	}
	reference, err := os.ReadFile(filepath.Join("docs", "cli-reference.md"))
	if err != nil {
		t.Fatal(err)
	}
	manPage, err := os.ReadFile(filepath.Join("man", "minecraft-ping.1"))
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := acceptedCLIOptions(source)
	if err != nil {
		t.Fatal(err)
	}
	sets := map[string][]string{
		"accepted options": accepted,
		"help output":      optionTokens(usageText()),
		"CLI reference":    markdownOptionTokens(string(reference)),
		"man page":         manPageOptionTokens(string(manPage)),
	}
	for name, options := range sets {
		if err := compareExactSets(accepted, options); err != nil {
			t.Errorf("%s option drift: %v", name, err)
		}
	}
}

func TestCLIOptionComparisonRejectsAcceptedAndHelpDrift(t *testing.T) {
	t.Parallel()

	source, err := os.ReadFile("cli.go")
	if err != nil {
		t.Fatal(err)
	}
	mutant := strings.Replace(
		string(source),
		`case "--version":`,
		`case "--numeric":
		return raw, true
	case "--version":`,
		1,
	)
	mutant = strings.Replace(
		mutant,
		"  --allow-private       allow private, loopback, and local-only IP targets",
		"  --allow-private       allow private, loopback, and local-only IP targets\n  --numeric             use numeric output",
		1,
	)
	accepted, err := acceptedCLIOptions([]byte(mutant))
	if err != nil {
		t.Fatal(err)
	}
	reference, err := os.ReadFile(filepath.Join("docs", "cli-reference.md"))
	if err != nil {
		t.Fatal(err)
	}
	if err := compareExactSets(accepted, markdownOptionTokens(string(reference))); err == nil {
		t.Fatal("option comparison accepted an undocumented parser and help option")
	}
}

func TestFunctionCatalogDocumentsSemanticContracts(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(filepath.Join("docs", "FUNCTIONS.md"))
	if err != nil {
		t.Fatal(err)
	}
	descriptions := functionDescriptions(string(data))

	if got := descriptions["main.parseEdition"]; !strings.Contains(got, "empty value or `java` as Java") || !strings.Contains(got, "rejects other values") {
		t.Errorf("main.parseEdition description does not state the empty-value and rejection contracts: %q", got)
	}
	if parsed, err := parseEdition(""); err != nil || parsed != editionJava {
		t.Fatalf("empty edition contract changed: edition=%v error=%v", parsed, err)
	}
	if _, err := parseEdition("unsupported"); err == nil {
		t.Fatal("unsupported edition contract changed")
	}

	if got := descriptions["main.targetSpec.validate"]; !strings.Contains(got, "checks the port only when the destination has an explicit port") {
		t.Errorf("main.targetSpec.validate description does not state the optional-port contract: %q", got)
	}
	if err := newTargetSpec("example.com", 0, false).validate(); err != nil {
		t.Fatalf("implicit-port target contract changed: %v", err)
	}
	if err := newTargetSpec("example.com", 0, true).validate(); err == nil {
		t.Fatal("invalid explicit-port target contract changed")
	}

	const runProbeDescription = "It runs one JSON probe and decodes the result."
	if got := descriptions["cmd/release-integration.runProbe"]; got != runProbeDescription {
		t.Errorf("cmd/release-integration.runProbe description = %q, want %q", got, runProbeDescription)
	}

	const resolveJavaRouteDescription = "It checks SRV only for an implicit-port host name. It inspects only the first result. It uses that result only when its trimmed target is nonempty and its port is nonzero. When the lookup fails or returns no records, the function returns `ctx.Err()` when it is nonnil. Otherwise, it uses the default route."
	if got := descriptions["main.pingClient.resolveJavaRouteContext"]; got != resolveJavaRouteDescription {
		t.Errorf("main.pingClient.resolveJavaRouteContext description = %q, want %q", got, resolveJavaRouteDescription)
	}
	resolver := &stubResolver{
		srvRecords: []*net.SRV{
			{Target: "", Port: 25570},
			{Target: "second.example.net.", Port: 25571},
		},
	}
	target := newTargetSpec("mc.example.com", 0, false)
	route, err := (pingClient{resolver: resolver}).resolveJavaRouteContext(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	defaultRoute := target.fallbackEndpoint(addressFamilyAny, editionJava)
	if route.Dial != defaultRoute || route.Handshake != defaultRoute {
		t.Fatalf("route after invalid first SRV result = %+v, want default route %+v", route, defaultRoute)
	}
	lookupFailure := errors.New("lookup failure")
	failedRoute, err := (pingClient{resolver: &stubResolver{srvErr: lookupFailure}}).resolveJavaRouteContext(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	if failedRoute.Dial != defaultRoute || failedRoute.Handshake != defaultRoute {
		t.Fatalf("route after non-context SRV failure = %+v, want default route %+v", failedRoute, defaultRoute)
	}
	canceledContext, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (pingClient{resolver: &stubResolver{}}).resolveJavaRouteContext(canceledContext, target); !errors.Is(err, context.Canceled) {
		t.Fatalf("empty SRV result with canceled context error = %v, want %v", err, context.Canceled)
	}

	const newBedrockClientDescription = "It creates the standard ping client for Bedrock."
	if got := descriptions["main.newBedrockClient"]; got != newBedrockClientDescription {
		t.Errorf("main.newBedrockClient description = %q, want %q", got, newBedrockClientDescription)
	}

	const randomUint64Description = "It gives an eight-byte zeroed buffer to the supplied callback. It ignores the returned byte count. It returns zero and wraps a callback error. Otherwise, it converts the full buffer to a big-endian 64-bit value."
	if got := descriptions["main.randomUint64With"]; got != randomUint64Description {
		t.Errorf("main.randomUint64With description = %q, want %q", got, randomUint64Description)
	}
	randomValue, err := randomUint64With(func(buffer []byte) (int, error) {
		if len(buffer) != 8 {
			t.Fatalf("random callback buffer length = %d, want 8", len(buffer))
		}
		buffer[0] = 0x12
		return 1, nil
	})
	if err != nil || randomValue != 0x1200000000000000 {
		t.Fatalf("randomUint64With short read = 0x%x, %v", randomValue, err)
	}
}

func repositoryDocumentationPaths(root string) ([]string, error) {
	paths := make(map[string]bool)
	rootEntries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	for _, entry := range rootEntries {
		if entry.IsDir() {
			continue
		}
		if filepath.Ext(entry.Name()) == ".md" || entry.Name() == "LICENSE" {
			paths[entry.Name()] = true
		}
	}

	for _, directory := range []string{"docs", "man"} {
		err := filepath.WalkDir(filepath.Join(root, directory), func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				return nil
			}
			extension := filepath.Ext(path)
			if extension != ".md" && extension != ".1" {
				return nil
			}
			relative, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			paths[filepath.ToSlash(relative)] = true
			return nil
		})
		if err != nil {
			return nil, err
		}
	}

	pullRequestTemplate := filepath.Join(root, ".github", "pull_request_template.md")
	if _, err := os.Stat(pullRequestTemplate); err != nil {
		return nil, err
	}
	paths[".github/pull_request_template.md"] = true

	issueTemplateRoot := filepath.Join(root, ".github", "ISSUE_TEMPLATE")
	err = filepath.WalkDir(issueTemplateRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		switch filepath.Ext(path) {
		case ".md", ".yaml", ".yml":
			relative, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			paths[filepath.ToSlash(relative)] = true
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return sortedSet(paths), nil
}

func supportedSurfaceIDs(section string) []string {
	ids := make(map[string]bool)
	_, rows, ok := strings.Cut(section, "### Other supported surfaces")
	if !ok {
		return nil
	}
	rowPattern := regexp.MustCompile("(?m)^\\| `([^`]+)` \\|")
	for _, match := range rowPattern.FindAllStringSubmatch(rows, -1) {
		ids[match[1]] = true
	}
	return sortedSet(ids)
}

func supportedSurfaceDescriptions(section string) map[string]string {
	descriptions := make(map[string]string)
	_, rows, ok := strings.Cut(section, "### Other supported surfaces")
	if !ok {
		return descriptions
	}
	rowPattern := regexp.MustCompile("(?m)^\\| `([^`]+)` \\| (.+) \\|$")
	for _, match := range rowPattern.FindAllStringSubmatch(rows, -1) {
		descriptions[match[1]] = match[2]
	}
	return descriptions
}

func markdownSecondLevelSections(document string) map[string]string {
	sections := make(map[string]string)
	var (
		name    string
		content strings.Builder
	)
	flush := func() {
		if name == "" {
			return
		}
		sections[name] = content.String()
		content.Reset()
	}
	for line := range strings.SplitSeq(document, "\n") {
		if strings.HasPrefix(line, "## ") {
			flush()
			name = strings.TrimSpace(strings.TrimPrefix(line, "## "))
			continue
		}
		if name != "" {
			content.WriteString(line)
			content.WriteByte('\n')
		}
	}
	flush()
	return sections
}

func markdownBullets(section string) []string {
	var (
		bullets []string
		current strings.Builder
	)
	flush := func() {
		if current.Len() == 0 {
			return
		}
		bullets = append(bullets, strings.TrimSpace(current.String()))
		current.Reset()
	}
	for line := range strings.SplitSeq(section, "\n") {
		switch {
		case strings.HasPrefix(line, "- "):
			flush()
			current.WriteString(strings.TrimPrefix(line, "- "))
		case current.Len() != 0 && (strings.HasPrefix(line, "  ") || strings.HasPrefix(line, "\t")):
			current.WriteByte(' ')
			current.WriteString(strings.TrimSpace(line))
		default:
			flush()
		}
	}
	flush()
	return bullets
}

func controlledStyleFindings(document string) []string {
	var findings []string
	passiveVoice := regexp.MustCompile(`(?i)\b(?:am|is|are|was|were|be|been|being|will be|must be|can be|may be|should be)\s+(?:(?:not|also|always|currently|only|explicitly|privately|already)\s+)*(?:[a-z]+ed|built|cut|done|found|given|kept|known|made|read|run|seen|sent|set|shown|taken|told|used|written)\b`)
	word := regexp.MustCompile(`[A-Za-z0-9]+(?:[-'][A-Za-z0-9]+)*`)
	inlineCode := regexp.MustCompile("`[^`]*`")
	linkTarget := regexp.MustCompile(`\]\([^)]*\)`)
	sentenceEnd := regexp.MustCompile(`[.!?]+(?:[[:space:]]+|$)`)

	checkUnit := func(line int, raw string) {
		prose := inlineCode.ReplaceAllString(raw, " technical-name ")
		prose = linkTarget.ReplaceAllString(prose, "]")
		prose = strings.TrimSpace(strings.TrimLeft(prose, "#>*-+0123456789. "))
		if prose == "" || regexp.MustCompile(`^[-:| ]+$`).MatchString(prose) {
			return
		}
		if strings.ContainsAny(prose, ";–—") {
			findings = append(findings, fmt.Sprintf("line %d uses disallowed punctuation", line))
		}
		for _, sentence := range sentenceEnd.Split(prose, -1) {
			sentence = strings.TrimSpace(sentence)
			if sentence == "" {
				continue
			}
			if count := len(word.FindAllString(sentence, -1)); count > 25 {
				findings = append(findings, fmt.Sprintf("line %d has a %d-word sentence (maximum 25)", line, count))
			}
			if passiveVoice.MatchString(sentence) {
				findings = append(findings, fmt.Sprintf("line %d has a passive-voice pattern: %q", line, sentence))
			}
		}
	}

	lines := strings.Split(document, "\n")
	var paragraph []string
	paragraphLine := 1
	inFence := false
	flush := func() {
		if len(paragraph) != 0 {
			checkUnit(paragraphLine, strings.Join(paragraph, " "))
			paragraph = nil
		}
	}
	for index, line := range lines {
		lineNumber := index + 1
		trimmed := strings.TrimSpace(line)
		if trimmed == ".EX" {
			flush()
			inFence = true
			continue
		}
		if trimmed == ".EE" {
			inFence = false
			continue
		}
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			flush()
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		switch {
		case trimmed == "":
			flush()
		case strings.HasPrefix(trimmed, "#"):
			flush()
		case strings.HasPrefix(trimmed, "."):
			flush()
		case strings.HasPrefix(trimmed, "|"):
			flush()
			for cell := range strings.SplitSeq(strings.Trim(trimmed, "|"), "|") {
				checkUnit(lineNumber, cell)
			}
		case regexp.MustCompile(`^(?:[-+*]|[0-9]+\.)[[:space:]]+`).MatchString(trimmed):
			flush()
			checkUnit(lineNumber, trimmed)
		default:
			if len(paragraph) == 0 {
				paragraphLine = lineNumber
			}
			paragraph = append(paragraph, trimmed)
		}
	}
	flush()
	return findings
}

func acceptedCLIOptions(source []byte) ([]string, error) {
	parsed, err := parser.ParseFile(token.NewFileSet(), "cli.go", source, 0)
	if err != nil {
		return nil, err
	}
	options := make(map[string]bool)
	for _, declaration := range parsed.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || (function.Name.Name != "consumeLongFlag" && function.Name.Name != "consumeShortFlags") {
			continue
		}
		ast.Inspect(function.Body, func(node ast.Node) bool {
			clause, ok := node.(*ast.CaseClause)
			if !ok {
				return true
			}
			for _, expression := range clause.List {
				literal, ok := expression.(*ast.BasicLit)
				if !ok {
					continue
				}
				value, err := strconv.Unquote(literal.Value)
				if err != nil {
					continue
				}
				switch literal.Kind {
				case token.STRING:
					if strings.HasPrefix(value, "--") {
						options[value] = true
					}
				case token.CHAR:
					if len([]rune(value)) == 1 {
						options["-"+value] = true
					}
				}
			}
			return true
		})
	}
	if len(options) == 0 {
		return nil, fmt.Errorf("cli.go does not declare accepted options")
	}
	return sortedSet(options), nil
}

func optionTokens(text string) []string {
	pattern := regexp.MustCompile("(?:^|[[:space:],\\\"`])(--?[A-Za-z0-9][A-Za-z0-9-]*)")
	options := make(map[string]bool)
	for _, match := range pattern.FindAllStringSubmatch(text, -1) {
		options[match[1]] = true
	}
	return sortedSet(options)
}

func markdownOptionTokens(document string) []string {
	section := between(document, "## Options", "## Important Notes")
	return optionTokens(section)
}

func manPageOptionTokens(document string) []string {
	section := between(document, ".SH OPTIONS", ".SH DESTINATIONS")
	return optionTokens(section)
}

func between(text string, start string, end string) string {
	startIndex := strings.Index(text, start)
	if startIndex < 0 {
		return ""
	}
	text = text[startIndex+len(start):]
	before, _, ok := strings.Cut(text, end)
	if !ok {
		return text
	}
	return before
}

func compareExactSets(expected []string, actual []string) error {
	expectedSet := make(map[string]bool, len(expected))
	actualSet := make(map[string]bool, len(actual))
	for _, value := range expected {
		expectedSet[value] = true
	}
	for _, value := range actual {
		actualSet[value] = true
	}
	var missing []string
	var stale []string
	for value := range expectedSet {
		if !actualSet[value] {
			missing = append(missing, value)
		}
	}
	for value := range actualSet {
		if !expectedSet[value] {
			stale = append(stale, value)
		}
	}
	sort.Strings(missing)
	sort.Strings(stale)
	if len(missing) != 0 || len(stale) != 0 {
		return fmt.Errorf("missing=%v stale=%v", missing, stale)
	}
	return nil
}

func sortedSet(set map[string]bool) []string {
	values := make([]string, 0, len(set))
	for value := range set {
		values = append(values, value)
	}
	sort.Strings(values)
	return values
}

func functionDescriptions(catalog string) map[string]string {
	descriptions := make(map[string]string)
	rowPattern := regexp.MustCompile("(?m)^\\| `([^`]+)` \\| (.+) \\|$")
	for _, match := range rowPattern.FindAllStringSubmatch(implementationFunctionCatalog(catalog), -1) {
		descriptions[match[1]] = match[2]
	}
	return descriptions
}

func compareFunctionCatalog(expected []string, catalog string) error {
	documented := make(map[string]bool)
	rowPattern := regexp.MustCompile("(?m)^\\| `([^`]+)` \\|")
	for _, match := range rowPattern.FindAllStringSubmatch(implementationFunctionCatalog(catalog), -1) {
		name := match[1]
		if documented[name] {
			return fmt.Errorf("docs/FUNCTIONS.md contains duplicate function %s", name)
		}
		documented[name] = true
	}

	expectedSet := make(map[string]bool, len(expected))
	for _, name := range expected {
		expectedSet[name] = true
	}
	var missing []string
	for name := range expectedSet {
		if !documented[name] {
			missing = append(missing, name)
		}
	}
	var stale []string
	for name := range documented {
		if !expectedSet[name] {
			stale = append(stale, name)
		}
	}
	if len(missing) == 0 && len(stale) == 0 {
		return nil
	}
	sort.Strings(missing)
	sort.Strings(stale)
	return fmt.Errorf("docs/FUNCTIONS.md function drift: missing=%v stale=%v", missing, stale)
}

func implementationFunctionCatalog(catalog string) string {
	_, implementation, ok := strings.Cut(catalog, "## Implementation function catalog")
	if ok {
		return implementation
	}
	return catalog
}

func productionFunctions(root string) ([]string, error) {
	functionSet := make(map[string]struct{})
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if path != root && (entry.Name() == ".git" || entry.Name() == "dist" || entry.Name() == "vendor") {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		directory := filepath.ToSlash(filepath.Dir(path))
		if directory == "." {
			directory = "main"
		}
		for _, declaration := range parsed.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok {
				continue
			}
			name := directory + "." + function.Name.Name
			if function.Recv != nil && len(function.Recv.List) != 0 {
				name = directory + "." + receiverName(function.Recv.List[0].Type) + "." + function.Name.Name
			}
			functionSet[name] = struct{}{}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	shellFunction := regexp.MustCompile(`(?m)^([A-Za-z_][A-Za-z0-9_]*)\(\)[[:space:]]*\{`)
	err = filepath.WalkDir(filepath.Join(root, "scripts"), func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || filepath.Ext(path) != ".sh" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		for _, match := range shellFunction.FindAllSubmatch(data, -1) {
			functionSet[filepath.ToSlash(relative)+":"+string(match[1])] = struct{}{}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	functions := make([]string, 0, len(functionSet))
	for name := range functionSet {
		functions = append(functions, name)
	}
	sort.Strings(functions)
	return functions, nil
}

func receiverName(expression ast.Expr) string {
	switch value := expression.(type) {
	case *ast.Ident:
		return value.Name
	case *ast.StarExpr:
		return receiverName(value.X)
	case *ast.IndexExpr:
		return receiverName(value.X)
	case *ast.IndexListExpr:
		return receiverName(value.X)
	default:
		return "unknown"
	}
}
