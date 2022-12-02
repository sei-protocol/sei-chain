# Makefile for the "localnode" docker image.

DANGLING_IMAGES := $(shell docker images -f "dangling=true" -q)

all: build

build:
	docker build --tag sei-chain/localnode localnode
.PHONY: build

clean:
	if [ ! -z "$(DANGLING_IMAGES)" ]; then docker rmi -f $(DANGLING_IMAGES); fi
.PHONY: clean