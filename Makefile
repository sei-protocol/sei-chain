#!/usr/bin/make -f

VERSION := $(shell echo $(shell git describe --tags) | sed 's/^v//')
COMMIT := $(shell git log -1 --format='%H')

export PROJECT_HOME=$(shell git rev-parse --show-toplevel)
export GO_PKG_PATH=$(HOME)/go/pkg
export GO111MODULE = on
NITRO_RELEASE_PATH := $(PROJECT_HOME)/nitro-replayer/target/release
NITRO_LIB_PATH := $(PROJECT_HOME)/x/nitro/

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

ldflags = -X github.com/cosmos/cosmos-sdk/version.Name=sei \
			-X github.com/cosmos/cosmos-sdk/version.ServerName=seid \
			-X github.com/cosmos/cosmos-sdk/version.Version=$(VERSION) \
			-X github.com/cosmos/cosmos-sdk/version.Commit=$(COMMIT) \
			-X "github.com/cosmos/cosmos-sdk/version.BuildTags=$(build_tags_comma_sep)"

ifeq ($(LINK_STATICALLY),true)
	ldflags += -linkmode=external -extldflags "-Wl,-z,muldefs -static"
endif
ldflags += $(LDFLAGS)
ldflags := $(strip $(ldflags))

# BUILD_FLAGS := -tags "$(build_tags)" -ldflags '$(ldflags)' -race
BUILD_FLAGS := -tags "$(build_tags)" -ldflags '$(ldflags)'

#### Command List ####

all: lint install

install: go.sum
		go install $(BUILD_FLAGS) ./cmd/seid

# In case when running seid fails with nitro issue or if you make changes to nitro, please use install-all
install-all: build-nitro install

install-price-feeder: go.sum
		go install $(BUILD_FLAGS) ./oracle/price-feeder

loadtest: go.sum
		go build $(BUILD_FLAGS) -o ./build/loadtest ./loadtest/

price-feeder: go.sum
		go build $(BUILD_FLAGS) -o ./build/price-feeder ./oracle/price-feeder

go.sum: go.mod
		@echo "--> Ensure dependencies have not been modified"
		@go mod verify

lint:
	golangci-lint run
	find . -name '*.go' -type f -not -path "./vendor*" -not -path "*.git*" | xargs gofmt -d -s
	go mod verify

build:
	go build $(BUILD_FLAGS) -o ./build/seid ./cmd/seid

# In case running seid fails with nitro issue or if you make changes to nitro, please use build-all
build-all: build-nitro build

build-nitro:
	@cd $(PROJECT_HOME)/nitro-replayer && cargo build --release
	if [ -f "$(NITRO_RELEASE_PATH)/libnitro_replayer.dylib" ]; then \
		cp $(NITRO_RELEASE_PATH)/libnitro_replayer.dylib $(NITRO_LIB_PATH)/replay; \
	fi
	if [ -f "$(NITRO_RELEASE_PATH)/libnitro_replayer.x86_64.so" ]; then \
		cp $(NITRO_RELEASE_PATH)/libnitro_replayer.x86_64.so $(NITRO_LIB_PATH)/replay; \
	fi

clean:
	rm -rf ./build



###############################################################################
###                       Local testing using docker container              ###
###############################################################################
# To start a 4-node cluster from scratch:
# make clean && make build-docker-node && docker-cluster-start
# To stop the 4-node cluster:
# make docker-cluster-stop
###############################################################################


# Build linux binary on other platforms
build-linux:
	GOOS=linux GOARCH=amd64 CGO_ENABLED=1 make build
.PHONY: build-linux

# Build docker image
build-docker-node:
	@cd docker && docker build --tag sei-chain/localnode localnode
.PHONY: build-docker-localnode

# Run a single docker container
run-docker-node:
	@rm -rf $(PROJECT_HOME)/build/generated
	docker run --rm \
	-v $(PROJECT_HOME):/sei-protocol/sei-chain:Z \
	-v $(GO_PKG_PATH)/mod:/root/go/pkg/mod:Z \
	sei-chain/localnode
.PHONY: run-docker-localnode

# Run a 4-node docker containers
docker-cluster-start: localnet-stop build-docker-localnode
	@rm -rf $(PROJECT_HOME)/build/generated
	@cd docker && docker-compose up
.PHONY: localnet-start

# Use this to skip the seid build process
docker-cluster-start-skipbuild: localnet-stop build-docker-localnode
	@rm -rf $(PROJECT_HOME)/build/generated
	@cd docker && SKIP_BUILD=true docker-compose up
.PHONY: localnet-start

# Stop 4-node docker containers
docker-cluster-stop:
	@cd docker && docker-compose down
.PHONY: localnet-stop