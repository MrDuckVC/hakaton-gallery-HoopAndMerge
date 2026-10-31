.DEFAULT_GOAL := build

GO ?= go
BIN_DIR := ./bin
LINT_BIN := $(BIN_DIR)/golangci-lint
LINT_VER := v2.13.2

.PHONY: build run test coverage vet fmt lint lint-fix contracts docker-build migrate-up migrate-status

build:
	$(GO) build -o $(BIN_DIR)/gallery ./cmd/server
	$(GO) build -o $(BIN_DIR)/migrate ./cmd/migrate

run:
	$(GO) run ./cmd/server

test:
	$(GO) test ./...

coverage:
	sh tools/test-coverage.sh
	$(GO) test -race -covermode=atomic -coverpkg=./... -coverprofile=coverage.out ./...
	sh tools/check-coverage.sh coverage.out

docker-build:
	docker build --file build/main.Dockerfile --tag gallery-backend:local .

vet:
	$(GO) vet ./...

fmt:
	$(GO) fmt ./...

contracts:
	node tools/build-contracts.mjs
	node tools/validate-contracts.mjs

$(LINT_BIN):
	@set -eu; installer=$$(mktemp); \
	trap 'rm -f "$$installer"' EXIT HUP INT TERM; \
	curl -sSfL https://golangci-lint.run/install.sh -o "$$installer"; \
	sh "$$installer" -b $(BIN_DIR) $(LINT_VER)

lint: $(LINT_BIN)
	$(LINT_BIN) run ./...

lint-fix: $(LINT_BIN)
	$(LINT_BIN) run --fix ./...

migrate-up:
	$(GO) run ./cmd/migrate up

migrate-status:
	$(GO) run ./cmd/migrate status
