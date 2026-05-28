# Getting Started

## Requirements

- Go 1.24 or newer
- `make`
- Optional: `gitleaks` and `actionlint` for local parity with release checks

## Five-Minute Review

```bash
git clone https://github.com/hairglasses/open-workstation-mcp.git
cd open-workstation-mcp
make ci
go run ./cmd/open-workstation-mcp manifest
go run ./cmd/open-workstation-mcp call workstation_status --param profile=desktop
go run ./cmd/open-workstation-mcp call window_focus_plan --param app=terminal --param intent=inspect
go run ./cmd/open-workstation-mcp call config_drift_report --param component=keybindings
go run ./cmd/open-workstation-mcp call safe_input_plan --param target=search --param text=hello
```

All commands are local and deterministic. The sample emits review packets and
does not read live desktop, browser, shell, account, or config state.

## Review Path

1. Start with `README.md` for positioning.
2. Read `docs/ARCHITECTURE.md` for the dry-run request path.
3. Compare output shapes in `docs/EXAMPLES.md`.
4. Read `PUBLIC_BOUNDARY.md` before adding tools or examples.
5. Run `make ci` before opening a pull request or publishing a release.
