MODULE_PATH ?= $(shell go list -m)
MAIN_PACKAGE := ./cmd/efanadp
FORMAT_DIRS := ./cmd ./internal

.PHONY: help deps style format build test race lint run config-reference

help:
	@printf '%s\n' \
		"efanadp make targets:" \
		"" \
		"Setup:" \
		"  make deps            Install pinned goimports, gofumpt, and golangci-lint with host Go" \
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
		"  make config-reference  Regenerate config.reference.yaml (pass ARGS='--in path --out path' when needed)"

deps:
	go install golang.org/x/tools/cmd/goimports@v0.47.0
	go install mvdan.cc/gofumpt@v0.10.0
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2

style:
	@files="$$( goimports -local $(MODULE_PATH) -l $(FORMAT_DIRS) )" || exit $$?; \
	[ -z "$$files" ] || { \
		printf "%s\n" "$$files"; \
		echo "Run 'make format' to fix style issues."; \
		exit 1; \
	}
	@files="$$( gofumpt -l $(FORMAT_DIRS) )" || exit $$?; \
	[ -z "$$files" ] || { \
		printf "%s\n" "$$files"; \
		echo "Run 'make format' to fix style issues."; \
		exit 1; \
	}

format:
	goimports -local $(MODULE_PATH) -l -w $(FORMAT_DIRS)
	gofumpt -l -w $(FORMAT_DIRS)

build:
	mkdir -p ./bin
	go build -o ./bin/efanadp $(MAIN_PACKAGE)

test:
	go test ./...

race:
	go test -race ./...

lint:
	golangci-lint run

run:
	go run $(MAIN_PACKAGE) $(ARGS)

config-reference:
	go run $(MAIN_PACKAGE) config-reference $(ARGS)
