FROM golang:1.25.6-bookworm AS go-dist

FROM ubuntu:24.04
ENV HOME="/root" PATH="/root/go/bin:/sei-protocol/sei-chain/integration_test/upgrade_module/scripts/:$PATH"
RUN set -eux; \
    export DEBIAN_FRONTEND=noninteractive; \
    for attempt in 1 2 3 4 5; do \
      if apt-get -o Acquire::Retries=5 -o APT::Update::Error-Mode=any update && \
         apt-get -o Acquire::Retries=5 install -y --no-install-recommends make build-essential git jq python3 curl ca-certificates vim uuid-runtime; then \
        update-ca-certificates; \
        rm -rf /var/lib/apt/lists/*; \
        break; \
      fi; \
      rm -rf /var/lib/apt/lists/*; \
      if [ "${attempt}" = "5" ]; then \
        exit 1; \
      fi; \
      sleep $((attempt * 10)); \
    done
COPY --from=go-dist /usr/local/go /usr/local/go
RUN set -eux; \
    for attempt in 1 2 3 4 5; do \
      if curl -fsSL https://foundry.paradigm.xyz -o /tmp/foundry.sh && \
         bash /tmp/foundry.sh && \
         /root/.foundry/bin/foundryup; then \
        rm -f /tmp/foundry.sh; \
        break; \
      fi; \
      rm -f /tmp/foundry.sh; \
      if [ "${attempt}" -eq 5 ]; then \
        echo "foundry install failed after retries"; \
        exit 1; \
      fi; \
      sleep 2; \
    done
RUN curl -sL https://deb.nodesource.com/setup_16.x | bash
RUN mkdir -p /root/go/pkg/mod && \
    mkdir -p /root/.cache && \
    chmod -R a+rwX /root
SHELL ["/bin/bash", "-c"]

WORKDIR /sei-protocol/sei-chain

EXPOSE 26656 26657 26658 9090 9091 7171

CMD ["/usr/bin/deploy.sh"]

STOPSIGNAL SIGTERM

COPY scripts/deploy.sh /usr/bin/deploy.sh
COPY scripts/step0_build.sh /usr/bin/build.sh
COPY scripts/step1_configure_init.sh /usr/bin/configure_init.sh
COPY scripts/step2_genesis.sh /usr/bin/genesis.sh
COPY scripts/step3_add_validator_to_genesis.sh /usr/bin/add_validator_to_gensis.sh
COPY scripts/step4_config_override.sh /usr/bin/config_override.sh
COPY scripts/step5_start_sei.sh /usr/bin/start_sei.sh
