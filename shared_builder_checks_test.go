package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestSharedBuilderChecksLoadsEnvFileWithoutExecutingIt(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash-based shared builder wrapper is not exercised on windows")
	}
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	repoRoot, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	scriptPath := filepath.Join(repoRoot, "scripts", "run_shared_builder_checks.sh")
	scriptContents, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	tempDir := t.TempDir()
	cloneDir := filepath.Join(tempDir, "clone")
	clone := exec.Command("git", "clone", "--no-local", repoRoot, cloneDir)
	clone.Dir = repoRoot
	if output, err := clone.CombinedOutput(); err != nil {
		t.Fatalf("git clone error = %v: %s", err, output)
	}
	cloneScriptPath := filepath.Join(cloneDir, "scripts", "run_shared_builder_checks.sh")
	if err := os.WriteFile(cloneScriptPath, scriptContents, 0o755); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	configName := exec.Command("git", "config", "user.name", "Codex Test")
	configName.Dir = cloneDir
	if output, err := configName.CombinedOutput(); err != nil {
		t.Fatalf("git config user.name error = %v: %s", err, output)
	}
	configEmail := exec.Command("git", "config", "user.email", "codex@example.invalid")
	configEmail.Dir = cloneDir
	if output, err := configEmail.CombinedOutput(); err != nil {
		t.Fatalf("git config user.email error = %v: %s", err, output)
	}
	commitScript := exec.Command("git", "commit", "--quiet", "--no-gpg-sign", "-am", "test current shared builder wrapper")
	commitScript.Dir = cloneDir
	if output, err := commitScript.CombinedOutput(); err != nil {
		t.Fatalf("git commit error = %v: %s", err, output)
	}

	fakeBinDir := filepath.Join(tempDir, "fake-bin")
	if err := os.Mkdir(fakeBinDir, 0o755); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}
	fakeUname := filepath.Join(fakeBinDir, "uname")
	if err := os.WriteFile(fakeUname, []byte("#!/usr/bin/env bash\nprintf 'Linux\\n'\n"), 0o755); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	markerPath := filepath.Join(tempDir, "marker")
	socketPath := filepath.Join(tempDir, "missing.sock")
	envFile := filepath.Join(tempDir, "shared-builder.env")
	envContents := strings.Join([]string{
		"SHARED_BUILDER_HOST_CLI=sh",
		"SHARED_BUILDER_SOCKET=" + socketPath,
		"SHARED_BUILDER_MARKER=$(printf injected > " + markerPath + ")",
		"",
	}, "\n")
	if err := os.WriteFile(envFile, []byte(envContents), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cmd := exec.Command("bash", filepath.Join(cloneDir, "scripts/run_shared_builder_checks.sh"))
	cmd.Dir = cloneDir
	cmd.Env = append(os.Environ(),
		"PATH="+fakeBinDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"SHARED_BUILDER_ENV_FILE="+envFile,
	)
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatal("run_shared_builder_checks.sh succeeded, want socket validation failure")
	}
	if strings.Contains(string(output), markerPath) {
		t.Fatalf("script output unexpectedly referenced the marker path: %s", output)
	}
	if _, err := os.Stat(markerPath); err == nil {
		t.Fatal("env file payload executed unexpectedly")
	}
	if !strings.Contains(string(output), socketPath) {
		t.Fatalf("script output = %q, want missing socket path %q", output, socketPath)
	}
}
