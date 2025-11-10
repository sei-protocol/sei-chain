#!/usr/bin/make -f

########################################
### Testing

PACKAGES=$(shell go list ./...)

BINDIR ?= $(GOPATH)/bin

## required to be run first by most tests
build_docker_test_image:
	docker build -t tester -f ./test/docker/Dockerfile .
.PHONY: build_docker_test_image

### coverage, app, persistence, and libs tests
test_cover:
	# run the go unit tests with coverage
	bash test/test_cover.sh
.PHONY: test_cover

test_abci_apps:
	bash abci/tests/test_app/test.sh
.PHONY: test_abci_apps

test_integrations:
	make build_docker_test_image
	make tools
	make install
	make test_cover
	make test_abci_apps
	make test_libs
.PHONY: test_integrations

test_release:
	@go test -tags release $(PACKAGES)
.PHONY: test_release

test100:
	@for i in {1..100}; do make test; done
.PHONY: test100

### go tests
test:
	@echo "--> Running go test"
	@go test -p 1 $(PACKAGES) -tags deadlock
.PHONY: test

test_race:
	@echo "--> Running go test --race"
	@go test -p 1 -v -race $(PACKAGES)
.PHONY: test_race
