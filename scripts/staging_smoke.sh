#!/usr/bin/env bash
set -euo pipefail

IMAGE="${MINECRAFT_STAGING_IMAGE:-minecraft-staging-image:ci}"
PORT_IPV4="${MINECRAFT_STAGING_PORT_IPV4:-25565}"
PORT_IPV6="${MINECRAFT_STAGING_PORT_IPV6:-25566}"
IPV6_RELAY_PID=""

cleanup() {
  if [[ -n "$IPV6_RELAY_PID" ]]; then
    kill "$IPV6_RELAY_PID" >/dev/null 2>&1 || true
    wait "$IPV6_RELAY_PID" 2>/dev/null || true
  fi
  docker rm -f mc-staging >/dev/null 2>&1 || true
}
trap cleanup EXIT

docker rm -f mc-staging >/dev/null 2>&1 || true

docker run -d --name mc-staging \
  -p "127.0.0.1:${PORT_IPV4}:25565" \
  -e EULA=TRUE \
  -e ONLINE_MODE=FALSE \
  -e MOTD="staging-smoke" \
  "$IMAGE" >/dev/null

wait_for_tcp_listener() {
  local family="$1"
  local host="$2"
  local port="$3"

  for i in {1..30}; do
    if nc "-${family}" -z "$host" "$port" >/dev/null 2>&1; then
      return 0
    fi
    sleep 5
    if [[ "$i" -eq 30 ]]; then
      echo "Timed out waiting for Minecraft staging service on ${host}:${port}" >&2
      docker logs mc-staging >&2 || true
      exit 1
    fi
  done
}

run_ping_smoke() {
  local family_flag="$1"
  local host="$2"
  local port="$3"

  go run . -server "$host" -port "$port" -allow-private -timeout 12s "$family_flag"
}

start_ipv6_loopback_relay() {
  python3 - "$PORT_IPV6" "$PORT_IPV4" <<'PY' &
import selectors
import socket
import sys

listen_port = int(sys.argv[1])
target_port = int(sys.argv[2])

listener = socket.socket(socket.AF_INET6, socket.SOCK_STREAM)
listener.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
listener.bind(("::1", listen_port))
listener.listen()

while True:
    client, _ = listener.accept()
    upstream = socket.create_connection(("127.0.0.1", target_port))

    sel = selectors.DefaultSelector()
    sel.register(client, selectors.EVENT_READ, upstream)
    sel.register(upstream, selectors.EVENT_READ, client)

    try:
        while True:
            for key, _ in sel.select():
                source = key.fileobj
                destination = key.data
                payload = source.recv(65536)
                if not payload:
                    raise EOFError
                destination.sendall(payload)
    except EOFError:
        pass
    finally:
        sel.close()
        client.close()
        upstream.close()
PY

  IPV6_RELAY_PID=$!
}

wait_for_tcp_listener 4 127.0.0.1 "$PORT_IPV4"
start_ipv6_loopback_relay
wait_for_tcp_listener 6 ::1 "$PORT_IPV6"

last_output_ipv4=""
last_output_ipv6=""
latency_ipv4=""
latency_ipv6=""

for i in {1..80}; do
  last_output_ipv4="$(run_ping_smoke -4 127.0.0.1 "$PORT_IPV4" 2>&1)" || true
  latency_ipv4="$(printf '%s\n' "$last_output_ipv4" | sed -nE 's/^Ping time is ([0-9]+).*$/\1/p' | tail -n 1)"

  last_output_ipv6="$(run_ping_smoke -6 ::1 "$PORT_IPV6" 2>&1)" || true
  latency_ipv6="$(printf '%s\n' "$last_output_ipv6" | sed -nE 's/^Ping time is ([0-9]+).*$/\1/p' | tail -n 1)"

  if [[ -n "$latency_ipv4" && -n "$latency_ipv6" ]]; then
    echo "staging smoke succeeded: ipv4_latency_ms=$latency_ipv4 ipv6_latency_ms=$latency_ipv6"
    break
  fi
  sleep 5
done

if [[ -n "$latency_ipv4" && -n "$latency_ipv6" ]]; then
  exit 0
fi

echo "minecraft-ping staging smoke failed" >&2
echo "IPv4 smoke output:" >&2
printf '%s\n' "$last_output_ipv4" >&2
echo "IPv6 smoke output:" >&2
printf '%s\n' "$last_output_ipv6" >&2
docker logs mc-staging >&2 || true
exit 1
