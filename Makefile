GO ?= $(HOME)/.local/go/bin/go
export PATH := $(dir $(GO)):$(PATH)

SANDBOX := $(CURDIR)/.sandbox
BIN := $(SANDBOX)/bin/coal
# Go walks into web/node_modules, which can hold stray Go packages.
PKGS = $(shell $(GO) list ./... | grep -v /node_modules/)

.PHONY: build test vet fmt check web dev e2e demo demo-init

build:
	$(GO) build -o $(BIN) ./cmd/coal

test:
	$(GO) test $(PKGS)

vet:
	$(GO) vet $(PKGS)

fmt:
	$(dir $(GO))gofmt -w cmd internal spec

# The anti-drift gate CI runs: gofmt, vet, build, the drift tests in
# internal/drift (-v so known-failure log lines show), and the site build
# with the schema compared byte for byte. site/build.sh needs bash and python3.
check: vet build
	@out=$$($(dir $(GO))gofmt -l cmd internal spec); if [ -n "$$out" ]; then echo "gofmt -w these files:"; echo "$$out"; exit 1; fi
	$(GO) test -v ./internal/drift/...
	@tmp=$$(mktemp -d); site/build.sh "$$tmp/site" >/dev/null && cmp spec/intent-manifest.schema.json "$$tmp/site/schema/intent-manifest/0.1.json"; rc=$$?; rm -rf "$$tmp"; exit $$rc

web:
	cd web && bun install --frozen-lockfile && bun run build

dev:
	$(MAKE) web build
	COAL_HOME=$(SANDBOX)/coal $(BIN) serve

# Builds the UI and the binary, then drives the embedded UI in headless Chromium.
e2e:
	$(MAKE) web build
	cd web && bun run e2e

# The five definition-of-done steps in a fresh .sandbox/demo-*/ dir.
demo:
	scripts/demo.sh

demo-init: build
	COAL_HOME=$(SANDBOX)/coal $(BIN) init
