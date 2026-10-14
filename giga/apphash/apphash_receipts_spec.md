# Giga Receipt Hash

**Status:** draft. **Depends on:** [Giga App Hash](apphash_spec.md), schema version 1.

This document defines `receiptHash`, the field of `appHashData` marked **PLACEHOLDER** in the
[App Hash specification](apphash_spec.md#the-app-hash): *"A commitment to the transaction receipts
produced by executing the block. Needs design. Its definition is owned by the EVM team and is not
specified here."* This document is that definition.

It defines a commitment scheme only — what `receiptHash` is a hash of, and how an individual
receipt or log can be proven against it. It does not define how receipts are stored, pruned, or
served over RPC; that remains the unmodified concern of the existing receipt store
(`sei-db/ledger_db/receipt/`). A field appears in this commitment only if it is an execution outcome
that cannot be derived from a transaction plus already-committed prior state without executing it;
see [Rationale](#rationale-non-normative) for the complete reasoning.

This document has no schema version of its own. The algorithm it defines is tied 1:1 to the App
Hash schema version in effect for the block (`appHashData.version`); a future change to anything
here can only ship by riding a bump to that version.

## Notation

In addition to the notation defined in the [App Hash specification](apphash_spec.md#notation):

- `u8(x)` is `x` as a single unsigned byte.
- A *tag* is a single domain-separation byte prefixed onto data before it is hashed.

## Merkle tree construction (MTH)

Both trees this document defines — the per-receipt logs tree and the per-block receipts tree — use
the same construction, parameterized by a pair of domain tags `(tagLeaf, tagNode)`, over an ordered
list of byte-string entries `D = [d_0, …, d_{n-1}]`:

```
MTH(tagLeaf, tagNode, {})      = SHA-256()                                       for n = 0
MTH(tagLeaf, tagNode, {d_0})   = SHA-256(tagLeaf ‖ d_0)                           for n = 1
MTH(tagLeaf, tagNode, D[0:n])  = SHA-256(tagNode ‖ MTH(D[0:k]) ‖ MTH(D[k:n]))      for n > 1
```

where, for `n > 1`, `k` is the largest power of two strictly less than `n`. This is RFC 6962's
Merkle Tree Hash (§2.1), applied with caller-chosen domain tags in place of RFC 6962's fixed
`0x00`/`0x01`, so that no leaf or internal-node hash from one tree instance can ever be mistaken for
one from the other. The empty-tree case (`n = 0`) uses no tag — `SHA-256()`, the hash of the empty
string — per RFC 6962's own definition, unmodified.

This document uses two tag pairs:

| Tree | `tagLeaf` | `tagNode` |
|---|---|---|
| Logs tree (per receipt) | `0x00` | `0x01` |
| Receipts tree (per block) | `0x02` | `0x03` |

> This unique-per-tree domain separation may be more than is needed. A logs tree's root never
> enters the receipts tree as a tree element in its own right — it is embedded as 32 opaque bytes
> inside `receiptPreimage`, which then gets its own leaf hash under the receipts tree's own tags —
> so there may be no path for a hash from one tree to be mistaken for one from the other even if
> both reused the same `0x00`/`0x01` pair. Reusing a single shared pair would also let this
> commitment be built directly on `sei-tendermint/crypto/merkle.HashFromByteSlices`, Sei's own
> existing RFC 6962 implementation (leaf `0x00`, node `0x01`, SHA-256, the same largest-power-of-two
> split used here), instead of a bespoke construction.

## `receiptHash`

```
receiptHash(N) = MTH(0x02, 0x03, [ receiptPreimage(r) for r in receipts(N), in transaction order ])
```

`receipts(N)` is block `N`'s list of transaction receipts. Each entry fed to the tree is:

```
receiptPreimage(r) = u8(r.status) ‖ varint(r.gasUsed) ‖ logsRoot(r)
```

| Field | Encoding |
|---|---|
| `status` | `u8`; `0` = failed, `1` = success |
| `gasUsed` | `varint`; gas used by this transaction alone, not cumulative across the block |
| `logsRoot` | raw bytes, see below |

`gasUsed` is LEB128, not `u64be`, matching the same choice `blockHash` already makes for
`totalGasWanted`/`totalGasEstimated` in the App Hash specification: gas figures are almost always
far smaller than 2⁶⁴, so a realistic value fits in 3–4 bytes instead of a fixed 8. `varint` is
self-delimiting — every byte but the last has its continuation bit set — so `logsRoot` needs no
separate marker for where `gasUsed` ends; it simply starts at the first byte whose continuation bit
is clear. `receiptPreimage` is therefore not a fixed size: it is 34 to 43 bytes, 1 (`status`) plus 1
to 10 (`varint(gasUsed)`, per the App Hash specification's own bound on `varint`) plus 32
(`logsRoot`). It is not itself a leaf hash — `MTH` applies the receipts tree's leaf tag (`0x02`) when
hashing it.

### `logsRoot`

```
logsRoot(r) = MTH(0x00, 0x01, [ logLeaf(l) for l in r.logs, in emission order ])
```

A receipt that emits no logs has `logsRoot(r) = SHA-256()` — the `n = 0` case above. Total log
count is not part of this commitment: a verifier holding the full log list recovers it for free as
`len(r.logs)`, and a verifier checking a single log in isolation needs it only as ordinary metadata
supplied alongside the compact proof, not as consensus-committed data (see
[Rationale](#do-we-need-log_count)).

### `logLeaf`

```
logLeaf(l) = l.address ‖ u8(l.topicCount) ‖ topics(l) ‖ l.data

topics(l)  = pad(l.topics, 0) ‖ pad(l.topics, 1) ‖ pad(l.topics, 2) ‖ pad(l.topics, 3)
pad(t, i)  = t[i]             if i < len(t)
           = 32 zero bytes    otherwise
```

| Offset | Size | Field | Encoding |
|---:|---:|---|---|
| 0 | 20 | `address` | raw bytes; the account that emitted the log |
| 20 | 1 | `topicCount` | `u8`; `0`–`4` |
| 21 | 128 | `topics` | 4 × 32 bytes; zero-padded past `topicCount` |
| 149 | variable | `data` | raw bytes; runs to the end of the leaf's input |

`logLeaf` is not itself a leaf hash — `MTH` applies the logs tree's leaf tag (`0x00`) when hashing
it. `data` carries no explicit length field: it is the sole variable-length component and it is
positioned last, so its extent is unambiguous from the preimage alone (see
[Rationale](#e-no-length-field-for-data)). `topicCount` is the sole authority on how many of the
four `topics` slots are real; zero-padding beyond it is never content-guessed.

## Test vectors

**Log 0** — `address` is 20 bytes of `0x11`, `topicCount` is 1, `topics[0]` is 32 bytes of `0x22`
(`topics[1..3]` zero), `data` is `beef`.

```
logLeaf(0) = 1111111111111111111111111111111111111111
             01
             2222222222222222222222222222222222222222222222222222222222222222
             0000000000000000000000000000000000000000000000000000000000000000
             0000000000000000000000000000000000000000000000000000000000000000
             0000000000000000000000000000000000000000000000000000000000000000
             beef
```

**Log 1** — `address` is 20 bytes of `0x33`, `topicCount` is 0, `data` is empty.

```
logLeaf(1) = 3333333333333333333333333333333333333333
             00
             0000000000000000000000000000000000000000000000000000000000000000
             0000000000000000000000000000000000000000000000000000000000000000
             0000000000000000000000000000000000000000000000000000000000000000
             0000000000000000000000000000000000000000000000000000000000000000
             
```

```
logsRoot({logLeaf(0), logLeaf(1)}) = c6ab01a590f123c121f3dad8fcc5e8a59ef73156492f384931e0158cd6d12e4e
logsRoot({})                       = e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855
```

**Receipt 0** — `status` 1, `gasUsed` 21000 (`varint` = `88a401`), `logsRoot` as
computed above from the two logs.

```
receiptPreimage(0) = 01 88a401 c6ab01a590f123c121f3dad8fcc5e8a59ef73156492f384931e0158cd6d12e4e
```

**Receipt 1** — `status` 0 (failed), `gasUsed` 250000 (`varint` = `90a10f`), no logs.

```
receiptPreimage(1) = 00 90a10f e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855
```

```
receiptHash({receiptPreimage(0)})                     = 8a260cd83fbaad29b2d7431933769fb1b9f0a9d5a3446e16144a567613ccd77e
receiptHash({receiptPreimage(0), receiptPreimage(1)}) = a9b25b60ed28b119d076dd14e007bb383442a045b0e01ad2b1defe68773aae69
```

(The single-receipt case above intentionally equals `MTH`'s `n = 1` leaf hash of `receiptPreimage(0)`
directly — a consistency check anyone re-implementing this can verify independently. Both
`receiptPreimage` entries above happen to be the same length, 36 bytes, because both 21000 and
250000 need exactly 3 LEB128 bytes — `1 + 3 + 32`. That's a coincidence of these two example values,
not a general property: `receiptPreimage` ranges from 34 to 43 bytes depending on `gasUsed`, and
`MTH` handles that variation the same as it handles any other variable-length tree entry.)

## Rationale (non-normative)

The remainder of this document is not part of the specification — it records why the scheme above
was chosen, for anyone evaluating or revising it later.

### Why these fields, and not others

A field belongs in the commitment if and only if it is an execution outcome that cannot be derived
from the transaction plus already-committed prior state without actually running the EVM. Everything
below fails that test and is deliberately excluded as implied:

| Field | Why it's implied |
|---|---|
| `tx_type`, `to` | literal fields of the transaction itself, already covered by the block's committed transaction list (`blockHash`) |
| `from` | ECDSA-recoverable from the transaction's own signature |
| `effective_gas_price` | a pure function of the transaction's fee fields and the block's base fee |
| `contract_address` (CREATE) | deterministic from `from` and the sender's nonce (or `from`+salt+initcode-hash for CREATE2) — pure arithmetic over the transaction plus `stateHash`'s pre-state |
| `tx_hash` | implied by the receipt's position matching the transaction list's position |
| `logs_bloom` | a pure, deterministic function of the logs already committed via `logsRoot` |
| `error_code` | a diagnostic refinement of `status`; nothing is identified as needing to cryptographically prove *which* failure occurred, only that one did |

`status`, `gasUsed`, and the logs themselves are the only fields that fail this test in the
*include* direction — none are derivable without executing, which is the entire reason this
commitment needs to exist at all.

No version byte appears in either preimage for the same reason given in the opening section: the
algorithm is governed by `appHashData.version`, so an independent per-receipt version tag would be
redundant. The stored receipt format keeps its own, separate versioning (`receiptDataV1` in
`sei-db/ledger_db/receipt/codec.go`) — a storage concern, decoupled from this document.

### Do we need `log_count`?

No. There are two ways anyone checks a receipt's logs. **Full reveal**: a verifier has the whole
list and checks it against `logsRoot` — the count falls out for free as `len(logs)`, and a different
count producing the same root would require an actual hash collision. **Compact proof**: a verifier
checks one log in isolation, needing the total count as an input to interpret the audit path's
shape — but that is ordinary metadata supplied alongside the proof, not consensus data. This is
exactly how RFC 6962 (Certificate Transparency) treats tree size: its Signed Tree Head binds
`(tree_size, root_hash)` via a *signature*, not by hashing size into the root, and CT needs that
pairing only to catch a log server showing different parties divergent histories over time (a
"split view"). This document has no such problem — a given receipt's `logsRoot` is fixed and
immutable the instant it is produced, authenticated once via the block's app hash. A prover who
lies about the count when constructing a proof simply computes the wrong candidate root under the
wrong tree shape, and verification fails; that mismatch is the check.

### Deviations from Ethereum, and why

Ethereum's receipt (`core/types.Receipt`) and its receipts-trie commitment are the natural reference
point. This design departs from it in five places.

#### (a) `gasUsed`, not a cumulative counter

Ethereum's receipt commits the running gas total across the whole block, so every receipt's
commitment depends on knowing the final gas totals of every transaction before it — a hard
sequential dependency; transaction *i*'s receipt cannot be hashed until `0..i-1` are fully ordered
and totaled. Per-transaction `gasUsed` has no such dependency: a receipt can be hashed the moment
its own transaction finishes executing, regardless of what happens before or after it in the block.
That serves parallel, multi-proposer block building directly — receipts can be produced and hashed
concurrently across execution lanes, or by multiple proposers assembling parts of a block at once,
with no serialization point waiting on final ordering. A verifier wanting the Ethereum-style running
total can still reconstruct it by summing `gasUsed` across receipts in transaction order; nothing is
lost, it is simply no longer a *blocking* dependency for commitment. `gasUsed` is varint-encoded
rather than fixed-width for the same reason it is varint-encoded in `blockHash`: it keeps every
receipt leaf closer to its true information content, which matters more here than it would on a
low-throughput chain.

#### (b) Drop `logs_bloom`

Nobody depends on a committed bloom value for correctness — it is a probabilistic filtering
convenience, not an execution outcome, and it is entirely reconstructible from the logs themselves.
An indexer that wants one can derive it in a single pass over data already committed via `logsRoot`;
there is no reason to pay for committing a field that is a pure, cheap function of data already
present.

#### (c) Logs as their own Merkle tree, not a flat hash or a bloom

The one place this design adds structure rather than removing it: a flat hash over all of a
receipt's logs can only prove "this is the exact right whole set," never one log in isolation.
Receipt (and log) data is meant to move between storage and DA planes that will not all hold the
full set, so individual log provability is the actual goal — hence a tree, not a hash.

#### (d) Fixed `topicCount` and always-four topic slots

EVM logs carry 0–4 topics by opcode semantics (`LOG0`–`LOG4`); Ethereum encodes them as a
variable-length list. Reserving all four slots unconditionally means topic *k* sits at the same
fixed byte offset in every log record, regardless of how many topics that particular log actually
used. That matters for downstream filtering: code scanning for a given event signature
(conventionally `topics[0]`) seeks directly to a known fixed offset across every record, instead of
first decoding a variable-length list to find where it starts.

#### (e) No length field for `data`

Ethereum's receipt-trie encoding (RLP) carries explicit lengths throughout, because RLP is a
general-purpose, randomly-decodable serialization. A hash preimage does not need that: `data` is the
sole variable-length component of `logLeaf` and it is positioned last, so there is no boundary
ambiguity to resolve by position alone. Hashing also always produces a fixed-size digest regardless
of input length, so the digest itself already reflects the data's extent — a verifier checking a
revealed log's data re-hashes whatever bytes are actually revealed and compares to the leaf hash;
there is no separate expected-length check to perform.

## References

- `giga/apphash/apphash_spec.md` — the parent App Hash specification this document fills in
- `sei-db/ledger_db/receipt/receipt_store.go`, `litt_receipt_store.go`, `codec.go` — the existing,
  unmodified receipt store
- `proto/evm/receipt.proto`, `x/evm/types/receipt.pb.go` — the current stored receipt encoding,
  unaffected by this document
- RFC 6962 (Certificate Transparency) §2.1 — Merkle Tree Hash construction; §3.2 — Signed Tree Head
  size/root binding
