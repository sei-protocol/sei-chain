# EVM Event Emission for Precompiles

## Overview

This implementation adds direct EVM log emission capability to Sei precompiles, starting with the staking precompile. Previously, precompiles only emitted events through the Cosmos SDK event system, making them invisible to EVM tools and indexers. With this implementation, precompile operations now emit standard EVM events that can be queried via Ethereum JSON-RPC methods.

## Implementation Details

### Architecture

The implementation follows the **Direct EVM Log Emission** approach, which:

- Emits EVM logs directly during precompile execution
- Makes events available on standard `eth_*` endpoints
- Provides better performance with no conversion overhead
- Ensures compatibility with Ethereum tooling

### Key Components

1. **Event Helper Functions** (`precompiles/common/evm_events.go`)
   - Generic `EmitEVMLog` function for emitting EVM logs from any precompile
   - Specific helper functions for each event type (Delegate, Redelegate, etc.)
   - Proper ABI encoding of event data

2. **Updated Precompile Implementation** (`precompiles/staking/staking.go`)
   - Each method now emits corresponding EVM events
   - Events are emitted after successful operation execution
   - Errors in event emission are logged but don't fail the transaction

3. **Solidity Interface Updates** (`precompiles/staking/Staking.sol`)
   - Event definitions added to the interface
   - Developers can now see what events to expect

## Staking Precompile Events

### Delegate Event

```solidity
event Delegate(address indexed delegator, string validator, uint256 amount);
```

Emitted when tokens are delegated to a validator.

### Redelegate Event

```solidity
event Redelegate(address indexed delegator, string srcValidator, string dstValidator, uint256 amount);
```

Emitted when tokens are redelegated from one validator to another.

### Undelegate Event

```solidity
event Undelegate(address indexed delegator, string validator, uint256 amount);
```

Emitted when tokens are undelegated from a validator.

### ValidatorCreated Event

```solidity
event ValidatorCreated(address indexed creator, string validatorAddress, string moniker);
```

Emitted when a new validator is created.

### ValidatorEdited Event

```solidity
event ValidatorEdited(address indexed editor, string validatorAddress, string moniker);
```

Emitted when a validator's parameters are edited.

## Usage Example

```javascript
// Web3.js example
const stakingABI = [...]; // Include the IStaking interface ABI
const stakingAddress = "0x0000000000000000000000000000000000001005";
const stakingContract = new web3.eth.Contract(stakingABI, stakingAddress);

// Listen for Delegate events
stakingContract.events.Delegate({
    filter: {delegator: userAddress},
    fromBlock: 'latest'
})
.on('data', function(event){
    console.log(`Delegated ${event.returnValues.amount} to ${event.returnValues.validator}`);
})
.on('error', console.error);

// Query past events
const events = await stakingContract.getPastEvents('Delegate', {
    filter: {delegator: userAddress},
    fromBlock: 0,
    toBlock: 'latest'
});
```

## Testing

The implementation includes comprehensive tests (`precompiles/staking/staking_events_test.go`) that verify:

- Events are emitted with correct signatures
- Event data is properly ABI-encoded
- Indexed parameters are correctly set
- All event types work as expected

To run the tests:

```bash
go test ./precompiles/staking -run TestStakingPrecompileEventsEmission -v
```

## Future Work

1. **Extend to Other Precompiles**: Apply the same pattern to other precompiles (bank, distribution, gov, etc.)
2. **Event Filtering**: Add support for more sophisticated event filtering
3. **Gas Optimization**: Optimize event emission gas costs
4. **Backward Compatibility**: Maintain Cosmos events during transition period

## Migration Guide

For developers currently using Cosmos SDK events:

1. **Update Event Listeners**: Switch from Cosmos event queries to EVM event filters
2. **Update Indexers**: Configure indexers to listen for EVM events instead
3. **Use Standard Tools**: Leverage existing Ethereum tooling (ethers.js, web3.js, etc.)

## Benefits

- **EVM Compatibility**: Events are queryable via standard Ethereum JSON-RPC
- **Tool Support**: Works with existing Ethereum indexers and analytics platforms
- **Better Performance**: No conversion overhead between event systems
- **Future-Proof**: Aligned with the plan to phase out Cosmos-side events
