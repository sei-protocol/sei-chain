ARG SEICTL_VERSION=v0.0.5@sha256:268fc871e8358e706f505f0ce9ef318761e0d00d317716e9d87218734ae1a81c

FROM ghcr.io/sei-protocol/seictl:${SEICTL_VERSION} AS seictl
FROM docker.io/golang:1.25.6-bookworm@sha256:2f768d462dbffbb0f0b3a5171009f162945b086f326e0b2a8fd5d29c3219ff14 AS builder
WORKDIR /go/src/sei-chain

COPY sei-wasmd/x/wasm/artifacts/v152/api/*.so /tmp/wasmd-libs/
COPY sei-wasmd/x/wasm/artifacts/v155/api/*.so /tmp/wasmd-libs/
COPY sei-wasmvm/internal/api/*.so /tmp/wasmvm-libs/
ARG TARGETARCH
RUN mkdir -p /go/lib && \
    case "${TARGETARCH}" in \
      amd64) ARCH_SUFFIX="x86_64" ;; \
      arm64) ARCH_SUFFIX="aarch64" ;; \
      *) echo "Unsupported architecture: ${TARGETARCH}" && exit 1 ;; \
    esac && \
    cp /tmp/wasmd-libs/libwasmvm152.${ARCH_SUFFIX}.so /go/lib/ && \
    cp /tmp/wasmd-libs/libwasmvm155.${ARCH_SUFFIX}.so /go/lib/ && \
    cp /tmp/wasmvm-libs/libwasmvm.${ARCH_SUFFIX}.so /go/lib/

COPY go.* ./
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go mod download

COPY . .
ENV CGO_ENABLED=1
ARG SEI_CHAIN_REF=""
ARG GO_BUILD_TAGS=""
ARG GO_BUILD_ARGS=""
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    BUILD_TAGS="netgo ledger ${GO_BUILD_TAGS}" && \
    VERSION_PKG="github.com/sei-protocol/sei-chain/sei-cosmos/version" && \
    LDFLAGS="\
      -X ${VERSION_PKG}.Name=sei \
      -X ${VERSION_PKG}.AppName=seid \
      -X ${VERSION_PKG}.Version=$(git describe --tags || echo "${SEI_CHAIN_REF}") \
      -X ${VERSION_PKG}.Commit=$(git log -1 --format='%H') \
      -X '${VERSION_PKG}.BuildTags=${BUILD_TAGS}'" && \
    go build -tags "${BUILD_TAGS}" -ldflags "${LDFLAGS}" ${GO_BUILD_ARGS} -o /go/bin/seid ./cmd/seid && \
    go build -tags "${BUILD_TAGS}" -ldflags "${LDFLAGS}" ${GO_BUILD_ARGS} -o /go/bin/price-feeder ./oracle/price-feeder

FROM docker.io/ubuntu:24.04@sha256:104ae83764a5119017b8e8d6218fa0832b09df65aae7d5a6de29a85d813da2fb

RUN apt-get update && \
    apt-get install -y --no-install-recommends ca-certificates && \
    rm -rf /var/lib/apt/lists/*

COPY --from=builder /go/bin/seid /go/bin/price-feeder /usr/bin/
COPY --from=seictl /usr/bin/seictl /usr/bin/
COPY --from=builder /go/lib/*.so /usr/lib/

ENTRYPOINT ["/usr/bin/seid"]