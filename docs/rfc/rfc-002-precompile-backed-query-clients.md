# RFC 002: Precompile-Backed Query Clients

## Changelog

- 2026-05-13: Initial draft

## Abstract

This RFC proposes a shared precompile query bridge for module query clients
during the migration away from Cosmos gRPC and REST query surfaces. Generated
`QueryClient` implementations already route every CLI query through a single
`gogogrpc.ClientConn.Invoke` call, so we can keep the generated clients as a
local compatibility API while replacing the transport beneath them with a
reusable middleware that packs the protobuf request into an EVM precompile
`eth_call` and unpacks the ABI result into the protobuf response.

Each module still owns the mapping between its protobuf query methods and its
precompile ABI methods, but the boilerplate for routing, EVM RPC calls, ABI
packing, ABI unpacking, height handling, and tests can be shared. The final
external query surface is EVM RPC only.

## Background

Today CLI query commands follow this shape:

```go
clientCtx, err := client.GetClientQueryContext(cmd)
queryClient := types.NewQueryClient(clientCtx)
res, err := queryClient.SomeQuery(context.Background(), req)
```

The generated client in `x/<module>/types/query.pb.go` implements each method
by calling `cc.Invoke(ctx, fullMethod, req, out, opts...)` on the
`github.com/gogo/protobuf/grpc.ClientConn` interface. `client.Context`
implements that `Invoke` method by issuing an ABCI query to the registered
module `QueryServer`.

The server side is usually in `keeper/grpc_query*.go` or `keeper/querier.go`.
`keeper/msg_server.go` implements transaction message servers, not query
clients. Any query migration should be explicit about that split:

- Query migration replaces gRPC/REST query surfaces with EVM RPC precompile
  queries.
- Message server deprecation is a separate write-path migration.
- Precompiles may still call existing keepers or message servers internally
  until those write paths are migrated.

Several precompiles already expose query-shaped view methods, such as bank
`balance`, `all_balances`, `supply`; staking `validator`, `delegation`,
`params`; distribution `rewards`; oracle `getExchangeRates` and
`getOracleTwaps` where available. Those methods return Solidity ABI values,
while the existing CLI code expects protobuf query responses.

## Discussion

### Goals

- Keep CLI call sites and generated `QueryClient` APIs stable.
- Avoid hand-writing a full query client implementation for every module.
- Put all common EVM RPC `eth_call` behavior in one package.
- Keep module-specific logic limited to a compact request/response mapping.
- Make unsupported query methods fail clearly unless their precompile binding
  exists.
- Make it usable from CLI and tests without relying on Cosmos gRPC or REST.
- Put all module binding registries under `precompiles/`.

### Non-goals

- This does not remove generated protobuf clients.
- This does not remove `MsgServer` implementations.
- This does not require every module query to have an immediate precompile
  equivalent.
- This does not redefine precompile ABIs. Missing ABI coverage should be added
  to the module precompile before routing that protobuf query through the bridge.
- This does not preserve gRPC or REST as long-term external query APIs.
- This does not add `QueryServer` shims over precompile calls.

### Proposed package

Add a shared package, for example:

```text
precompiles/query/
  conn.go          // gogogrpc.ClientConn middleware
  binding.go       // typed protobuf <-> ABI binding helpers
  eth_call.go      // Ethereum JSON-RPC eth_call adapter
  address.go       // Sei/EVM address conversion helpers

precompiles/<module>/query/
  registry.go      // module protobuf query <-> module precompile bindings
  convert.go       // module-specific ABI/protobuf conversion helpers
```

The central type is a `gogogrpc.ClientConn` wrapper:

```go
type Conn struct {
    caller   EVMCaller
    bindings Registry
}

func NewConn(caller EVMCaller, bindings Registry, opts ...Option) *Conn

func (c *Conn) Invoke(
    ctx context.Context,
    method string,
    req any,
    reply any,
    opts ...grpc.CallOption,
) error
```

`Invoke` checks the registry by generated gRPC full method name. If a binding
exists, it executes an EVM RPC `eth_call` to the precompile and fills `reply`.
If no binding exists, it returns an unsupported-query error. `NewStream` should
return a clear unsupported error because generated module query clients
currently use unary queries.

The caller is backed by Ethereum JSON-RPC:

```go
type EVMCaller interface {
    CallContract(
        ctx context.Context,
        msg ethereum.CallMsg,
        blockNumber *big.Int,
    ) ([]byte, error)
}
```

For CLI usage, this caller is normally `ethclient.Client`, connected to the
configured EVM RPC endpoint. It must not be backed by x/evm `StaticCall` or any
Cosmos gRPC/REST query path. Tests can use an in-process fake `EVMCaller`.
`Env.EthCall` builds an `ethereum.CallMsg` with the configured default caller,
precompile address, and ABI input, then passes the selected block number to
`CallContract`; bindings can override the caller if a query's semantics depend
on it.

### Binding model

Each module contributes a small registry. A binding says:

