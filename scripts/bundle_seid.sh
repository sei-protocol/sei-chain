#!/bin/bash

SOURCE_ROOT=$(git rev-parse --show-toplevel)
cd "${SOURCE_ROOT}" || exit
export GOPATH=$HOME/go
export PATH=$GOPATH/bin:$PATH
make clean
make build
cp -r "$GOPATH/pkg/mod/github.com/!cosm!wasm" ./build/
cp -r "x/nitro/replay/libnitro_replayer.x86_64.so" ./build/
cp -r "x/nitro/replay/libnitro_replayer.dylib" ./build/
zip -r seid_bundle.zip ./build/*
