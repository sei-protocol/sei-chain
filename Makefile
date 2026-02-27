#!/usr/bin/make -f

VERSION := $(shell echo $(shell git describe --tags))
COMMIT := $(shell git log -1 --format='%H')

BUILDDIR ?= $(CURDIR)/build
INVARIANT_CHECK_INTERVAL ?= $(INVARIANT_CHECK_INTERVAL:-0)
export PROJECT_HOME=$(shell git rev-parse --show-toplevel)
export GO_PKG_PATH=$(HOME)/go/pkg
export GO111MODULE = on

# process build tags

LEDGER_ENABLED ?= true
build_tags = netgo
ifeq ($(LEDGER_ENABLED),true)
	ifeq ($(OS),Windows_NT)
		GCCEXE = $(shell where gcc.exe 2> NUL)
		ifeq ($(GCCEXE),)
			$(error gcc.exe not installed for ledger support, please install or set LEDGER_ENABLED=false)
		else
			build_tags += ledger
		endif
	else
		UNAME_S = $(shell uname -s)
		ifeq ($(UNAME_S),OpenBSD)
			$(warning OpenBSD detected, disabling ledger support (https://github.com/cosmos/cosmos-sdk/issues/1988))
		else
			GCC = $(shell command -v gcc 2> /dev/null)
			ifeq ($(GCC),)
				$(error gcc not installed for ledger support, please install or set LEDGER_ENABLED=false)
			else
				build_tags += ledger
			endif
		endif
	endif
endif

build_tags += $(BUILD_TAGS)
build_tags := $(strip $(build_tags))

whitespace :=
whitespace += $(whitespace)
comma := ,
build_tags_comma_sep := $(subst $(whitespace),$(comma),$(build_tags))

# process linker flags

ldflags = -X github.com/sei-protocol/sei-chain/sei-cosmos/version.Name=sei \
			-X github.com/sei-protocol/sei-chain/sei-cosmos/version.AppName=seid \
			-X github.com/sei-protocol/sei-chain/sei-cosmos/version.Version=$(VERSION) \
			-X github.com/sei-protocol/sei-chain/sei-cosmos/version.Commit=$(COMMIT) \
			-X "github.com/sei-protocol/sei-chain/sei-cosmos/version.BuildTags=$(build_tags_comma_sep)"

# go 1.23+ needs a workaround to link memsize (see https://github.com/fjl/memsize).
# NOTE: this is a terribly ugly and unstable way of comparing version numbers,
# but that's what you get when you do anything nontrivial in a Makefile.
ifeq ($(firstword $(sort go1.23 $(shell go env GOVERSION))), go1.23)
	ldflags += -checklinkname=0
endif
ifeq ($(LINK_STATICALLY),true)
	ldflags += -linkmode=external -extldflags "-Wl,-z,muldefs -static"
endif
ldflags += $(LDFLAGS)
ldflags := $(strip $(ldflags))

# BUILD_FLAGS := -tags "$(build_tags)" -ldflags '$(ldflags)' -race
BUILD_FLAGS := -tags "$(build_tags)" -ldflags '$(ldflags)'
BUILD_FLAGS_MOCK_BALANCES := -tags "$(build_tags) mock_balances" -ldflags '$(ldflags)'
BUILD_FLAGS_BENCHMARK := -tags "$(build_tags) benchmark mock_balances" -ldflags '$(ldflags)'

#### Command List ####

all: lint install

install: go.sum
		go install $(BUILD_FLAGS) ./cmd/seid

install-mock-balances: go.sum
		go install $(BUILD_FLAGS_MOCK_BALANCES) ./cmd/seid

install-bench: go.sum
		go install $(BUILD_FLAGS_BENCHMARK) ./cmd/seid

install-with-race-detector: go.sum
		go install -race $(BUILD_FLAGS) ./cmd/seid

install-price-feeder: go.sum
		go install $(BUILD_FLAGS) ./oracle/price-feeder

###############################################################################
###                       RocksDB Backend Support                           ###
###############################################################################
# Prerequisites:
# - build-essential (gcc, g++, make)
# - pkg-config
# - cmake
# - git
# - zlib development headers
# - bzip2 development headers
# - snappy development headers
# - lz4 development headers
# - zstd development headers
# - jemalloc development headers
# - gflags development headers
# - liburing development headers
#
# Installation on Ubuntu/Debian:
# sudo apt-get update
# sudo apt-get install -y build-essential pkg-config cmake git zlib1g-dev \
#     libbz2-dev libsnappy-dev liblz4-dev libzstd-dev libjemalloc-dev \
#     libgflags-dev liburing-dev
#
# Usage:
# 1. Build RocksDB (one time): make build-rocksdb
# 2. Install seid with RocksDB: make install-rocksdb
###############################################################################

# Build and install RocksDB from source (one time setup)
build-rocksdb:
	@echo "Building RocksDB v8.9.1..."
	@if [ -d "rocksdb" ]; then \
		echo "rocksdb directory already exists, removing..."; \
		rm -rf rocksdb; \
	fi
	git clone https://github.com/facebook/rocksdb.git
	cd rocksdb && \
		git checkout v8.9.1 && \
		make clean && \
		CXXFLAGS='-march=native -DNDEBUG' make -j"$$(nproc)" shared_lib && \
		sudo make install-shared
	@echo '/usr/local/lib' | sudo tee /etc/ld.so.conf.d/rocksdb.conf
	@sudo ldconfig
	@echo "RocksDB installation complete!"

# Install seid with RocksDB backend support
install-rocksdb: go.sum
	@echo "Checking for RocksDB installation..."
	@if ! ldconfig -p | grep -q librocksdb; then \
		echo "Error: RocksDB not found. Please run 'make build-rocksdb' first."; \
		exit 1; \
	fi
	@echo "RocksDB found, proceeding with installation..."
	CGO_CFLAGS="-I/usr/local/include" \
	CGO_LDFLAGS="-L/usr/local/lib -lrocksdb -lz -lbz2 -lsnappy -llz4 -lzstd -ljemalloc" \
	go install $(BUILD_FLAGS) -tags "$(build_tags) rocksdbBackend" ./cmd/seid
	@echo "seid installed with RocksDB backend support!"

loadtest: go.sum
		go build $(BUILD_FLAGS) -o ./build/loadtest ./loadtest/

price-feeder: go.sum
		go build $(BUILD_FLAGS) -o ./build/price-feeder ./oracle/price-feeder

go.sum: go.mod
		@echo "--> Ensure dependencies have not been modified"
		@go mod verify

lint:
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.8.0 run
	go fmt ./...
	go vet ./...
	go mod tidy
	go mod verify

build:
	go build $(BUILD_FLAGS) -o ./build/seid ./cmd/seid

build-verbose:
	go build -x -v $(BUILD_FLAGS) -o ./build/seid ./cmd/seid

build-price-feeder:
	go build $(BUILD_FLAGS) -o ./build/price-feeder ./oracle/price-feeder

clean:
	rm -rf ./build

build-loadtest:
	go build -o build/loadtest ./loadtest/


###############################################################################
###                       Local testing using docker container              ###
###############################################################################
# To start a 4-node cluster from scratch:
# make clean && make docker-cluster-start
# To stop the 4-node cluster:
# make docker-cluster-stop
# If you have already built the binary, you can skip the build:
# make docker-cluster-start-skipbuild
###############################################################################


# Build linux binary on other platforms
build-linux:
	@if [ "$$(uname -m)" = "aarch64" ] || [ "$$(uname -m)" = "arm64" ]; then \
		echo "Building for ARM64..."; \
		GOOS=linux GOARCH=arm64 CGO_ENABLED=1 make build; \
	else \
		echo "Building for AMD64..."; \
		GOOS=linux GOARCH=amd64 CGO_ENABLED=1 CC=x86_64-linux-gnu-gcc make build; \
	fi
.PHONY: build-linux

build-price-feeder-linux:
	@if [ "$$(uname -m)" = "aarch64" ] || [ "$$(uname -m)" = "arm64" ]; then \
		GOOS=linux GOARCH=arm64 CGO_ENABLED=1 make build-price-feeder; \
	else \
		GOOS=linux GOARCH=amd64 CGO_ENABLED=1 CC=x86_64-linux-gnu-gcc make build-price-feeder; \
	fi
.PHONY: build-price-feeder-linux

# Auto-detect platform: use arm64 on ARM Macs, amd64 elsewhere
DOCKER_PLATFORM ?= $(shell if [ "$$(uname -m)" = "arm64" ]; then echo "linux/arm64"; else echo "linux/amd64"; fi)
export DOCKER_PLATFORM

# Build docker image for detected platform
build-docker-node:
	@echo "Building for $(DOCKER_PLATFORM)..."
	@cd docker && docker build --tag sei-chain/localnode localnode --platform $(DOCKER_PLATFORM)
.PHONY: build-docker-node

build-rpc-node:
	@cd docker && docker build --tag sei-chain/rpcnode rpcnode --platform linux/x86_64
.PHONY: build-rpc-node

# Run a single node docker container
run-local-node: kill-sei-node build-docker-node
	@rm -rf $(PROJECT_HOME)/build/generated
	docker run --rm \
	--name sei-node \
	--network host \
	--user="$(shell id -u):$(shell id -g)" \
	-v $(PROJECT_HOME):/sei-protocol/sei-chain:Z \
	-v $(GO_PKG_PATH)/mod:/root/go/pkg/mod:Z \
	-v $(shell go env GOCACHE):/root/.cache/go-build:Z \
	--platform linux/x86_64 \
	sei-chain/localnode
.PHONY: run-local-node

# Run a single rpc state sync node docker container
run-rpc-node: build-rpc-node
	docker run --rm \
	--name sei-rpc-node \
	--network docker_localnet \
	--user="$(shell id -u):$(shell id -g)" \
	-v $(PROJECT_HOME):/sei-protocol/sei-chain:Z \
	-v $(PROJECT_HOME)/../sei-tendermint:/sei-protocol/sei-tendermint:Z \
    -v $(PROJECT_HOME)/../sei-cosmos:/sei-protocol/sei-cosmos:Z \
    -v $(PROJECT_HOME)/../sei-db:/sei-protocol/sei-db:Z \
	-v $(GO_PKG_PATH)/mod:/root/go/pkg/mod:Z \
	-v $(shell go env GOCACHE):/root/.cache/go-build:Z \
	-p 26668-26670:26656-26658 \
	--platform linux/x86_64 \
	sei-chain/rpcnode
.PHONY: run-rpc-node

run-rpc-node-skipbuild: build-rpc-node
	docker run --rm \
	--name sei-rpc-node \
	--network docker_localnet \
	--user="$(shell id -u):$(shell id -g)" \
	-v $(PROJECT_HOME):/sei-protocol/sei-chain:Z \
	-v $(PROJECT_HOME)/../sei-tendermint:/sei-protocol/sei-tendermint:Z \
    -v $(PROJECT_HOME)/../sei-cosmos:/sei-protocol/sei-cosmos:Z \
    -v $(PROJECT_HOME)/../sei-db:/sei-protocol/sei-db:Z \
	-v $(GO_PKG_PATH)/mod:/root/go/pkg/mod:Z \
	-v $(shell go env GOCACHE):/root/.cache/go-build:Z \
	-p 26668-26670:26656-26658 \
	--platform linux/x86_64 \
	--env SKIP_BUILD=true \
	sei-chain/rpcnode
.PHONY: run-rpc-node

kill-sei-node:
	docker ps --filter name=sei-node --filter status=running -aq | xargs docker kill 2> /dev/null || true

kill-rpc-node:
	docker ps --filter name=sei-rpc-node --filter status=running -aq | xargs docker kill 2> /dev/null || true

# Run a 4-node docker containers
docker-cluster-start: docker-cluster-stop build-docker-node
	@rm -rf $(PROJECT_HOME)/build/generated
	@mkdir -p $(shell go env GOPATH)/pkg/mod
	@mkdir -p $(shell go env GOCACHE)
	@cd docker && \
		if [ "$${DOCKER_DETACH:-}" = "true" ]; then \
			DETACH_FLAG="-d"; \
		else \
			DETACH_FLAG=""; \
		fi; \
		DOCKER_PLATFORM=$(DOCKER_PLATFORM) USERID=$(shell id -u) GROUPID=$(shell id -g) GOCACHE=$(shell go env GOCACHE) NUM_ACCOUNTS=10 INVARIANT_CHECK_INTERVAL=${INVARIANT_CHECK_INTERVAL} UPGRADE_VERSION_LIST=${UPGRADE_VERSION_LIST} MOCK_BALANCES=${MOCK_BALANCES} GIGA_EXECUTOR=${GIGA_EXECUTOR} GIGA_OCC=${GIGA_OCC} docker compose up $$DETACH_FLAG

.PHONY: localnet-start

# Use this to skip the seid build process
docker-cluster-start-skipbuild: docker-cluster-stop build-docker-node
	@rm -rf $(PROJECT_HOME)/build/generated
	@cd docker && \
		if [ "$${DOCKER_DETACH:-}" = "true" ]; then \
			DETACH_FLAG="-d"; \
		else \
			DETACH_FLAG=""; \
		fi; \
		DOCKER_PLATFORM=$(DOCKER_PLATFORM) USERID=$(shell id -u) GROUPID=$(shell id -g) GOCACHE=$(shell go env GOCACHE) NUM_ACCOUNTS=10 SKIP_BUILD=true docker compose up $$DETACH_FLAG
.PHONY: localnet-start

# Stop 4-node docker containers
docker-cluster-stop:
	@cd docker && DOCKER_PLATFORM=$(DOCKER_PLATFORM) USERID=$(shell id -u) GROUPID=$(shell id -g) GOCACHE=$(shell go env GOCACHE) docker compose down
.PHONY: localnet-stop

# Run GIGA EVM integration tests with a GIGA-enabled cluster
# This starts a fresh cluster with GIGA_EXECUTOR and GIGA_OCC enabled,
# runs the EVM GIGA tests, then stops the cluster.
giga-integration-test:
	@echo "=== Starting GIGA Integration Tests ==="
	@$(MAKE) docker-cluster-stop || true
	@rm -rf $(PROJECT_HOME)/build/generated
	@GIGA_EXECUTOR=true GIGA_OCC=true DOCKER_DETACH=true $(MAKE) docker-cluster-start
	@echo "Waiting for cluster to be ready..."
	@timeout=300; elapsed=0; \
	while [ $$elapsed -lt $$timeout ]; do \
		if [ -f "build/generated/launch.complete" ] && [ $$(cat build/generated/launch.complete | wc -l) -ge 4 ]; then \
			echo "All 4 nodes are ready (took $${elapsed}s)"; \
			break; \
		fi; \
		sleep 5; \
		elapsed=$$((elapsed + 5)); \
		echo "  Waiting... ($${elapsed}s elapsed)"; \
	done; \
	if [ $$elapsed -ge $$timeout ]; then \
		echo "ERROR: Cluster failed to start within $${timeout}s"; \
		$(MAKE) docker-cluster-stop; \
		exit 1; \
	fi
	@echo "Waiting 10s for nodes to stabilize..."
	@sleep 10
	@echo "=== Running GIGA EVM Tests ==="
	@./integration_test/evm_module/scripts/evm_giga_tests.sh || ($(MAKE) docker-cluster-stop && exit 1)
	@echo "=== Stopping cluster ==="
	@$(MAKE) docker-cluster-stop
	@echo "=== GIGA Integration Tests Complete ==="
.PHONY: giga-integration-test

# Run a mixed-mode cluster: node 0 uses GIGA_EXECUTOR, nodes 1-3 use standard V2.
# Any determinism divergence between giga and V2 will cause the giga node to halt.
docker-cluster-start-giga-mixed: docker-cluster-stop build-docker-node
	@rm -rf $(PROJECT_HOME)/build/generated
	@mkdir -p $(shell go env GOPATH)/pkg/mod
	@mkdir -p $(shell go env GOCACHE)
	@cd docker && \
		if [ "$${DOCKER_DETACH:-}" = "true" ]; then \
			DETACH_FLAG="-d"; \
		else \
			DETACH_FLAG=""; \
		fi; \
		DOCKER_PLATFORM=$(DOCKER_PLATFORM) USERID=$(shell id -u) GROUPID=$(shell id -g) GOCACHE=$(shell go env GOCACHE) NUM_ACCOUNTS=10 INVARIANT_CHECK_INTERVAL=${INVARIANT_CHECK_INTERVAL} UPGRADE_VERSION_LIST=${UPGRADE_VERSION_LIST} MOCK_BALANCES=${MOCK_BALANCES} \
		docker compose -f docker-compose.yml -f docker-compose.giga-mixed.yml up $$DETACH_FLAG
.PHONY: docker-cluster-start-giga-mixed

# Run the giga mixed-mode integration test.
# Starts a cluster where only node 0 runs giga (sequential), nodes 1-3 run standard V2.
# Then runs hardhat tests. If giga produces different results, node 0 will halt.
giga-mixed-integration-test:
	@echo "=== Starting GIGA Mixed-Mode Integration Tests ==="
	@echo "=== Node 0: GIGA_EXECUTOR=true, Nodes 1-3: standard V2 ==="
	@$(MAKE) docker-cluster-stop || true
	@rm -rf $(PROJECT_HOME)/build/generated
	@DOCKER_DETACH=true $(MAKE) docker-cluster-start-giga-mixed
	@echo "Waiting for cluster to be ready..."
	@timeout=300; elapsed=0; \
	while [ $$elapsed -lt $$timeout ]; do \
		if [ -f "build/generated/launch.complete" ] && [ $$(cat build/generated/launch.complete | wc -l) -ge 4 ]; then \
			echo "All 4 nodes are ready (took $${elapsed}s)"; \
			break; \
		fi; \
		sleep 5; \
		elapsed=$$((elapsed + 5)); \
		echo "  Waiting... ($${elapsed}s elapsed)"; \
	done; \
	if [ $$elapsed -ge $$timeout ]; then \
		echo "ERROR: Cluster failed to start within $${timeout}s"; \
		$(MAKE) docker-cluster-stop; \
		exit 1; \
	fi
	@echo "Waiting 10s for nodes to stabilize..."
	@sleep 10
	@echo "=== Running GIGA EVM Tests (mixed mode) ==="
	@./integration_test/evm_module/scripts/evm_giga_tests.sh || (echo "TEST FAILURE - check if node 0 (giga) halted due to consensus mismatch" && $(MAKE) docker-cluster-stop && exit 1)
	@echo "=== Stopping cluster ==="
	@$(MAKE) docker-cluster-stop
	@echo "=== GIGA Mixed-Mode Integration Tests Complete ==="
.PHONY: giga-mixed-integration-test

# Implements test splitting and running. This is pulled directly from
# the github action workflows for better local reproducibility.

GO_TEST_FILES != find $(CURDIR) -name "*_test.go"

# default to four splits by default
NUM_SPLIT ?= 4

$(BUILDDIR):
	mkdir -p $@

# The format statement filters out all packages that don't have tests.
# Note we need to check for both in-package tests (.TestGoFiles) and
# out-of-package tests (.XTestGoFiles).
$(BUILDDIR)/packages.txt:$(GO_TEST_FILES) $(BUILDDIR)
	go list -f "{{ if (or .TestGoFiles .XTestGoFiles) }}{{ .ImportPath }}{{ end }}" ./... | sort > $@

TARGET_PACKAGE := github.com/sei-protocol/sei-chain/occ_tests

split-test-packages:$(BUILDDIR)/packages.txt
	split -d -n l/$(NUM_SPLIT) $< $<.
test-group-%:split-test-packages
	@echo "🔍 Checking for special package: $(TARGET_PACKAGE)"
	@if grep -q "$(TARGET_PACKAGE)" $(BUILDDIR)/packages.txt.$*; then \
		echo "🔒 Found $(TARGET_PACKAGE), running with -parallel=1"; \
		PARALLEL="-parallel=1"; \
	else \
		echo "⚡ Not found, running with -parallel=4"; \
		PARALLEL="-parallel=4"; \
	fi; \
	cat $(BUILDDIR)/packages.txt.$* | xargs go test $$PARALLEL -mod=readonly -timeout=10m -race -coverprofile=$*.profile.out -covermode=atomic -coverpkg=./...
