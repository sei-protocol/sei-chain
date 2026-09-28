#!/usr/bin/env bash
# Brings up a 4-validator Autobahn (giga/evmonly) devnet, plus one
# non-validator archival RPC fullnode, as plain `seid` processes on
# localhost. No Docker, no k8s. See BARE_METAL.md.
#
# Usage: ./integration_test/autobahn/bare_metal_up.sh (from anywhere; paths
# below are resolved relative to this script). SKIP_BUILD=1 skips `make
# build` on a repeat run against an unchanged binary.
set -euo pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
STATE="$REPO/build/generated/autobahn-bare-metal"
SEID="$REPO/build/seid"
N=4
CHAIN_ID=sei

sedi() { if [[ "$(uname)" == "Darwin" ]]; then sed -i '' "$@"; else sed -i "$@"; fi; }

for pidfile in "$STATE"/logs/*.pid; do
  [ -f "$pidfile" ] || continue
  if kill -0 "$(cat "$pidfile")" 2>/dev/null; then
    echo "already running (state at $STATE) — run bare_metal_down.sh first" >&2
    exit 1
  fi
done

cd "$REPO"
if [ "${SKIP_BUILD:-}" != "1" ]; then
  echo "== building seid =="
  make build
fi

echo "== initializing $N node homes under $STATE =="
rm -rf "$STATE"
mkdir -p "$STATE/logs"

for i in $(seq 0 $((N - 1))); do
  HOME_DIR="$STATE/node_$i"
  P2P_PORT=$((26656 + i * 10))
  EVMRPC_PORT=$((8545 + i * 10))

  "$SEID" init "sei-node-$i" --chain-id "$CHAIN_ID" --home "$HOME_DIR" >/dev/null 2>&1

  CFG="$HOME_DIR/config/config.toml"
  APP="$HOME_DIR/config/app.toml"

  # seid init defaults mode to "full"; every node here is a committee
  # validator, so it must run in validator mode to produce blocks.
  sedi -e 's/^mode = "full"/mode = "validator"/' "$CFG"

  # Each node listens on its own localhost port. No persistent-peers needed:
  # Giga dials committee peers directly from autobahn.json's address book,
  # independent of classic Tendermint P2P/PEX.
  sedi -e "s|^laddr = \"tcp://0.0.0.0:26656\"|laddr = \"tcp://127.0.0.1:${P2P_PORT}\"|" "$CFG"

  # Autobahn serves EVM JSON-RPC only: drop the surfaces it never serves.
  sedi -e '/^\[rpc\]/,/^\[/ s|^laddr = .*|laddr = ""|' "$CFG"
  sedi -e '/^\[api\]/,/^\[/ s/^enable = .*/enable = false/' "$APP"
  sedi -e '/^\[grpc\]/,/^\[/ s/^enable = .*/enable = false/' "$APP"
  sedi -e '/^\[grpc-web\]/,/^\[/ s/^enable = .*/enable = false/' "$APP"

  # The port the EVM-only RPC actually listens on, distinct per node.
  sedi -e "/^\[giga.execution\]/,/^\[/ s|^evm_rpc_port = .*|evm_rpc_port = ${EVMRPC_PORT}|" "$APP"

  echo "127.0.0.1:${P2P_PORT}" >"$HOME_DIR/config/autobahn_address.txt"
  echo "http://127.0.0.1:${EVMRPC_PORT}" >"$HOME_DIR/config/evmrpc_url.txt"
done

echo "== generating the shared committee config =="
"$SEID" tendermint gen-autobahn-config \
  "$STATE"/node_0/config "$STATE"/node_1/config "$STATE"/node_2/config "$STATE"/node_3/config \
  --output "$STATE/autobahn.json" \
  --persistent-state-dir data/autobahn \
  --blockdb-retention 24h
sedi -e 's/"dial_interval": "10s"/"dial_interval": "1s"/' "$STATE/autobahn.json"

for i in $(seq 0 $((N - 1))); do
  sedi -e 's|^autobahn-config-file = ""|autobahn-config-file = "'"$STATE/autobahn.json"'"|' \
    "$STATE/node_$i/config/config.toml"
done

