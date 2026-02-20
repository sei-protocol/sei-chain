#!/bin/bash

set -euo pipefail

echo "Generating protobuf code..."

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

go run github.com/bufbuild/buf/cmd/buf@v1.58.0 generate
go run github.com/bufbuild/buf/cmd/buf@v1.58.0 generate --template sei-tendermint/internal/buf.gen.yaml

# We can't manipulate the outputs enough to eliminate the extra move-abouts.
# So we just copy the files we want to the right places manually.
# The repo restructure should help this in the future.
cp -rf ./build/proto/gocosmos/github.com/sei-protocol/sei-chain/* ./
cp -rf ./build/proto/gocosmos/github.com/sei-protocol/sei-chain/sei-cosmos/* ./sei-cosmos
cp -rf ./build/proto/gocosmos/github.com/sei-protocol/sei-chain/sei-wasmd/* ./sei-wasmd

# Use gogofaster for tendermint and iavl because that's the generator they used originally. 
# See:
# * https://github.com/sei-protocol/sei-tendermint/blob/46d0a598a7f5c67cbdefea37c8da18df2c25d184/buf.gen.yaml#L3
# * https://github.com/sei-protocol/sei-iavl/blob/ff17b3473ee2438caa1777930a0bf73d267527fa/buf.gen.yaml#L9
cp -rf ./build/proto/gogofaster/github.com/sei-protocol/sei-chain/sei-tendermint/* ./sei-tendermint
cp -rf ./build/proto/gogofaster/github.com/sei-protocol/sei-chain/sei-iavl/* ./sei-iavl

rm -rf ./build/proto

echo "Protobuf code generation complete."
