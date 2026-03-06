FROM ubuntu:latest
ENV HOME="/root" PATH="/root/go/bin:/sei-protocol/sei-chain/integration_test/upgrade_module/scripts/:$PATH"
RUN apt-get update && apt-get install -y make build-essential git jq python3 curl vim uuid-runtime
RUN set -eux; \
    GO_TARBALL="go1.25.6.linux-amd64.tar.gz"; \
    GO_URL="https://go.dev/dl/${GO_TARBALL}"; \
    GO_TARBALL_PATH="/tmp/${GO_TARBALL}"; \
    for attempt in 1 2 3 4 5; do \
      curl -fSL --retry 5 --retry-delay 2 --retry-all-errors "${GO_URL}" -o "${GO_TARBALL_PATH}"; \
      if tar -tzf "${GO_TARBALL_PATH}" > /dev/null 2>&1; then \
        break; \
      fi; \
      rm -f "${GO_TARBALL_PATH}"; \
      if [ "${attempt}" -eq 5 ]; then \
        echo "failed to download a valid Go tarball after retries"; \
        exit 1; \
      fi; \
      sleep 2; \
    done; \
    tar -xzf "${GO_TARBALL_PATH}" -C /usr/local/; \
    rm -f "${GO_TARBALL_PATH}"
RUN curl -L https://foundry.paradigm.xyz | bash
RUN curl -sL https://deb.nodesource.com/setup_16.x | bash
RUN /root/.foundry/bin/foundryup
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

