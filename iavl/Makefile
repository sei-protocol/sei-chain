GOTOOLS := github.com/golangci/golangci-lint/cmd/golangci-lint
VERSION := $(shell echo $(shell git describe --tags) | sed 's/^v//')
COMMIT := $(shell git log -1 --format='%H')
BRANCH=$(shell git rev-parse --abbrev-ref HEAD)
DOCKER_BUF := docker run -v $(shell pwd):/workspace --workdir /workspace bufbuild/buf
DOCKER := $(shell which docker)
HTTPS_GIT := https://github.com/cosmos/iavl.git

PDFFLAGS := -pdf --nodefraction=0.1
CMDFLAGS := -ldflags -X TENDERMINT_IAVL_COLORS_ON=on 
LDFLAGS := -ldflags "-X github.com/cosmos/iavl.Version=$(VERSION) -X github.com/cosmos/iavl.Commit=$(COMMIT) -X github.com/cosmos/iavl.Branch=$(BRANCH)"

all: lint test install

install:
ifeq ($(COLORS_ON),)
	go install ./cmd/iaviewer
else
	go install $(CMDFLAGS) ./cmd/iaviewer
endif
.PHONY: install

test-short:
	@echo "--> Running go test"
	@go test ./... $(LDFLAGS) -v --race --short
.PHONY: test-short

test:
	@echo "--> Running go test"
	@go test ./... $(LDFLAGS) -v 
.PHONY: test

tools:
	go get -v $(GOTOOLS)
.PHONY: tools

format:
	find . -name '*.go' -type f -not -path "*.git*" -not -name '*.pb.go' -not -name '*pb_test.go' | xargs gofmt -w -s
	find . -name '*.go' -type f -not -path "*.git*"  -not -name '*.pb.go' -not -name '*pb_test.go' | xargs goimports -format
.PHONY: format

# look into .golangci.yml for enabling / disabling linters
lint:
	@echo "--> Running linter"
	@golangci-lint run
	@go mod verify
.PHONY: lint

# bench is the basic tests that shouldn't crash an aws instance
bench:
	cd benchmarks && \
		go test $(LDFLAGS) -tags cleveldb,rocksdb,boltdb,badgerdb -run=NOTEST -bench=Small . && \
		go test $(LDFLAGS) -tags cleveldb,rocksdb,boltdb,badgerdb -run=NOTEST -bench=Medium . && \
		go test $(LDFLAGS) -run=NOTEST -bench=RandomBytes .
.PHONY: bench

# fullbench is extra tests needing lots of memory and to run locally
fullbench:
	cd benchmarks && \
		go test $(LDFLAGS) -run=NOTEST -bench=RandomBytes . && \
		go test $(LDFLAGS) -tags cleveldb,rocksdb,boltdb,badgerdb -run=NOTEST -bench=Small . && \
		go test $(LDFLAGS) -tags cleveldb,rocksdb,boltdb,badgerdb -run=NOTEST -bench=Medium . && \
		go test $(LDFLAGS) -tags cleveldb,rocksdb,boltdb,badgerdb -run=NOTEST -timeout=30m -bench=Large . && \
		go test $(LDFLAGS) -run=NOTEST -bench=Mem . && \
		go test $(LDFLAGS) -run=NOTEST -timeout=60m -bench=LevelDB .
.PHONY: fullbench

# note that this just profiles the in-memory version, not persistence
profile:
	cd benchmarks && \
		go test $(LDFLAGS) -bench=Mem -cpuprofile=cpu.out -memprofile=mem.out . && \
		go tool pprof ${PDFFLAGS} benchmarks.test cpu.out > cpu.pdf && \
		go tool pprof --alloc_space ${PDFFLAGS} benchmarks.test mem.out > mem_space.pdf && \
		go tool pprof --alloc_objects ${PDFFLAGS} benchmarks.test mem.out > mem_obj.pdf
.PHONY: profile

explorecpu:
	cd benchmarks && \
		go tool pprof benchmarks.test cpu.out
.PHONY: explorecpu

exploremem:
	cd benchmarks && \
		go tool pprof --alloc_objects benchmarks.test mem.out
.PHONY: exploremem

delve:
	dlv test ./benchmarks -- -test.bench=.
.PHONY: delve

all: tools
.PHONY: all

tools: protobuf
.PHONY: tools

check: check_tools
.PHONY: check

check_tools:
	@# https://stackoverflow.com/a/25668869
	@echo "Found tools: $(foreach tool,$(notdir $(GOTOOLS)),\
        $(if $(shell which $(tool)),$(tool),$(error "No $(tool) in PATH")))"
.PHONY: check_tools

tools-clean:
	rm -f $(CERTSTRAP) $(PROTOBUF) $(GOX) $(GOODMAN)
	rm -rf /usr/local/include/google/protobuf
	rm -f /usr/local/bin/protoc
.PHONY: tooks-clean

###
# Non Go tools
###

.PHONY: lint test tools install delve exploremem explorecpu profile fullbench bench proto-gen proto-lint proto-check-breaking

proto-lint:
	@$(DOCKER_BUF) check lint --error-format=json
.PHONY: proto-lint

proto-check-breaking:
	@$(DOCKER_BUF) check breaking --against-input $(HTTPS_GIT)#branch=master
.PHONY: proto-check-breaking

proto-gen:
	@echo "Generating Protobuf files"
	$(DOCKER) run --rm -v $(CURDIR):/workspace --workdir /workspace tendermintdev/sdk-proto-gen:master sh scripts/protocgen.sh
.PHONY: proto-gen-d
