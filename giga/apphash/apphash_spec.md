# Giga App Hash

**Status:** draft. **Schema version:** 1.

This document defines the app hash that Giga validators compute for each executed block and agree on through
Autobahn. It specifies what goes into the app hash and how those inputs are combined. Several inputs are
currently placeholders that need further design; each one is marked **PLACEHOLDER**, and they are collected in
[Open design work](#open-design-work).

## Notation

- `‖` is byte concatenation.
- `u64be(x)` is `x` as an 8-byte unsigned big-endian integer.
- `u32le(x)` is `x` as a 4-byte unsigned little-endian integer.
- `SHA-256` is FIPS 180-4 SHA-256, and `BLAKE3-256` is unkeyed BLAKE3 with a 32-byte output.
- A *hash* is exactly 32 bytes.

## The app hash

The app hash of block `N` is

```
appHash(N) = SHA-256("sei-apphash" ‖ serialize(appHashData(N)))
```

`"sei-apphash"` is a domain separation tag: its 11 ASCII bytes, with no terminator or length prefix. It is part of
the hash input only, not of the serialization.

`appHashData(N)` holds eight fields. Its serialization is the fields in the order below, with no padding,
separators, or length prefixes, for a fixed total of 177 bytes.

| Offset | Size | Field             | Encoding  | Status                                   |
|-------:|-----:|-------------------|-----------|------------------------------------------|
|      0 |    1 | `version`         | `u8`      | Defined                                  |
|      1 |    8 | `chainID`         | `u64be`   | Defined                                  |
|      9 |    8 | `blockHeight`     | `u64be`   | Defined                                  |
|     17 |   32 | `blockHash`       | raw bytes | **PLACEHOLDER**                          |
|     49 |   32 | `stateHash`       | raw bytes | Defined                                  |
|     81 |   32 | `bud`             | raw bytes | Defined                                  |
|    113 |   32 | `receiptHash`     | raw bytes | **PLACEHOLDER**                          |
|    145 |   32 | `previousAppHash` | raw bytes | Defined, except for the activation block |

A decoder reads `version` from the first byte and rejects any version it does not support. It then rejects
any input that is not exactly the length that version defines, which is 177 bytes for version 1.

### Versioning

`version` identifies this specification. It is bumped on any change to the serialization and on any change to
how any field is computed, including when a placeholder below is replaced by its real definition. A block's
app hash is always computed under the version in effect at that block.

## Fields

### `version`

The schema version. This document defines version `1`.

### `chainID`

The EVM chain ID of the chain the block belongs to.

### `blockHeight`

The Autobahn global block number of the block.

### `blockHash`: PLACEHOLDER

The hash that identifies the block. For now, this is the hash Autobahn assigns to the block's lane
`BlockHeader`, which is SHA-256 over that header's canonical encoding and is the block hash execution already
uses. The header contains:

- `lane_id`: the lane that produced the block.
- `block_number`: the block's number within its lane, which is not `blockHeight`.
- `parent_hash`: the hash of the previous header in the same lane.
- `payload_hash`: SHA-256 over the canonical encoding of the block's payload, which holds the block's
  transactions in block order, the time the block was created, and its total gas wanted and total gas estimated.

**Needs design.** Which header this commits to, which fields that header covers, and its exact byte encoding
must be specified precisely and intentionally. This definition is owned by the consensus team.

### `stateHash`

A commitment to the full live EVM state after executing the block. It is the BLAKE3-256 checksum of a lattice
hash over every entry in the FlatKV live state database.

**The lattice.** A lattice value `L` is a vector of 1024 unsigned 16-bit limbs `L[0] … L[1023]`. Lattice values
are added and subtracted limb by limb, modulo 2^16. The zero vector is the identity.

**Leaves.** A key-value entry `(k, v)` with non-empty `k` and non-empty `v` maps to the lattice value

```
leaf(k, v)[i] = u16le(X[2i], X[2i+1])     for i in 0 … 1023
X             = the first 2048 bytes of the BLAKE3 extendable output of
                u32le(len(k)) ‖ k ‖ u32le(len(v)) ‖ v
```

where `u16le(a, b) = a + 256·b`. An entry with an empty key or an empty value contributes nothing.

**The global lattice value.** `G(N)` is the sum of `leaf(k, v)` over every entry `(k, v)` held after block `N`
in FlatKV's four data stores: `account`, `code`, `storage`, and `misc`. Keys beginning with the ASCII prefix
`_meta/` are store metadata and are excluded. Because the sum is commutative, it does not depend on which store
holds an entry or on the order in which entries are summed.

**Checksum.**

```
stateHash(N) = BLAKE3-256(G(N)[0] as u16le ‖ G(N)[1] as u16le ‖ … ‖ G(N)[1023] as u16le)
```

that is, BLAKE3-256 over the 2048-byte little-endian serialization of `G(N)`. For the empty state, `G` is the zero
vector and `stateHash` is BLAKE3-256 of 2048 zero bytes.

**Keys and values.** `k` and `v` are FlatKV's physical key and stored value encodings. These encodings are part
of the consensus definition, and a change to either is a change to `stateHash`. Every stored value includes the
height of the block that last wrote it, so writing a value changes `stateHash` even when the written contents
equal the previous contents. `stateHash` therefore commits to which entries each block wrote, not only to the
resulting state.

*Non-normative:* implementations need not recompute the sum. They can carry `G(N-1)` forward and, for each
entry the block writes, subtract the leaf of its previous value (if any) and add the leaf of its new value
(unless it was deleted).

### `bud`

The Block Update Digest of the block: a commitment to the key-value changes produced by executing it. It is
defined by the [BUD specification](../../sei-db/bud/bud_spec.md), which also defines the BUD proofs and BUD
state proofs built against it.

### `receiptHash`: PLACEHOLDER

A commitment to the transaction receipts produced by executing the block.

**Needs design.** Its definition is owned by the EVM team and is not specified here.

### `previousAppHash`

The app hash of block `blockHeight - 1`. This chains every app hash to the full history before it.

For the first block whose app hash is computed under this scheme, `previousAppHash` is 32 zero bytes.

**Needs design.** The activation height, i.e. that first block, is not yet defined. It is not expected to be
block 0, since the chain already exists without this app hash.

## Open design work

| Item                         | Needed                                                           | Owner          |
|------------------------------|------------------------------------------------------------------|----------------|
| `blockHash`                  | A precise, intentional definition of the header and its encoding | Consensus team |
| `receiptHash`                | A full definition                                                | EVM team       |
| `previousAppHash` activation | The height of the first block computed under this scheme         | TBD            |

## Test vector

| Field             | Value                               |
|-------------------|-------------------------------------|
| `version`         | `1`                                 |
| `chainID`         | `0x1112131415161718`                |
| `blockHeight`     | `0x0102030405060708`                |
| `blockHash`       | 32 bytes of `0xa1`                  |
| `stateHash`       | 32 bytes of `0xb2`                  |
| `bud`             | 32 bytes of `0xc3`                  |
| `receiptHash`     | 32 bytes of `0xd4`                  |
| `previousAppHash` | 32 bytes of `0xe5`                  |

Serialization (177 bytes, hex, wrapped for readability):

```
01
1112131415161718
0102030405060708
a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1
b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2
c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3
d4d4d4d4d4d4d4d4d4d4d4d4d4d4d4d4d4d4d4d4d4d4d4d4d4d4d4d4d4d4d4d4
e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5
```

App hash, i.e. SHA-256 of `"sei-apphash"` followed by the serialization:

```
5cc80f7617e8286151501cfccacbf5bc8b60826b7e224832519b84bdfef6c533
```

Verified by [`TestSerializeLayout`](app_hash_test.go) (serialization) and
[`TestHashGoldenVector`](app_hash_test.go) (app hash).
