BINARY := bin/cryptosim
MODULE_ROOT ?= $(shell git -C "$(CURDIR)" rev-parse --show-toplevel)
PACKAGE := ./sei-db/state_db/bench/cryptosim/main

.PHONY: build
build:
	@mkdir -p "$(CURDIR)/bin"
	cd "$(MODULE_ROOT)" && go build -o "$(CURDIR)/$(BINARY)" "$(PACKAGE)"

.PHONY: clean
clean:
	rm -f "$(CURDIR)/$(BINARY)"
