# Examples

```bash
go run ./cmd/open-workstation-mcp manifest
go run ./cmd/open-workstation-mcp call workstation_status --param profile=desktop
go run ./cmd/open-workstation-mcp call window_focus_plan --param app=terminal --param intent=inspect
go run ./cmd/open-workstation-mcp call config_drift_report --param component=keybindings
go run ./cmd/open-workstation-mcp call safe_input_plan --param target=search --param text=hello
```

All output is synthetic. The sample never reads or mutates the live workstation.
