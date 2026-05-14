BINARY   := tg-simulate
MODULE   := github.com/tagn/tg-simulate
TF_BINARY ?= tofu

# Install destination (defaults to GOPATH/bin, which is usually on PATH)
INSTALL_DIR ?= $(shell go env GOPATH)/bin

.PHONY: build install test test-integration test-all lint fmt vet run-simple-chain run-diamond run-greenfield clean help

## build: compile the binary into the project root
build:
	go build -o $(BINARY) ./cmd/tg-simulate/

## install: install the binary to GOPATH/bin
install:
	go install ./cmd/tg-simulate/

## test: run unit tests
test:
	go test ./...

## test-integration: run integration tests (requires terragrunt + terraform/tofu on PATH)
test-integration:
	TF_BINARY=$(TF_BINARY) go test -tags integration -timeout 10m -v ./internal/runner/...

## test-all: run unit tests then integration tests
test-all: test test-integration

## fmt: format all Go source files
fmt:
	go fmt ./...

## vet: run go vet
vet:
	go fmt ./...
	go vet ./...

## lint: run golangci-lint (install from https://golangci-lint.run/usage/install/)
lint:
	golangci-lint run ./...

## run-simple-chain: simulate the simple-chain fixture (a → b → c, no prior state)
run-simple-chain: build
	TF_BINARY=$(TF_BINARY) ./$(BINARY) run --working-dir ./testdata/simple-chain

## run-diamond: simulate the diamond fixture (a → b, a → c, b+c → d)
run-diamond: build
	TF_BINARY=$(TF_BINARY) ./$(BINARY) run --working-dir ./testdata/diamond

## run-greenfield: simulate the single-unit greenfield fixture
run-greenfield: build
	TF_BINARY=$(TF_BINARY) ./$(BINARY) run --working-dir ./testdata/greenfield

## run-with-changes: apply the with-changes fixture then re-simulate after changing a trigger
##   Copies the fixture to a temp dir so the source tree stays clean.
##   Shows force-new propagation: changing a's trigger causes b to be replaced.
run-with-changes: build
	@TMPDIR=$$(mktemp -d) && \
	cp -r ./testdata/with-changes/. "$$TMPDIR/" && \
	echo "=== Applied state (v1) ===" && \
	TG_TF_PATH=$(TF_BINARY) terragrunt run --all apply --non-interactive --working-dir "$$TMPDIR" 2>/dev/null && \
	echo "" && \
	echo "=== Simulation: baseline (no code change) ===" && \
	TF_BINARY=$(TF_BINARY) ./$(BINARY) run --working-dir "$$TMPDIR" && \
	echo "" && \
	echo "=== Changing a/main.tf: trigger v1 → v2 ===" && \
	sed -i '' 's/"v1"/"v2"/' "$$TMPDIR/a/main.tf" && \
	echo "" && \
	echo "=== Simulation: after upstream trigger change ===" && \
	TF_BINARY=$(TF_BINARY) ./$(BINARY) run --working-dir "$$TMPDIR" ; \
	rm -rf "$$TMPDIR"

## clean: remove the compiled binary, scratch caches, and test state files
clean:
	rm -f $(BINARY)
	rm -rf .tg-simulate-cache/
	find ./testdata -name "terraform.tfstate" -o -name "terraform.tfstate.backup" | xargs rm -f 2>/dev/null || true
	find ./testdata -name ".terragrunt-cache" -type d | xargs rm -rf 2>/dev/null || true

## help: list available targets
help:
	@grep -E '^##' Makefile | sed 's/^## //' | column -t -s ':'
