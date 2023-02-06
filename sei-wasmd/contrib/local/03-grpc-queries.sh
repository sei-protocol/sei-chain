#!/bin/bash
set -o errexit -o nounset -o pipefail

DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" >/dev/null 2>&1 && pwd)"

echo "-----------------------"
PROTO_THRD="$DIR/../../third_party/proto"
PROTO_WASMD="$DIR/../../proto"
PROTO_WASMD_QUERY="$PROTO_WASMD/cosmwasm/wasm/v1/query.proto"

echo "### List all codes"
RESP=$(grpcurl -plaintext -import-path "$PROTO_THRD" -import-path "$PROTO_WASMD" -proto "$PROTO_WASMD_QUERY" \
  localhost:9090 cosmwasm.wasm.v1.Query/Codes)
echo "$RESP" | jq

CODE_ID=$(echo "$RESP" | jq -r '.codeInfos[-1].codeId')
echo "### List contracts by code"
RESP=$(grpcurl -plaintext -import-path "$PROTO_THRD" -import-path "$PROTO_WASMD" -proto "$PROTO_WASMD_QUERY" \
  -d "{\"codeId\": $CODE_ID}" localhost:9090 cosmwasm.wasm.v1.Query/ContractsByCode)
echo "$RESP" | jq

echo "### Show history for contract"
CONTRACT=$(echo "$RESP" | jq -r ".contracts[-1]")
grpcurl -plaintext -import-path "$PROTO_THRD" -import-path "$PROTO_WASMD" -proto "$PROTO_WASMD_QUERY" \
  -d "{\"address\": \"$CONTRACT\"}" localhost:9090 cosmwasm.wasm.v1.Query/ContractHistory | jq

echo "### Show contract state"
grpcurl -plaintext -import-path "$PROTO_THRD" -import-path "$PROTO_WASMD" -proto "$PROTO_WASMD_QUERY" \
  -d "{\"address\": \"$CONTRACT\"}" localhost:9090 cosmwasm.wasm.v1.Query/AllContractState | jq

echo "Empty state due to 'burner' contract cleanup"
