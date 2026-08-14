# Sei's EVM RPC

Sei provides the standard [Ethereum JSON-RPC API](https://ethereum.org/en/developers/docs/apis/json-rpc/)
alongside a small set of legacy Sei-specific methods.

## Ethereum endpoints

The `eth_*` methods provide the EVM-compatible view of the chain and are the
supported replacements for removed legacy methods. They process EVM transactions,
ignore Cosmos-native transactions, and preserve compatibility with Ethereum tools.

## Legacy Sei endpoints

The remaining `sei_*` methods are:

- `sei_getSeiAddress`
- `sei_getEVMAddress`
- `sei_getCosmosTx`
- `sei_getTransactionReceipt`

These methods are available only on EVM HTTP and are gated by
`[evm].enabled_legacy_sei_apis`. The address and Cosmos transaction helpers are
enabled by default. The receipt method includes synthetic receipts and must be
enabled explicitly.
