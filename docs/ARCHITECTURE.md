# Architecture

`open-workstation-mcp` demonstrates a public-safe workstation automation shape
without reading live desktop state.

## Request Path

```text
CLI command
  -> synthetic parameters
  -> typed workstation tool
  -> dry-run policy middleware
  -> response budget
  -> reviewable JSON output
```

## Components

| Area | Purpose |
| --- | --- |
| `internal/workstationmcp` | Tool contracts, synthetic handlers, middleware, and response shaping. |
| `cmd/open-workstation-mcp` | CLI entrypoint for manifest and tool calls. |
| `scripts/check-public-boundary.sh` | Sample-output boundary and local hygiene checks. |

## Safety Model

- Tool calls use explicit parameters such as `profile=desktop` or `app=terminal`.
- Output is synthetic and review-only.
- The sample does not inspect compositor trees, shell history, browser profiles,
  clipboard contents, account config, or local file paths.
- All write-like behavior is represented as a plan, never as a live mutation.
