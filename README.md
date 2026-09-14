# open-workstation-mcp

[![ci](https://github.com/hairglasses/open-workstation-mcp/actions/workflows/ci.yml/badge.svg)](https://github.com/hairglasses/open-workstation-mcp/actions/workflows/ci.yml)
[![Go](https://img.shields.io/badge/Go-1.24+-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

Public-safe Go sample for MCP-style workstation automation patterns: typed tool
contracts, dry-run desktop action plans, synthetic readiness snapshots,
configuration drift summaries, and response-budgeted CLI output.

This repository is intentionally small. It demonstrates how to package a
Linux/Wayland workstation automation surface without publishing private dotfiles,
live desktop state, browser sessions, account data, cookies, or host-specific
paths.

## Start Here

For a quick review path:

1. Run the five-minute commands in [docs/GETTING_STARTED.md](docs/GETTING_STARTED.md).
2. Compare output shapes in [docs/EXAMPLES.md](docs/EXAMPLES.md).
3. Review the dry-run data flow in [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md).
4. Use [docs/PORTFOLIO_PROOF.md](docs/PORTFOLIO_PROOF.md) for the architecture
   diagram, walkthrough plan, tradeoffs, and interview prompts.
5. Check [PUBLIC_BOUNDARY.md](PUBLIC_BOUNDARY.md) before adding tools or examples.

## Host MCP Consolidation

`open-workstation-mcp` is the consolidated host MCP server for Hairglasses Studio workstation infrastructure, providing 44 tools across three core domains with a single Prometheus metrics port (9095):

- **systemd** (13 tools): `systemd_status`, `systemd_start`, `systemd_stop`, `systemd_restart`, `systemd_enable`, `systemd_disable`, `systemd_logs`, `systemd_list_units`, `systemd_list_timers`, `systemd_failed`, `systemd_investigate_unit`, `systemd_create_timer`, `systemd_analyze`
- **tmux** (14 tools): `tmux_list_sessions`, `tmux_list_windows`, `tmux_list_panes`, `tmux_capture_pane`, `tmux_send_keys`, `tmux_new_session`, `tmux_new_window`, `tmux_kill_session`, `tmux_wait_for_text`, `tmux_search_panes`, `tmux_workspace`, `tmux_resize_pane`, `tmux_snapshot`, `tmux_template`
- **process** (17 tools): `ps_list`, `ps_tree`, `kill_process`, `port_list`, `gpu_status`, `system_info`, `investigate_port`, `investigate_service`, `fd_list`, `mem_map`, `net_connections`, `investigate_memory`, `investigate_cpu`, `ps_search`, `proc_inspect`, `liveness_gate`, `dbus_probe`

Backward-compatibility symlinks in `~/.local/bin/` (`systemd-mcp`, `tmux-mcp`, `process-mcp`) all point to this unified binary.

## What Works Now

- Register typed workstation tools and emit a deterministic manifest.
- Expose 44 host management tools across systemd, tmux, and process domains.
- Expose Prometheus metrics on port 9095.
- Generate dry-run focus/input/config plans from synthetic parameters.
- Keep all sample outputs local, deterministic, and fixture-safe.
- Enforce a dry-run policy middleware and response-size budget.
- Run tests, vet, smoke commands, and public-boundary checks with `make ci`.

## Usage

```bash
make ci
go run ./cmd/open-workstation-mcp manifest
go run ./cmd/open-workstation-mcp call workstation_status --param profile=desktop
go run ./cmd/open-workstation-mcp call window_focus_plan --param app=terminal --param intent=inspect
go run ./cmd/open-workstation-mcp call config_drift_report --param component=keybindings
go run ./cmd/open-workstation-mcp call safe_input_plan --param target=search --param text=hello
```

See [docs/EXAMPLES.md](docs/EXAMPLES.md) for expected output and validation
commands.

## Public Boundary

This is not a mirror of any private workstation or dotfiles repository. It does
not read real compositor state, shell history, browser profiles, OAuth files,
clipboard contents, account configs, or local filesystem paths. All examples are
synthetic and dry-run oriented.

See [PUBLIC_BOUNDARY.md](PUBLIC_BOUNDARY.md) before adding tools or examples.

## Verification

```bash
make ci
gitleaks detect --source . --no-git --redact
```

`make ci` runs tests, vet, a temporary build, deterministic smoke commands,
public-boundary checks, and optional local `gitleaks` / `actionlint` checks when
those tools are installed.
