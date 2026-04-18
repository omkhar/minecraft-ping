#!/usr/bin/env bash

set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "${script_dir}/.." && pwd)"
blacklist_file="${repo_root}/.go-mutesting.blacklist"

tool="$(command -v go-mutesting || true)"
if [[ -z "${tool}" && -x "$(go env GOPATH)/bin/go-mutesting" ]]; then
  tool="$(go env GOPATH)/bin/go-mutesting"
fi
if [[ -z "${tool}" ]]; then
  echo "go-mutesting not found in PATH or GOPATH/bin" >&2
  exit 1
fi

blacklist_args=()
if [[ -f "${blacklist_file}" ]]; then
  blacklist_args+=(--blacklist "${blacklist_file}")
fi

package_dir=""
backup_dir=""

restore_package() {
  if [[ -z "${package_dir}" || -z "${backup_dir}" || ! -d "${backup_dir}" ]]; then
    return
  fi

  find "${package_dir}" -mindepth 1 -maxdepth 1 -exec rm -rf {} +
  cp -R "${backup_dir}/." "${package_dir}/"
  rm -rf "${backup_dir}"
  backup_dir=""
}

trap 'restore_package' EXIT INT TERM

mapfile -t packages < <(cd "${repo_root}" && go list ./...)

eligible_packages=()
for package_path in "${packages[@]}"; do
  package_info="$(cd "${repo_root}" && go list -f '{{.Name}} {{len .TestGoFiles}} {{len .XTestGoFiles}}' "${package_path}")"
  read -r package_name package_tests package_xtests <<<"${package_info}"

  if [[ "${package_name}" == "main" ]]; then
    continue
  fi
  if (( package_tests + package_xtests == 0 )); then
    continue
  fi

  eligible_packages+=("${package_path}")
done

if [[ "${#eligible_packages[@]}" -eq 0 ]]; then
  echo "No eligible non-main packages found for mutation testing."
  exit 0
fi

for package_path in "${eligible_packages[@]}"; do
  package_dir="$(cd "${repo_root}" && go list -f '{{.Dir}}' "${package_path}")"
  backup_dir="$(mktemp -d)"
  echo "Running mutation tests for ${package_path}"
  cp -R "${package_dir}/." "${backup_dir}/"

  if ! (
    cd "${repo_root}"
    "${tool}" --config .go-mutesting.yml "${blacklist_args[@]}" "${package_path}"
  ); then
    restore_package
    exit 1
  fi

  restore_package
done

trap - EXIT INT TERM
