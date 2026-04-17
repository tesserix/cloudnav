BINARY      := cloudnav
PKG         := github.com/tesserix/cloudnav
VERSION     ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT      ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE        ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS     := -s -w \
	-X $(PKG)/internal/version.Version=$(VERSION) \
	-X $(PKG)/internal/version.Commit=$(COMMIT) \
	-X $(PKG)/internal/version.Date=$(DATE)

GOFLAGS     := -trimpath
BIN_DIR     := bin

.PHONY: all build dev run test lint fmt tidy clean install snapshot release doctor help

all: build

build: ## Build the binary into ./bin
	@mkdir -p $(BIN_DIR)
	go build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $(BIN_DIR)/$(BINARY) ./cmd/$(BINARY)

dev: ## Run the TUI against your current CLI sessions
	go run ./cmd/$(BINARY)

run: build
	./$(BIN_DIR)/$(BINARY)

doctor: build
	./$(BIN_DIR)/$(BINARY) doctor

test: ## Run unit tests
	go test -race -count=1 ./...

lint: ## Run golangci-lint
	golangci-lint run ./...

fmt: ## Format with gofumpt (fallback gofmt)
	@command -v gofumpt >/dev/null 2>&1 && gofumpt -w . || gofmt -w .

tidy:
	go mod tidy

install: build ## Install binary into $GOPATH/bin
	go install $(GOFLAGS) -ldflags '$(LDFLAGS)' ./cmd/$(BINARY)

snapshot: ## Build a local multi-arch snapshot via GoReleaser (no publish)
	goreleaser release --clean --snapshot --skip=publish

release: ## Tag and publish via GoReleaser (requires GITHUB_TOKEN)
	goreleaser release --clean

clean:
	rm -rf $(BIN_DIR) dist

help:
	@awk 'BEGIN{FS=":.*##"; printf "Targets:\n"} /^[a-zA-Z_-]+:.*##/ {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)
