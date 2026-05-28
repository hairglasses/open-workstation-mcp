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
4. Check [PUBLIC_BOUNDARY.md](PUBLIC_BOUNDARY.md) before adding tools or examples.

## What Works Now

- Register typed workstation tools and emit a deterministic manifest.
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
