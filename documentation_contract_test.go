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

type documentedContainer struct {
	Image  string `json:"image"`
	Tag    string `json:"tag"`
	Digest string `json:"digest"`
}

// runtimeManifest lists the runtime inventory. It does not repeat values that
// Dependabot updates: go.mod has the Go version, the staging Dockerfile has the
// image tag and digest, and each workflow "uses:" line has the action SHA and
// release tag. The tests read those values from their source files.
type runtimeManifest struct {
	StagingImage           string                         `json:"staging_image"`
	PackageSmokeContainers map[string]documentedContainer `json:"package_smoke_containers"`
	Runners                []string                       `json:"runners"`
	Actions                []string                       `json:"actions"`
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

var (
	goVersionPattern       = regexp.MustCompile(`^1\.(0|[1-9][0-9]*)(\.(0|[1-9][0-9]*))?$`)
	releaseTagPattern      = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?(\+[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$`)
	actionCommitSHAPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
)

func TestRuntimeManifestRejectsMutableOrMalformedPins(t *testing.T) {
	for name, manifest := range map[string]runtimeManifest{
		"mutable tool": {
			StagingImage: "golang",
			Tools:        map[string]string{"actionlint": "latest"},
		},
		"action with ref": {
			StagingImage: "golang",
			Actions:      []string{"actions/checkout@v7"},
		},
		"duplicate action": {
			StagingImage: "golang",
			Actions:      []string{"actions/checkout", "actions/checkout"},
		},
		"staging image with tag": {
			StagingImage: "golang:latest",
		},
		"duplicate runner": {
			StagingImage: "golang",
			Runners:      []string{"ubuntu-24.04", "ubuntu-24.04"},
		},
		"valid pins": {
			StagingImage: "golang",
			Runners:      []string{"ubuntu-24.04"},
			Actions:      []string{"actions/checkout"},
			Tools:        map[string]string{"actionlint": "v1.7.12"},
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

func TestWorkflowActionPinsRejectMutableOrInconsistentPins(t *testing.T) {
	sha := "3d3c42e5aac5ba805825da76410c181273ba90b1"
	otherSHA := "24c54180a607b1449ed407dd24f251e4e9147c8d"
	for name, test := range map[string]struct {
		actions []parsedWorkflowAction
		valid   bool
	}{
		"valid pins": {
			actions: []parsedWorkflowAction{
				{name: "actions/checkout", sha: sha, version: "v7.0.1"},
				{name: "actions/checkout", sha: sha, version: "v7.0.1"},
				{name: "github/codeql-action/init", sha: otherSHA, version: "v4.38.3"},
				{name: "github/codeql-action/analyze", sha: otherSHA, version: "v4.38.3"},
			},
			valid: true,
		},
		"missing release tag": {
			actions: []parsedWorkflowAction{{name: "actions/checkout", sha: sha}},
		},
		"mutable release tag": {
			actions: []parsedWorkflowAction{{name: "actions/checkout", sha: sha, version: "v7"}},
		},
		"branch release tag": {
			actions: []parsedWorkflowAction{{name: "actions/checkout", sha: sha, version: "main"}},
		},
		"non-SHA ref": {
			actions: []parsedWorkflowAction{{name: "actions/checkout", sha: "v7.0.1", version: "v7.0.1"}},
		},
		"two SHAs for one repository": {
			actions: []parsedWorkflowAction{
				{name: "github/codeql-action/init", sha: otherSHA, version: "v4.38.3"},
				{name: "github/codeql-action/analyze", sha: sha, version: "v4.38.3"},
			},
		},
		"two tags for one SHA": {
			actions: []parsedWorkflowAction{
				{name: "actions/checkout", sha: sha, version: "v7.0.1"},
				{name: "actions/checkout", sha: sha, version: "v7.0.2"},
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			err := validateWorkflowActionPins(test.actions)
			if test.valid && err != nil {
				t.Fatal(err)
			}
			if !test.valid && err == nil {
				t.Fatal("validateWorkflowActionPins accepted a mutable or inconsistent pin")
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

	// go.mod is the source of the Go version. Dependabot can change it.
	if goVersion := goDirectiveVersion(mustReadDocumentationFile(t, "go.mod")); !goVersionPattern.MatchString(goVersion) {
		t.Fatalf("go.mod has an invalid Go version %q", goVersion)
	}

	// The Dockerfile is the source of the staging tag and digest. Dependabot can change them.
	dockerfile := mustReadDocumentationFile(t, "docker/staging-minecraft.Dockerfile")
	container := stagingBuildContainerPattern.FindStringSubmatch(dockerfile)
	if len(container) != 4 || container[2] == "latest" {
		t.Fatal("staging Dockerfile does not use a pinned build container")
	}
	if container[1] != manifest.StagingImage {
		t.Fatalf("staging container image = %s, manifest = %s", container[1], manifest.StagingImage)
	}

	packageSmoke := mustReadDocumentationFile(t, "scripts/release_linux_package_smoke.sh")
	if err := validatePackageSmokeContainers(manifest.PackageSmokeContainers, packageSmoke); err != nil {
		t.Error(err)
	}

	allowedRunners := make(map[string]bool, len(manifest.Runners))
	for _, runner := range manifest.Runners {
		allowedRunners[runner] = true
	}
	documentedActions := make(map[string]bool, len(manifest.Actions))
	for _, action := range manifest.Actions {
		documentedActions[action] = true
	}
	seenRunners := make(map[string]bool)
	seenActions := make(map[string]bool)
	var workflowPins []parsedWorkflowAction
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
			if !documentedActions[action.name] {
				t.Errorf("%s uses undocumented action %s", filepath.Base(path), action.name)
				continue
			}
			seenActions[action.name] = true
		}
		workflowPins = append(workflowPins, actions...)

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
	if err := validateWorkflowActionPins(workflowPins); err != nil {
		t.Error(err)
	}
	for _, action := range manifest.Actions {
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
	if manifest.StagingImage == "" || strings.ContainsAny(manifest.StagingImage, ":@ \t") {
		return fmt.Errorf("runtime manifest has an invalid staging image name %q", manifest.StagingImage)
	}
	semverPattern := regexp.MustCompile(`^v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?(\+[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$`)
	seenActions := make(map[string]bool, len(manifest.Actions))
	for _, name := range manifest.Actions {
		if name == "" || seenActions[name] || strings.ContainsAny(name, "@ \t") {
			return fmt.Errorf("runtime manifest has an empty, duplicate, or versioned action %q", name)
		}
		seenActions[name] = true
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

// validateWorkflowActionPins checks the pins on the workflow "uses:" lines.
// Each action must use a full commit SHA and a full release tag comment. All
// actions from one repository must use the same SHA and the same tag.
func validateWorkflowActionPins(actions []parsedWorkflowAction) error {
	type pin struct{ sha, version string }
	repositoryPins := make(map[string]pin)
	for _, action := range actions {
		if !actionCommitSHAPattern.MatchString(action.sha) {
			return fmt.Errorf("%s uses non-SHA ref %q", action.name, action.sha)
		}
		if !releaseTagPattern.MatchString(action.version) {
			return fmt.Errorf("%s@%s has release tag comment %q, want a full vX.Y.Z tag", action.name, action.sha, action.version)
		}
		parts := strings.SplitN(action.name, "/", 3)
		if len(parts) < 2 {
			return fmt.Errorf("action %q has no owner/repository name", action.name)
		}
		repository := parts[0] + "/" + parts[1]
		current := pin{sha: action.sha, version: action.version}
		if previous, ok := repositoryPins[repository]; ok && previous != current {
			return fmt.Errorf("%s is pinned as %s # %s and %s # %s", repository, previous.sha, previous.version, current.sha, current.version)
		}
		repositoryPins[repository] = current
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
		"`go.mod`",
		"docker/staging-minecraft.Dockerfile",
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
