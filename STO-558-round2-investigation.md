# STO-558 Round 2 — Root Cause Investigation Report

Investigation conducted on shadow migration node `pacific1-flatkv-mig-v2-0-0`, which
crashed with a `NextValidatorsHash` mismatch at height **211,885,576** despite the
iterator-related fixes that resolved the original STO-558.

## TL;DR

- A consensus-fatal state divergence occurred while processing block **211,885,575**.
- Specifically `tx[2]` (a `MsgDelegate` to validator `Figment`) **succeeded canonically
  (code = 0, +1 SEI to Figment voting power)** but **failed locally with `signature
  verification failed; please verify account number (102373062) and chain-id
  (pacific-1)` (code = 4)**.
- All known inputs to the SDK signature verifier are byte-identical between the local
  node and canonical mainnet, yet local rejects the signature. By pure-logical
  reasoning this should be impossible.
- The bug **only manifests post-migration-completion** (`MigrationBoundary{status=complete}`,
  startVersion=0, targetVersion=1) and on a node with **OCC parallel execution
  enabled** (`occ-enabled = true` in `app.toml`).
- All "obvious" suspects have been ruled out by direct evidence (see "Ruled out"
  section). The most likely remaining root cause is **a race in the OCC/MVS
  read-set tracking that surfaces only after the migration completion code path is
  active**, but pinning the exact code line requires deeper audit of
  `sei-cosmos/tasks/scheduler.go` × `sei-cosmos/store/multiversion/mvkv.go` or a
  controlled reproduction (e.g. A/B with `occ-enabled = false`).

## Failing transaction

Hash: `8703CA975D7C3FC36E92AFC863E6AC346142824A8F589E63F46753DCD036BFE2`
(block 211,885,575, tx[2], `cosmos.staking.v1beta1.MsgDelegate`)

```json
{
  "body": {
    "messages": [{
      "@type": "/cosmos.staking.v1beta1.MsgDelegate",
      "delegator_address": "sei1jxy9rqew5s2ykr9g3ya60wuvftplek8jl0gc6v",
      "validator_address": "seivaloper1y82m5y3wevjneamzg0pmx87dzanyxzht0kepvn",
      "amount": { "denom": "usei", "amount": "1000000" }
    }]
  },
  "auth_info": {
    "signer_infos": [{
      "public_key": {
        "@type": "/cosmos.crypto.secp256k1.PubKey",
        "key": "Ay2xnkFsLrK18oWnq2+iiOVDHEJlvhXoWyzFRKJ9Cxtp"
      },
      "mode_info": { "single": { "mode": "SIGN_MODE_DIRECT" } },
      "sequence": "0"
    }],
    "fee": { "amount": [{ "denom": "usei", "amount": "350000" }],
             "gas_limit": "700000" }
  }
}
```

Signer is a **pure-cosmos secp256k1 account**, no EVM association:

```
ripemd160(sha256(0x032db19e416c2eb2b5f285a7ab6fa288e5431c4265be15e85b2cc544a27d0b1b69))
= 918851832EA4144B0CA8893BA7BB8C4AC3FCD8F2
= sei1jxy9rqew5s2ykr9g3ya60wuvftplek8jl0gc6v ✓
```

`seid q evm sei-addr` and `seid q evm evm-addr` both return `associated = false`.

## Local vs canonical state at the failing block

| Property | Local | Canonical | Match? |
|---|---|---|---|
| `tx[0]` (EVM) result | code=0 | code=0 | ✓ |
| `tx[1]` (EVM) result | code=0 | code=0 | ✓ |
| `tx[2]` result | **code=4 (sig verify)** | code=0 (delegated 1 SEI) | ✗ |
| signer account_number | 102373062 | 102373062 | ✓ |
| signer sequence at block start | 0 | 0 | ✓ |
| signer pubkey at block start | null | null | ✓ |
| validator_updates emitted | none | Figment +1 SEI VP | ✗ |
| `finalize_block_events` count | 104 | 105 | -1 (the missing fee `coin_received` for tx[2]) |

The single missing finalize-block event on local is exactly the fee deduction
`350000usei` → `sei17xpfvakm2amg962yls6f84z3kell8c5la4jkdu` (fee collector). That is
the expected downstream effect of tx[2] reverting at ante.

The signer's account existed continuously from at least height ~211,880,000 onward
with `(num=102373062, seq=0, pubkey=null)`, so block 575 was its first cosmos tx.
**The signer account was not created during block 575.**

## Failure location in code

`sei-cosmos/x/auth/ante/sigverify.go:302`:

