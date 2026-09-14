GO ?= env GOWORK=off go

.PHONY: test vet build smoke boundary ci

test:
	$(GO) test -race ./...

vet:
	$(GO) vet ./...

build:
	$(GO) build -o open-workstation-mcp ./cmd/open-workstation-mcp

smoke: build
	./open-workstation-mcp -tools
	$(GO) run ./cmd/open-workstation-mcp manifest
	$(GO) run ./cmd/open-workstation-mcp call workstation_status --param profile=desktop

boundary:
	GO_CMD="$(GO)" scripts/check-public-boundary.sh

ci: test vet build smoke boundary
