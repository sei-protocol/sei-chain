#!/bin/bash

SOURCE_ROOT=$(git rev-parse --show-toplevel)
cd "${SOURCE_ROOT}" || exit
make clean
make build
cd build || exit
cp -r "$GOPATH/pkg/mod/github.com/\!cosm\!wasm" ./
zip -r seid_bundle.zip ./*
