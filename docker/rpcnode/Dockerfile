FROM golang:1.25.6-bookworm AS go-dist

FROM ubuntu:24.04
ARG RUNTIME_ONLY=false
ENV HOME="/root" PATH="/root/go/bin:$PATH"
RUN set -eux; \
    apt-get update; \
    if [ "$RUNTIME_ONLY" = "true" ]; then \
      apt-get install -y --no-install-recommends jq curl ca-certificates; \
    else \
      apt-get install -y --no-install-recommends make git build-essential jq curl ca-certificates; \
    fi; \
    rm -rf /var/lib/apt/lists/*
COPY --from=go-dist /usr/local/go /usr/local/go
RUN mkdir -p /root/go/pkg/mod && \
    mkdir -p /root/.cache && \
    chmod -R a+rwX /root
SHELL ["/bin/bash", "-c"]

WORKDIR /sei-protocol/sei-chain

EXPOSE 26656 26657 26658 9090 9091

CMD ["/usr/bin/deploy.sh"]

STOPSIGNAL SIGTERM

COPY scripts/deploy.sh /usr/bin/deploy.sh
COPY scripts/step0_build.sh /usr/bin/build.sh
COPY scripts/step1_configure_init.sh /usr/bin/configure_init.sh
COPY scripts/step2_start_sei.sh /usr/bin/start_sei.sh
