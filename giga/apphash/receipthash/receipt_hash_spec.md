# Giga Receipt Hash

**Status:** draft. **Depends on:** [Giga App Hash](../apphash_spec.md), schema version 1.

This document defines `receiptHash`, the field of `appHashData` in the
[App Hash specification](../apphash_spec.md#the-app-hash) that commits to the transaction receipts
produced by executing the block.

It defines a commitment scheme only — what `receiptHash` is a hash of, and how an individual
receipt or log can be proven against it. It does not define how receipts are stored, pruned, or
served over RPC; that remains the unmodified concern of the existing receipt store
(`sei-db/ledger_db/receipt/`). A field appears in this commitment only if it is an execution outcome
that cannot be derived from a transaction plus already-committed prior state without executing it;
see [Rationale](#rationale-non-normative) for the complete reasoning.

This document defines receipt hash version 1. The version is committed in `receiptHash` itself, so a
future change to anything here ships as a new version.

## Notation

In addition to the notation defined in the [App Hash specification](../apphash_spec.md#notation):

- `u8(x)` is `x` as a single unsigned byte.
- A *tag* is a single domain-separation byte prefixed onto data before it is hashed.

## Merkle tree construction (MTH)

Both trees this document defines — the per-receipt logs tree and the per-block receipts tree — use
the BUD tree construction of the [BUD specification](../bud/bud_spec.md#bud-tree), over an ordered
list of byte-string entries `D = [d_0, …, d_{n-1}]` in place of serialized budlets:

```
leafHash(d)            = SHA-256(u8(0x00) ‖ d)
innerHash(left, right) = SHA-256(u8(0x01) ‖ left ‖ right)
MTH(D)                 = the BUD tree root over leafHash(d_0) … leafHash(d_{n-1})
```

`MTH({})` is 32 zero bytes; otherwise `MTH` is RFC 6962's Merkle Tree Hash (§2.1). The two trees
share their tags: the receipts tree holds a logs tree only as its root, embedded as 32 opaque bytes
inside `receiptPreimage`, so no node of one tree can stand in for a node of the other.

## `receiptHash`

```
receiptHash(N) = SHA-256("sei-receipts" ‖ u8(version) ‖ u64be(n) ‖ MTH([ receiptPreimage(r) for r in receipts(N) ]))
```

`"sei-receipts"` is a domain separation tag: its 12 ASCII bytes, with no terminator or length prefix.
`version` is `1`, the receipt hash version this document defines. `n` is the number of receipts in
`receipts(N)`, committed so that a proof cannot claim a different count and index for the same root
(see [Rationale](#logcount)).

`receipts(N)` holds the receipts of block `N`'s transactions that consumed a nonce, in block order. A
transaction that execution rejected without running it (a spent or unreachable nonce, a sender that
cannot pay, or no block gas left) consumed no nonce and has no receipt here, so a transaction hash
appears in at most one receipt. Each entry fed to the tree is:

```
receiptPreimage(r) = u8(r.status) ‖ r.txHash ‖ varint(r.blockIndex) ‖ varint(r.gasUsed) ‖ varint(r.logCount) ‖ logsRoot(r)
```

| Field | Encoding |
|---|---|
| `status` | `u8`; `0` = failed, `1` = success |
| `txHash` | raw bytes; the hash of the receipt's transaction |
| `blockIndex` | `varint`; the transaction's position in block `N`'s transaction list, counting every transaction, rejected ones included |
| `gasUsed` | `varint`; gas used by this transaction alone, not cumulative across the block |
| `logCount` | `varint`; `len(r.logs)` — see [Rationale](#logcount) for why this is committed here |
| `logsRoot` | raw bytes, see below |

`blockIndex`, `gasUsed`, and `logCount` are LEB128, not fixed-width, matching the same choice
`blockHash` already makes for `totalGasWanted`/`totalGasEstimated`/`block_number` in the App Hash
specification: these
figures are almost always far smaller than 2⁶⁴, so a realistic value fits in a few bytes instead of
a fixed-width field. `varint` is self-delimiting — every byte but the last has its continuation bit
set, and that last byte (continuation bit clear) is *included* in the value it terminates, not a
separator before the next field. So each varint runs through and including its first
clear-continuation-bit byte, the next field starts at the byte immediately after that, and
`logsRoot` starts immediately after `logCount`'s varint ends. `receiptPreimage` is therefore not a
fixed size: it is 68 to 95 bytes — 1 (`status`) plus 32 (`txHash`) plus 1 to 10 for each of the
three varints, per the App Hash specification's own bound on `varint`, plus 32 (`logsRoot`). It is
not itself a leaf hash — `MTH` applies the leaf tag (`0x00`) when hashing it.

### `logsRoot`

```
logsRoot(r) = MTH([ logLeaf(l) for l in r.logs, in emission order ])
```

A receipt that emits no logs has `logsRoot(r)` of 32 zero bytes — the `n = 0` case above. Verifying a
single log against `logsRoot` in isolation (without the rest of the logs) requires an authenticated
`len(r.logs)`; that value is committed as `logCount` in `receiptPreimage`, not derived here (see
[Rationale](#logcount)).

### `logLeaf`

```
logLeaf(l)  = l.address ‖ u8(l.topicCount) ‖ topics(l) ‖ l.data

topicCount  = len(l.topics), always 0-4 by EVM LOG0-LOG4 semantics
topics(l)   = pad(l, 0) ‖ pad(l, 1) ‖ pad(l, 2) ‖ pad(l, 3)
pad(l, i)   = l.topics[i]     if i < l.topicCount
            = 32 zero bytes   otherwise
```

| Offset | Size | Field | Encoding |
|---:|---:|---|---|
| 0 | 20 | `address` | raw bytes; the account that emitted the log |
| 20 | 1 | `topicCount` | `u8`; `0`–`4` |
| 21 | 128 | `topics` | 4 × 32 bytes; zero-padded past `topicCount` |
| 149 | variable | `data` | raw bytes; runs to the end of the leaf's input |

`logLeaf` is not itself a leaf hash — `MTH` applies the leaf tag (`0x00`) when hashing
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
logsRoot({})                       = 0000000000000000000000000000000000000000000000000000000000000000
```

**Receipt 0** — `status` 1, `txHash` 32 bytes of `0xaa`, `blockIndex` 0 (`varint` = `00`),
`gasUsed` 21000 (`varint` = `88a401`), `logCount` 2 (`varint` = `02`), `logsRoot` as computed above
from the two logs.

```
receiptPreimage(0) = 01 aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa 00 88a401 02 c6ab01a590f123c121f3dad8fcc5e8a59ef73156492f384931e0158cd6d12e4e
```

**Receipt 1** — `status` 0 (failed), `txHash` 32 bytes of `0xbb`, `blockIndex` 2 (`varint` = `02`;
the transaction at position 1 was rejected, so it has no receipt), `gasUsed` 250000
(`varint` = `90a10f`), `logCount` 0 (`varint` = `00`), no logs.

```
receiptPreimage(1) = 00 bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb 02 90a10f 00 0000000000000000000000000000000000000000000000000000000000000000
```

**Receipt 2** — `status` 1, `txHash` 32 bytes of `0xcc`, `blockIndex` 3 (`varint` = `03`),
`gasUsed` 50000 (`varint` = `d08603`), `logCount` 1 (`varint` = `01`), and Log 1 as its only log,
so `logsRoot` is the leaf hash of `logLeaf(1)`.

```
receiptPreimage(2) = 01 cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc 03 d08603 01 17cfcd0320b8675c9e08a6306059ce867f9e113f50c52f987a81cc2abdc4d71c
```

```
receiptHash({})                                                           = e040c5d218b18c10ff1b8fb7660cb7779e69bd3a482c9717c4815b87b6035b88
receiptHash({receiptPreimage(0)})                                         = 32b95177fd02367881c7fbadad25ace4c913704bfb0461a4933801f6707f84c8
receiptHash({receiptPreimage(0), receiptPreimage(1)})                     = 78ba037d0b37247d421b702148ccdb97b66b95ab98190ec5dc017d9b12f4b59f
receiptHash({receiptPreimage(0), receiptPreimage(1), receiptPreimage(2)}) = 22b544a23d66ff07ce814e6e151ad46c8d9d490699af6b5091ceccfaa9573d20
```

(All three preimages happen to be 70 bytes here — each `blockIndex` and `logCount` needs 1 LEB128
byte and each `gasUsed` needs 3 — so the lengths coincide even though the field values differ. That's
an artifact of these particular example values, not a general property: `receiptPreimage` ranges
from 68 to 95 bytes, and `MTH` handles that variation the same as it handles any other
variable-length tree entry.)

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
| `logs_bloom` | a pure, deterministic function of the logs already committed via `logsRoot` |
| `error_code` | a diagnostic refinement of `status`; nothing is identified as needing to cryptographically prove *which* failure occurred, only that one did |

`status`, `gasUsed`, and the logs themselves are the only fields that fail this test in the
*include* direction — none are derivable without executing, which is the entire reason this
commitment needs to exist at all.

`txHash` and `blockIndex` identify the transaction a receipt belongs to. `receipts(N)` leaves out
rejected transactions, and which transactions were rejected is itself only known by executing, so
a receipt's position in the tree does not identify its transaction. `txHash` identifies it without
the block; `blockIndex` places it in the block's transaction list, which can hold the same
transaction more than once.

No version byte appears in either preimage: `receiptHash` commits the version once, so a
per-receipt version tag would be redundant. The stored receipt format keeps its own, separate versioning (`receiptDataV1` in
`sei-db/ledger_db/receipt/codec.go`) — a storage concern, decoupled from this document.

### `logCount`

Yes for the logs tree, committed as `logCount` in `receiptPreimage` — reversing an earlier draft of
this document, which argued it wasn't needed. That argument conflated two separate justifications
for authenticating a Merkle tree's size, and only addressed one of them.

The one it addressed: RFC 6962 (Certificate Transparency) binds `(tree_size, root_hash)` via its
Signed Tree Head's *signature*, not by hashing size into the root. CT needs that pairing to catch a
log server showing different parties divergent histories over time (a "split view") — and this
document doesn't have that problem, since a given receipt's `logsRoot` is fixed and immutable the
instant it's produced.

The one it missed: an RFC 6962 Merkle audit path (§2.1.1) does not, by itself, authenticate the tree
size or leaf index it's checked against — those are told to the verifier as ordinary, unauthenticated
inputs. A party who already holds every log in a receipt (which a prover constructing a proof
generally does) can reuse real internal-node hashes from the true tree as "sibling" values and
present them against a *smaller, false* claimed size and a *different* claimed index. The
verification arithmetic can still land on the true `logsRoot`, with no hash collision required,
because nothing in that arithmetic checks that the claimed size is the *only* size consistent with
the root. That lets a prover misstate which position a log sits at, or how many logs a receipt
emitted — exactly the kind of claim this commitment exists to make unforgeable.

This is fixable either by committing the count directly, or by pointing to an already-authenticated
source of it elsewhere. Neither tree has such a source, so both commit their counts directly. The
receipts tree's count is `n` in `receiptHash`: `receipts(N)` leaves out rejected transactions, so
the block's transaction count that `blockHash` commits is not its size. The logs tree's count is
`logCount`, in `receiptPreimage`: nothing else commits how many logs a given transaction emitted.

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
- `giga/apphash/bud/bud_spec.md` — the BUD tree construction both trees use
- `sei-db/ledger_db/receipt/receipt_store.go`, `litt_receipt_store.go`, `codec.go` — the existing,
  unmodified receipt store
- `proto/evm/receipt.proto`, `x/evm/types/receipt.pb.go` — the current stored receipt encoding,
  unaffected by this document
- RFC 6962 (Certificate Transparency) §2.1 — Merkle Tree Hash construction; §2.1.1 — Merkle Audit
  Path (does not itself authenticate tree size or leaf index); §3.2 — Signed Tree Head size/root
  binding
