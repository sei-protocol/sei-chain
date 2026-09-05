BINARY := bin/walrussim
MODULE_ROOT ?= $(shell git -C "$(CURDIR)" rev-parse --show-toplevel)
CMD_DIR := ./sei-db/db_engine/walrus/walrussim/cmd

.PHONY: build
build:
	@mkdir -p "$(CURDIR)/bin"
	cd "$(MODULE_ROOT)" && go build -o "$(CURDIR)/$(BINARY)" "$(CMD_DIR)/walrussim"

.PHONY: clean
clean:
	rm -f "$(CURDIR)/$(BINARY)"
