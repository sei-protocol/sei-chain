GOTOOLS := github.com/golangci/golangci-lint/cmd/golangci-lint
VERSION := $(shell echo $(shell git describe --tags) | sed 's/^v//')
COMMIT := $(shell git log -1 --format='%H')
BRANCH=$(shell git rev-parse --abbrev-ref HEAD)
DOCKER_BUF := docker run -v $(shell pwd):/workspace --workdir /workspace bufbuild/buf
DOCKER := $(shell which docker)
HTTPS_GIT := https://github.com/cosmos/iavl.git
UNAME_S := $(shell uname -s)

ifeq ($(UNAME_S),Darwin)
    ROCKSDB_PATH := $(shell brew --prefix rocksdb)
    SNAPPY_PATH := $(shell brew --prefix snappy)
    LZ4_PATH := $(shell brew --prefix lz4)
    ZSTD_PATH := $(shell brew --prefix zstd)
endif

# NOTE: Requires to sudo apt install -y librocksdb-dev libsnappy-dev liblz4-dev libzstd-dev + sudo apt install -y zlib1g-dev + clone rocksdb + DEBUG_LEVEL=0 make shared_lib install-shared
ifeq ($(UNAME_S),Linux)
    ROCKSDB_PATH := /usr/local
    SNAPPY_PATH := /usr
    LZ4_PATH := /usr
    ZSTD_PATH := /usr
endif

install:
ifeq ($(COLORS_ON),)
	CGO_CFLAGS="-I$(ROCKSDB_PATH)/include" CGO_LDFLAGS="-L$(ROCKSDB_PATH)/lib -L$(SNAPPY_PATH)/lib -L$(LZ4_PATH)/lib -L$(ZSTD_PATH)/lib -lrocksdb -lstdc++ -lm -lz -lsnappy -llz4 -lzstd" go install ./cmd/dumpkv
else
	CGO_CFLAGS="-I$(ROCKSDB_PATH)/include" CGO_LDFLAGS="-L$(ROCKSDB_PATH)/lib -L$(SNAPPY_PATH)/lib -L$(LZ4_PATH)/lib -L$(ZSTD_PATH)/lib -lrocksdb -lstdc++ -lm -lz -lsnappy -llz4 -lzstd" go install $(CMDFLAGS) ./cmd/dumpkv
endif
.PHONY: install
