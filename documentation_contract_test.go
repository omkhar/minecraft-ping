package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

type documentedRuntime struct {
	Version string `json:"version"`
	SHA     string `json:"sha"`
}

type documentedContainer struct {
	Image  string `json:"image"`
	Tag    string `json:"tag"`
	Digest string `json:"digest"`
}

type runtimeManifest struct {
	Go                     string                         `json:"go"`
	StagingContainer       documentedContainer            `json:"staging_container"`
	PackageSmokeContainers map[string]documentedContainer `json:"package_smoke_containers"`
	Runners                []string                       `json:"runners"`
	Actions                map[string]documentedRuntime   `json:"actions"`
	Tools                  map[string]string              `json:"tools"`
}

var runtimeToolVariables = map[string]string{
	"actionlint":   "ACTIONLINT_VERSION",
	"deadcode":     "DEADCODE_VERSION",
	"gitleaks":     "GITLEAKS_VERSION",
	"gocritic":     "GOCRITIC_VERSION",
	"goimports":    "GOIMPORTS_MODULE_VERSION",
	"go-mutesting": "GO_MUTESTING_VERSION",
	"gosec":        "GOSEC_VERSION",
	"govulncheck":  "GOVULNCHECK_VERSION",
	"ineffassign":  "INEFFASSIGN_VERSION",
	"staticcheck":  "STATICCHECK_VERSION",
	"syft":         "SYFT_VERSION",
}

var runtimeToolNames = []string{
	"actionlint",
	"cosign",
	"deadcode",
	"gitleaks",
	"gocritic",
	"goimports",
	"go-mutesting",
	"goreleaser",
	"gosec",
	"govulncheck",
	"ineffassign",
	"staticcheck",
	"syft",
}

var stagingBuildContainerPattern = regexp.MustCompile(
	`(?m)^FROM ([^:@\s]+):([^@\s]+)@sha256:([0-9a-f]{64}) AS build\r?$`,
)

func TestRuntimeManifestRejectsMutableOrMalformedPins(t *testing.T) {
	validSHA := "3d3c42e5aac5ba805825da76410c181273ba90b1"
	validDigest := "1ecb7edf62a0408027bd5729dfd6b1b8766e578e8df93995b225dfd0944eb651"
	for name, manifest := range map[string]runtimeManifest{
		"mutable tool": {
			Go:    "1.26.5",
			Tools: map[string]string{"actionlint": "latest"},
		},
		"mutable action": {
			Go:      "1.26.5",
			Actions: map[string]documentedRuntime{"actions/checkout": {Version: "main", SHA: validSHA}},
		},
		"invalid action SHA": {
			Go:      "1.26.5",
			Actions: map[string]documentedRuntime{"actions/checkout": {Version: "v7.0.1", SHA: "v7"}},
		},
		"invalid container digest": {
			Go:               "1.26.5",
			StagingContainer: documentedContainer{Image: "golang", Tag: "1.26.5-bookworm", Digest: "latest"},
		},
		"duplicate runner": {
			Go:      "1.26.5",
			Runners: []string{"ubuntu-24.04", "ubuntu-24.04"},
		},
		"valid pins": {
			Go:               "1.26.5",
			StagingContainer: documentedContainer{Image: "golang", Tag: "1.26.5-bookworm", Digest: validDigest},
			Runners:          []string{"ubuntu-24.04"},
			Actions:          map[string]documentedRuntime{"actions/checkout": {Version: "v7.0.1", SHA: validSHA}},
			Tools:            map[string]string{"actionlint": "v1.7.12"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			err := validateRuntimeManifest(manifest)
			if name == "valid pins" && err != nil {
				t.Fatal(err)
			}
			if name != "valid pins" && err == nil {
				t.Fatal("validateRuntimeManifest accepted a mutable or malformed pin")
			}
		})
	}
}

func TestRuntimeManifestRejectsIncompleteOrUndocumentedToolSets(t *testing.T) {
	valid := map[string]string{
		"actionlint":   "v1.7.12",
		"cosign":       "v3.1.2",
		"deadcode":     "v0.48.0",
		"gitleaks":     "v8.30.1",
		"gocritic":     "v0.14.4",
		"goimports":    "v0.48.0",
		"go-mutesting": "v0.0.0-20251226130216-48d0401f00fb",
		"goreleaser":   "v2.17.0",
		"gosec":        "v2.28.0",
		"govulncheck":  "v1.6.0",
		"ineffassign":  "v0.2.0",
		"staticcheck":  "v0.7.0",
		"syft":         "v1.49.0",
	}
	if err := validateRuntimeToolSet(valid); err != nil {
		t.Fatal(err)
	}
	delete(valid, "actionlint")
	if err := validateRuntimeToolSet(valid); err == nil {
		t.Fatal("validateRuntimeToolSet accepted a missing tool")
	}
	valid["actionlint"] = "v1.7.12"
	valid["unverified-peer-mutant"] = "v999.0.0"
	if err := validateRuntimeToolSet(valid); err == nil {
		t.Fatal("validateRuntimeToolSet accepted an undocumented tool")
	}
}

