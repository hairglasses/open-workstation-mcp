# open-workstation-mcp

[![ci](https://github.com/hairglasses/open-workstation-mcp/actions/workflows/ci.yml/badge.svg)](https://github.com/hairglasses/open-workstation-mcp/actions/workflows/ci.yml)

Public-safe Go sample for MCP-style workstation automation patterns: typed tool
contracts, dry-run desktop action plans, synthetic readiness snapshots,
configuration drift summaries, and response-budgeted CLI output.

This repository is intentionally small. It demonstrates how to package a
Linux/Wayland workstation automation surface without publishing private dotfiles,
live desktop state, browser sessions, account data, cookies, or host-specific
paths.

## What Works Now

- Register typed workstation tools and emit a deterministic manifest.
- Generate dry-run focus/input/config plans from synthetic parameters.
- Keep all sample outputs local, deterministic, and fixture-safe.
- Enforce a dry-run policy middleware and response-size budget.
- Run tests, vet, smoke commands, and public-boundary checks with `make ci`.

## Usage

```bash
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
