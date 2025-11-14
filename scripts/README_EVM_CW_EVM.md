# EVM->CW->EVM Call Pattern Error Reproduction

This directory contains a script to reproduce the `EVM->CW->EVM call pattern` error.

## Error Location
- **File**: `x/evm/keeper/evm.go`
- **Line**: 78
- **Error**: "sei does not support EVM->CW->EVM call pattern"

## The Pattern

The error occurs when:
1. An EVM contract calls a CosmWasm contract via the Wasmd precompile (EVM -> CW)
2. The CosmWasm contract attempts to call back to an EVM contract (CW -> EVM)
3. This creates an EVM->CW->EVM call pattern which is not supported

## The Correct Setup: CW20->ERC20 Pointer

To trigger this error, you need a **CW20->ERC20 pointer** (not ERC20->CW20). Here's why:

### Pointer Direction Matters
- **CW20->ERC20 Pointer**: An ERC20 contract that points to a CW20 contract
  - Calls from EVM to this pointer use `delegatecall` to the Wasmd precompile
  - If the CW20 tries to call back to EVM → **triggers the error**

- **ERC20->CW20 Pointer**: A CW20 contract that points to an ERC20 contract
  - This pattern is different and doesn't trigger the same error

## How It Works

The script `reproduce_evm_cw_evm_error.sh` does the following:

### Step 1: Deploy ERC20 Token
Deploys an ERC20 token that the CW20 contract will call back to

### Step 2: Deploy CW20 Wrapper Contract
Deploys the CW20 wrapper contract (`example/cosmwasm/cw20/artifacts/cwerc20.wasm`) which wraps the ERC20 token.
This CW20 uses `DelegateCallEvm` to interact with the ERC20 contract.

### Step 3: Register CW20->ERC20 Pointer
Creates an ERC20 pointer contract for the CW20 using `seid tx evm register-pointer ERC20 <cw20-address>`.
This deploys a `CW20ERC20Pointer.sol` contract that delegates to the CW20.

### Step 4: Call the Pointer's transfer() Function
Calls `transfer()` on the ERC20 pointer from an EVM address.
This triggers the flow:
1. **EVM**: transfer() called on ERC20 pointer
2. **Pointer → CW**: delegatecall to Wasmd precompile → executes CW20's transfer
3. **CW → EVM**: CW20 tries to call ERC20 via `DelegateCallEvm`
4. **ERROR**: "sei does not support EVM->CW->EVM call pattern"

## Usage

```bash
# Ensure local chain is running
./build/seid start

# In another terminal, run the script
./scripts/reproduce_evm_cw_evm_error.sh
```

## Debugging

To debug this with a breakpoint:

### Using Delve (Go debugger)
```bash
# Start chain with debugger
dlv exec ./build/seid -- start

# Set breakpoint
(dlv) break x/evm/keeper/evm.go:79

# Continue
(dlv) continue

# In another terminal, run the script
./scripts/reproduce_evm_cw_evm_error.sh
```

### Using VS Code / GoLand
1. Set a breakpoint at `x/evm/keeper/evm.go:79`
2. Start debugging session with `seid start`
3. Run the script in another terminal
4. Debugger will pause at the breakpoint

## The Check

The error is triggered by this condition in `x/evm/keeper/evm.go:78`:

```go
if ctx.IsEVM() && !ctx.EVMEntryViaWasmdPrecompile() {
    return nil, errors.New("sei does not support EVM->CW->EVM call pattern")
}
```

### Context State
When the error occurs at `x/evm/keeper/evm.go:78`:
- `ctx.IsEVM()` = `true` (we're in an EVM transaction context)
- `ctx.EVMEntryViaWasmdPrecompile()` = `false` (was reset when CW->EVM call started)

## Related Code

- **Pointer Registration**: `x/evm/keeper/msg_server.go:268-345` - Registers CW20->ERC20 pointers
- **Pointer Contract**: `contracts/src/CW20ERC20Pointer.sol` - ERC20 contract that delegates to CW20
- **Wasmd Precompile**: `precompiles/wasmd/wasmd.go:354-361` - Handles delegatecall from pointers
- **Error Check**: `x/evm/keeper/evm.go:78-79` - Detects and blocks EVM->CW->EVM pattern
- **CW20 Contract**: `example/cosmwasm/cw20/src/contract.rs` - Uses `DelegateCallEvm` to call ERC20

## Why This Error Exists

The EVM->CW->EVM pattern is blocked because:
1. It creates complex reentrancy scenarios
2. Gas accounting becomes difficult across context switches
3. State management is complicated when bouncing between EVM and CW

The allowed pattern is: **EVM -> CW** (one-way via Wasmd precompile)

## Cleaning Up

The script creates:
- An ERC20 contract
- A CW20 wrapper contract
- An ERC20 pointer contract (CW20->ERC20)
- Transaction log at `/tmp/evm_cw_evm_error.log`

To clean up, reset your local chain.
