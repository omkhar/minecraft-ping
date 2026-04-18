#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 2 ]]; then
  echo "Usage: $0 <sha> <timeout_minutes> [contexts_csv]" >&2
  exit 2
fi

sha="$1"
timeout_minutes="$2"
contexts_csv="${3:-}"

if ! command -v gh >/dev/null 2>&1; then
  echo "gh CLI is required" >&2
  exit 2
fi
if ! command -v jq >/dev/null 2>&1; then
  echo "jq is required" >&2
  exit 2
fi

repo="${GITHUB_REPOSITORY:-}"
if [[ -z "$repo" ]]; then
  remote_url="$(git config --get remote.origin.url || true)"
  if [[ "$remote_url" =~ github.com[:/]([^/]+)/([^/.]+)(\.git)?$ ]]; then
    repo="${BASH_REMATCH[1]}/${BASH_REMATCH[2]}"
  else
    echo "Unable to determine GitHub repository" >&2
    exit 2
  fi
fi

declare -a wanted_contexts=()
wanted_json='[]'
if [[ -n "$contexts_csv" ]]; then
  IFS=',' read -r -a wanted_contexts <<<"$contexts_csv"
  wanted_json="$(printf '%s\n' "${wanted_contexts[@]}" | jq -R . | jq -s .)"
fi

deadline=$((SECONDS + timeout_minutes * 60))

echo "Waiting for check runs on $repo@$sha (timeout=${timeout_minutes}m)"
if [[ ${#wanted_contexts[@]} -gt 0 ]]; then
  echo "Required contexts: ${wanted_contexts[*]}"
fi

while true; do
  all_checks='[]'
  page=1
  while true; do
    page_json="$(gh api -H 'Accept: application/vnd.github+json' "repos/$repo/commits/$sha/check-runs?per_page=100&page=$page")"
    page_checks="$(jq -c '.check_runs' <<<"$page_json")"
    all_checks="$(jq -c --argjson current "$all_checks" --argjson next "$page_checks" '$current + $next' <<<"null")"

    if [[ "$(jq 'length' <<<"$page_checks")" -lt 100 ]]; then
      break
    fi
    page=$((page + 1))
  done

  json="$(jq -c --argjson check_runs "$all_checks" '{check_runs: $check_runs}' <<<"null")"
  latest_checks="$(jq -c '[.check_runs | sort_by(.name, .id) | group_by(.name)[] | last]' <<<"$json")"

  if [[ ${#wanted_contexts[@]} -gt 0 ]]; then
    filter='[.[] | select(.name as $n | ($wanted | index($n) != null))]'
    checks="$(jq -c --argjson wanted "$wanted_json" "$filter" <<<"$latest_checks")"
    missing_contexts="$(jq -c --argjson wanted "$wanted_json" '$wanted - (map(.name) | unique)' <<<"$latest_checks")"
  else
    checks="$latest_checks"
    missing_contexts='[]'
  fi

  total="$(jq 'length' <<<"$checks")"
  completed="$(jq '[.[] | select(.status == "completed")] | length' <<<"$checks")"
  pending="$(jq '[.[] | select(.status != "completed")] | length' <<<"$checks")"
  failed="$(jq '[.[] | select(.status == "completed" and .conclusion != "success")] | length' <<<"$checks")"
  missing_count="$(jq 'length' <<<"$missing_contexts")"

  if [[ "$failed" -gt 0 ]]; then
    echo "Detected non-success check runs:" >&2
    jq -r '.[] | select(.status == "completed" and .conclusion != "success") | "- \(.name): \(.conclusion)"' <<<"$checks" >&2
    exit 1
  fi

  if [[ "$total" -gt 0 && "$missing_count" -eq 0 && "$pending" -eq 0 && "$completed" -eq "$total" ]]; then
    echo "All required checks completed successfully."
    exit 0
  fi

  if (( SECONDS >= deadline )); then
    if [[ "$missing_count" -gt 0 ]]; then
      echo "Missing required check contexts:" >&2
      jq -r '.[] | "- \(.)"' <<<"$missing_contexts" >&2
    fi
    echo "Timed out waiting for checks (completed=$completed total=$total pending=$pending missing=$missing_count)" >&2
    exit 1
  fi

  echo "Still waiting... completed=$completed total=$total pending=$pending missing=$missing_count"
  sleep 20
done
