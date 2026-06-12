# Implementing Precompile-Backed Module Query Clients

This note captures the context needed to generate implementations for additional
modules after the bank module example. The goal is to preserve existing query
CLI behavior while moving the implementation behind EVM RPC calls to module
precompiles.

## Architecture Context

- Query CLI commands live under `x/<module>/client/cli/query.go`.
- The protobuf `QueryClient` interface for each module is generated in
  `x/<module>/types/query.pb.go`.
- New client implementations should be backed by the module's EVM precompile
  under `precompiles/<module>/`.
- Binding registries belong under `precompiles/<module>/query/`.
- Shared binding, pagination, ABI decoding, and address helpers belong under
  `precompiles/query/`.
- Do not add server-side gRPC or REST routing. The long-term goal is to
  deprecate gRPC and REST and preserve EVM RPC endpoints.
- Do not route through `x/evm` `StaticCall` from server code. CLI clients should
  use EVM RPC `eth_call`.
- No query intended to survive this migration should remain keeper-backed at the
  CLI client layer. If a precompile query is missing, implement the precompile
  view first, then bind the CLI query to it.

## Existing Flow

The reusable connection in `precompiles/query` implements the gRPC
`ClientConn` shape expected by generated `types.NewQueryClient`.

The flow is:

1. CLI command builds a module query client.
2. The precompile-backed client calls `types.NewQueryClient(precompilequery.NewConn(...))`.
3. `Conn.Invoke` looks up the full protobuf method name in a module registry.
4. The binding validates and packs the protobuf request into ABI call arguments.
5. `Env.EthCall` executes `eth_call` against the module precompile.
6. The binding unpacks ABI output into the generated protobuf response type.

Bank is the reference implementation:

- `precompiles/bank/query/registry.go`
- `sei-cosmos/x/bank/client/cli/query.go`
- `sei-cosmos/x/bank/client/cli/query_integration_test.go`

## Implementation Checklist For A New Module

1. Read the module's current query CLI commands and generated query interface:
   `x/<module>/client/cli/query.go` and `x/<module>/types/query.pb.go`.
2. Read the current keeper query implementation only to mirror request
   validation, response shape, ordering, pagination, missing-value behavior, and
   error behavior.
3. Check the module precompile ABI and implementation under
   `precompiles/<module>/`. Add any missing read-only precompile methods needed
   by the CLIs.
4. Create `precompiles/<module>/query/registry.go`.
5. Define one `pquery.Binding[Request, Response]` per protobuf query method the
   module should keep.
6. Use full protobuf method strings from `query.pb.go`, for example
   `/cosmos.bank.v1beta1.Query/Balance`.
7. In each `Pack` function, validate the request the same way the old query path
   did, then return ABI arguments.
8. In each `Unpack` function, reconstruct the generated protobuf response. Aim
   for exact JSON/protobuf parity. If exact parity would make the implementation
   much more complex, document the variation in the binding with
   `DocumentedVariation` and `Variation`.
9. Keep generic decoding and pagination helpers in `precompiles/query`, not in
   module-specific registries.
10. Update the module CLI to construct the precompile-backed query client by
    default.
11. Add a hidden backend-selection flag while comparing against legacy behavior
    in tests. Bank uses `--query-client-backend=precompile|legacy`.
12. Ensure `--height` is respected by passing `clientCtx.Height` into
    `precompilequery.WithDefaultBlockNumber`.

## Height Handling

`precompilequery.Conn` chooses the EVM block number in this order:

1. gRPC block height metadata on the outgoing context, if present.
2. `WithDefaultBlockNumber(clientCtx.Height)`, when the CLI passed `--height`.
3. `nil`, which lets the EVM RPC endpoint use latest.

Integration tests should assert both legacy and precompile backends observe the
same height for:

- no `--height`
- explicit `--height=<historical height>`

## Pagination And Ordering

Many precompiles return whole in-memory collections, then the binding applies
Cosmos pagination locally with helpers from `precompiles/query`.

Important details:

- Match `sei-cosmos/types/query.Paginate`, including the case where `Offset > 0`
  and `Key != nil`. An empty page-key byte slice still conflicts with offset.
- Preserve keeper ordering semantics. For example, bank balances preserve reverse
  iterator order, but bank total supply normalizes the returned page through
  `sdk.Coins` sorting because the keeper path builds the response through
  `Coins.Add`.
- Compare JSON CLI output, not just Go structs, because the user-facing contract
  is the CLI response.
- Include page-key, limit, count-total, reverse, invalid offset+key, empty
  collection, and missing item cases when relevant.

## Address Handling

If a precompile expects EVM addresses but the old CLI accepts Sei bech32
addresses, use `Env` helpers from `precompiles/query/address.go`:

- `EVMAddressForSeiAddress`
- `SeiAddressForEVMAddress`

Choose `RequireAssociation` or `AllowCastAddress` deliberately based on the old
query semantics and the precompile's expected contract.

## Test Expectations

For each module, add tests that actually execute the Cobra CLI commands. The
test shape should mirror bank's `query_integration_test.go`:

- Build controlled app states for latest and historical heights.
- Provide a fake legacy Tendermint RPC client that routes protobuf queries to
  the old keeper query server.
- Provide a fake JSON-RPC server that handles `eth_call`, chooses the requested
  block height, and executes the module precompile.
- Run each CLI case twice, once with the legacy backend and once with the
  precompile backend.
- Compare successful outputs with `require.JSONEq`.
- For expected errors, assert both backends fail.
- Cover every query CLI command in the module, with and without `--height`.
- Include edge cases around missing denoms/IDs, invalid addresses, pagination,
  reverse order, page-key, default latest height, and historical height.

Existing older tests that assume Tendermint RPC/gRPC should opt into the legacy
backend until they are rewritten or removed.

## Future AI Prompt Context

When asking an AI agent to implement another module, include this context:

```text
Implement precompile-backed query clients for x/<module>.

Architectural constraints:
- Query CLIs are in x/<module>/client/cli/query.go.
- Generated QueryClient is in x/<module>/types/query.pb.go.
- Bindings must live under precompiles/<module>/query.
- Reusable helpers must live under precompiles/query.
- Do not add server-side gRPC/REST integration and do not route through x/evm StaticCall.
- The CLI should default to the precompile-backed QueryClient.
- Add a hidden backend flag only for tests so outputs can be compared against the old QueryClient.
- Respect --height by passing clientCtx.Height to precompilequery.WithDefaultBlockNumber.
- Implement missing read-only precompile methods needed by the query CLIs.
- No query being migrated should remain keeper-backed in the new client.

Use bank as the reference:
- precompiles/bank/query/registry.go
- sei-cosmos/x/bank/client/cli/query.go
- sei-cosmos/x/bank/client/cli/query_integration_test.go

Add integration tests that execute the actual CLI commands, compare JSON output
between legacy and precompile backends, and cover every query CLI with and
without --height plus pagination and missing/invalid input edge cases.
```
