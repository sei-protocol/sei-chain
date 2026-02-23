# Base image must exist as sei-chain/localnode-base:go1.25.6 (build or pull+tag first).
FROM sei-chain/localnode-base:go1.25.6
ENV PATH="/root/go/bin:/sei-protocol/sei-chain/integration_test/upgrade_module/scripts/:$PATH"
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
COPY scripts/step6_start_price_feeder.sh /usr/bin/start_price_feeder.sh
