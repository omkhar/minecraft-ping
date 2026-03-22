#!/usr/bin/env bash
set -euo pipefail

IMAGE="${MINECRAFT_STAGING_IMAGE:-minecraft-staging-image:ci}"
CONTAINER_CLI="${CONTAINER_CLI:-docker}"
WORK_DIR="$(mktemp -d)"
trap 'rm -rf "$WORK_DIR"' EXIT

go build -o "$WORK_DIR/minecraft-ping" .
go build -o "$WORK_DIR/minecraft-staging-server" ./cmd/staging-server

go run ./cmd/release-integration \
  -binary "$WORK_DIR/minecraft-ping" \
  -backend "${MINECRAFT_RELEASE_INTEGRATION_BACKEND:-binary}" \
  -server-binary "$WORK_DIR/minecraft-staging-server" \
  -container-cli "$CONTAINER_CLI" \
  -image-archive "${MINECRAFT_STAGING_IMAGE_ARCHIVE:-}" \
  -image-tag "$IMAGE"
