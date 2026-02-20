# Build with context = repo root so we can COPY wasm libs. Use: docker build -f docker/rpcnode/Dockerfile .
FROM ubuntu:25.04
ENV HOME="/root" PATH="/root/go/bin:$PATH"
# Prebuilt seid loads libwasmvm*.so from image (no mount required)
ENV LD_LIBRARY_PATH="/opt/sei-wasmvm/lib"
# BuildKit cache mounts speed up repeated builds (require DOCKER_BUILDKIT=1)
RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
    --mount=type=cache,target=/var/lib/apt/lists,sharing=locked \
    apt-get update && apt-get install -y make git build-essential jq python3 curl vim uuid-runtime
RUN curl -L https://go.dev/dl/go1.25.6.linux-amd64.tar.gz | tar xvzf - -C /usr/local/
RUN mkdir -p /root/go/pkg/mod && \
    mkdir -p /root/.cache && \
    chmod -R a+rwX /root
SHELL ["/bin/bash", "-c"]

WORKDIR /sei-protocol/sei-chain

EXPOSE 26656 26657 26658 9090 9091

CMD ["/usr/bin/deploy.sh"]

STOPSIGNAL SIGTERM

# Wasm VM libs baked into image so binary works without repo mount for libs
RUN mkdir -p /opt/sei-wasmvm/lib
COPY sei-wasmvm/internal/api/libwasmvm.x86_64.so /opt/sei-wasmvm/lib/
COPY sei-wasmd/x/wasm/artifacts/v152/api/libwasmvm152.x86_64.so /opt/sei-wasmvm/lib/
COPY sei-wasmd/x/wasm/artifacts/v155/api/libwasmvm155.x86_64.so /opt/sei-wasmvm/lib/

COPY docker/rpcnode/scripts/deploy.sh /usr/bin/deploy.sh
COPY docker/rpcnode/scripts/step0_build.sh /usr/bin/build.sh
COPY docker/rpcnode/scripts/step1_configure_init.sh /usr/bin/configure_init.sh
COPY docker/rpcnode/scripts/step2_start_sei.sh /usr/bin/start_sei.sh
