#!/bin/bash

# Script to reproduce EVM->CW->EVM call pattern error
# This triggers the error at x/evm/keeper/evm.go:78
# Error: "sei does not support EVM->CW->EVM call pattern"
#
# CORRECT PATTERN: CW20->ERC20 Pointer
# 1. Deploy ERC20 (for CW20 to call back to)
# 2. Deploy CW20 that wraps the ERC20 (cwerc20.wasm)
# 3. Register CW20->ERC20 pointer
# 4. Call the ERC20 pointer's transfer() function
# 5. Pointer -> CW20 -> tries to call ERC20 -> ERROR!

set -e

# Get script directory and change to repo root
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$REPO_ROOT"

# Configuration
SEID="seid"
CHAIN_ID="sei-chain"
KEY_NAME="admin"
KEYRING="test"
GAS="5000000"

echo "================================================"
echo "  EVM->CW->EVM Call Pattern Error Reproducer"
echo "================================================"
echo ""
echo "Target: x/evm/keeper/evm.go:78"
echo "Error: 'sei does not support EVM->CW->EVM call pattern'"
echo "Pattern: ERC20 Pointer -> CW20 -> ERC20 (via DelegateCallEvm)"
echo ""

# Get admin address
ADMIN_ADDR=$($SEID keys show $KEY_NAME -a --keyring-backend $KEYRING)
echo "Admin: $ADMIN_ADDR"

# ============================================================
# STEP 1: Deploy ERC20 Token with Initial Supply (for CW20 to call back to)
# ============================================================
echo ""
echo "Step 1/4: Deploying ERC20 contract with initial supply..."

# Compile ERC20 with initial mint
solc --bin contracts/ERC20WithMint.sol -o /tmp --overwrite 2>&1 > /dev/null

DEPLOY_OUTPUT=$($SEID tx evm deploy /tmp/ERC20WithMint.bin \
    --from $KEY_NAME \
    --keyring-backend $KEYRING \
    --chain-id $CHAIN_ID \
    --gas-limit $GAS \
    --gas-prices 0.1usei \
    --broadcast-mode block \
    --yes \
    2>&1)

ERC20_ADDR=$(echo "$DEPLOY_OUTPUT" | grep "Deployed to:" | awk '{print $3}')

if [ -z "$ERC20_ADDR" ]; then
    echo "  Error: Failed to deploy ERC20 contract"
    echo "$DEPLOY_OUTPUT"
    exit 1
fi

echo "  ✓ ERC20 deployed at: $ERC20_ADDR"
echo "  ✓ Admin now has 1,000,000 tokens in ERC20"

# ============================================================
# STEP 2: Deploy CW20 Contract that wraps the ERC20
# ============================================================
echo ""
echo "Step 2/4: Deploying CW20 wrapper contract..."

WASM_FILE="example/cosmwasm/cw20/artifacts/cwerc20.wasm"

# Store contract
STORE_OUTPUT=$($SEID tx wasm store "$WASM_FILE" \
    --from $KEY_NAME \
    --chain-id $CHAIN_ID \
    --keyring-backend $KEYRING \
    --gas auto \
    --gas-adjustment 1.5 \
    --gas-prices 0.1usei \
    --broadcast-mode block \
    --yes \
    --output json 2>&1)

STORE_JSON=$(echo "$STORE_OUTPUT" | grep -A 9999 '^{' | head -1)
CODE_ID=$(echo "$STORE_JSON" | jq -r '.logs[0].events[] | select(.type=="store_code") | .attributes[] | select(.key=="code_id") | .value')
echo "  ✓ Code ID: $CODE_ID"

# Instantiate CW20 with the ERC20 address
INIT_MSG="{\"erc20_address\":\"${ERC20_ADDR}\"}"
INSTANTIATE_OUTPUT=$($SEID tx wasm instantiate "$CODE_ID" "$INIT_MSG" \
    --from $KEY_NAME \
    --label "cw20-erc20-wrapper" \
    --chain-id $CHAIN_ID \
    --keyring-backend $KEYRING \
    --gas auto \
    --gas-adjustment 1.5 \
    --gas-prices 0.1usei \
    --broadcast-mode block \
    --no-admin \
    --yes \
    --output json 2>&1)

INSTANTIATE_JSON=$(echo "$INSTANTIATE_OUTPUT" | grep -A 9999 '^{' | head -1)
CW20_ADDR=$(echo "$INSTANTIATE_JSON" | jq -r '.logs[0].events[] | select(.type=="instantiate") | .attributes[] | select(.key=="_contract_address") | .value')
echo "  ✓ CW20 Address: $CW20_ADDR"

# ============================================================
# STEP 3: Register CW20->ERC20 Pointer
# ============================================================
echo ""
echo "Step 3/4: Registering CW20->ERC20 pointer..."

