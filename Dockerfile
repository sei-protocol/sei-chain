FROM docker.io/golang:1.24-bookworm@sha256:fc58bb98c4b7ebc8211c94df9dee40489e48363c69071bceca91aa59023b0dee AS builder
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

# Cache Go modules
COPY go.work go.work.sum ./
COPY go.mod go.sum ./
COPY sei-wasmvm/go.mod sei-wasmvm/go.sum ./sei-wasmvm/
COPY sei-wasmd/go.mod sei-wasmd/go.sum ./sei-wasmd/
COPY sei-cosmos/go.mod sei-cosmos/go.sum ./sei-cosmos/
COPY sei-tendermint/go.mod sei-tendermint/go.sum ./sei-tendermint/
COPY sei-ibc-go/go.mod sei-ibc-go/go.sum ./sei-ibc-go/
COPY sei-db/go.mod sei-db/go.sum ./sei-db/
RUN go mod download

COPY . .
ENV CGO_ENABLED=1
ARG SEI_CHAIN_REF=""
ARG GO_BUILD_TAGS=""
ARG GO_BUILD_ARGS=""
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    BUILD_TAGS="netgo ledger ${GO_BUILD_TAGS}" && \
    VERSION_PKG="github.com/cosmos/cosmos-sdk/version" && \
    LDFLAGS="\
      -X ${VERSION_PKG}.Name=sei \
      -X ${VERSION_PKG}.AppName=seid \
      -X ${VERSION_PKG}.Version=$(git describe --tags || echo "${SEI_CHAIN_REF}") \
      -X ${VERSION_PKG}.Commit=$(git log -1 --format='%H') \
      -X '${VERSION_PKG}.BuildTags=${BUILD_TAGS}'" && \
    go build -tags "${BUILD_TAGS}" -ldflags "${LDFLAGS}" ${GO_BUILD_ARGS} -o /go/bin/seid ./cmd/seid && \
    go build -tags "${BUILD_TAGS}" -ldflags "${LDFLAGS}" ${GO_BUILD_ARGS} -o /go/bin/price-feeder ./oracle/price-feeder

FROM docker.io/ubuntu:24.04@sha256:104ae83764a5119017b8e8d6218fa0832b09df65aae7d5a6de29a85d813da2fb

COPY --from=builder /go/bin/seid /go/bin/price-feeder /usr/bin/
COPY --from=builder /go/lib/*.so /usr/lib/

ENTRYPOINT ["/usr/bin/seid"]