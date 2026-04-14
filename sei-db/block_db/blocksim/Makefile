BINARY := bin/blocksim
LOGGER_BINARY := bin/configure-logger
MODULE_ROOT ?= $(shell git -C "$(CURDIR)" rev-parse --show-toplevel)
CMD_DIR := ./sei-db/block_db/blocksim/cmd

.PHONY: build
build:
	@mkdir -p "$(CURDIR)/bin"
	cd "$(MODULE_ROOT)" && go build -o "$(CURDIR)/$(BINARY)" "$(CMD_DIR)/blocksim"
	cd "$(MODULE_ROOT)" && go build -o "$(CURDIR)/$(LOGGER_BINARY)" "$(CMD_DIR)/configure-logger"

.PHONY: clean
clean:
	rm -f "$(CURDIR)/$(BINARY)" "$(CURDIR)/$(LOGGER_BINARY)"
