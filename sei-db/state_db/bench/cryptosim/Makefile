BINARY := bin/cryptosim
LOGGER_BINARY := bin/configure-logger
MODULE_ROOT ?= $(shell git -C "$(CURDIR)" rev-parse --show-toplevel)
CMD_DIR := ./sei-db/state_db/bench/cryptosim/cmd

# The AVX-512 LtHash backend in sei-db/state_db/sc/flatkv/lthash is compiled in
# only under GOEXPERIMENT=simd, and the simd/archsimd API it calls landed in Go
# 1.27. Naming the toolchain here makes `go build` fetch it on demand, so the
# binary picks the fast backend with no environment setup on the host. Override
# either variable in the environment to build the portable path instead.
GOTOOLCHAIN ?= go1.27.1
GOEXPERIMENT ?= simd
export GOTOOLCHAIN
export GOEXPERIMENT

.PHONY: build
build:
	@mkdir -p "$(CURDIR)/bin"
	cd "$(MODULE_ROOT)" && go build -o "$(CURDIR)/$(BINARY)" "$(CMD_DIR)/cryptosim"
	cd "$(MODULE_ROOT)" && go build -o "$(CURDIR)/$(LOGGER_BINARY)" "$(CMD_DIR)/configure-logger"

.PHONY: clean
clean:
	rm -f "$(CURDIR)/$(BINARY)" "$(CURDIR)/$(LOGGER_BINARY)"