- Which protobuf full method it handles, for example
  `/cosmos.bank.v1beta1.Query/Balance`.
- Which precompile address and ABI method it calls, for example bank
  `balance(address,string)`.
- How to pack the protobuf request into ABI arguments.
- How to unpack ABI outputs into the protobuf response.
- Whether the response is byte-for-byte equivalent to the old protobuf query
  response, or documents an intentional variation.

Sketch:

```go
type Binding[Req any, Resp any] struct {
    FullMethod       string
    Precompile       common.Address
    ABI              abi.ABI
    ABIMethod        string
    Pack             func(context.Context, *Env, *Req) ([]any, error)
    Unpack           func(context.Context, *Env, *Req, []any, *Resp) error
    ABIForHeight     func(height int64) abi.ABI
    ResponseShape    ResponseShape
}
```

The generic binding performs the mechanical work:

```go
func (b Binding[Req, Resp]) Invoke(ctx context.Context, env *Env, req any, reply any) error {
    typedReq, ok := req.(*Req)
    if !ok {
        return fmt.Errorf("expected %T, got %T", (*Req)(nil), req)
    }
    typedReply, ok := reply.(*Resp)
    if !ok {
        return fmt.Errorf("expected %T, got %T", (*Resp)(nil), reply)
    }

    args, err := b.Pack(ctx, env, typedReq)
    if err != nil {
        return err
    }
    input, err := b.ABI.Pack(b.ABIMethod, args...)
    if err != nil {
        return err
    }
    output, err := env.EthCall(ctx, b.Precompile, input)
    if err != nil {
        return err
    }
    values, err := b.ABI.Unpack(b.ABIMethod, output)
    if err != nil {
        return err
    }
    return b.Unpack(ctx, env, typedReq, values, typedReply)
}
```

This leaves every module with only the semantic translation.

### Example: bank balance

The bank query client method:

```go
Balance(ctx, &banktypes.QueryBalanceRequest{
    Address: "sei1...",
    Denom: "usei",
})
```

can map to the bank precompile:

```solidity
function balance(address acc, string memory denom)
    external view returns (uint256 amount);
```

The binding is roughly:

```go
query.Bind[
    banktypes.QueryBalanceRequest,
    banktypes.QueryBalanceResponse,
](
    "/cosmos.bank.v1beta1.Query/Balance",
    common.HexToAddress(bank.BankAddress),
    bank.GetABI(),
    bank.BalanceMethod,
    func(ctx context.Context, env *query.Env, req *banktypes.QueryBalanceRequest) ([]any, error) {
        evmAddr, err := env.EVMAddressForSeiAddress(ctx, req.Address, query.AllowCastAddress)
        if err != nil {
            return nil, err
        }
        return []any{evmAddr, req.Denom}, nil
    },
    func(_ context.Context, _ *query.Env, req *banktypes.QueryBalanceRequest, out []any, resp *banktypes.QueryBalanceResponse) error {
        amount := sdk.NewIntFromBigInt(out[0].(*big.Int))
        resp.Balance = &sdk.Coin{Denom: req.Denom, Amount: amount}
        return nil
    },
)
```

All other bank bindings follow the same pattern. The only bespoke code is
address conversion and struct conversion.

### Response shape

The default expectation is that precompile-backed queries reconstruct the old
protobuf responses exactly. This makes CLI behavior predictable and gives
parity tests a simple equality assertion.

Some variation is acceptable when exact reconstruction would make the
precompile ABI or conversion logic disproportionately complicated. Those cases
must be explicit in the binding registry:

```go
type ResponseShape uint8

const (
    ExactProtobufShape ResponseShape = iota
    DocumentedVariation
)
```

A binding that uses `DocumentedVariation` should include a short note in the
module registry explaining the difference. Examples might include omitted
pagination metadata when the EVM-facing query intentionally exposes a simpler
iterator model, or formatting differences where the old protobuf field was an
implementation detail rather than part of the retained query contract.

### Address conversion

Precompile ABIs frequently use EVM `address` values while protobuf queries
frequently use Bech32 Sei addresses. The bridge should centralize this to avoid
subtle inconsistency.

`Env` should expose:

```go
type AddressPolicy uint8

const (
    RequireAssociation AddressPolicy = iota
    AllowCastAddress
)

func (e *Env) EVMAddressForSeiAddress(ctx context.Context, sei string, policy AddressPolicy) (common.Address, error)
func (e *Env) SeiAddressForEVMAddress(ctx context.Context, evm common.Address, policy AddressPolicy) (sdk.AccAddress, error)
```

The implementation must use the `addr` precompile through EVM RPC when
association is required:

- `getEvmAddr(string)` for Sei Bech32 to EVM address.
- `getSeiAddr(address)` for EVM address to Sei Bech32.

When `AllowCastAddress` is requested and no association exists, the helper can
mirror `x/evm/keeper/address.go` locally by casting the 20-byte address. Each
binding chooses the policy that matches the precompile method it calls.

