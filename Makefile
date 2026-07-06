CACHE_DIR := $(CURDIR)/.gocache
TOOLS_DIR := $(CACHE_DIR)/bin
export GOCACHE := $(CACHE_DIR)/build
export GOMODCACHE := $(CACHE_DIR)/mod
GO ?= go
MODULE_PATH = $(shell $(GO) list -m)
MAIN_PACKAGE := ./cmd/efanadp
FORMAT_DIRS := ./cmd ./internal
GOIMPORTS := $(TOOLS_DIR)/goimports
GOFUMPT := $(TOOLS_DIR)/gofumpt
GOLANGCI_LINT := $(TOOLS_DIR)/golangci-lint

.PHONY: help deps style format build test race lint run curl config-reference

help:
	@printf '%s\n' \
		"efanadp make targets:" \
		"" \
		"Setup:" \
		"  make deps            Install pinned goimports, gofumpt, and golangci-lint into .gocache/bin" \
		"" \
		"Build and test:" \
		"  make style           Check goimports/gofumpt style" \
		"  make format          Apply goimports/gofumpt formatting" \
		"  make build           Build ./bin/efanadp" \
		"  make test            Run Go tests" \
		"  make race            Run Go tests with race detection" \
		"  make lint            Run golangci-lint" \
		"" \
		"Common:" \
		"  make run             Run the interactive TUI (pass ARGS='--config path' when needed)" \
		"  make curl            Test egress IP through local SOCKS5" \
		"  make config-reference  Regenerate config.reference.yaml (pass ARGS='--in path --out path' when needed)"

deps: export GOBIN := $(TOOLS_DIR)
deps:
	mkdir -p $(TOOLS_DIR)
	$(GO) install golang.org/x/tools/cmd/goimports@v0.47.0
	$(GO) install mvdan.cc/gofumpt@v0.10.0
	$(GO) install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2

$(GOIMPORTS) $(GOFUMPT) $(GOLANGCI_LINT):
	@echo "$(@F) is missing. Run 'make deps' first."
	@exit 1

style: $(GOIMPORTS) $(GOFUMPT)
	@files="$$( $(GOIMPORTS) -local $(MODULE_PATH) -l $(FORMAT_DIRS) )" || exit $$?; \
	[ -z "$$files" ] || { \
		printf "%s\n" "$$files"; \
		echo "Run 'make format' to fix style issues."; \
		exit 1; \
	}
	@files="$$( $(GOFUMPT) -l $(FORMAT_DIRS) )" || exit $$?; \
	[ -z "$$files" ] || { \
		printf "%s\n" "$$files"; \
		echo "Run 'make format' to fix style issues."; \
		exit 1; \
	}

format: $(GOIMPORTS) $(GOFUMPT)
	$(GOIMPORTS) -local $(MODULE_PATH) -l -w $(FORMAT_DIRS)
	$(GOFUMPT) -l -w $(FORMAT_DIRS)

build:
	mkdir -p ./bin
	$(GO) build -o ./bin/efanadp $(MAIN_PACKAGE)

test:
	$(GO) test ./...

race:
	$(GO) test -race ./...

lint: export GOLANGCI_LINT_CACHE := $(CACHE_DIR)/lint
lint: $(GOLANGCI_LINT)
	$(GOLANGCI_LINT) run

run:
	$(GO) run $(MAIN_PACKAGE) $(ARGS)

curl:
	curl --fail --silent --show-error --proxy "socks5h://127.0.0.1:10865" "https://ifconfig.me/ip"

config-reference:
	$(GO) run $(MAIN_PACKAGE) config-reference $(ARGS)
