#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Run heavyweight validation inside an isolated container on a shared Linux builder.

The script clones the current checkout into a unique run root, builds a dedicated
runner image, mounts a Docker-compatible socket into that runner, and executes
the release-path checks there with unique caches, image tags, container names,
and integration ports.

Environment:
  SHARED_BUILDER_ENV_FILE         Optional env file loaded before resolving
                                  shared-builder settings.
                                  Default: ./.shared-builder.local.env
  SHARED_BUILDER_ROOT              Base directory for per-run worktrees.
                                  Default: $HOME/codex-runs/minecraft-ping
  SHARED_BUILDER_RUN_ID           Override the generated run identifier.
  SHARED_BUILDER_HOST_CLI         Host container runtime used for the outer
                                  runner container. Default: ${CONTAINER_CLI:-docker}
  SHARED_BUILDER_SOCKET           Docker-compatible socket path mounted into the
                                  runner container. Default: /var/run/docker.sock
  SHARED_BUILDER_PACKAGE_ARCHES   Comma-separated package smoke architectures.
                                  Default: host arch only
  SHARED_BUILDER_GORELEASER_VERSION
                                  Override the CI-pinned goreleaser version.
  SHARED_BUILDER_KEEP_RUN_ROOT    Keep the isolated checkout after success.
                                  Default: 1
  SHARED_BUILDER_KEEP_RUNNER_IMAGE
                                  Keep the runner image after the run. Default: 0
EOF
}

if [[ "${1:-}" == "--help" ]] || [[ "${1:-}" == "-h" ]]; then
  usage
  exit 0
fi

if [[ "$(uname -s)" != "Linux" ]]; then
  echo "shared builder checks require a Linux host because the runner uses host networking" >&2
  exit 1
fi

repo_root="$(git rev-parse --show-toplevel 2>/dev/null || true)"
if [[ -z "${repo_root}" ]]; then
  echo "shared builder checks must start from a git checkout" >&2
  exit 1
fi

cd "${repo_root}"

env_file="${SHARED_BUILDER_ENV_FILE:-${repo_root}/.shared-builder.local.env}"
if [[ -f "${env_file}" ]]; then
  # shellcheck disable=SC1090
  . "${env_file}"
fi

if [[ -n "$(git status --porcelain --untracked-files=normal)" ]]; then
  echo "shared builder checks require a clean checkout; commit or stash changes first" >&2
  exit 1
fi

host_cli="${SHARED_BUILDER_HOST_CLI:-${CONTAINER_CLI:-docker}}"
socket_path="${SHARED_BUILDER_SOCKET:-/var/run/docker.sock}"
run_id="${SHARED_BUILDER_RUN_ID:-$(date -u +%Y%m%dT%H%M%SZ)-$$}"
run_id="$(printf '%s' "${run_id}" | tr -cs '[:alnum:]._-' '-')"
runs_root="${SHARED_BUILDER_ROOT:-${HOME}/codex-runs/minecraft-ping}"
keep_run_root="${SHARED_BUILDER_KEEP_RUN_ROOT:-1}"
keep_runner_image="${SHARED_BUILDER_KEEP_RUNNER_IMAGE:-0}"
run_root="${runs_root}/${run_id}"
checkout="${run_root}/repo"
runner_image="minecraft-ping-build-runner:${run_id}"

if ! command -v "${host_cli}" >/dev/null 2>&1; then
  echo "host container runtime not found: ${host_cli}" >&2
  exit 1
fi

if [[ ! -S "${socket_path}" ]]; then
  echo "docker-compatible socket not found: ${socket_path}" >&2
  exit 1
fi

socket_gid="$(stat -c '%g' "${socket_path}" 2>/dev/null || stat -f '%g' "${socket_path}")"

mkdir -p "${runs_root}"
if [[ -e "${run_root}" ]]; then
  echo "run root already exists: ${run_root}" >&2
  exit 1
fi
mkdir -p "${run_root}"

cleanup() {
  local status=$?

  if [[ "${keep_runner_image}" != "1" ]]; then
    "${host_cli}" image rm -f "${runner_image}" >/dev/null 2>&1 || true
  fi

  if [[ ${status} -eq 0 && "${keep_run_root}" == "0" ]]; then
    rm -rf "${run_root}"
  fi
}
trap cleanup EXIT

echo "cloning clean checkout into ${checkout}"
git clone --no-local "${repo_root}" "${checkout}"

echo "building runner image ${runner_image}"
"${host_cli}" build -f "${checkout}/docker/build-runner.Dockerfile" -t "${runner_image}" "${checkout}"

runner_args=(
  run
  --rm
  --network
  host
  --user
  "$(id -u):$(id -g)"
  --group-add
  "${socket_gid}"
  -e
  "SHARED_BUILDER_RUN_ID=${run_id}"
  -e
  "SHARED_BUILDER_CONTAINER_CLI=docker"
  -e
  "HOME=/tmp/shared-builder-home"
  -v
  "${checkout}:/workspace"
  -v
  "${socket_path}:/var/run/docker.sock"
  -w
  /workspace
)

if [[ -n "${SHARED_BUILDER_PACKAGE_ARCHES:-}" ]]; then
  runner_args+=(-e "SHARED_BUILDER_PACKAGE_ARCHES=${SHARED_BUILDER_PACKAGE_ARCHES}")
fi

if [[ -n "${SHARED_BUILDER_GORELEASER_VERSION:-}" ]]; then
  runner_args+=(-e "SHARED_BUILDER_GORELEASER_VERSION=${SHARED_BUILDER_GORELEASER_VERSION}")
fi

if [[ -n "${MINECRAFT_STAGING_IMAGE:-}" ]]; then
  runner_args+=(-e "MINECRAFT_STAGING_IMAGE=${MINECRAFT_STAGING_IMAGE}")
fi

if [[ -n "${MINECRAFT_RELEASE_INTEGRATION_CONTAINER_NAME:-}" ]]; then
  runner_args+=(-e "MINECRAFT_RELEASE_INTEGRATION_CONTAINER_NAME=${MINECRAFT_RELEASE_INTEGRATION_CONTAINER_NAME}")
fi

for variable in \
  MINECRAFT_RELEASE_INTEGRATION_JAVA_IPV4_PORT \
  MINECRAFT_RELEASE_INTEGRATION_JAVA_IPV6_PORT \
  MINECRAFT_RELEASE_INTEGRATION_BEDROCK_IPV4_PORT \
  MINECRAFT_RELEASE_INTEGRATION_BEDROCK_IPV6_PORT; do
  if [[ -n "${!variable:-}" ]]; then
    runner_args+=(-e "${variable}=${!variable}")
  fi
done

echo "running shared builder checks inside ${runner_image}"
"${host_cli}" "${runner_args[@]}" "${runner_image}" bash ./scripts/run_shared_builder_inner.sh

echo "shared builder checks completed"
echo "checkout: ${checkout}"
echo "dist: ${checkout}/dist"
