#!/usr/bin/env bash
set -euo pipefail

DIST_DIR="${1:-dist}"
GORELEASER_BIN="${GORELEASER_BIN:-$(command -v goreleaser || true)}"

if [[ -z "$GORELEASER_BIN" ]]; then
  echo "goreleaser binary not found in PATH; set GORELEASER_BIN to override" >&2
  exit 1
fi

if [[ ! -d "$DIST_DIR" ]]; then
  echo "release dist directory does not exist: $DIST_DIR" >&2
  exit 1
fi

if [[ ! -f "$DIST_DIR/checksums.txt" ]]; then
  echo "release dist directory does not contain checksums.txt: $DIST_DIR" >&2
  exit 1
fi

if ! git rev-parse --show-toplevel >/dev/null 2>&1; then
  echo "release reproducibility smoke must run from a git checkout" >&2
  exit 1
fi

if ! git diff --quiet --ignore-submodules HEAD --; then
  echo "release reproducibility smoke requires a clean tracked worktree" >&2
  exit 1
fi

worktree=""
cleanup() {
  if [[ -n "$worktree" ]]; then
    git worktree remove --force "$worktree" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

worktree="$(mktemp -d)"
git worktree add --detach "$worktree" HEAD >/dev/null

(
  cd "$worktree"
  "$GORELEASER_BIN" release --snapshot --clean --skip=sign >/dev/null
)

if ! diff -u "$DIST_DIR/checksums.txt" "$worktree/dist/checksums.txt"; then
  echo "release artifacts are not reproducible across checkout paths" >&2
  exit 1
fi

echo "release reproducibility smoke succeeded"
