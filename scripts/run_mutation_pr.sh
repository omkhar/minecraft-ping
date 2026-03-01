#!/usr/bin/env bash
set -euo pipefail

MUTEST_BIN="$(go env GOPATH)/bin/go-mutesting"
if [[ ! -x "$MUTEST_BIN" ]]; then
  echo "go-mutesting not installed at $MUTEST_BIN" >&2
  exit 2
fi

base_ref="${GITHUB_BASE_REF:-main}"
git fetch origin "$base_ref" --depth=1 >/dev/null 2>&1 || true

mapfile -t go_files < <(git diff --name-only "origin/$base_ref...HEAD" -- '*.go' || true)
if [[ "${#go_files[@]}" -eq 0 ]]; then
  echo "No Go files changed; skipping PR mutation run."
  exit 0
fi

mapfile -t dirs < <(printf '%s\n' "${go_files[@]}" | xargs -n1 dirname | sort -u)
for dir in "${dirs[@]}"; do
  [[ -d "$dir" ]] || continue
  echo "Running mutation tests for $dir"
  "$MUTEST_BIN" --config .go-mutesting.yml --exec-timeout 30 "$dir"
done
