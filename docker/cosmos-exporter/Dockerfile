# Build from the repository root:
#   docker build -f docker/cosmos-exporter/Dockerfile .
FROM docker.io/golang:1.27.1-bookworm@sha256:648f440f42a0958804efb24df176f806f9d353b41f1c0627f666428e40310f6b AS builder
WORKDIR /go/src/sei-chain

COPY go.* ./
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go mod download

COPY . .

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /go/bin/sei-cosmos-exporter ./cmd/sei-cosmos-exporter

# Provides /bin/bash for the wait-then-exec wrapper in the pod spec.
# UID/GID 65532 matches sei-k8s-controller's sidecarSecurityContext.
FROM docker.io/ubuntu:24.04@sha256:104ae83764a5119017b8e8d6218fa0832b09df65aae7d5a6de29a85d813da2fb

RUN apt-get update && \
    apt-get install -y --no-install-recommends ca-certificates && \
    rm -rf /var/lib/apt/lists/* && \
    groupadd --system --gid 65532 nonroot && \
    useradd --system --uid 65532 --gid 65532 --shell /sbin/nologin --no-create-home nonroot

COPY --from=builder /go/bin/sei-cosmos-exporter /usr/local/bin/sei-cosmos-exporter

EXPOSE 9300

USER nonroot:nonroot

ENTRYPOINT ["/usr/local/bin/sei-cosmos-exporter"]
CMD ["--denom", "usei", \
     "--denom-coefficient", "1000000", \
     "--bech-prefix", "sei", \
     "--listen-address", ":9300"]
