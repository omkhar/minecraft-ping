#!/usr/bin/env bash

set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "${script_dir}/.." && pwd)"

rm -rf "${repo_root}/artifacts" "${repo_root}/dist"
find "${repo_root}" -maxdepth 1 -type f -name '*.local.env' -delete
rm -f "${repo_root}/coverage.out" "${repo_root}/mcping.log" "${repo_root}/report.json"
find "${repo_root}" -name '.DS_Store' -type f -delete
find "${repo_root}" -path "${repo_root}/.git" -prune -o -type f \( -name '*.tmp' -o -name '*.orig' -o -name '*.rej' \) -delete