func TestPackageSmokeContainerContractIsBidirectional(t *testing.T) {
	script := `run_container_smoke \
  "debian" \
  "debian:13@sha256:fac46bff2e02f51425b6e33b0e1169f55dfb053d83511ca28aa50c09fd5ed7a4" \
  "true"
run_container_smoke \
  "alpine" \
  "alpine:3.24@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b" \
  "true"`
	manifest := map[string]documentedContainer{
		"alpine": {Image: "alpine", Tag: "3.24", Digest: "28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b"},
		"debian": {Image: "debian", Tag: "13", Digest: "fac46bff2e02f51425b6e33b0e1169f55dfb053d83511ca28aa50c09fd5ed7a4"},
	}
	if err := validatePackageSmokeContainers(manifest, script); err != nil {
		t.Fatal(err)
	}
	delete(manifest, "alpine")
	if err := validatePackageSmokeContainers(manifest, script); err == nil {
		t.Fatal("validatePackageSmokeContainers accepted a script-only container")
	}
	manifest["alpine"] = documentedContainer{Image: "alpine", Tag: "3.24", Digest: "28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b"}
	manifest["unused"] = documentedContainer{Image: "unused", Tag: "1.0", Digest: "28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b"}
	if err := validatePackageSmokeContainers(manifest, script); err == nil {
		t.Fatal("validatePackageSmokeContainers accepted a manifest-only container")
	}
}

