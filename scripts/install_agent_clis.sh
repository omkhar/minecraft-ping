#!/usr/bin/env bash

set -euo pipefail

: "${OPENAI_API_KEY:?OPENAI_API_KEY is required}"
: "${ANTHROPIC_API_KEY:?ANTHROPIC_API_KEY is required}"
: "${GEMINI_API_KEY:?GEMINI_API_KEY is required}"

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "${script_dir}/.." && pwd)"
agent_cli_root="${repo_root}/.github/agent-clis"
agent_cli_bin="${agent_cli_root}/node_modules/.bin"

run_npm_without_provider_secrets() {
  env -u OPENAI_API_KEY -u ANTHROPIC_API_KEY -u GEMINI_API_KEY npm "$@"
}

(
  cd "${agent_cli_root}"
  run_npm_without_provider_secrets ci --include=dev --ignore-scripts
  # Claude Code's postinstall hard-links the per-platform native binary over an
  # 11-byte stub at bin/claude.exe; run only that package's lifecycle scripts.
  run_npm_without_provider_secrets rebuild @anthropic-ai/claude-code --foreground-scripts --ignore-scripts=false
)

export PATH="${agent_cli_bin}:${PATH}"
if [[ -n "${GITHUB_PATH:-}" ]]; then
  printf '%s\n' "${agent_cli_bin}" >>"${GITHUB_PATH}"
fi

printf '%s' "${OPENAI_API_KEY}" | codex login --with-api-key >/dev/null
