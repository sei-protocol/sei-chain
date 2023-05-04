BUILD_DIR ?= $(CURDIR)/build
COMMIT    := $(shell git log -1 --format='%H')

all: test-unit install

.PHONY: all

###############################################################################
##                                  Version                                  ##
###############################################################################

ifeq (,$(VERSION))
  VERSION := $(shell git describe --exact-match 2>/dev/null)
  # if VERSION is empty, then populate it with branch's name and raw commit hash
  ifeq (,$(VERSION))
    VERSION := $(BRANCH)-$(COMMIT)
  endif
endif

###############################################################################
##                              Build / Install                              ##
###############################################################################

ldflags = -X price-feeder/cmd.Version=$(VERSION) \
		  -X price-feeder/cmd.Commit=$(COMMIT)

BUILD_FLAGS := -ldflags '$(ldflags)'

CGO_FLAG = CGO_ENABLED=0
ifeq ($(shell uname),Linux)
  CGO_FLAG = CGO_ENABLED=1
endif

build: go.sum
	@echo "--> Building..."
	$(CGO_FLAG) go build -mod=readonly -o $(BUILD_DIR)/ $(BUILD_FLAGS) ./...

install: go.sum
	@echo "--> Installing..."
	$(CGO_FLAG) go install -mod=readonly $(BUILD_FLAGS) ./...

.PHONY: build install

###############################################################################
##                              Tests & Linting                              ##
###############################################################################

test-unit:
	@echo "--> Running tests"
	@go test -mod=readonly -race ./... -v

.PHONY: test-unit

lint:
	@echo "--> Running linter"
	@go run github.com/golangci/golangci-lint/cmd/golangci-lint run --timeout=10m

.PHONY: lint
