#!/usr/bin/env bash

set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "${script_dir}/.." && pwd)"

disallowed_files=(
  "docker/build-runner.Dockerfile"
  "scripts/run_shared_builder_checks.sh"
  "scripts/run_shared_builder_inner.sh"
  "shared_builder_checks_test.go"
)

for relative_path in "${disallowed_files[@]}"; do
  if [[ -e "${repo_root}/${relative_path}" ]]; then
    echo "internal-only file should not exist in the public repo: ${relative_path}" >&2
    exit 1
  fi
done

disallowed_dirs=(
  ".codex"
  ".gemini"
)

for relative_path in "${disallowed_dirs[@]}"; do
  if [[ -d "${repo_root}/${relative_path}" ]]; then
    echo "agent-specific directory should not exist in the public repo: ${relative_path}" >&2
    exit 1
  fi
done

temp_artifacts="$(
  find "${repo_root}" -path "${repo_root}/.git" -prune -o -type f \( -name '*.tmp' -o -name '*.orig' -o -name '*.rej' \) -print
)"
if [[ -n "${temp_artifacts}" ]]; then
  echo "temporary or patch-reject files should not exist in the public repo" >&2
  printf '%s\n' "${temp_artifacts}" >&2
  exit 1
fi

scan_paths=(
  "README.md"
  "CONTRIBUTING.md"
  "SECURITY.md"
  "SUPPORT.md"
  "GOVERNANCE.md"
  "CHANGELOG.md"
  "AGENTS.md"
  "CLAUDE.md"
  "GEMINI.md"
  "Makefile"
  "docs"
  "scripts/run_mutation_pr.sh"
  "scripts/run_mutation_supported.sh"
  "scripts/run_release_integration.sh"
  "scripts/clean_repo_detritus.sh"
  "scripts/read_goreleaser_version.sh"
  "scripts/release_archive_smoke.sh"
  "scripts/install_agent_clis.sh"
  "scripts/release_linux_package_smoke.sh"
  "scripts/release_reproducibility.sh"
  "scripts/sync_agent_surfaces.sh"
  "scripts/verify_agent_runtime_smoke.sh"
  "scripts/verify_agent_surfaces.sh"
  ".github/ISSUE_TEMPLATE"
  ".github/CODEOWNERS"
  ".github/dependabot.yml"
  ".github/labeler.yml"
  ".github/pull_request_template.md"
  ".github/release-drafter.yml"
  ".github/goreleaser-version.txt"
  ".github/workflows"
  ".agents/skills"
  ".claude/skills"
)

existing_paths=()
for relative_path in "${scan_paths[@]}"; do
  if [[ -e "${repo_root}/${relative_path}" ]]; then
    existing_paths+=("${repo_root}/${relative_path}")
  fi
done

patterns=(
  "run_shared_builder"
  "build-runner.Dockerfile"
  "codex-runs"
  ".shared-builder.local.env"
  "When this repository is public"
  "Once the repository is public"
  "visibility and ownership support"
)

for pattern in "${patterns[@]}"; do
  if rg -n -F -- "${pattern}" "${existing_paths[@]}" >/dev/null; then
    echo "public repo surface still contains forbidden text: ${pattern}" >&2
    rg -n -F -- "${pattern}" "${existing_paths[@]}" >&2
    exit 1
  fi
done
