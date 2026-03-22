#!/usr/bin/env bash
set -euo pipefail

IMAGE="${MINECRAFT_STAGING_IMAGE:-minecraft-staging-image:ci}"
CONTAINER_CLI="${CONTAINER_CLI:-docker}"
CONTAINER_NAME="${MINECRAFT_RELEASE_INTEGRATION_CONTAINER_NAME:-minecraft-ping-release-integration-$$}"
JAVA_IPV4_PORT="${MINECRAFT_RELEASE_INTEGRATION_JAVA_IPV4_PORT:-45565}"
JAVA_IPV6_PORT="${MINECRAFT_RELEASE_INTEGRATION_JAVA_IPV6_PORT:-45566}"
BEDROCK_IPV4_PORT="${MINECRAFT_RELEASE_INTEGRATION_BEDROCK_IPV4_PORT:-49132}"
BEDROCK_IPV6_PORT="${MINECRAFT_RELEASE_INTEGRATION_BEDROCK_IPV6_PORT:-49133}"
WORK_DIR="$(mktemp -d)"
trap 'rm -rf "$WORK_DIR"' EXIT

go build -o "$WORK_DIR/minecraft-ping" .
go build -o "$WORK_DIR/minecraft-staging-server" ./cmd/staging-server

go run ./cmd/release-integration \
  -binary "$WORK_DIR/minecraft-ping" \
  -backend "${MINECRAFT_RELEASE_INTEGRATION_BACKEND:-binary}" \
  -container-name "$CONTAINER_NAME" \
  -java-ipv4-port "$JAVA_IPV4_PORT" \
  -java-ipv6-port "$JAVA_IPV6_PORT" \
  -bedrock-ipv4-port "$BEDROCK_IPV4_PORT" \
  -bedrock-ipv6-port "$BEDROCK_IPV6_PORT" \
  -server-binary "$WORK_DIR/minecraft-staging-server" \
  -container-cli "$CONTAINER_CLI" \
  -image-archive "${MINECRAFT_STAGING_IMAGE_ARCHIVE:-}" \
  -image-tag "$IMAGE"
