#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

go_cmd="${GO_CMD:-go}"
forbidden_output_pattern='gmail|linkedin|oauth|cookie|tenant|mitch|mitchell|/home/hg|hairglasses-studio|jobb|secret|password|token|api[_-]?key|@[A-Za-z0-9._%+-]+\.[A-Za-z]{2,}'
tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT

echo "== sample output boundary =="
$go_cmd run ./cmd/open-workstation-mcp manifest > "$tmp_dir/manifest.json"
$go_cmd run ./cmd/open-workstation-mcp call workstation_status --param profile=desktop > "$tmp_dir/status.json"
$go_cmd run ./cmd/open-workstation-mcp call window_focus_plan --param app=terminal --param intent=inspect > "$tmp_dir/focus.json"
$go_cmd run ./cmd/open-workstation-mcp call config_drift_report --param component=keybindings > "$tmp_dir/drift.json"
$go_cmd run ./cmd/open-workstation-mcp call safe_input_plan --param target=search --param text=hello > "$tmp_dir/input.json"

if rg --ignore-case --line-number "$forbidden_output_pattern" "$tmp_dir"; then
  echo "sample output contains private-boundary markers" >&2
  exit 1
fi

echo "== gitleaks =="
if command -v gitleaks >/dev/null 2>&1; then
  gitleaks detect --source . --no-git --redact
else
  echo "gitleaks not installed; skipping local secret scan"
fi

echo "== actionlint =="
if command -v actionlint >/dev/null 2>&1; then
  actionlint .github/workflows/*.yml
else
  echo "actionlint not installed; skipping workflow lint"
fi

echo "public boundary checks passed"
