#!/usr/bin/env bash
set -euo pipefail

repo_root="$(git rev-parse --show-toplevel 2>/dev/null || true)"
if [[ -z "${repo_root}" ]]; then
  echo "unable to determine repository root" >&2
  exit 1
fi

version_file="${repo_root}/.github/goreleaser-version.txt"
if [[ ! -f "${version_file}" ]]; then
  echo "missing GoReleaser version file: ${version_file}" >&2
  exit 1
fi

version="$(tr -d '[:space:]' < "${version_file}")"
if [[ -z "${version}" ]]; then
  echo "GoReleaser version file is empty: ${version_file}" >&2
  exit 1
fi

printf '%s\n' "${version}"
