#!/usr/bin/env bash
set -euo pipefail

repo_root="$(git rev-parse --show-toplevel 2>/dev/null || true)"
if [[ -z "${repo_root}" ]]; then
  echo "shared builder checks must run from a git checkout" >&2
  exit 1
fi
cd "${repo_root}"

run_id="${SHARED_BUILDER_RUN_ID:-shared-builder-$$}"
state_root="${SHARED_BUILDER_STATE_ROOT:-/tmp/minecraft-ping-shared-builder-${run_id}}"
container_cli="${SHARED_BUILDER_CONTAINER_CLI:-docker}"

mkdir -p \
  "${state_root}/gocache" \
  "${state_root}/gobin" \
  "${state_root}/gomodcache" \
  "${state_root}/gopath" \
  "${state_root}/gotmp" \
  "${state_root}/home" \
  "${state_root}/tmp"

export HOME="${state_root}/home"
export GOBIN="${state_root}/gobin"
export GOCACHE="${state_root}/gocache"
export GOMODCACHE="${state_root}/gomodcache"
export GOPATH="${state_root}/gopath"
export GOTMPDIR="${state_root}/gotmp"
export TMPDIR="${state_root}/tmp"
export CONTAINER_CLI="${container_cli}"

if ! command -v "${container_cli}" >/dev/null 2>&1; then
  echo "container CLI not found in runner image: ${container_cli}" >&2
  exit 1
fi

safe_run_id="$(printf '%s' "${run_id}" | tr -cs '[:alnum:]._-' '-')"
export MINECRAFT_STAGING_IMAGE="${MINECRAFT_STAGING_IMAGE:-minecraft-staging-image:${safe_run_id}}"
export MINECRAFT_RELEASE_INTEGRATION_CONTAINER_NAME="${MINECRAFT_RELEASE_INTEGRATION_CONTAINER_NAME:-minecraft-ping-release-${safe_run_id}}"

if [[ -z "${MINECRAFT_RELEASE_INTEGRATION_JAVA_IPV4_PORT:-}" ]] || \
  [[ -z "${MINECRAFT_RELEASE_INTEGRATION_JAVA_IPV6_PORT:-}" ]] || \
  [[ -z "${MINECRAFT_RELEASE_INTEGRATION_BEDROCK_IPV4_PORT:-}" ]] || \
  [[ -z "${MINECRAFT_RELEASE_INTEGRATION_BEDROCK_IPV6_PORT:-}" ]]; then
  hash="$(printf '%s' "${safe_run_id}" | cksum | awk '{print $1}')"
  base_port=$((40000 + (hash % 20000)))
  export MINECRAFT_RELEASE_INTEGRATION_JAVA_IPV4_PORT="${MINECRAFT_RELEASE_INTEGRATION_JAVA_IPV4_PORT:-${base_port}}"
  export MINECRAFT_RELEASE_INTEGRATION_JAVA_IPV6_PORT="${MINECRAFT_RELEASE_INTEGRATION_JAVA_IPV6_PORT:-$((base_port + 1))}"
  export MINECRAFT_RELEASE_INTEGRATION_BEDROCK_IPV4_PORT="${MINECRAFT_RELEASE_INTEGRATION_BEDROCK_IPV4_PORT:-$((base_port + 2))}"
  export MINECRAFT_RELEASE_INTEGRATION_BEDROCK_IPV6_PORT="${MINECRAFT_RELEASE_INTEGRATION_BEDROCK_IPV6_PORT:-$((base_port + 3))}"
fi

resolve_goreleaser_version() {
  if [[ -n "${SHARED_BUILDER_GORELEASER_VERSION:-}" ]]; then
    printf '%s\n' "${SHARED_BUILDER_GORELEASER_VERSION}"
    return 0
  fi

  awk '/^[[:space:]]*GORELEASER_VERSION:/ { print $2; exit }' .github/workflows/go.yml
}

detect_package_arch() {
  case "$(uname -m)" in
    x86_64|amd64)
      printf 'amd64\n'
      ;;
    aarch64|arm64)
      printf 'arm64\n'
      ;;
    *)
      return 1
      ;;
  esac
}

goreleaser_version="$(resolve_goreleaser_version)"
if [[ -z "${goreleaser_version}" ]]; then
  echo "unable to resolve GoReleaser version from SHARED_BUILDER_GORELEASER_VERSION or .github/workflows/go.yml" >&2
  exit 1
fi

echo "installing goreleaser ${goreleaser_version} into ${GOBIN}"
GOTOOLCHAIN="$(go env GOVERSION)" go install "github.com/goreleaser/goreleaser/v2@${goreleaser_version}"
export GORELEASER_BIN="${GOBIN}/goreleaser"

echo "running isolated Go validation"
go mod verify
go test ./...
go test -race ./...
go test -shuffle=on -count=1 ./...
go vet ./...

echo "building snapshot release artifacts"
"${GORELEASER_BIN}" release --snapshot --clean --skip=sign

echo "smoke testing release archives"
scripts/release_archive_smoke.sh dist

echo "checking release reproducibility"
scripts/release_reproducibility.sh dist

package_arches="${SHARED_BUILDER_PACKAGE_ARCHES:-}"
if [[ -z "${package_arches}" ]]; then
  package_arches="$(detect_package_arch || true)"
fi

if [[ -n "${package_arches}" ]]; then
  IFS=, read -r -a package_arch_list <<<"${package_arches}"
  for arch in "${package_arch_list[@]}"; do
    arch="$(printf '%s' "${arch}" | xargs)"
    [[ -n "${arch}" ]] || continue
    echo "smoke testing linux packages for ${arch}"
    scripts/release_linux_package_smoke.sh dist "${arch}"
  done
fi

echo "building isolated staging image ${MINECRAFT_STAGING_IMAGE}"
"${container_cli}" build -f docker/staging-minecraft.Dockerfile -t "${MINECRAFT_STAGING_IMAGE}" .

echo "running container-backed release integration"
MINECRAFT_RELEASE_INTEGRATION_BACKEND=container scripts/run_release_integration.sh

echo "shared builder checks succeeded"
echo "dist: ${repo_root}/dist"
