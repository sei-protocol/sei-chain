# ---------- Builder ----------
FROM docker.io/golang:1.24.5 AS go-builder
WORKDIR /app/sei-chain

RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates wget && \
    rm -rf /var/lib/apt/lists/*

# Cache Go modules
COPY go.mod go.sum ./
COPY sei-wasmvm/go.mod sei-wasmvm/go.sum ./sei-wasmvm/
COPY sei-wasmd/go.mod sei-wasmd/go.sum ./sei-wasmd/
RUN go mod download

# Copy source and build (CGO enabled for libwasmvm)
COPY . .
ENV CGO_ENABLED=1
RUN make build

# Collect libwasmvm*.so: try ./seiwasmd; else auto-derive version and download glibc .so
ARG WASMVM_VERSION=""
RUN set -eux; \
    mkdir -p /build/deps; \
    LIBWASM_DIR="./sei-wasmd"; \
    found=0; \
    # Copy from module cache if present
    FILES="$(find "$LIBWASM_DIR" -type f -name 'libwasmvm*.so' -print || true)"; \
    if [ -n "$FILES" ]; then \
        echo "$FILES" | xargs -r -n1 -I{} cp -v "{}" /build/deps/; \
        found=1; \
    fi; \
    # If not found, derive version (or use provided WASMVM_VERSION) and download
    if [ "$found" -eq 0 ]; then \
        if [ -z "$WASMVM_VERSION" ]; then \
            WASMVM_VERSION="$(go list -f '{{if .Replace}}{{.Replace.Version}}{{else}}{{.Version}}{{end}}' -m github.com/CosmWasm/wasmvm 2>/dev/null | sed 's/^v//' | sed 's/-.*//')"; \
            [ -n "$WASMVM_VERSION" ] || { echo "wasmvm version not found in go.mod; set --build-arg WASMVM_VERSION=vX.Y.Z"; exit 1; }; \
            WASMVM_VERSION="v${WASMVM_VERSION}"; \
        fi; \
        case "${TARGETARCH:-$(go env GOARCH)}" in \
            amd64) ARCH=x86_64 ;; \
            arm64) ARCH=aarch64 ;; \
            *) echo "unsupported arch: ${TARGETARCH:-$(go env GOARCH)}"; exit 1 ;; \
        esac; \
        wget -O /build/deps/libwasmvm.${ARCH}.so \
            "https://github.com/CosmWasm/wasmvm/releases/download/${WASMVM_VERSION}/libwasmvm.${ARCH}.so"; \
        found=1; \
    fi; \
    ls -l /build/deps

# ---------- Runtime ----------
FROM ubuntu:24.04
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates && \
    rm -rf /var/lib/apt/lists/*

COPY --from=go-builder /app/sei-chain/build/seid /bin/seid
COPY --from=go-builder /build/deps/libwasmvm*.so /usr/lib/

# Ensure generic symlink exists and refresh linker cache
RUN bash -lc '\
    set -eux; \
    arch=$(uname -m); case "$arch" in x86_64|amd64) a=x86_64 ;; aarch64|arm64) a=aarch64 ;; *) a="" ;; esac; \
    if [ -n "$a" ] && [ -f "/usr/lib/libwasmvm.${a}.so" ]; then ln -sf "/usr/lib/libwasmvm.${a}.so" /usr/lib/libwasmvm.so; fi; \
    ldconfig'

EXPOSE 1317 26656 26657 8545 9090
ENTRYPOINT ["/bin/seid"]