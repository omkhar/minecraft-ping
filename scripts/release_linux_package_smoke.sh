#!/usr/bin/env bash
set -euo pipefail

DIST_DIR="${1:-dist}"
TARGET_ARCH="${2:-}"

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

  for pattern in "$@"; do
    match="$(find "$DIST_DIR" -maxdepth 1 -type f -name "$pattern" | sort | head -n 1 || true)"
    if [[ -n "$match" ]]; then
      basename "$match"
      return 0
    fi
  done

  echo "missing release package matching patterns: $*" >&2
  exit 1
}

deb_pkg="$(find_package "${deb_patterns[@]}")"
rpm_pkg="$(find_package "${rpm_patterns[@]}")"
apk_pkg="$(find_package "${apk_patterns[@]}")"

run_container_smoke() {
  local label="$1"
  local image="$2"
  local command="$3"

  echo "smoke: ${label} (${TARGET_ARCH})"
  docker run --rm \
    --platform "$docker_platform" \
    -v "$(cd "$DIST_DIR" && pwd):/dist:ro" \
    "$image" \
    sh -ceu "$command"
}

run_container_smoke \
  "debian" \
  "debian:12-slim" \
  "export DEBIAN_FRONTEND=noninteractive
   apt-get update
   apt-get install -y ca-certificates
   apt-get install -y /dist/$deb_pkg
   /usr/bin/minecraft-ping -V
   /usr/bin/minecraft-ping -h >/dev/null"

run_container_smoke \
  "fedora" \
  "fedora:42" \
  "dnf install -y --setopt=localpkg_gpgcheck=0 /dist/$rpm_pkg
   /usr/bin/minecraft-ping -V
   /usr/bin/minecraft-ping -h >/dev/null"

run_container_smoke \
  "alpine" \
  "alpine:3.21" \
  "apk add --no-cache --allow-untrusted /dist/$apk_pkg
   /usr/bin/minecraft-ping -V
   /usr/bin/minecraft-ping -h >/dev/null"

echo "linux package smoke succeeded for $TARGET_ARCH"
