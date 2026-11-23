BINARY := bin/lakesim
MODULE_ROOT ?= $(shell git -C "$(CURDIR)" rev-parse --show-toplevel)
CMD_DIR := ./sei-db/bench/lakesim/cmd/lakesim

.PHONY: build
build:
	@mkdir -p "$(CURDIR)/bin"
	cd "$(MODULE_ROOT)" && go build -o "$(CURDIR)/$(BINARY)" "$(CMD_DIR)"

.PHONY: clean
clean:
	rm -f "$(CURDIR)/$(BINARY)"
