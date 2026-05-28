# Examples

All examples are synthetic and dry-run oriented.

## Commands

```bash
go run ./cmd/open-workstation-mcp manifest
go run ./cmd/open-workstation-mcp call workstation_status --param profile=desktop
go run ./cmd/open-workstation-mcp call window_focus_plan --param app=terminal --param intent=inspect
go run ./cmd/open-workstation-mcp call config_drift_report --param component=keybindings
go run ./cmd/open-workstation-mcp call safe_input_plan --param target=search --param text=hello
```

All output is synthetic. The sample never reads or mutates the live workstation.

## Workstation Status

Expected shape:

```json
{
  "ok": true,
  "tool": "workstation_status",
  "dry_run": true,
  "payload": {
    "mutation_mode": "dry-run",
    "public_boundary": "no live host state read",
    "session": "synthetic-wayland"
  }
}
```

## Focus Plan

Expected shape:

```json
{
  "ok": true,
  "tool": "window_focus_plan",
  "dry_run": true,
  "payload": {
    "app": "terminal",
    "intent": "inspect",
    "plan": ["discover visible windows from a synthetic tree"]
  }
}
```

## Safe Input Plan

The input plan previews text and stops before any live keystroke or paste
action.
