#!/bin/bash

SOURCE_ROOT=$(git rev-parse --show-toplevel)
cd "${SOURCE_ROOT}" || exit
export GOPATH=$HOME/go
export PATH=$GOPATH/bin:$PATH
make clean
make build
mkdir -p build/libs
cp "$GOPATH/pkg/mod/github.com/!cosm!wasm/wasmvm@v1.0.0/api/libwasmvm.x86_64.so" ./build/libs
cp "x/nitro/replay/libnitro_replayer.x86_64.so" ./build/libs
zip -r seid_bundle.zip ./build/*
