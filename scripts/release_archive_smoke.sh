#!/usr/bin/env bash
set -euo pipefail

DIST_DIR="${1:-dist}"

if [[ ! -d "$DIST_DIR" ]]; then
  echo "release dist directory does not exist: $DIST_DIR" >&2
  exit 1
fi

require_single_archive() {
  local pattern="$1"
  local match
  local -a matches=()

  while IFS= read -r match; do
    [[ -n "$match" ]] || continue
    matches+=("$match")
  done < <(find "$DIST_DIR" -maxdepth 1 -type f -name "$pattern" | sort)
  if [[ "${#matches[@]}" -ne 1 ]]; then
    echo "expected exactly one archive matching $pattern, found ${#matches[@]}" >&2
    exit 1
  fi

  printf '%s\n' "${matches[0]}"
}

check_tar_archive() {
  local archive="$1"
  local binary="$2"
  local entries

  entries="$(tar -tzf "$archive")"
  grep -Fxq "LICENSE" <<<"$entries" || { echo "missing LICENSE in $archive" >&2; exit 1; }
  grep -Fxq "README.md" <<<"$entries" || { echo "missing README.md in $archive" >&2; exit 1; }
  grep -Fxq "man/minecraft-ping.1" <<<"$entries" || { echo "missing man/minecraft-ping.1 in $archive" >&2; exit 1; }
  grep -Fxq "$binary" <<<"$entries" || { echo "missing $binary in $archive" >&2; exit 1; }
}

check_zip_archive() {
  local archive="$1"
  local binary="$2"
  local entries

  entries="$(unzip -Z1 "$archive")"
  grep -Fxq "LICENSE" <<<"$entries" || { echo "missing LICENSE in $archive" >&2; exit 1; }
  grep -Fxq "README.md" <<<"$entries" || { echo "missing README.md in $archive" >&2; exit 1; }
  grep -Fxq "man/minecraft-ping.1" <<<"$entries" || { echo "missing man/minecraft-ping.1 in $archive" >&2; exit 1; }
  grep -Fxq "$binary" <<<"$entries" || { echo "missing $binary in $archive" >&2; exit 1; }
}

check_source_archive() {
  local archive="$1"
  local entries

  entries="$(tar -tzf "$archive")"
  grep -Fxq "LICENSE" <<<"$entries" || { echo "missing LICENSE in $archive" >&2; exit 1; }
  grep -Fxq "README.md" <<<"$entries" || { echo "missing README.md in $archive" >&2; exit 1; }
  grep -Fxq "go.mod" <<<"$entries" || { echo "missing go.mod in $archive" >&2; exit 1; }
  grep -Fxq "man/minecraft-ping.1" <<<"$entries" || { echo "missing man/minecraft-ping.1 in $archive" >&2; exit 1; }
}

check_tar_archive "$(require_single_archive 'minecraft-ping_*_Darwin_amd64.tar.gz')" "minecraft-ping"
check_tar_archive "$(require_single_archive 'minecraft-ping_*_Darwin_arm64.tar.gz')" "minecraft-ping"
check_tar_archive "$(require_single_archive 'minecraft-ping_*_Linux_amd64.tar.gz')" "minecraft-ping"
check_tar_archive "$(require_single_archive 'minecraft-ping_*_Linux_arm64.tar.gz')" "minecraft-ping"
check_zip_archive "$(require_single_archive 'minecraft-ping_*_Windows_amd64.zip')" "minecraft-ping.exe"
check_zip_archive "$(require_single_archive 'minecraft-ping_*_Windows_arm64.zip')" "minecraft-ping.exe"
check_source_archive "$(require_single_archive 'minecraft-ping_*_source.tar.gz')"

echo "release archive smoke succeeded"
