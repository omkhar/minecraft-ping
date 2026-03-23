#!/usr/bin/env bash
set -euo pipefail

MUTEST_BIN="$(go env GOPATH)/bin/go-mutesting"
if [[ ! -x "$MUTEST_BIN" ]]; then
  echo "go-mutesting not installed at $MUTEST_BIN" >&2
  exit 2
fi

base_ref="${GITHUB_BASE_REF:-main}"
fetch_remote="origin"
if [[ -n "${GITHUB_TOKEN:-}" && -n "${GITHUB_REPOSITORY:-}" ]]; then
  fetch_remote="https://x-access-token:${GITHUB_TOKEN}@github.com/${GITHUB_REPOSITORY}.git"
fi

git fetch "$fetch_remote" "$base_ref:refs/remotes/origin/$base_ref" --depth=1 >/dev/null
if ! git rev-parse --verify "origin/$base_ref^{commit}" >/dev/null 2>&1; then
  echo "unable to resolve origin/$base_ref after fetch" >&2
  exit 1
fi

mapfile -t go_files < <(git diff --name-only "origin/$base_ref...HEAD" -- '*.go')
if [[ "${#go_files[@]}" -eq 0 ]]; then
  echo "No Go files changed; skipping PR mutation run."
  exit 0
fi

mapfile -t dirs < <(printf '%s\n' "${go_files[@]}" | xargs -n1 dirname | sort -u)

eligible_dirs=()
for dir in "${dirs[@]}"; do
  [[ -d "$dir" ]] || continue

  package_path="$dir"
  if [[ "$package_path" != "." ]]; then
    package_path="./$package_path"
  fi

  if ! package_info="$(go list -f '{{.Name}} {{len .TestGoFiles}} {{len .XTestGoFiles}}' "$package_path" 2>/dev/null)"; then
    echo "Skipping mutation tests for $dir (unable to load package metadata)"
    continue
  fi

  read -r package_name package_tests package_xtests <<<"$package_info"
  if [[ "$package_name" == "main" ]]; then
    echo "Skipping mutation tests for $dir (package main is not a supported mutation target)"
    continue
  fi

  if (( package_tests + package_xtests == 0 )); then
    echo "Skipping mutation tests for $dir (no package tests found)"
    continue
  fi

  eligible_dirs+=("$dir")
done

if [[ "${#eligible_dirs[@]}" -eq 0 ]]; then
  echo "No eligible non-main Go packages changed; skipping PR mutation run."
  exit 0
fi

for dir in "${eligible_dirs[@]}"; do
  echo "Running mutation tests for $dir"
  "$MUTEST_BIN" --config .go-mutesting.yml --exec-timeout 30 "$dir"
done
