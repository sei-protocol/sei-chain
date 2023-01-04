FROM ubuntu:latest
RUN apt-get update && \
    apt-get install -y make git golang jq python3
RUN rm -rf build/generated

VOLUME [ "/sei-protocol" ]
VOLUME [ "/root/go/pkg/mod" ]
WORKDIR /sei-protocol/sei-chain

EXPOSE 26656 26657 26658

CMD ["/usr/bin/deploy.sh"]

STOPSIGNAL SIGTERM

COPY scripts/deploy.sh /usr/bin/deploy.sh
COPY scripts/step0_build.sh /usr/bin/build.sh
COPY scripts/step1_configure_init.sh /usr/bin/configure_init.sh
COPY scripts/step2_genesis.sh /usr/bin/genesis.sh
COPY scripts/step3_add_validator_to_genesis.sh /usr/bin/add_validator_to_gensis.sh
COPY scripts/step4_persistent_peers.sh /usr/bin/persistent_peers.sh
COPY scripts/step5_start_sei.sh /usr/bin/start_sei.sh
