# Testmaker — build / test / lint entry points.
# Multi-module go.work workspace: most targets loop over every go.mod.

GO            ?= go
GOLANGCI      ?= golangci-lint
ARCHLINT      ?= go-arch-lint
MODULES       := $(shell find . -name go.mod -not -path '*/.*' -exec dirname {} \;)
REPORTS       := reports

# `make serve` runtime config. Config + mutable state (sqlite db, figural-media
# blobs) live under a per-user home, never the working directory. Override either.
TESTMAKER_HOME ?= $(HOME)/.testmaker
SERVE_ADDR     ?= :8080

# Where `go install` drops the binary (GOBIN, else GOPATH/bin).
GOBIN_DIR      := $(shell $(GO) env GOBIN)
ifeq ($(strip $(GOBIN_DIR)),)
GOBIN_DIR      := $(shell $(GO) env GOPATH)/bin
endif

# Pinned tool versions (installed by `make install`).
GOLANGCI_VERSION := v2.12.2
ARCHLINT_VERSION := v1.15.0

.DEFAULT_GOAL := all

## all: build then unit-test (default)
.PHONY: all
all: build test

## build: compile every module in the workspace
.PHONY: build
build:
	@for m in $(MODULES); do echo "== build $$m =="; (cd $$m && $(GO) build ./...) || exit 1; done

## test: unit tests only (-short -race) across every module
.PHONY: test
test:
	@mkdir -p $(REPORTS)
	@for m in $(MODULES); do echo "== test $$m =="; (cd $$m && $(GO) test -short -race -timeout 120s ./...) || exit 1; done

## serve: go install the CLI, seed the home dir, and run the global binary's HTTP API on SERVE_ADDR (default :8080)
.PHONY: serve
serve:
	$(GO) install ./cmd/testmaker
	@mkdir -p "$(TESTMAKER_HOME)/data/catalog" "$(TESTMAKER_HOME)/data/prompts" "$(TESTMAKER_HOME)/data/blobs"
	@cp -n data/catalog/sources.json "$(TESTMAKER_HOME)/data/catalog/" 2>/dev/null || true
	@for f in data/prompts/*.yaml; do cp -n "$$f" "$(TESTMAKER_HOME)/data/prompts/" 2>/dev/null || true; done
	@echo "serving on $(SERVE_ADDR); TESTMAKER_HOME=$(TESTMAKER_HOME) (config + data + seeds); binary $(GOBIN_DIR)/testmaker"
	TESTMAKER_HOME="$(TESTMAKER_HOME)" "$(GOBIN_DIR)/testmaker" -serve "$(SERVE_ADDR)"

# Web app (operator console + test player). Bun is OPTIONAL: every Go target
# works without it; these targets are the only ones that need it.
WEB_DIR := web

## webui: build the web app into cmd/testmaker/webui/dist (requires bun)
.PHONY: webui
webui:
	cd $(WEB_DIR) && bun install --frozen-lockfile && bun run build
	@touch cmd/testmaker/webui/dist/.keep

## webui-dev: run the Vite dev server (HMR), proxying /api to localhost:8080
.PHONY: webui-dev
webui-dev:
	cd $(WEB_DIR) && bun install && bun run dev

## webui-test: run the web unit/component tests (Vitest)
.PHONY: webui-test
webui-test:
	cd $(WEB_DIR) && bun install --frozen-lockfile && bun run test:run

## webui-lint: typecheck the web app (tsc --noEmit)
.PHONY: webui-lint
webui-lint:
	cd $(WEB_DIR) && bun install --frozen-lockfile && bun run typecheck

## serve-all: build the web app, then serve the single binary (SPA + API)
.PHONY: serve-all
serve-all: webui serve

## fmt: format all Go files in place (the only auto-fix)
.PHONY: fmt
fmt:
	@gofmt -w $(shell git ls-files '*.go')

## lint-fix: alias for fmt
.PHONY: lint-fix
lint-fix: fmt

## vet: go vet across every module
.PHONY: vet
vet:
	@for m in $(MODULES); do echo "== vet $$m =="; (cd $$m && $(GO) vet ./...) || exit 1; done

## arch-lint: enforce the layer graph in .go-arch-lint.yml
.PHONY: arch-lint
arch-lint:
	@$(ARCHLINT) check || exit 1

## arch-graph: render the dependency graph to reports/arch.svg
.PHONY: arch-graph
arch-graph:
	@mkdir -p $(REPORTS)
	@$(ARCHLINT) graph --out $(REPORTS)/arch.svg

## golangci: run golangci-lint across every module
.PHONY: golangci
golangci:
	@for m in $(MODULES); do echo "== golangci $$m =="; (cd $$m && $(GOLANGCI) run --timeout=5m) || exit 1; done

## lint: gofmt check + vet + arch-lint + golangci (the full static gate)
.PHONY: lint
lint:
	@echo "== gofmt =="; test -z "$$(gofmt -l $(shell git ls-files '*.go'))" || (echo "gofmt needed:"; gofmt -l $(shell git ls-files '*.go'); exit 1)
	@$(MAKE) vet
	@$(MAKE) arch-lint
	@$(MAKE) golangci

## tidy: sync the workspace and tidy every module
.PHONY: tidy
tidy:
	@$(GO) work sync
	@for m in $(MODULES); do echo "== tidy $$m =="; (cd $$m && $(GO) mod tidy) || exit 1; done

## install: install pinned dev/CI tools and enable git hooks
.PHONY: install
install: hooks
	$(GO) install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_VERSION)
	$(GO) install github.com/fe3dback/go-arch-lint@$(ARCHLINT_VERSION)

## hooks: enable the repo's git hooks (pre-commit rejects binary files)
.PHONY: hooks
hooks:
	@git config core.hooksPath .githooks
	@echo "git hooks enabled (core.hooksPath=.githooks)"

## check: the CI aggregate — build, lint, unit-test
.PHONY: check
check: build lint test

## clean: clear Go caches and reports
.PHONY: clean
clean:
	@$(GO) clean -cache -testcache
	@rm -rf $(REPORTS) bin

## help: list targets
.PHONY: help
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/## //'
