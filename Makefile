GO ?= env GOWORK=off go

.PHONY: test vet build smoke boundary ci

test:
	$(GO) test ./...

vet:
	$(GO) vet ./...

build:
	tmp_dir="$$(mktemp -d)"; \
	trap 'rm -rf "$$tmp_dir"' EXIT; \
	$(GO) build -o "$$tmp_dir/open-workstation-mcp" ./cmd/open-workstation-mcp

smoke:
	$(GO) run ./cmd/open-workstation-mcp manifest
	$(GO) run ./cmd/open-workstation-mcp call workstation_status --param profile=desktop
	$(GO) run ./cmd/open-workstation-mcp call window_focus_plan --param app=terminal --param intent=inspect
	$(GO) run ./cmd/open-workstation-mcp call config_drift_report --param component=keybindings
	$(GO) run ./cmd/open-workstation-mcp call safe_input_plan --param target=search --param text=hello

boundary:
	GO_CMD="$(GO)" scripts/check-public-boundary.sh

ci: test vet build smoke boundary