echo "== initializing the archival RPC fullnode (rpc_0) =="
RPC_HOME="$STATE/rpc_0"
RPC_P2P_PORT=26696
RPC_EVMRPC_PORT=8585

"$SEID" init "sei-rpc-0" --chain-id "$CHAIN_ID" --home "$RPC_HOME" >/dev/null 2>&1

RPC_CFG="$RPC_HOME/config/config.toml"
RPC_APP="$RPC_HOME/config/app.toml"

# seid init already defaults mode to "full" — no flip needed, this is the
# one node in the topology that must NOT be a validator.
sedi -e "s|^laddr = \"tcp://0.0.0.0:26656\"|laddr = \"tcp://127.0.0.1:${RPC_P2P_PORT}\"|" "$RPC_CFG"
sedi -e '/^\[rpc\]/,/^\[/ s|^laddr = .*|laddr = ""|' "$RPC_CFG"
sedi -e '/^\[api\]/,/^\[/ s/^enable = .*/enable = false/' "$RPC_APP"
sedi -e '/^\[grpc\]/,/^\[/ s/^enable = .*/enable = false/' "$RPC_APP"
sedi -e '/^\[grpc-web\]/,/^\[/ s/^enable = .*/enable = false/' "$RPC_APP"
sedi -e "/^\[giga.execution\]/,/^\[/ s|^evm_rpc_port = .*|evm_rpc_port = ${RPC_EVMRPC_PORT}|" "$RPC_APP"
# Archival: [giga.storage] mode is left "" (auto), which follows this node's
# own "full" mode and opens the state store (SS) for historical queries.
# lookback_window = -1 keeps that history forever instead of the default
# 1000-block rollback window.
sedi -e '/^\[giga.storage\]/,/^\[/ s|^lookback_window = .*|lookback_window = -1|' "$RPC_APP"
sedi -e 's|^autobahn-config-file = ""|autobahn-config-file = "'"$STATE/autobahn.json"'"|' "$RPC_CFG"

echo "== starting $N validators + 1 archival RPC fullnode =="
for i in $(seq 0 $((N - 1))); do
  "$SEID" start --chain-id "$CHAIN_ID" --home "$STATE/node_$i" \
    >"$STATE/logs/node$i.log" 2>&1 &
  echo $! >"$STATE/logs/node$i.pid"
done
"$SEID" start --chain-id "$CHAIN_ID" --home "$RPC_HOME" \
  >"$STATE/logs/rpc0.log" 2>&1 &
echo $! >"$STATE/logs/rpc0.pid"

echo "== waiting for the RPCs to come up =="
wait_for_rpc() {
  local label="$1" port="$2" logfile="$3"
  for _ in $(seq 1 30); do
    if curl -s -o /dev/null -X POST -H 'content-type: application/json' \
      -d '{"jsonrpc":"2.0","id":1,"method":"eth_chainId","params":[]}' \
      "http://127.0.0.1:$port"; then
      return 0
    fi
    sleep 1
  done
  echo "$label's RPC never came up; see $logfile" >&2
  exit 1
}
for i in $(seq 0 $((N - 1))); do
  wait_for_rpc "node_$i" "$((8545 + i * 10))" "$STATE/logs/node$i.log"
done
wait_for_rpc "rpc_0" "$RPC_EVMRPC_PORT" "$STATE/logs/rpc0.log"

echo
echo "$N validators running:"
for i in $(seq 0 $((N - 1))); do
  echo "  node_$i: http://127.0.0.1:$((8545 + i * 10))  (log: $STATE/logs/node$i.log)"
done
echo
echo "1 archival RPC fullnode (non-validator, SS enabled) running:"
echo "  rpc_0:  http://127.0.0.1:$RPC_EVMRPC_PORT  (log: $STATE/logs/rpc0.log)"
echo
echo "Try it (against a validator; rpc_0 will catch up within a second or two):"
echo "  cast send --rpc-url http://127.0.0.1:8545 --private-key \$(cast wallet new --json | jq -r '.data[0].private_key') \\"
echo "    --legacy --gas-price 1gwei --value 1ether 0x000000000000000000000000000000000000dEaD"
echo
echo "Stop with: ./integration_test/autobahn/bare_metal_down.sh"
