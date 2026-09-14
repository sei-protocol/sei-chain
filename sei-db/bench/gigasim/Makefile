BINARY := bin/gigasim
LOGGER_BINARY := bin/configure-logger
MODULE_ROOT ?= $(shell git -C "$(CURDIR)" rev-parse --show-toplevel)
CMD_DIR := ./sei-db/bench/gigasim/cmd

.PHONY: build
build:
	@mkdir -p "$(CURDIR)/bin"
	cd "$(MODULE_ROOT)" && go build -o "$(CURDIR)/$(BINARY)" "$(CMD_DIR)/gigasim"
	cd "$(MODULE_ROOT)" && go build -o "$(CURDIR)/$(LOGGER_BINARY)" "$(CMD_DIR)/configure-logger"

.PHONY: clean
clean:
	rm -f "$(CURDIR)/$(BINARY)" "$(CURDIR)/$(LOGGER_BINARY)"
