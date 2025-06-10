# docker build . -t sei-protocol/sei:latest
# docker run --rm -it sei-protocol/sei:latest /bin/sh
FROM golang:1.21.4-alpine AS go-builder

# this comes from standard alpine nightly file
#  https://github.com/rust-lang/docker-rust-nightly/blob/master/alpine3.12/Dockerfile
# with some changes to support our toolchain, etc
SHELL ["/bin/sh", "-ecuxo", "pipefail"]
# we probably want to default to latest and error
# since this is predominantly for dev use
# hadolint ignore=DL3018
RUN apk add --no-cache ca-certificates build-base git
# NOTE: add these to run with LEDGER_ENABLED=true
# RUN apk add libusb-dev linux-headers

WORKDIR /code

# Download dependencies and CosmWasm libwasmvm if found.
ADD go.mod go.sum ./
RUN set -eux; \
    export ARCH=$(uname -m); \
    # Currently github.com/CosmWasm/wasmvm is being overriden by github.com/sei-protocol/sei-wasmvm
    # (see go.mod). However the rust precompiles are still fetched from the upstream repository.
    # Here we assume that the sei-wasm release version is prefixed with the wasmvm release version
    # with the matching precompiles. Therefore, to compute the download url, we just strip the suffix
    # of the sei-wasm release version.
    WASM_VERSION=$(go list -f {{.Replace.Version}} -m github.com/CosmWasm/wasmvm | sed s/-.*//); \
    if [ ! -z "${WASM_VERSION}" ]; then \
      wget -O /lib/libwasmvm_muslc.a https://github.com/CosmWasm/wasmvm/releases/download/${WASM_VERSION}/libwasmvm_muslc.${ARCH}.a; \
    fi; \
    wget -O /lib/libwasmvm152_muslc.a https://github.com/sei-protocol/sei-wasmd/releases/download/v0.3.6/libwasmvm152_muslc.${ARCH}.a; \
    wget -O /lib/libwasmvm155_muslc.a https://github.com/sei-protocol/sei-wasmd/releases/download/v0.3.6/libwasmvm155_muslc.${ARCH}.a; \
    go mod download;

# Copy over code
COPY . /code/

# force it to use static lib (from above) not standard libgo_cosmwasm.so file
# then log output of file /code/build/seid
# then ensure static linking
RUN LEDGER_ENABLED=false BUILD_TAGS=muslc LINK_STATICALLY=true make build -B \
  && file /code/build/seid \
  && echo "Ensuring binary is statically linked ..." \
  && (file /code/build/seid | grep "statically linked")

# --------------------------------------------------------
FROM alpine:3.18

COPY --from=go-builder /code/build/seid /usr/bin/seid


# rest server, tendermint p2p, tendermint rpc
EXPOSE 1317 26656 26657

CMD ["/usr/bin/seid", "version"]