### Height and version handling

The current `client.Context.Invoke` honors the gRPC block-height metadata header
and sets the ABCI query height. The replacement bridge should translate the
same CLI height selection into the `eth_call` block number parameter. If no
height is selected, it should use latest block semantics.

Historical precompile ABI differences should be handled in one of two ways:

- Prefer using the same EVM RPC block number for both the `eth_call` execution
  and the binding's `ABIForHeight(height int64)` decision.
- If a binding does not support an old height, return an explicit unsupported
  height error rather than falling back to keeper queries.

This avoids x/evm `StaticCall` entirely and keeps the query path on the EVM RPC
surface that will remain after gRPC and REST are deprecated.

### Error and fallback policy

Failure behavior should be deterministic and explicit:

- No binding: return an unsupported-query error.
- Binding marks the queried height unsupported: return an unsupported-height
  error.
- Type mismatch, ABI pack failure, ABI unpack failure, or precompile revert:
  return the error.

There should be no production fallback to keeper-backed gRPC/REST queries. For
dual-run rollout tests, a separate test harness can call the old query server
and compare responses, but the precompile-backed client should not silently
fall back.

### CLI integration

Module CLI code can stay almost identical. Each module changes only its query
client construction and receives an EVM RPC endpoint:

```go
clientCtx, err := client.GetClientQueryContext(cmd)
if err != nil {
    return err
}

rpcURL, err := cmd.Flags().GetString(evmcli.FlagRPC)
if err != nil {
    return err
}

evmClient, err := ethclient.DialContext(cmd.Context(), rpcURL)
if err != nil {
    return err
}

queryClient := banktypes.NewQueryClient(
    query.NewConn(
        evmClient,
        bankprecompilequery.Registry(),
        query.WithDefaultBlockNumber(clientCtx.Height),
    ),
)
```

To avoid repeating that in every command, the module can expose:

```go
func NewPrecompileBackedQueryClient(
    clientCtx client.Context,
    evmClient query.EVMCaller,
) banktypes.QueryClient {
    return banktypes.NewQueryClient(query.NewConn(
        evmClient,
        Registry(),
        query.WithDefaultBlockNumber(clientCtx.Height),
    ))
}
```

Then each CLI command only changes:

```go
queryClient := bankquery.NewPrecompileBackedQueryClient(clientCtx, evmClient)
```

Each module should add an `--evm-rpc` query flag, reusing the existing EVM CLI
default of `http://<local-address>:8545` where practical. Cosmos node RPC
flags can remain during the transition only for command plumbing and output
formatting; retained query execution should use EVM RPC.

### Testing strategy

The shared package should have table tests for:

- Routing to a binding by full method.
- Returning unsupported-query errors when no binding exists.
- Translating CLI height selection into the `eth_call` block number.
- Returning pack, call, and unpack errors without fallback.
- Type mismatch diagnostics.

Each module registry should have parity tests:

- Seed keeper state.
- Query via the existing generated client against the existing query server.
- Query via the precompile-backed generated client against EVM RPC `eth_call`.
- Compare protobuf responses for `ExactProtobufShape` bindings.
- Assert documented differences for `DocumentedVariation` bindings.

The parity tests should start with low-cardinality queries such as bank
`Balance`, bank `Supply`, staking `Params`, staking `Pool`, and distribution
`DelegationTotalRewards`, then move to paginated and struct-heavy queries.

### Rollout plan

1. Add the shared `precompiles/query` bridge with no module behavior changes.
2. Add or complete precompile query methods for the module queries that should
   remain available after gRPC/REST deprecation.
3. Add bank bindings under `precompiles/bank/query` and parity tests, because
   bank has simple read methods and good existing query coverage.
4. Switch bank CLI construction to the EVM RPC-backed helper.
5. Add staking and distribution bindings, paying special attention to
   pagination and nested struct conversion.
6. Repeat until every retained module query has a precompile-backed binding.
7. Remove or stop registering deprecated gRPC/REST query surfaces when retained
   query coverage has moved to EVM RPC.
8. Only after query parity is proven, design the separate write-path migration
   for `MsgServer`.

### Decisions

- Query execution uses EVM RPC `eth_call`, not x/evm `StaticCall` or any
  Cosmos gRPC/REST endpoint.
- gRPC and REST are not long-term compatibility surfaces for these queries.
- Retained queries should be implemented in precompiles; no retained query
  should remain keeper-backed in the final state.
- Binding registries live under `precompiles/<module>/query`.
- Responses should match old protobuf responses by default. Documented
  variations are acceptable when exact parity would overcomplicate the
  precompile ABI or conversion logic.

### References

- `sei-cosmos/client/grpc_query.go`
- `evmrpc/simulate.go`
- `precompiles/addr/Addr.sol`
- `precompiles/bank/Bank.sol`
- `precompiles/staking/Staking.sol`
- `precompiles/distribution/Distribution.sol`
- `precompiles/oracle/Oracle.sol`
