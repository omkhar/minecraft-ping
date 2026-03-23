#!/usr/bin/env bash
set -euo pipefail

DIST_DIR="${1:-dist}"
TARGET_ARCH="${2:-}"
CONTAINER_CLI="${CONTAINER_CLI:-docker}"

if [[ -z "$TARGET_ARCH" ]]; then
  echo "usage: $0 <dist-dir> <amd64|arm64>" >&2
  exit 1
fi

if [[ ! -d "$DIST_DIR" ]]; then
  echo "release dist directory does not exist: $DIST_DIR" >&2
  exit 1
fi

case "$TARGET_ARCH" in
  amd64)
    docker_platform="linux/amd64"
    deb_patterns=("*amd64.deb")
    rpm_patterns=("*x86_64.rpm" "*amd64.rpm")
    apk_patterns=("*x86_64.apk" "*amd64.apk")
    ;;
  arm64)
    docker_platform="linux/arm64"
    deb_patterns=("*arm64.deb")
    rpm_patterns=("*aarch64.rpm" "*arm64.rpm")
    apk_patterns=("*aarch64.apk" "*arm64.apk")
    ;;
  *)
    echo "unsupported release architecture: $TARGET_ARCH" >&2
    exit 1
    ;;
esac

find_package() {
  local pattern
  local match
  local -a matches=()
  local -a unique_matches=()

  for pattern in "$@"; do
    while IFS= read -r match; do
      [[ -n "$match" ]] || continue
      matches+=("$(basename "$match")")
    done < <(find "$DIST_DIR" -maxdepth 1 -type f -name "$pattern" | sort)
  done

  if [[ "${#matches[@]}" -eq 0 ]]; then
    echo "missing release package matching patterns: $*" >&2
    exit 1
  fi

  while IFS= read -r match; do
    [[ -n "$match" ]] || continue
    unique_matches+=("$match")
  done < <(printf '%s\n' "${matches[@]}" | sort -u)
  matches=("${unique_matches[@]}")
  if [[ "${#matches[@]}" -ne 1 ]]; then
    printf 'expected exactly one package for patterns %s, found:\n' "$*" >&2
    printf '  %s\n' "${matches[@]}" >&2
    exit 1
  fi

  printf '%s\n' "${matches[0]}"
}

deb_pkg="$(find_package "${deb_patterns[@]}")"
rpm_pkg="$(find_package "${rpm_patterns[@]}")"
apk_pkg="$(find_package "${apk_patterns[@]}")"
expected_version="${deb_pkg#minecraft-ping_}"
expected_version="${expected_version%_linux_*}"
expected_version_line="minecraft-ping ${expected_version}"

run_container_smoke() {
  local label="$1"
  local image="$2"
  local command="$3"
  local container_id

  echo "smoke: ${label} (${TARGET_ARCH})"
  container_id="$("$CONTAINER_CLI" create --platform "$docker_platform" "$image" sh -ceu "$command")"
  "$CONTAINER_CLI" cp "$(cd "$DIST_DIR" && pwd)/." "${container_id}:/dist"
  if ! "$CONTAINER_CLI" start -a "$container_id"; then
    "$CONTAINER_CLI" rm -f "$container_id" >/dev/null 2>&1 || true
    return 1
  fi
  "$CONTAINER_CLI" rm -f "$container_id" >/dev/null 2>&1 || true
}

manpage_check='test -f /usr/share/man/man1/minecraft-ping.1 || test -f /usr/share/man/man1/minecraft-ping.1.gz'

run_container_smoke \
  "debian" \
  "debian:12" \
  "export DEBIAN_FRONTEND=noninteractive
   dpkg -i /dist/$deb_pkg
   ${manpage_check}
   test \"\$(/usr/bin/minecraft-ping -V)\" = \"$expected_version_line\"
   /usr/bin/minecraft-ping -h >/dev/null"

run_container_smoke \
  "fedora" \
  "fedora:42" \
  "rpm -i --nosignature /dist/$rpm_pkg
   ${manpage_check}
   test \"\$(/usr/bin/minecraft-ping -V)\" = \"$expected_version_line\"
   /usr/bin/minecraft-ping -h >/dev/null"

run_container_smoke \
  "alpine" \
  "alpine:3.21" \
  "apk add --no-cache --no-network --allow-untrusted --repositories-file /dev/null --force-non-repository /dist/$apk_pkg
   ${manpage_check}
   test \"\$(/usr/bin/minecraft-ping -V)\" = \"$expected_version_line\"
   /usr/bin/minecraft-ping -h >/dev/null"

echo "linux package smoke succeeded for $TARGET_ARCH"
