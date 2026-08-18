# Sei's EVM RPC

Sei provides the standard [Ethereum JSON-RPC API](https://ethereum.org/en/developers/docs/apis/json-rpc/)
alongside a small set of legacy Sei-specific methods.

## Ethereum endpoints

The `eth_*` methods provide the EVM-compatible view of the chain. They process EVM
transactions, ignore Cosmos-native transactions, and preserve compatibility with
Ethereum tools. They replace removed legacy methods only where the underlying data
is EVM-originated.

## Legacy Sei endpoints

The remaining `sei_*` methods are:

- `sei_getSeiAddress`
- `sei_getEVMAddress`
- `sei_getCosmosTx`

These methods are available only on EVM HTTP and are gated by
`[evm].enabled_legacy_sei_apis`. The address and Cosmos transaction helpers are
enabled by default.

There is no remaining JSON-RPC method for discovering synthetic logs from
Cosmos-originated transactions.