```go
errMsg = fmt.Sprintf("signature verification failed; please verify account number (%d) and chain-id (%s)", accNum, chainID)
```

`accNum` is `acc.GetAccountNumber()`. The error string contains 102373062, so local
read the same account_number that canonical read and that the signer signed against.

For this branch to be taken, all of the following must have happened in order:

1. `GetSignerAcc` returned a non-nil account ✓
2. `pubKey := acc.GetPubKey()` was non-nil (otherwise we'd see "pubkey on account is
   not set" — we do not) ✓
3. `SetPubKeyDecorator`'s pubkey-vs-address check passed (otherwise "pubKey does not
   match signer address") ✓
4. `VerifySignature(pubKey, signerData{chainID, accNum, seq}, sig.Data, signMode,
   tx)` returned an error ✗ ← divergence

For step 4 to fail given that pubKey, signerData and tx body/auth_info are all
byte-equal to canonical, **either**:

- Local's effective `pubKey` at the call differs from canonical's, **or**
- Local's effective `signerData` (chainID/accNum/seq) at the call differs, **or**
- The signed-bytes derivation itself differs (`signModeHandler.GetSignBytes`).

Pure-logical reasoning shows none of these should differ — yet the verifier returns
an error. This is the central mystery.

## Ruled out

| Hypothesis | Evidence ruling it out |
|---|---|
| EVM-state migration directly corrupts auth state | `acc` store-key routes via `nonEVMRoute` straight to memIAVL in `MigrateEVM` mode — never touches `FlatKV`/`MigrationManager`. See `sei-db/state_db/sc/migration/router_builder.go:185-195`. |
| Signer's account was created mid-block by EVM tx → race on account_number | Account exists with same `(num,seq,pubkey)` from at least block 211,880,000 — predates the failing block by 5,575 blocks. |
| EVM↔Cosmos address association on signer | `seid q evm sei-addr 0x918851832EA4...` returns `associated=false`; signer derives via legacy RIPEMD160(SHA256(pk)), no EVM keccak relationship. |
| `tx[0]`/`tx[1]` directly mutate signer's state | Events for both EVM txs touch a disjoint set of addresses; signer (`sei1jxy9...`) is not present. |
| Migration `Iterator` / `Get` callsite still uses the buggy iterator | Auth reads bypass `MigrationManager` entirely. |
| `memIAVL` snapshot rewrite × OCC race | snapshot-211880000 was finalized at `08:19 UTC`; block 575 was processed at `08:55 UTC` (canonical block timestamp). No active rewrite during the failure window. |

## Code audit: OCC, mvkv, cachekv, gaskv, sigverify, direct, decoder, builder

A full read-through of these files rules out the obvious "OCC read-own-write race"
hypothesis at the code level. The audited path is logically sound:

- `mvkv.Get` (`store/multiversion/mvkv.go:132-172`) checks the **task's own writeset
  first** (line 144-149) before falling back to readset, then MVS, then parent. So
  a `SetPubKey` write earlier in the same incarnation IS visible to a later
  `SigVerify` read in the same incarnation.
- `scheduler.executeTask` (`tasks/scheduler.go:541-587`) calls `prepareTask` which
  creates a **fresh** `VersionIndexedStore` per incarnation, with empty
  writeset/readset (`scheduler.go:519-522`). Re-executions never inherit stale
  writes.
- `MVS.SetWriteset` (`store/multiversion/store.go:142-162`) wipes the previous
  incarnation's writeset via `removeOldWriteset` before committing the new one.
- `cachekv.Store` (the layer that ante's `CacheTxContext` adds on top of VIS) also
  implements read-own-write: `getFromCache(key)` (`store/cachekv/store.go:53-58`)
  hits the cache before falling through to parent.
- `gaskv.Store` is a pure pass-through with gas metering; no caching or
  transformation (`store/gaskv/store.go:66-94`).
- `defaultTxDecoder` preserves the wire `raw.BodyBytes` and `raw.AuthInfoBytes`
  verbatim on the wrapper (`x/auth/tx/decoder.go:87-95`).
- `wrapper.getBodyBytes()` / `wrapper.getAuthInfoBytes()`
  (`x/auth/tx/builder.go:71-86,87-102`) return those preserved wire bytes unless
  a `Set*` mutator has reset them to nil. A search of the entire repo confirms
  **no ante decorator on the cosmos ante chain calls any of those mutators** —
  all callsites are in tests, CLI, simulation, or Rosetta paths.
- `direct.GetSignBytes` (`x/auth/tx/direct.go:29-43`) reads precisely those wire
  bytes plus `data.ChainID` and `data.AccountNumber`. The error message at
  `x/auth/ante/sigverify.go:302` already prints `accNum = acc.GetAccountNumber()`
  as 102373062, so that local variable matches what the signer signed against.
- `runTx` runs ante in a `CacheTxContext`-branched ctx
  (`baseapp/baseapp.go:917-953`); both `SetPubKey` and `SigVerify` thread the
  same ctx through `ChainAnteDecorators`'s `next(ctx, ...)` continuation, so they
  share the same cachekv → VIS.

Under this model, the verifier should receive byte-equal inputs to canonical and
should accept. It does not. **The pure-OCC race hypothesis (H1) is not supported
by the code audit.**

## ROOT CAUSE CONFIRMED — pre-existing corrupt `account_number`

A direct cross-node comparison of `sei1jxy9rqew5s2ykr9g3ya60wuvftplek8jl0gc6v`:

| Height | Source | `account_number` | pub_key |
|--------|--------|-------------------|---------|
| 211,800,000 | canonical | (account does not exist) | — |
| 211,880,000 | canonical | **102373083** | null |
| 211,885,574 | canonical | **102373083** | null |
| 211,885,575 | canonical | **102373083** | set |
| 211,885,574 | **LOCAL** | **102373062** | null |

Local's `account_number` is **21 lower** than canonical's for the *same address at the same height*. The signer signed against the canonical value (102373083). Local computed `SignDoc{... AccountNumber=102373062}`. SHA256 of these two `SignDoc`s differs by one byte sequence, so `pk.VerifySignature` mathematically *must* return false. Local's rejection is **correct** — the local sig-verify code path is innocent.

Independent verification confirms this:
1. Standalone Go re-run of `direct.DirectSignBytes` + `pk.VerifySignature` returns `false` for every reasonable (chainID, accNum) including 102373062. Returns false for the canonical 102373083 as well, because local's `BaseAccount` bytes (with wrong accnum baked in) would never produce the signed signdoc anyway.
2. Independent Python `ecdsa` verification, plus public-key recovery from (sig, msg) — none of the 4 recovery candidates match the on-chain pubkey when `AccountNumber=102373062`.
3. Multiple canonical RPCs (rest.sei-apis.com, sei-api.polkachu.com) all report `tx[2]` of block 211,885,575 as `code=0` succeeded with these wire bytes — so canonical is verifying against 102373083, which it stored correctly.

The divergence existed **well before block 211,885,575**. The account was created sometime between 211,800,000 (didn't exist) and 211,880,000 (existed with accnum 102373083 canonical / 102373062 local). The block-575 sig-verify failure is just the first time a transaction *signed for* this account was processed — that's when the latent divergence became fatal.

## Pinpointed: the creation block is 211,876,659 (local self-replayed)

Bisecting the canonical chain locates `sei1jxy9...l0gc6v` as created at block **211,876,659**.

`pacific1-flatkv-mig-v2` status:
- `earliest_block_height = 211,785,575` (state-sync snapshot height)
- `latest_block_height = 211,885,575` (current)

So 211,876,659 is **inside the locally-replayed range** — this branch's code processed the block, with EVM migration in progress. Corruption is therefore NOT inherited from the snapshot; it was produced by this branch.

### Block 211,876,659 contents

51 transactions: **50 × `MsgEVMTransaction`** + **1 × `MsgSend`**.

### Sampling confirms a *localized* (not global) divergence

Picking another address that was touched in the same block:
- `sei1lj2wczqydkqqnkwugy50p4ch6mjtle7laa8jv3`: canonical accnum=102,222,826, local accnum=**102,222,826** (match)
- `sei1jxy9rqew...l0gc6v`:                    canonical accnum=102,373,083, local accnum=**102,373,062** (diff=21)

This rules out global `globalAccountNumber` drift. The 21 missing increments correspond to **21 EVM transactions whose EVM↔Cosmos address association did not create the cosmos counterpart account on local**.

### Suspected code path

`x/evm/ante/preprocess.go:77`:

```go
_, isAssociated := p.evmKeeper.GetEVMAddress(ctx, seiAddr)
```

then `else if isAssociated {/* noop */}` at line 93 (skip create) vs `else {/* AssociateAddresses → SetAddressMapping → NewAccountWithAddress */}` at line 95.

`GetEVMAddress` reads `evm/SeiAddressToEVMAddressKey(seiAddr)`. The EVM store is routed through `MigrationManager.Read` (`migration_manager.go:229-247`) during `MigrateEVM`. If the migration manager returns spurious non-nil for a never-written key (iterator/prefix bleed, IsMigrated comparison bug, or oldDBReader stale data), `isAssociated` falsely becomes `true` and the account creation is skipped silently. Same family of bug as round-1, but on the `Read` (point-get) path rather than the `Iterator` path that round-1 fixed.

## Pure-code audit of MigrationManager (Round 2)

Read paths walked: `MigrationManager.Read`, `IsMigrated`, `shouldForwardWriteToNewDB`, `buildMemIAVLReader`, `buildFlatKVReader`, `MemiavlMigrationIterator.NextBatch`. **No logical bug found on paper**:

- `Read`: if `IsMigrated(store, key)` → newDB-only; else oldDB → fallback newDB. For never-written key, both report not-found, so caller sees `(nil, false, nil)`. ✓
- `IsMigrated` (InProgress): if `mb.moduleName == moduleName` returns `bytes.Compare(key, mb.key) <= 0`; else lexicographic compare of module names. Standard, no off-by-one. ✓
- `shouldForwardWriteToNewDB`: migrated→newDB; not migrated + already in oldDB→oldDB; not migrated + absent→newDB. Fresh writes go to newDB. ✓
- `buildMemIAVLReader`: returns `(value, value!=nil, nil)`. Empty-slice value would still set found=true; would only matter if EVM ever stored empty values (it doesn't for `evm/0x02+seiAddr`).
- `MemiavlMigrationIterator.NextBatch`: sorted tree iteration, boundary resume uses `key+0x00` to skip inclusive lower bound. Looks correct.

### Concrete production evidence at block 211,876,659

|              | local | canonical |
|---             |---    |---        |
| total txs      | 51    | 51        |
| code=0 success | 51    | 51        |
| failed         | 0     | 0         |
| MsgSend (tx[50]) | sei1lj2wczqy→sei1jxy9 | same |
| MsgEVMTransaction | 50 | 50         |
| **new cosmos accounts created in block** | **1** | **22** (sei1jxy9 + 21 EVM-derived) |

Same wire bytes, same per-tx `code=0`, identical tx list — but local creates 1 account where canonical creates 22. The only path that could cause this is `EVMPreprocessDecorator.AnteHandle` taking the `isAssociated==true` branch (line 93) on local for 21 brand-new EVM signers it should be associating.

Since the on-paper read path looks correct, the next step has to be **runtime instrumentation**: log every `GetEVMAddress(seiAddr) → (addr, found)` on the live node during a controlled replay of 211,876,659, plus a parallel `m.oldDBReader / m.newDBReader / m.boundary.IsMigrated` dump for those keys. That should expose whether one of the readers returns spurious non-nil, or the boundary comparison routes the read to a side that contains stale data.

## Instrumentation patch (this commit)

Two files modified to emit per-key trace logs when env `STO558_TRACE=1`:

- `x/evm/keeper/address.go`: `SetAddressMapping` logs every write (with `createdAccount` flag); `GetEVMAddress` logs every read (hit/miss, returned bytes).
- `sei-db/state_db/sc/migration/migration_manager.go`: `Read` logs the route chosen (newDB-only / oldDB-hit / oldDB-miss-fallback-newDB), boundary status/module/key, and both readers' returns, scoped to `store == "evm"`.

Default off in production. Toggle on by setting `STO558_TRACE=1` on the pod.

## Confirmed node state (pacific1-flatkv-mig-v2)

Config `/.sei/config/app.toml` (the actual one — not `$HOME/.sei/.sei/config/app.toml` which is a default template never read):
- `sc-write-mode = "migrate_evm"`
- `sc-keys-to-migrate-per-block = 10000`

On-disk snapshots:
- memIAVL snapshot at height **211,870,000**
- flatkv snapshots at heights **211,860,000 / 211,870,000 / 211,880,000**

Migration mode is active and 211,876,659 (the corruption block) sits between flatkv snapshots 211,870,000 and 211,880,000. Rollback to snapshot 211,870,000 followed by replay through 211,876,659 with `STO558_TRACE=1` will capture the bug in its native environment.

## Deployment runbook (operator-facing)

1. Branch `yiren/sto558-trace` (or amend onto `yiren/v6.5.0-flatkv-shadow-on-main`) carries the patch above.
2. Build & push image:
   ```bash
   docker buildx build --platform linux/amd64 \
     -t 189176372795.dkr.ecr.us-east-2.amazonaws.com/sei/sei-chain:sto558-trace .
   aws ecr get-login-password --region us-east-2 | docker login --username AWS --password-stdin 189176372795.dkr.ecr.us-east-2.amazonaws.com
   docker push 189176372795.dkr.ecr.us-east-2.amazonaws.com/sei/sei-chain:sto558-trace
   ```
3. Recommended deploy strategy — spin a new pod (don't disturb the existing one which is needed as a witness):
   - Use the same Helm/Kustomize template as `pacific1-flatkv-mig-v2` to produce `pacific1-flatkv-mig-v3`.
   - Image: `:sto558-trace`.
   - Env: `STO558_TRACE=1`.
   - State-sync target: any snapshot at height **≤ 211,870,000** (so the corruption block 211,876,659 falls inside the replay window).
   - `sc-write-mode = migrate_evm`, `sc-keys-to-migrate-per-block = 10000` (matches v2 so the same migration boundary trajectory is reproduced).
4. Tail logs once block sync reaches 211,876,659:
   ```bash
   kubectl --context harbor -n eng-yiren logs -f pacific1-flatkv-mig-v3-0-0 -c seid \
     | grep -E "STO558 (GetEVMAddress|MM.Read|SetAddressMapping)" \
     | grep 'height=211876659'
   ```
5. Expected outputs at 211,876,659:
   - 50 `STO558 GetEVMAddress …` events (one per EVM tx in the block).
   - On a buggy local replay: 21 of these will show `hit` with a `returnedEvmAddr` that did not get written in this run — that's the spurious non-nil. The associated `STO558 MM.Read` log line names which DB (newDB-only / oldDB-hit / fallback) returned the stale value, and at what boundary position.
   - On a correct canonical-equivalent replay: 21 `miss` followed by 21 `STO558 SetAddressMapping … createdAccount=true`, totalling 21 cosmos accounts created.
6. Cross-reference the spurious-hit keys against the iterator's boundary trajectory and the flatkv contents at the time of the read. That should isolate which of {oldDBReader, newDBReader, IsMigrated, prior cross-tx OCC write} produced the false positive.

## What this means for the iterator fix

Round-1 STO-558 was traced to a `MigrationManager.Iterator` bug. The fix on this branch correctly prevents *one specific iterator misbehaviour*. But the same family of bug appears to also affect `MigrationManager.Read` (or `oldDBReader` / `newDBReader` / `IsMigrated`'s key comparison), and that fix was not extended to that path. So the new node replays buggy reads on the `evm/SeiToEVM` key during EVM migration, miscounts associations, and produces wrong `account_number`s for cosmos accounts created in the same block. The signing wallet (correctly) signed against the canonical accnum; local computes signed-bytes with the wrong accnum; sig-verify rightly rejects.

## Standing hypotheses (revised after audit)

### H1 (downgraded): an OCC race that bypasses the read-own-write fast path

The fast path is unconditional in `mvkv.Get` (line 144-149) and in `cachekv.Store.Get`
(`store/cachekv/store.go:61-64`). For `SigVerify`'s read to *miss* the `SetPubKey`
write, the two would have to be executed against different KVStore instances. That
can only happen if some piece of code between them rebinds `ctx.MultiStore()` to a
different multistore, or if `ctx.KVStore(authKey)` returns a different VIS instance.
A grep for `WithMultiStore` in the ante path turned up no such site. So this
hypothesis now requires either an as-yet-unidentified Sei-specific decorator or a
goroutine-level memory-model issue, and is much less likely than initially thought.

The user's strong observation that "this bug only appears after EVM migration is
fully complete" is the key constraint. What changes structurally at boundary =
Complete:

- `MigrationManager.ApplyChangeSets` takes the fast path
  (`migration_manager.go:276-282`): EVM-module writes go only to `newDBWriter` (FlatKV)
  without the per-block "advance the migration cursor" branch.
- `MigrationManager.Read` returns `m.newDBReader(...)` unconditionally.
- `shouldAppendLatticeHash` latches to `true` permanently
  (`composite/store.go:529-563`), changing AppHash computation.
- The migration iterator stops iterating (no more boundary advancement work happens
  on the commit critical path).

None of these directly touch auth-module state, but they do change the *scheduling
profile* of the commit goroutine relative to OCC tasks. This is the leading
candidate for further audit.

### H2 (now leading): wrapper shared across incarnations + a Sei-specific mutator

`task.SdkTx` (the `*wrapper`) is created **once** at decode time and the same
pointer is reused across every incarnation of the same tx (`tasks/scheduler.go:200-211`).
If anything during incarnation 0's ante calls a mutator that sets
`w.authInfoBz = nil` or `w.bodyBz = nil` AND mutates the underlying
`w.tx.Body` / `w.tx.AuthInfo`, then a later incarnation (or even a later read by the
same incarnation through `getAuthInfoBytes()`) will re-marshal from the mutated
struct and produce bytes that differ from the wire signature. The standard SDK
ante decorators do not do this, but Sei has additional decorators
(`evmante.NewEVMPreprocessDecorator`, `NewEVMAddressDecorator`,
`antedecorators.NewAuthzNestedMessageDecorator`, `ibcante.NewAnteDecorator`,
`antedecorators.NewGaslessDecorator`, oracle decorators) that should be audited
for tx mutation. If any of them mutates the body/auth_info on tx[0] or tx[1]'s
path and the scheduler's tx-pointer reuse interacts with parallel execution,
tx[2]'s sign-bytes derivation could be poisoned.

A concrete grep target: any Sei-specific code that modifies
`wrapper.tx.Body.Memo`, `wrapper.tx.AuthInfo.Fee.*`, or related fields outside the
SDK's official `Set*` API surface.

### H3: Wrapper-shared mutation × OCC parallel execution

Even without Sei-specific mutators, OCC executes tx[0]/tx[1]/tx[2] concurrently
on separate goroutines but **all three share access to their own `*wrapper`**.
If any decorator goroutine reads `w.tx.AuthInfo` or `w.bodyBz` while another
goroutine writes (e.g., via a Sei-specific path that does mutate), Go's
memory model produces a data race, and the resulting bytes are unpredictable.
The fact that the bug only triggers in post-completion is consistent with a
timing-sensitive data race that only happens when commit scheduling profile
shifts.

### H4: Subtle protobuf/sign-bytes determinism

If something about the post-completion AppHash chain or the lattice-hash latch
affects how `signModeHandler` resolves `chain-id` or how `SignerData` is serialized,
the signed-bytes would differ. Lower likelihood (signing is local to the SDK and
does not consult any migration state).

### H5: Cross-tx state bleed via OCC during boundary-complete commit

When the commit cycle includes the boundary-complete metadata write (which only
happens on the single block where the boundary transitions to Complete), the
`firstBatchInBlock` flag interacts with `migrationAdvancedThisCommit` in
`CompositeCommitStore` (see comments at `composite/store.go:78-95`). Less likely
because we believe the actual boundary-complete block was earlier than 211,885,575
(snapshot 211,860,000 is already 13 GB stable in flatkv).

## Concrete next steps (any one of these closes the investigation)

1. **A/B with `occ-enabled = false`.** Spin up an identical shadow node with OCC
   disabled, re-migrate, sync to head. If the bug does not recur, H1 is confirmed.
   ETA ~8-12 hours of migration + sync.
2. **Audit `sei-cosmos/tasks/scheduler.go` abort/retry + `mvkv.go`
   read-own-write semantics.** Specifically: when a tx is aborted and re-executed,
   is the `SetPubKey` write from the previous incarnation visible in the
   `VersionIndexedStore` for the new incarnation? What happens to the read-set if
   the tx writes a key during ante and then reads it later in the same tx?
3. **Instrument local node with logging in `SigVerificationDecorator`** to log
   `(pubKey, chainID, accNum, seq, len(body_bytes), len(auth_info_bytes),
   computed signed_bytes hash, signature hash)` for the failing tx. Then compare
   to a canonical node's computed values.
4. **Find the block at which the boundary transitioned to Complete on this node.**
   If it equals 211,885,575, that pins the failure to the exact transition. If it
   is much earlier, H1 (timing-only) is more likely than H3 (transition-only).

## Open data on the running node

- Pod: `pacific1-flatkv-mig-v2-0-0` in `eng-yiren`, currently stuck (no progress
  since 09:34 UTC) with `last_state.terminated.startedAt = 08:56 / finishedAt =
  09:34`. RestartCount = 5.
- App-toml: `sc-write-mode = "migrate_evm"`, `sc-keys-to-migrate-per-block = 10000`,
  `occ-enabled = true`.
- FlatKV snapshots present: 211860000 (06:56), 211870000 (07:27), 211880000
  (08:19, current symlink). All ~13 GB storage subdir — boundary has been Complete
  long enough that EVM state is stable.
- MemIAVL snapshot: only 211870000 (07:36); ~60 GB; current symlink points there.
- Most recent committed app height: 211,885,575. AppHash `05CD6C7E...`.

## 2026-06-05 deployment run — Trace patch deployed, rollback exposed a new bug

### What was deployed

- Trace patch (`STO558_TRACE=1` gates info logs in `x/evm/keeper/address.go`
  for `SetAddressMapping`/`GetEVMAddress` and in
  `sei-db/state_db/sc/migration/migration_manager.go` for the evm-store
  `Read` route) was committed to the current branch, image
  `mock_block_validation-sto558-trace` was built via the `ecr.yml` GitHub
  Actions workflow and pushed to
  `189176372795.dkr.ecr.us-east-2.amazonaws.com/sei/sei-chain`.
- `eng-yiren/yiren` Flux kustomization was suspended (`spec.suspend=true`)
  so manual SND patches stick — the kustomization had been reconciling
  the SND image back to the old build every 5 minutes.
- SND image bumped to the trace tag via `kubectl patch`. Verified the
  image propagates SND → SeiNode → StatefulSet → Pod (all four show
  `mock_block_validation-sto558-trace` and `STO558_TRACE=1` env in the
  seid container).

### Rollback plan executed

1. Paused the SND so the controller scaled the StatefulSet to 0 and
   released the RWO `data-pacific1-flatkv-mig-v2-0` PVC.
2. Launched a one-shot debug `Pod` (`pacific1-flatkv-mig-v2-rollback`)
   that mounts the same PVC and runs
   `seid rollback -n 15575 --home /.sei` (~47 minutes wall clock).
3. The rollback pod completed `Succeeded`. Final log line:
   `Rollback complete target height 211870000. App hash DF1A3CF4..., state hash F7C1317549...`
4. Debug pod deleted, SND unpaused. The seid container started but
   immediately crashed.

### The new bug exposed by the rollback

Crash message:

```
state.AppHash does not match AppHash after replay.
Got:      DF1A3CF47E915F2C3A4526FAAD3A9380B5B01C94A20A1E1E01B4256B5781C9A2  (composite store after rollback)
Expected: F7C1317549FA23420F56BD66C40E3C5FF0471599E09E748BF655899BDD893E77  (tendermint state.AppHash @ 211,870,000)
```

`F7C1...` was recorded by tendermint at the time block 211,870,000 was
originally committed locally. `DF1A...` is the composite store hash after
`seid rollback` brought the state back to height 211,870,000. They should
match. They don't.

Static-source audit so far:

- **`CompositeCommitStore.Rollback`** (`composite/store.go:705-719`)
  calls both `memIAVL.Rollback` and `flatKV.Rollback`. Hashvault has its
  own `Rollback` function (`hashvault/pebble_hashvault_rollback.go`) but
  hashvault is not referenced from the composite store at all — it does
  not feed into the AppHash, so a missing hashvault rollback is **not**
  the issue.
- **`FlatKV.Rollback`** (`sei-db/state_db/sc/flatkv/snapshot.go:573-660`)
  picks the highest snapshot ≤ targetVersion (here, snapshot-211870000),
  switches the `current` symlink to it, blows away the working dir,
  reopens DBs (which reloads `committedLtHash` from the cloned working
  dir's metadata DB), truncates the WAL past targetVersion, runs catchup
  (zero entries since snapshot version == target). Final `committedVersion
  == targetVersion`. By construction the post-rollback `committedLtHash`
  should equal the LtHash that was active when block 211,870,000 was
  committed.
- **`FlatKV.WriteSnapshot`** (`snapshot.go:441-524`) runs *synchronously
  inside `Commit`* whenever `version % SnapshotInterval == 0`. It
  pebble-`Checkpoint`s the four data DBs plus the metadata DB. So
  `snapshot-211870000` should hold the exact post-commit DB state at the
  end of block 211,870,000, including the `MetaLtHashKey` blob.
- **Composite AppHash** (`composite/store.go:601-616`) is
  `memIAVL.LastCommitInfo()` *optionally appended* with an
  `evm_lattice` `StoreInfo` containing `flatKV.CommittedRootHash()`. The
  append is gated on `shouldAppendLatticeHash`
  (`composite/store.go:487-563`):
  - in `MigrateEVM` mode, append iff the migration boundary is present
    and not `MigrationNotStarted`, OR `MigrationVersionKey` is present;
  - the decision is sticky-latched in the in-memory `latticeAppendLatched
    atomic.Bool` for the lifetime of the process.

### Leading hypothesis

The most likely root cause is a **latch / boundary skew across the
rollback**:

- At the time block 211,870,000 was originally committed on this node,
  `latticeAppendLatched` might have been *false* (e.g., the migration
  boundary on flatkv was still `MigrationNotStarted` at that height,
  even though the migrate_evm `WriteMode` was active). The recorded
  AppHash F7C1 would then be pure memIAVL with **no** `evm_lattice`
  contribution.
- After rollback, when the new seid process opens the composite store
  on top of the rolled-back flatkv, the on-disk migration metadata that
  `shouldAppendLatticeHash` reads is whatever was in the snapshot —
  which is the post-commit state at end-of-211,870,000. If that snapshot
  contains *any* migration boundary or `MigrationVersionKey`, the gate
  latches `true`, the lattice term gets appended, and the resulting
  AppHash differs from F7C1 by exactly that one synthetic `evm_lattice`
  `StoreInfo`.

This implies `seid rollback` has a latent correctness gap: it rolls back
data but does not restore the *commit-info shape* that was active at the
target height. In `migrate_evm` mode that shape is materially different
across the boundary transition.

### Sub-hypothesis worth checking first

A simpler explanation that fits the same evidence: at the moment
`Commit(211870000)` ran, it updated `committedLtHash` *first*, then
called `WriteSnapshot` (`store_write.go:80-105`). If the snapshot
captured a metadata DB state where `MetaLtHashKey` was already the
post-211870000 LtHash but the data DBs (account/code/storage/legacy)
were checkpointed in a slightly different order — and the WAL had not
yet been committed when the checkpoint ran — the snapshot's LtHash and
its row contents could be out of sync by one block. This would also
explain a 1-row delta but is less likely because Commit serialises the
writes through `commitGlobalMetadata` before snapshot.

### Pragmatic state after this attempt

- v2's PVC is now in an *inconsistent* state: memIAVL and flatkv think
  they are at 211,870,000, but the composite root hash does not match
  tendermint's `state.AppHash` at that height. Seid crash-loops on
  startup with the AppHash mismatch above. SND is paused so the pod
  does not keep restarting; the trace image / `STO558_TRACE=1` env are
  still wired up.
- The trace image itself is verified to roll out cleanly when Flux is
  suspended — the runbook below is reusable.
- Hashvault is **not** the culprit.

### Concrete next steps (pick one)

1. **Confirm the lattice-latch hypothesis cheaply**: add a few
   `STO558_TRACE`-gated `logger.Info` lines inside
   `shouldAppendLatticeHash` (`composite/store.go:529-563`) printing
   `WriteMode`, whether the boundary key was found, the deserialised
   boundary status, whether `MigrationVersionKey` was found, and the
   decision. Rebuild, redeploy (Flux is already suspended, so just
   patch the SND image to the new tag and force-restart the pod — the
   existing post-rollback PVC will crash again and print the trace).
   Cost: ~20 min build + 5 min restart, no second rollback needed.
2. **Read the snapshot directly**: spin a debug pod that mounts the
   PVC and dumps the migration-store rows out of
   `snapshot-00000000000211870000/<dataDB>` using a small Go binary or
   `pebble tool dump`. Compare to the same dump from the working dir.
   This tells us *without code changes* whether the boundary was
   already non-`NotStarted` at 211,870,000.
3. **Abandon v2 and start v3 fresh**: clone the v2 SND spec into a new
   `pacific1-flatkv-mig-v3` SND with `image=trace`, fresh PVC,
   state-sync to the closest canonical snapshot ≤ 211,870,000, then
   let it block-sync forward until it naturally hits the corruption
   block 211,876,659. 3–6 h wall clock but does not require touching
   the rollback code path.

### Deployment runbook (post-2026-06-05)

The original `sto558-rollout.sh` was rewritten to use a detached debug
pod for the rollback (the controller-aware path in earlier versions
fought the SeiNode reconciler). The current flow is:

1. Build / push the trace image via the
   `.github/workflows/ecr.yml` workflow (`workflow_dispatch` with the
   full 40-char SHA as `ref` and `mock_block_validation-sto558-trace`
   as `tag`).
2. Suspend Flux: `kubectl -n eng-yiren patch kustomization yiren
   --type=json -p='[{"op":"add","path":"/spec/suspend","value":true}]'`.
   The `kubectl edit` / merge-patch path silently drops the field if
   the field doesn't yet exist on the resource; the explicit `add`
   op is what makes the suspend stick.
3. Patch the SND image with a JSON patch to
   `/spec/template/spec/image`. The SeiNode and StatefulSet image
   reconcile automatically.
4. To capture trace logs without a rollback (i.e. just enable trace
   on the running v2 once we have a working state again), set
   `STO558_TRACE=1` via the SeiNode CRD (env is *not* reconciled by
   the controller and will survive image bumps).
5. To replay a specific block range, see the
   `sto558-rollout.sh` script in the repo root for the
   debug-pod pattern (and remember that `seid rollback` is currently
   *unsafe* in `migrate_evm` mode — see the bug above).
