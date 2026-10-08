MODULE := $(shell go list -m)
GOOS ?= $(shell go env GOOS)
GOARCH ?= $(shell go env GOARCH)
ARGS ?=

.PHONY: help deps style format build test race lint run

help:
	@printf "%s\n" \
		"Usage: make [target] [VARIABLE=value ...]" \
		"" \
		Targets: \
		"  make help       Show available targets (default)" \
		"  make deps       Install pinned Go formatting and linting tools" \
		"  make style      Check Go formatting without modifying files" \
		"  make format     Format Go imports and Go code" \
		"  make build      Build the CLI with CGO disabled in dist/ (default: host platform)" \
		"  make test       Run offline Go unit tests without test caching" \
		"  make race       Run Go tests with race detection without test caching" \
		"  make lint       Run Go linters" \
		"  make run        Run the CLI with go run; pass CLI arguments via ARGS" \
		"" \
		Examples: \
		"  make format && make build && make test && make race && make lint" \
		"  make run ARGS='--config proxy.yaml'" \
		"  make build GOOS=linux GOARCH=arm64" \
		"  make run ARGS='config-reference --in config.yaml --out config.reference.yaml'"

deps:
	go install golang.org/x/tools/cmd/goimports@v0.50.0
	go install mvdan.cc/gofumpt@v0.12.0
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0

style:
	@files=$$(goimports -local $(MODULE) -l .) || exit $$?; \
	[ -n "$$files" ] || { files=$$(gofumpt -l .) || exit $$?; }; \
	[ -z "$$files" ] || { \
		printf "%s\n" "$$files"; \
		echo "Run 'make format' to fix style issues."; \
		exit 1; \
	}

format:
	goimports -local $(MODULE) -l -w .
	gofumpt -l -w .

build:
	mkdir -p ./dist
	CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) go build \
		-trimpath -ldflags "-s -w" \
		-o ./dist/efanadp-$(GOOS)-$(GOARCH) ./cmd/efanadp
	@echo "built dist/efanadp-$(GOOS)-$(GOARCH)"

test:
	go test -count 1 ./...

race:
	go test -race -count 1 ./...

lint:
	golangci-lint run

run:
	go run ./cmd/efanadp $(ARGS)