func TestRuntimeManifestMatchesRepository(t *testing.T) {
	manifestData := mustReadDocumentationFile(t, "docs/runtime-versions.json")
	var manifest runtimeManifest
	if err := decodeStrictJSON(strings.NewReader(manifestData), &manifest); err != nil {
		t.Fatal(err)
	}
	if err := validateRuntimeManifest(manifest); err != nil {
		t.Fatal(err)
	}
	if err := validateRuntimeToolSet(manifest.Tools); err != nil {
		t.Fatal(err)
	}

	goVersion := goDirectiveVersion(mustReadDocumentationFile(t, "go.mod"))
	if goVersion != manifest.Go {
		t.Fatalf("go.mod version = %q, manifest version = %q", goVersion, manifest.Go)
	}

	dockerfile := mustReadDocumentationFile(t, "docker/staging-minecraft.Dockerfile")
	container := stagingBuildContainerPattern.FindStringSubmatch(dockerfile)
	if len(container) != 4 {
		t.Fatal("staging Dockerfile does not use a pinned build container")
	}
	if container[1] != manifest.StagingContainer.Image || container[2] != manifest.StagingContainer.Tag || container[3] != manifest.StagingContainer.Digest {
		t.Fatalf("staging container = %s:%s@sha256:%s, manifest = %+v", container[1], container[2], container[3], manifest.StagingContainer)
	}

	packageSmoke := mustReadDocumentationFile(t, "scripts/release_linux_package_smoke.sh")
	if err := validatePackageSmokeContainers(manifest.PackageSmokeContainers, packageSmoke); err != nil {
		t.Error(err)
	}

	allowedRunners := make(map[string]bool, len(manifest.Runners))
	for _, runner := range manifest.Runners {
		allowedRunners[runner] = true
	}
	seenRunners := make(map[string]bool)
	seenActions := make(map[string]bool)
	var workflowContent strings.Builder
	workflowRoot := filepath.Join(".github", "workflows")
	err := filepath.WalkDir(workflowRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return walkErr
		}
		content := mustReadPath(t, path)
		workflowContent.WriteString(content)
		workflowContent.WriteByte('\n')
		actions, runners, parseErr := parseWorkflowRuntime(content)
		if parseErr != nil {
			t.Errorf("%s: %v", filepath.Base(path), parseErr)
			return nil
		}
		for _, action := range actions {
			pin, ok := manifest.Actions[action.name]
			if !ok {
				t.Errorf("%s uses undocumented action %s", filepath.Base(path), action.name)
				continue
			}
			seenActions[action.name] = true
			if action.sha != pin.SHA {
				t.Errorf("%s uses %s@%s, want %s", filepath.Base(path), action.name, action.sha, pin.SHA)
			}
			if action.version != pin.Version {
				t.Errorf("%s labels %s as %q, want %q", filepath.Base(path), action.name, action.version, pin.Version)
			}
		}

		for _, runner := range runners {
			if !allowedRunners[runner] {
				t.Errorf("%s uses undocumented runner %s", filepath.Base(path), runner)
				continue
			}
			seenRunners[runner] = true
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for action := range manifest.Actions {
		if !seenActions[action] {
			t.Errorf("documented action %s is not used", action)
		}
	}
	for _, runner := range manifest.Runners {
		if !seenRunners[runner] {
			t.Errorf("documented runner %s is not used", runner)
		}
	}
	checkToolVersions(t, manifest.Tools, workflowContent.String())
}

func validateRuntimeManifest(manifest runtimeManifest) error {
	semverPattern := regexp.MustCompile(`^v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?(\+[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$`)
	if !semverPattern.MatchString(manifest.Go) || strings.HasPrefix(manifest.Go, "v") {
		return fmt.Errorf("runtime manifest has an invalid Go version %q", manifest.Go)
	}
	shaPattern := regexp.MustCompile(`^[0-9a-f]{40}$`)
	for name, action := range manifest.Actions {
		if name == "" || !semverPattern.MatchString(action.Version) ||
			!strings.HasPrefix(action.Version, "v") || !shaPattern.MatchString(action.SHA) {
			return fmt.Errorf("runtime manifest action %q has a mutable or malformed pin", name)
		}
	}
	for name, version := range manifest.Tools {
		if name == "" || !semverPattern.MatchString(version) {
			return fmt.Errorf("runtime manifest tool %q has a mutable or malformed pin", name)
		}
	}
	seenRunners := make(map[string]bool, len(manifest.Runners))
	for _, runner := range manifest.Runners {
		if runner == "" || seenRunners[runner] {
			return fmt.Errorf("runtime manifest has an empty or duplicate runner %q", runner)
		}
		seenRunners[runner] = true
	}
	if err := validateRuntimeContainer("staging", manifest.StagingContainer); err != nil {
		return err
	}
	for name, container := range manifest.PackageSmokeContainers {
		if name == "" {
			return fmt.Errorf("runtime manifest has an unnamed package-smoke container")
		}
		if err := validateRuntimeContainer(name, container); err != nil {
			return err
		}
	}
	return nil
}

func validateRuntimeContainer(name string, container documentedContainer) error {
	if container.Image == "" || container.Tag == "" ||
		!regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(container.Digest) {
		return fmt.Errorf("runtime manifest container %q has a mutable or malformed pin", name)
	}
	return nil
}

func validateRuntimeToolSet(tools map[string]string) error {
	expected := make(map[string]bool, len(runtimeToolNames))
	for _, name := range runtimeToolNames {
		expected[name] = true
		if _, ok := tools[name]; !ok {
			return fmt.Errorf("runtime manifest does not document tool %s", name)
		}
	}
	var extras []string
	for name := range tools {
		if !expected[name] {
			extras = append(extras, name)
		}
	}
	if len(extras) != 0 {
		sort.Strings(extras)
		return fmt.Errorf("runtime manifest documents tools that have no version contract: %s", strings.Join(extras, ", "))
	}
	return nil
}

func validatePackageSmokeContainers(manifest map[string]documentedContainer, script string) error {
	callPattern := regexp.MustCompile(`(?m)^[ \t]*run_container_smoke[ \t]*\\[ \t]*\r?$`)
	blockPattern := regexp.MustCompile(`(?m)^[ \t]*run_container_smoke[ \t]*\\[ \t]*\r?\n[ \t]*"([^"\r\n]+)"[ \t]*\\[ \t]*\r?\n[ \t]*"([^"\r\n]+)"[ \t]*\\[ \t]*\r?$`)
	blocks := blockPattern.FindAllStringSubmatch(script, -1)
	if len(blocks) != len(callPattern.FindAllStringIndex(script, -1)) {
		return fmt.Errorf("package smoke script has a malformed container call")
	}
	imagePattern := regexp.MustCompile(`^([^:@\s]+):([^@\s]+)@sha256:([0-9a-f]{64})$`)
	used := make(map[string]documentedContainer, len(blocks))
	for _, block := range blocks {
		name := block[1]
		if _, exists := used[name]; exists {
			return fmt.Errorf("package smoke script uses container %s more than once", name)
		}
		image := imagePattern.FindStringSubmatch(block[2])
		if len(image) != 4 {
			return fmt.Errorf("package smoke container %s is not immutable", name)
		}
		used[name] = documentedContainer{Image: image[1], Tag: image[2], Digest: image[3]}
	}
	for name, want := range manifest {
		got, ok := used[name]
		if !ok {
			return fmt.Errorf("documented package smoke container %s is not used", name)
		}
		if got != want {
			return fmt.Errorf("package smoke container %s = %+v, manifest = %+v", name, got, want)
		}
	}
	for name := range used {
		if _, ok := manifest[name]; !ok {
			return fmt.Errorf("package smoke script uses undocumented container %s", name)
		}
	}
	return nil
}

func checkToolVersions(t *testing.T, tools map[string]string, workflows string) {
	t.Helper()
	for tool, variable := range runtimeToolVariables {
		want, ok := tools[tool]
		if !ok {
			t.Errorf("runtime manifest does not document tool %s", tool)
			continue
		}
		matches := regexp.MustCompile(`(?m)^[ \t]*`+regexp.QuoteMeta(variable)+`:[ \t]*([^\s#]+)`).FindAllStringSubmatch(workflows, -1)
		if len(matches) == 0 {
			t.Errorf("workflow variable %s is not used", variable)
			continue
		}
		for _, match := range matches {
			if match[1] != want {
				t.Errorf("workflow variable %s = %s, manifest = %s", variable, match[1], want)
			}
		}
	}

	if got, want := strings.TrimSpace(mustReadDocumentationFile(t, ".github/goreleaser-version.txt")), tools["goreleaser"]; got != want {
		t.Errorf("GoReleaser version = %s, manifest = %s", got, want)
	}
	cosign := regexp.MustCompile(`(?m)^[ \t]*cosign-release:[ \t]*([^\s#]+)`).FindStringSubmatch(workflows)
	if len(cosign) != 2 || cosign[1] != tools["cosign"] {
		t.Errorf("Cosign version = %v, manifest = %s", cosign, tools["cosign"])
	}
}

func TestRuntimeDocumentationCoversManifest(t *testing.T) {
	runtimeDoc := mustReadDocumentationFile(t, "docs/RUNTIME.md")
	for _, statement := range []string{
		"Go 1.26.5",
		"ubuntu-24.04-arm",
		"windows-11-arm",
		"docs/runtime-versions.json",
	} {
		if !strings.Contains(runtimeDoc, statement) {
			t.Errorf("docs/RUNTIME.md does not contain %q", statement)
		}
	}
}

func TestGoDirectiveVersionHandlesWindowsLineEndings(t *testing.T) {
	for _, content := range []string{"go 1.26.5\n", "go 1.26.5\r\n"} {
		if got := goDirectiveVersion(content); got != "1.26.5" {
			t.Fatalf("goDirectiveVersion() = %q, want 1.26.5", got)
		}
	}
}

func TestStagingBuildContainerPatternHandlesLineEndings(t *testing.T) {
	digest := strings.Repeat("a", 64)
	line := "FROM golang:1.26.5-bookworm@sha256:" + digest + " AS build"
	for name, content := range map[string]string{
		"LF":   line + "\n",
		"CRLF": line + "\r\n",
	} {
		t.Run(name, func(t *testing.T) {
			match := stagingBuildContainerPattern.FindStringSubmatch(content)
			if len(match) != 4 {
				t.Fatalf("FindStringSubmatch() = %q, want four fields", match)
			}
			if match[1] != "golang" || match[2] != "1.26.5-bookworm" || match[3] != digest {
				t.Fatalf("FindStringSubmatch() = %q, want pinned golang container", match)
			}
		})
	}
}

func goDirectiveVersion(goMod string) string {
	match := regexp.MustCompile(`(?m)^go[ \t]+([^ \t\r\n]+)[ \t]*\r?$`).FindStringSubmatch(goMod)
	if len(match) != 2 {
		return ""
	}
	return match[1]
}

func mustReadDocumentationFile(t *testing.T, relative string) string {
	t.Helper()
	return mustReadPath(t, filepath.FromSlash(relative))
}

func mustReadPath(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(fmt.Errorf("read %s: %w", path, err))
	}
	// Windows checkouts can use CRLF line endings.
	return strings.ReplaceAll(string(content), "\r\n", "\n")
}