# Register EVM pointer for the CW20 contract
# This calls the Pointer precompile to create an ERC20 contract that points to the CW20
REGISTER_OUTPUT=$($SEID tx evm register-evm-pointer CW20 "$CW20_ADDR" \
    --from $KEY_NAME \
    --keyring-backend $KEYRING \
    --chain-id $CHAIN_ID \
    --gas-limit 7000000 \
    --gas-fee-cap 1000000000000 \
    --evm-rpc http://127.0.0.1:8545 \
    2>&1)

echo "$REGISTER_OUTPUT"

# Extract transaction hash
TX_HASH=$(echo "$REGISTER_OUTPUT" | grep "Transaction hash:" | awk '{print $3}')

if [ -z "$TX_HASH" ]; then
    echo "  Error: Failed to register pointer"
    echo "$REGISTER_OUTPUT"
    exit 1
fi

echo "  ✓ Pointer registered, tx: $TX_HASH"

# Wait for the pointer to be created
echo "  Waiting for pointer to be indexed..."
sleep 3

# Retry loop to query the pointer
for i in {1..10}; do
    POINTER_ADDR=$($SEID q evm pointer CW20 "$CW20_ADDR" --output json 2>&1 | jq -r '.pointer // empty')
    if [ -n "$POINTER_ADDR" ] && [ "$POINTER_ADDR" != "null" ]; then
        break
    fi
    echo "  Retry $i/10..."
    sleep 1
done

if [ -z "$POINTER_ADDR" ] || [ "$POINTER_ADDR" == "null" ]; then
    echo "  Error: Failed to query pointer address after retries"
    $SEID q evm pointer CW20 "$CW20_ADDR" 2>&1
    exit 1
fi

echo "  ✓ ERC20 Pointer Address: $POINTER_ADDR"

# ============================================================
# STEP 4: Call Solo Precompile to Claim CW20 - Triggers EVM->CW->EVM
# ============================================================
echo ""
echo "Step 4/4: Claiming CW20 tokens via Solo precompile..."
echo ""
echo "  This will trigger: Solo Precompile -> CW20 transfer -> ERC20 (via DelegateCallEvm)"
echo "  Expected error: 'sei does not support EVM->CW->EVM call pattern'"
echo ""

# Get admin's EVM address (claimer)
ADMIN_EVM_ADDR=$($SEID q evm evm-addr "$ADMIN_ADDR" --output json 2>&1 | jq -r '.evm_address // empty')
if [ -z "$ADMIN_EVM_ADDR" ]; then
    echo "  Admin is not associated. Associating now..."
    $SEID tx evm associate-address --from $KEY_NAME --keyring-backend $KEYRING --chain-id $CHAIN_ID --gas-prices 0.1usei --broadcast-mode block --yes > /dev/null 2>&1
    sleep 2
    ADMIN_EVM_ADDR=$($SEID q evm evm-addr "$ADMIN_ADDR" --output json 2>&1 | jq -r '.evm_address')
fi

echo "  Admin EVM: $ADMIN_EVM_ADDR"
echo "  CW20: $CW20_ADDR"
echo ""

# Generate claim payload
CLAIM_PAYLOAD=$($SEID tx evm print-claim-specific "$ADMIN_EVM_ADDR" CW20 "$CW20_ADDR" \
    --from $KEY_NAME \
    --keyring-backend $KEYRING \
    --chain-id $CHAIN_ID \
    --yes 2>&1)

echo "  Generated claim payload"

# Encode claimSpecific(bytes) call
CALLDATA=$(cast calldata "claimSpecific(bytes)" "0x$CLAIM_PAYLOAD")
CALLDATA_NO_PREFIX="${CALLDATA#0x}"

echo "  Calling Solo precompile at 0x000000000000000000000000000000000000100C..."
echo ""

# Call Solo precompile
SOLO_ADDR="0x000000000000000000000000000000000000100C"
$SEID tx evm call-contract "$SOLO_ADDR" "$CALLDATA_NO_PREFIX" \
    --from $KEY_NAME \
    --keyring-backend $KEYRING \
    --chain-id $CHAIN_ID \
    --gas-limit $GAS \
    --gas-prices 0.1usei \
    --broadcast-mode block \
    --yes \
    2>&1 | tee /tmp/evm_cw_evm_error.log

echo ""
echo "================================================"
echo "  Transaction Complete"
echo "================================================"
echo ""
echo "Output saved to: /tmp/evm_cw_evm_error.log"
echo ""
echo "The error should have occurred at:"
echo "  File: x/evm/keeper/evm.go"
echo "  Line: 78"
echo "  Check: ctx.IsEVM() && !ctx.EVMEntryViaWasmdPrecompile()"
echo ""
