## RPC Test Overview
An RPC test under `evmrpc/tests` involves generating the chain state as if the chain started from a specified genesis state and processed a number of specified blocks. This is encapsulated in one `SetupTestServer` call. Specifically, the first argument to `SetupTestServer` is a `[][][]byte` which represents a list of blocks (note that `[]byte` represents a transaction and `[][]byte` represents a block), and the second argument is a series of `func(sdk.Context, *app.App)` which represents the genesis state initialiers and allows test writers to set genesis state directly through keeper functions without having to write it as a json file. Once a test server is set up, test writers can call `Run` with a function parameter `func(port int)`, within which requests can be made via calls like `sendRequestWithNamespace(<namespace e.g. eth>, port, <endpoint e.g. getTransactionByHash>, parameters...)`. 

### Transaction Generation
To generate a transaction to feed into `SetupTestServer`, it follows a three-step process:
1. create unsigned message (e.g. `send(<nonce>)` generates a simple EVM send message)
2. sign the message (e.g. `signTxWithMnemonic` signs an EVM message, and `signCosmosTxWithMnemonic` signs a Cosmos message)
3. encode the message into bytes (e.g. `encodeEvmTx` encodes an EVM tx, `encodeCosmosTx` encodes a Cosmos tx)
There are also helper functions that consolidate these three steps into one line (e.g. `signAndEncodeCosmosTx(bankSendMsg(...),...)` or `signTxWithMnemonic(send(...),...)`), but there might be times when explicit steps are preferred (e.g. record transaction hash before it's encoded).

### Genesis State Initialization
The "genesis state" of the chain backing a test server can be initialized via a variable number of functions with signature `func(ctx sdk.Context, a *app.App)`, where `*app.App` provides keeper access to allow directly setting state with the familiar keeper functions. Some of the common initializers are already predefined:
- `mnemonicInitializer` sets address association and initial funds for an account specified by the mnemonic.
- `erc20Initializer` creates an ERC20 contract with `ERC20.bin`
    - the ERC20 created this way has a deterministic address stored in `erc20Addr`
- `cw20Initializer` creates a CW20 contract with `cw20_base.wasm`
    - the CW20 created this way has a deterministic address `sei18cszlvm6pze0x9sz32qnjq4vtd45xehqs8dq7cwy8yhq35wfnn3quh5sau`

### Regression Tests
Tests under `regression_test.go` are a special variant of RPC tests. It's used to reproduce states on pacific-1 and check correctness of `debug_traceTransaction`. Specifically, if one wants to write a regression test for a transaction of hash 0xABCDEF on pacific-1, they would need to first call `debug_traceStateAccess` with params being `["0xABCDEF"]` against a live pacific RPC node, and store the entirety of the json response under `evmrpc/tests/mock_data/transactions/0xABCDEF.json`. If the transaction accesses any WASM code, it needs to be retrieved via `seid q wasm code [code_id] evmrpc/tests/mock_data/[code_id].code --node <live pacific node>` as well. Once the state files are in place, the actual regression test is a simple one-liner like:
```golang
testTx(t,
	"0xABCDEF",
	"v6.1.0",
	"0x1da69",
	"",
	true,
)
```
where the first argument is the transaction hash, the second argument is the binary version this transaction executed on, the third argument is the expected gas usage, the fourth argument is the expected output, and the last argument indicates whether an error is expected. You can find plenty of examples under `regression_test.go`.
