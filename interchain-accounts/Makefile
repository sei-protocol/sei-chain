#!/usr/bin/make -f
PACKAGES=$(shell go list ./...)
DOCKER := $(shell which docker)
DOCKER_BUF := $(DOCKER) run --rm -v $(CURDIR):/workspace --workdir /workspace bufbuild/buf:1.0.0-rc8

###############################################################################
###                           Install                                       ###
###############################################################################
install: go.sum
		@echo "--> Installing icad"
		@go install ./cmd/icad

install-debug: go.sum
	go build -gcflags="all=-N -l" ./cmd/icad

go.sum: go.mod
	@echo "--> Ensure dependencies have not been modified"
	GO111MODULE=on go mod verify

test:
	@go test -mod=readonly $(PACKAGES) -cover

lint:
	@echo "--> Running linter"
	@golangci-lint run
	@go mod verify

###############################################################################
###                                Protobuf                                 ###
###############################################################################

containerProtoVer=v0.2
containerProtoImage=tendermintdev/sdk-proto-gen:$(containerProtoVer)
containerProtoGen=cosmos-sdk-proto-gen-$(containerProtoVer)
containerProtoGenSwagger=cosmos-sdk-proto-gen-swagger-$(containerProtoVer)
containerProtoFmt=cosmos-sdk-proto-fmt-$(containerProtoVer)

proto-all: proto-format proto-lint proto-gen

proto-gen:
	@echo "Generating Protobuf files"
	$(DOCKER) run --rm -v $(CURDIR):/workspace --workdir /workspace tendermintdev/sdk-proto-gen sh ./scripts/protocgen.sh

proto-format:
	@echo "Formatting Protobuf files"
	@if docker ps -a --format '{{.Names}}' | grep -Eq "^${containerProtoFmt}$$"; then docker start -a $(containerProtoFmt); else docker run --name $(containerProtoFmt) -v $(CURDIR):/workspace --workdir /workspace tendermintdev/docker-build-proto \
		find ./ -not -path "./third_party/*" -name "*.proto" -exec clang-format -i {} \; ; fi

proto-lint:
	@$(DOCKER_BUF) lint --error-format=json

proto-check-breaking:
	@$(DOCKER_BUF) breaking --against $(HTTPS_GIT)#branch=main

TM_URL              = https://raw.githubusercontent.com/tendermint/tendermint/v0.34.0-rc6/proto/tendermint
GOGO_PROTO_URL      = https://raw.githubusercontent.com/regen-network/protobuf/cosmos
CONFIO_URL          = https://raw.githubusercontent.com/confio/ics23/v0.6.3
SDK_PROTO_URL 		= https://raw.githubusercontent.com/cosmos/cosmos-sdk/v0.41.0/proto/cosmos

TM_CRYPTO_TYPES     = third_party/proto/tendermint/crypto
TM_ABCI_TYPES       = third_party/proto/tendermint/abci
TM_TYPES            = third_party/proto/tendermint/types
TM_VERSION          = third_party/proto/tendermint/version
TM_LIBS             = third_party/proto/tendermint/libs/bits
TM_P2P              = third_party/proto/tendermint/p2p

SDK_QUERY 			= third_party/proto/cosmos/base/query/v1beta1
SDK_BASE 			= third_party/proto/cosmos/base/v1beta1
SDK_UPGRADE			= third_party/proto/cosmos/upgrade

GOGO_PROTO_TYPES    = third_party/proto/gogoproto
CONFIO_TYPES        = third_party/proto/confio

proto-update-deps:
	@mkdir -p $(GOGO_PROTO_TYPES)
	@curl -sSL $(GOGO_PROTO_URL)/gogoproto/gogo.proto > $(GOGO_PROTO_TYPES)/gogo.proto

	@mkdir -p $(SDK_QUERY)
	@curl -sSL $(SDK_PROTO_URL)/base/query/v1beta1/pagination.proto > $(SDK_QUERY)/pagination.proto

	@mkdir -p $(SDK_BASE)
	@curl -sSL $(SDK_PROTO_URL)/base/v1beta1/coin.proto > $(SDK_BASE)/coin.proto

	@mkdir -p $(SDK_UPGRADE)
	@curl -sSL $(SDK_PROTO_URL)/upgrade/v1beta1/upgrade.proto > $(SDK_UPGRADE)/v1beta1/upgrade.proto

## Importing of tendermint protobuf definitions currently requires the
## use of `sed` in order to build properly with cosmos-sdk's proto file layout
## (which is the standard Buf.build FILE_LAYOUT)
## Issue link: https://github.com/tendermint/tendermint/issues/5021
	@mkdir -p $(TM_TYPES)
	@curl -sSL $(TM_URL)/types/types.proto > $(TM_TYPES)/types.proto
	@curl -sSL $(TM_URL)/types/validator.proto > $(TM_TYPES)/validator.proto

	@mkdir -p $(TM_VERSION)
	@curl -sSL $(TM_URL)/version/types.proto > $(TM_VERSION)/types.proto

	@mkdir -p $(TM_LIBS)
	@curl -sSL $(TM_URL)/libs/bits/types.proto > $(TM_LIBS)/types.proto

	@mkdir -p $(TM_CRYPTO_TYPES)
	@curl -sSL $(TM_URL)/crypto/proof.proto > $(TM_CRYPTO_TYPES)/proof.proto
	@curl -sSL $(TM_URL)/crypto/keys.proto > $(TM_CRYPTO_TYPES)/keys.proto

	@mkdir -p $(CONFIO_TYPES)
	@curl -sSL $(CONFIO_URL)/proofs.proto > $(CONFIO_TYPES)/proofs.proto

## insert go package option into proofs.proto file
## Issue link: https://github.com/confio/ics23/issues/32
	@sed -i '4ioption go_package = "github.com/confio/ics23/go";' $(CONFIO_TYPES)/proofs.proto

.PHONY: proto-all proto-gen proto-gen-any proto-swagger-gen proto-format proto-lint proto-check-breaking proto-update-deps

###############################################################################
###                                Initialize                               ###
###############################################################################

init: kill-dev install 
	@echo "Initializing both blockchains..."
	./network/init.sh
	./network/start.sh
	@echo "Initializing relayer..." 
	./network/hermes/restore-keys.sh
	./network/hermes/create-conn.sh

init-golang-rly: kill-dev install
	@echo "Initializing both blockchains..."
	./network/init.sh
	./network/start.sh
	@echo "Initializing relayer..."
	./network/relayer/interchain-acc-config/rly.sh

start: 
	@echo "Starting up test network"
	./network/start.sh

start-rly:
	./network/hermes/start.sh

kill-dev:
	@echo "Killing icad and removing previous data"
	-@rm -rf ./data
	-@killall icad 2>/dev/null
