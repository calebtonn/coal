GO ?= $(HOME)/.local/go/bin/go
export PATH := $(dir $(GO)):$(PATH)

SANDBOX := $(CURDIR)/.sandbox
BIN := $(SANDBOX)/bin/coal
# Go walks into web/node_modules, which can hold stray Go packages.
PKGS = $(shell $(GO) list ./... | grep -v /node_modules/)

.PHONY: build test vet fmt web dev e2e demo-init

build:
	$(GO) build -o $(BIN) ./cmd/coal

test:
	$(GO) test $(PKGS)

vet:
	$(GO) vet $(PKGS)

fmt:
	$(dir $(GO))gofmt -w cmd internal spec

web:
	cd web && bun install --frozen-lockfile && bun run build

dev:
	$(MAKE) web build
	COAL_HOME=$(SANDBOX)/coal $(BIN) serve

# Builds the UI and the binary, then drives the embedded UI in headless Chromium.
e2e:
	$(MAKE) web build
	cd web && bun run e2e

demo-init: build
	COAL_HOME=$(SANDBOX)/coal $(BIN) init
