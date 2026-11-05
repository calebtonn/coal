GO ?= $(HOME)/.local/go/bin/go
export PATH := $(dir $(GO)):$(PATH)

SANDBOX := $(CURDIR)/.sandbox
BIN := $(SANDBOX)/bin/coal

.PHONY: build test vet fmt web dev demo-init

build:
	$(GO) build -o $(BIN) ./cmd/coal

test:
	$(GO) test ./...

vet:
	$(GO) vet ./...

fmt:
	$(dir $(GO))gofmt -w cmd internal spec

web:
	@echo "not yet"

dev:
	@echo "not yet"

demo-init: build
	COAL_HOME=$(SANDBOX)/coal $(BIN) init
