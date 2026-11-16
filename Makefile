CGO_ENABLED ?= 0
GO ?= env GOWORK=off go
GO_BUILD_FLAGS ?= -trimpath -buildvcs=true

.PHONY: test vet build smoke boundary ci

test:
	env CGO_ENABLED=1 GOWORK=off go test -race -count=1 ./... < /dev/null

vet:
	$(GO) vet ./... < /dev/null

build:
	env CGO_ENABLED=$(CGO_ENABLED) GOWORK=off go build $(GO_BUILD_FLAGS) -o open-workstation-mcp ./cmd/open-workstation-mcp < /dev/null

smoke: build
	./open-workstation-mcp -tools
	$(GO) run ./cmd/open-workstation-mcp manifest
	$(GO) run ./cmd/open-workstation-mcp call workstation_status --param profile=desktop

boundary:
	GO_CMD="$(GO)" scripts/check-public-boundary.sh

ci: test vet build smoke boundary
