SHELL := /bin/bash

# All .go files in this tree are guarded by `//go:build littdb_wip` so the
# subtree is invisible to the parent module's default go tooling. Every
# target in this Makefile therefore passes `-tags littdb_wip` explicitly.
GO_TAGS := littdb_wip

# `-mod=mod` lets `go build` auto-add missing requires to go.mod as we pull
# additional upstream packages in-tree. Harmless once `go mod tidy` has been
# run, and useful during iterative porting.
GO_MOD_FLAG := -mod=mod

# Resolve + prune go.mod / go.sum against the current set of imports.
.PHONY: tidy
tidy:
	go mod tidy

# Download transitive deps into the module cache + go.sum without requiring
# tidy to complete. Useful as a one-shot refresh after editing go.mod.
.PHONY: deps
deps:
	go mod download all

# Build the litt CLI tool.
.PHONY: build
build:
	go build $(GO_MOD_FLAG) -tags=$(GO_TAGS) -o ./bin/litt ./cli

# Build every package in the tree (including tests).
.PHONY: build-all
build-all:
	go build $(GO_MOD_FLAG) -tags=$(GO_TAGS) ./...

# Remove the bin directory if it exists.
.PHONY: clean
clean:
	rm -rf ./bin

# Build the litt CLI tool with debug flags.
.PHONY: debug-build
debug-build: clean
	go build $(GO_MOD_FLAG) -tags=$(GO_TAGS) -gcflags "all=-N -l" -o ./bin/litt ./cli

# Run all LittDB unit tests.
.PHONY: test
test: build
	go test $(GO_MOD_FLAG) -tags=$(GO_TAGS) ./... -timeout=10m -v -p=1 -parallel=8

# Run all LittDB unit tests with verbose output.
.PHONY: test-verbose
test-verbose: build
	go test $(GO_MOD_FLAG) -tags=$(GO_TAGS) ./... -v -timeout=10m -p=1 -parallel=8
