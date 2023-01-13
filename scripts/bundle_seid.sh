#!/bin/bash

SOURCE_ROOT=$(git rev-parse --show-toplevel)
cd "${SOURCE_ROOT}" || exit
export GOPATH=$HOME/go
export PATH=$GOPATH/bin:$PATH
make clean
make build-linux
cd build || exit
ls -l $GOPATH
ls -l $GOPATH/pkg/mod
cp -r "$GOPATH/pkg/mod/github.com/\!cosm\!wasm" ./
zip -r seid_bundle.zip ./*
