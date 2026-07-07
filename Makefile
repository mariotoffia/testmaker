# Testmaker — build / test / lint entry points.
# Multi-module go.work workspace: most targets loop over every go.mod.

GO            ?= go
GOLANGCI      ?= golangci-lint
ARCHLINT      ?= go-arch-lint
MODULES       := $(shell find . -name go.mod -not -path '*/.*' -exec dirname {} \;)
REPORTS       := reports

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

## install: install pinned dev/CI tools
.PHONY: install
install:
	$(GO) install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_VERSION)
	$(GO) install github.com/fe3dback/go-arch-lint@$(ARCHLINT_VERSION)

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
