#!/usr/bin/env bash

set -euo pipefail

: "${CODEX_CLI_VERSION:?CODEX_CLI_VERSION is required}"
: "${CLAUDE_CODE_VERSION:?CLAUDE_CODE_VERSION is required}"
: "${GEMINI_CLI_VERSION:?GEMINI_CLI_VERSION is required}"
: "${OPENAI_API_KEY:?OPENAI_API_KEY is required}"
: "${ANTHROPIC_API_KEY:?ANTHROPIC_API_KEY is required}"
: "${GEMINI_API_KEY:?GEMINI_API_KEY is required}"

npm install -g --ignore-scripts \
  "@openai/codex@${CODEX_CLI_VERSION}" \
  "@google/gemini-cli@${GEMINI_CLI_VERSION}"

# Claude Code's postinstall hard-links the per-platform native binary over an
# 11-byte stub at bin/claude.exe; skipping it leaves an unusable claude on $PATH.
npm install -g "@anthropic-ai/claude-code@${CLAUDE_CODE_VERSION}"

printf '%s' "${OPENAI_API_KEY}" | codex login --with-api-key >/dev/null
