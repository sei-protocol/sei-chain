#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 5 ]]; then
  echo "usage: $0 FINAL_HOME NODE0_IP NODE1_IP NODE2_IP NODE3_IP" >&2
  exit 2
fi

final_home=$1
shift
private_ips=("$@")
repo_dir=$(git rev-parse --show-toplevel)
cd "$repo_dir"

generated_dir=$repo_dir/build/generated
native_dir=$repo_dir/build/autobahn-native
homes_dir=$native_dir/homes
rm -rf "$generated_dir" "$native_dir"
mkdir -p "$generated_dir" "$homes_dir"

for node_index in 0 1 2 3; do
  node_home=$homes_dir/node-$node_index
  mkdir -p "$node_home/go/bin"
  HOME=$node_home \
    GOBIN=$node_home/go/bin \
    PATH="$node_home/go/bin:$repo_dir/build:/usr/local/go/bin:$PATH" \
    ID=$node_index \
    NUM_ACCOUNTS=0 \
    VALIDATOR=true \
    docker/localnode/scripts/step1_configure_init.sh
done

: > "$generated_dir/persistent_peers.txt"
for node_index in 0 1 2 3; do
  node_home=$homes_dir/node-$node_index
  seid=$node_home/go/bin/seid
  node_id=$(HOME=$node_home "$seid" tendermint show-node-id)
  printf '%s@%s:26656\n' "$node_id" "${private_ips[$node_index]}" >> "$generated_dir/persistent_peers.txt"
  printf '%s:26656\n' "${private_ips[$node_index]}" > "$generated_dir/node_$node_index/autobahn_address.txt"
  printf 'http://%s:8545\n' "${private_ips[$node_index]}" > "$generated_dir/node_$node_index/evmrpc_url.txt"
done

node_zero_home=$homes_dir/node-0
HOME=$node_zero_home \
  PATH="$node_zero_home/go/bin:/usr/local/go/bin:$PATH" \
  ADD_VALIDATOR_SCRIPT=$repo_dir/docker/localnode/scripts/step3_add_validator_to_genesis.sh \
  docker/localnode/scripts/step2_genesis.sh

for node_index in 0 1 2 3; do
  node_home=$homes_dir/node-$node_index
  HOME=$node_home \
    PATH="$node_home/go/bin:/usr/local/go/bin:$PATH" \
    ID=$node_index \
    CLUSTER_SIZE=4 \
    NODE_IP=${private_ips[$node_index]} \
    AUTOBAHN=true \
    AUTOBAHN_EVMONLY_IN_MEMORY=true \
    GIGA_EXECUTOR=true \
    GIGA_OCC=true \
    docker/localnode/scripts/step4_config_override.sh

  sed -i \
    -e "s|^autobahn-config-file = .*|autobahn-config-file = \"$final_home/.sei/config/autobahn.json\"|" \
    "$node_home/.sei/config/config.toml"
  sed -i \
    -e "s|^snapshot-directory = .*|snapshot-directory = \"$final_home/.sei/data/snapshots\"|" \
    "$node_home/.sei/config/app.toml"
  mkdir -p "$node_home/.sei/data"
  printf '{"height":"0","round":0,"step":0}\n' > "$node_home/.sei/data/priv_validator_state.json"
  tar -C "$node_home" -czf "$native_dir/node-$node_index.tgz" .sei
done

touch "$native_dir/configuration.ready"
