#!/bin/bash

set -euo pipefail

rm -rf ./build/proto

# We have to build regen-network protoc-gen-gocosmos from source because
# the module uses replace directive, which makes it impossible to use
# go install like a healthy human being.
#
# As a workaround, we download the source code to a temporary location
# and build the binary. buf.gen.yaml then implicitly uses the path to the
# built binary. This is ugly but it works, and results in the least amount
# of changes across the repo to have _a_ working solution without accidentally
# breaking anything else or introduce too much change as part of automating
# the proto generation.
go get github.com/regen-network/cosmos-proto/protoc-gen-gocosmos@v0.3.1
mkdir -p ./build/proto/gocosmos
build_out="${PWD}/build/proto/gocosmos"
pushd "$(go env GOMODCACHE)/github.com/regen-network/cosmos-proto@v0.3.1" &&
  go build -o "${build_out}/protoc-gen-gocosmos" ./protoc-gen-gocosmos &&
  popd

buf generate

# We can't manipulate the outputs enough to eliminate the extra move-abouts.
# So we just copy the files we want to the right places manually.
# The repo restructure should help this in the future.
cp -rf ./build/proto/gocosmos/github.com/sei-protocol/sei-chain/* ./
cp -rf ./build/proto/gocosmos/github.com/cosmos/cosmos-sdk/* ./sei-cosmos
cp -rf ./build/proto/gocosmos/github.com/CosmWasm/wasmd/* ./sei-wasmd

# Use gogofaster for tendermint because that's the generator it is using currently.
cp -rf ./build/proto/gogofaster/github.com/tendermint/tendermint/* ./sei-tendermint
