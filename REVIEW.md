# Review guidelines for AI agents

Repo-specific conventions for automated PR review (Codex, Cursor, Claude, and
any other AI reviewer). The patterns below look like bugs in isolation
but are normal consequences of how Sei cuts releases, rolls out upgrades, and
processes transactions. Do not report them as findings unless a concrete
additional signal (see the caveats under each) makes them real.

## 1. Referencing a not-yet-created release tag is expected

Sei only creates a new release tag — and registers it in `app/tags` with a
handler in `app/upgrades.go` — *after* the code that depends on it has
already been merged to main, never before. So it is completely normal for a
PR to introduce a version-gated constant (e.g. `const FooUpgrade = "v6.7"`)
that:

- does not yet appear anywhere in `app/tags`,
- has no corresponding upgrade handler in `app/upgrades.go`, and
- therefore cannot currently be produced by `ctx.ClosestUpgradeName()` /
  `ctx.LatestUpgrade()`.

Do **not** flag this as "the tag/handler is never registered," "this branch
is permanently unreachable/dead," or "this constant doesn't match anything
in the tree." The tag is cut and the handler wired up in a follow-up step
after this PR lands, using the same version string, following the existing
`app/tags` naming convention.

This only becomes a real finding when:
- the PR description or diff explicitly claims *this* PR adds the tag/handler
  (i.e. it's the release-cut PR) and it doesn't, or
- the referenced version string doesn't follow Sei's tag naming convention
  (compare against the existing entries in `app/tags`).

## 2. Version-gated logic and block/state sync: don't assume cross-version execution

Do not flag scenarios along the lines of "new code could process a
pre-upgrade height and diverge from what was originally committed," or
"this upgrade gate can never activate because the upgrade hasn't run yet,"
as correctness bugs — unless the diff itself has a concrete logic bug in the
gate (e.g. an inverted comparison, wrong field, off-by-one on the height).

Operationally, a given binary is never used to execute or re-execute a
height range that predates its own earliest registered upgrade. Block/state
sync always proceeds version-by-version: e.g. if a node's target height
spans releases v6.0 and v6.1, the node first syncs with the v6.0 binary up
to v6.1's upgrade height, halts, switches to the v6.1 binary, and continues
syncing from there. New code never processes old, not-yet-upgraded state.

Consequences for review:
- Height/upgrade-name gates (`ctx.ClosestUpgradeName()`, `ctx.IsTracing()`,
  semver comparisons against an upgrade constant, etc.) do not need extra
  defenses against "a newer binary ran against pre-upgrade state" — that
  situation does not occur in Sei's deployment model.
- By the time a binary is live (or tracing/replaying) at or after its own
  upgrade height, that upgrade's handler has necessarily already applied on
  that node, so upgrade-gated branches are reachable for those blocks. Don't
  call them "permanently unreachable" solely because the tag/handler isn't
  registered yet at PR-review time — see §1.

If you believe a version gate is genuinely broken on its own logic (wrong
comparison direction, wrong constant, wrong context field), still report
it — this guidance only rules out the "the tag doesn't exist yet" and
"old code might run against post-upgrade state" false positives.

## 3. Some Cosmos transactions are gasless: don't require `--fees` on them

Do not flag a `seid tx` invocation (in tests, scripts, or docs) as broken for
omitting `--fees` before checking whether the message type is gasless. Sei's
ante handlers classify certain transactions as gasless and skip minimum-fee
validation entirely for them, so `minimum-gas-prices`-based fee arithmetic
(`ceil(min-gas-price × gas-limit)`) does not apply.

The classification lives in `IsTxGasless` (`app/antedecorators/gasless.go`)
and is consumed by both the CheckTx and DeliverTx ante paths
(`app/ante/cosmos_checktx.go`, `app/ante/cosmos_delivertx.go`), where
`CheckAndChargeFees` returns before any fee comparison when the transaction
is gasless. As of this writing the gasless set is:

- a single-message `MsgAssociate` (`seid tx evm native-associate`) whose
  sender is **not yet associated** — the common case in bootstrap helpers and
  association tests; the sender only needs a nonzero balance (at least 1 wei),
  not a fee, and
- `MsgAggregateExchangeRateVote` from a validator without a vote in the
  current window.

Consequences for review:
- A fee-less `native-associate` for a fresh (unassociated) account is
  correct. Do not report "insufficient fees at CheckTx" for it, and do not
  infer breakage from sibling commands that do pass `--fees` (e.g.
  `bank send`, `associate-contract-address`) — those message types are not
  gasless, so the asymmetry is intentional.
- Best-effort re-association of an already-associated account is not gasless
  and its CheckTx would reject, but helpers written as try/catch plus a
  poll for `associated == true` (e.g. `associateKey` in
  `contracts/test/lib.js`) don't need the transaction to land in that case.

This becomes a real finding only when the diff makes a **non**-gasless
message's invocation fee-less, or when a test strictly requires a gasless-set
transaction to land for a sender that is already associated (or already voted)
at the time of broadcast — then the fee path does apply and the transaction is
rejected. When in doubt, check `IsTxGasless` for the authoritative set rather
than reasoning from `minimum-gas-prices` alone.

## 4. Genesis rewrites must preserve consensus parameters

Any code that rewrites an existing genesis document must carry every consensus
parameter deliberately customized earlier in the generation flow unless replacing
those values is an explicit responsibility of the rewrite. Rebuilding only the
chain ID, validators, application state, and genesis time silently restores omitted
consensus parameters to their defaults.

Review the complete genesis-generation flow rather than only the initial write.
When a later step collects gentxs or normalizes genesis time, tests must read the
final file after that step and verify that customized consensus parameter values
remain intact.

## 5. Deprecated configuration keys need an explicit policy

The current Viper unmarshalling path ignores unknown TOML keys, so placeholder
struct fields are not required merely to let an existing configuration file
load. A placeholder has value only when a reachable warning, validation, or
migration path consumes it.

Do not request global unknown-field errors as a local deprecation fix. Changing
the decoder to reject unknown keys alters compatibility for every configuration
section and must be an intentional change at the shared decoding choke point,
with dedicated startup or TOML-decoding tests that record the behavior.

A deprecated field is a real finding when the code or documentation promises a
warning or migration but no startup path consumes it, or when removing it changes
documented compatibility without an explicit replacement policy.

## 6. An exported method on an `evmrpc` API struct is a live RPC endpoint — always flag it

`go-ethereum`'s `rpc.Server` auto-registers every exported method on a
`Service` struct passed to `RegisterName` (`evmrpc/server.go`) as a callable
JSON-RPC method — there is no separate opt-in step. This applies to
`InfoAPI`, `FilterAPI`, `DebugAPI`, and any other struct in the `RegisterName`
list in `server.go`. See `evmrpc/AGENTS.md` ("Exported receivers are RPC
surface") for the full rule.

Unlike the other entries in this file, **this is not a false-positive
suppression — it is a directive to actively flag the pattern**: if a diff adds
or renames a method to be exported (capitalized) on one of these structs, and
the method is not clearly intended as a new public RPC endpoint (e.g. it is a
helper, a testing convenience, or an internal computation), call it out as a
correctness/security finding, even if the diff "looks like" a harmless rename
or refactor.

Do not require this for methods that are obviously meant to be public RPC
methods (matching an `eth_`/`debug_`/`sei_` spec method name), and do not flag
lower-case (unexported) helpers, or methods added via `evmrpc/export_test.go`
`*ForTest` wrappers (those are `_test.go`-only and excluded from production
builds).
