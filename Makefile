.PHONY: build clean test fmt vet lint

BINARY_NAME=lattice
BUILD_DIR=bin
GO=go
BUILD_LDFLAGS=-s -w
BUILD_FLAGS=-trimpath -ldflags="$(BUILD_LDFLAGS)"

# Lattice is pure Go — the SQLite driver is modernc.org/sqlite (no cgo).
# Disabling CGO globally makes that a contract: any future import that pulls
# in a C toolchain fails the build instead of silently re-introducing it.
export CGO_ENABLED=0

ifneq (,$(wildcard ./.env))
    include .env
    export
endif

build:
	$(GO) build $(BUILD_FLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/server

clean:
	rm -rf $(BUILD_DIR)

test:
	$(GO) test ./...

fmt:
	$(GO) fmt ./...

vet:
	$(GO) vet ./...

lint: vet
	$(GO) run honnef.co/go/tools/cmd/staticcheck@latest ./...

.DEFAULT_GOAL := build
