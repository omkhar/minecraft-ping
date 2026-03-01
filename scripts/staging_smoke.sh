#!/usr/bin/env bash
set -euo pipefail

USER_AGENT="${USER_AGENT:-Better Uptime Bot Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/74.0.3729.169 Safari/537.36}"
IMAGE="${MINECRAFT_STAGING_IMAGE:-minecraft-staging-image:ci}"
PORT="${MINECRAFT_STAGING_PORT:-25565}"

cleanup() {
  docker rm -f mc-staging >/dev/null 2>&1 || true
}
trap cleanup EXIT

docker rm -f mc-staging >/dev/null 2>&1 || true

docker run -d --name mc-staging -p "${PORT}:25565" \
  -e EULA=TRUE \
  -e ONLINE_MODE=FALSE \
  -e MOTD="staging-smoke" \
  "$IMAGE" >/dev/null

for i in {1..30}; do
  if nc -z 127.0.0.1 "$PORT" >/dev/null 2>&1; then
    break
  fi
  sleep 5
  if [[ "$i" -eq 30 ]]; then
    echo "Timed out waiting for minecraft staging service" >&2
    docker logs mc-staging || true
    exit 1
  fi
done

echo "Using user-agent string for consistency: $USER_AGENT"

for i in {1..20}; do
  if output="$(go run . -server 127.0.0.1 -port "$PORT" -allow-private -timeout 12s 2>/tmp/mcping.err)"; then
    latency="$(printf '%s\n' "$output" | sed -nE 's/^Ping time is ([0-9]+).*$/\1/p' | tail -n 1)"
    if [[ -n "$latency" ]]; then
      echo "staging smoke succeeded: latency_ms=$latency"
      exit 0
    fi
  fi
  sleep 3
done

echo "minecraft-ping staging smoke failed" >&2
cat /tmp/mcping.err >&2 || true
docker logs mc-staging >&2 || true
exit 1
