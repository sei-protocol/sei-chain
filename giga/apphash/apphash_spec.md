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
- `varint(x)` is the unsigned LEB128 encoding of `x`: little-endian base-128 digits, each in a byte whose high bit
  is set on every byte but the last. It is 1 to 10 bytes long, and `varint(0)` is the single byte `0x00`.
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
|     17 |   32 | `blockHash`       | raw bytes | Defined                                  |
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

### `blockHash`

The hash Autobahn assigns to the block's lane header, and the block hash execution exposes to the EVM.

```
blockHash(N) = SHA-256(encodeHeader(header(N)))
```

The header contains these fields, in the order they are encoded:

| Field          | Encoding      | Meaning                                                                                                |
|----------------|---------------|--------------------------------------------------------------------------------------------------------|
| `block_number` | `varint(u64)` | The block's number within its lane, from 0. It is not `blockHeight`, which is the global block number. |
| `parent_hash`  | 32 bytes      | Hash of the previous header in the same lane, or 32 zero bytes when `block_number` is 0.               |
| `payload_hash` | 32 bytes      | SHA-256 over the encoding of the block's payload, defined below.                                       |
| `validator`    | 32 bytes      | Ed25519 public key of the validator whose lane produced the block.                                     |
| `joined`       | `varint(u64)` | Epoch in which that validator most recently joined the committee.                                      |

`validator` and `joined` together are `laneID`, the lane that produced the block. They are encoded last.

The payload holds:

- `createdAt`: the time the producer created the block, split into `seconds`, the Unix seconds, and `nanos`,
  the sub-second part in nanoseconds (`0 … 999,999,999`).
- `txs`: the block's transactions in block order, each as its raw bytes.
- `totalGasWanted`: the total gas the transactions want.
- `totalGasEstimated`: the total gas the producer estimated they would use.

**Encoding.** Each field starts with a tag byte `(field_number << 3) | wire_type`, and fields are written in
field-number order. Wire type `0` is a `varint`. Wire type `2` is length-delimited: the tag is followed by
`varint(length)` and then that many bytes.

| Tag    | Computed as    | Fields                                                          |
|--------|----------------|-----------------------------------------------------------------|
| `0x08` | `(1 << 3) \| 0` | `seconds`                                                       |
| `0x0a` | `(1 << 3) \| 2` | `createdAt`, `validator`, and the key inside `validator`        |
| `0x10` | `(2 << 3) \| 0` | `block_number`, `joined`, `nanos`                               |
| `0x1a` | `(3 << 3) \| 2` | `parent_hash`                                                   |
| `0x22` | `(4 << 3) \| 2` | `payload_hash`                                                  |
| `0x2a` | `(5 << 3) \| 2` | `laneID`                                                        |
| `0x32` | `(6 << 3) \| 2` | each transaction                                                |
| `0x38` | `(7 << 3) \| 0` | `totalGasWanted`                                                |
| `0x40` | `(8 << 3) \| 0` | `totalGasEstimated`                                             |

`0x20` is the length 32, not a tag. The `0x22` after the first `0x0a` in `laneID` is the length 34, the size of
the key message, not the `payload_hash` tag.

```
encodeHeader(h) = 0x10 ‖ varint(h.block_number)
                ‖ 0x1a ‖ 0x20 ‖ h.parent_hash
                ‖ 0x22 ‖ 0x20 ‖ h.payload_hash
                ‖ 0x2a ‖ varint(len(laneID(h))) ‖ laneID(h)

laneID(h)       = 0x0a ‖ 0x22 ‖ 0x0a ‖ 0x20 ‖ h.validator
                ‖ 0x10 ‖ varint(h.joined)
```

`len(laneID(h))` is `37 + len(varint(h.joined))`.

```
payload_hash     = SHA-256(encodePayload(p))

encodePayload(p) = 0x0a ‖ varint(len(createdAt(p))) ‖ createdAt(p)
                 ‖ 0x32 ‖ varint(len(tx)) ‖ tx          for each tx in p.txs, in order
                 ‖ 0x38 ‖ varint(p.totalGasWanted)
                 ‖ 0x40 ‖ varint(p.totalGasEstimated)

createdAt(p)     = 0x08 ‖ varint(p.seconds) ‖ 0x10 ‖ varint(p.nanos)
```

Every field other than `txs` is encoded, including when its value is zero; a payload with no transactions
contributes no `txs` bytes. `seconds` is encoded as `varint` of its 64-bit two's-complement value and `nanos`
as `varint` of its 32-bit two's-complement value, so a negative `seconds` encodes to 10 bytes.

**Test vector.** `validator` is 32 bytes of `0x11`, `joined` is 7, `block_number` is 300, `parent_hash` is 32
bytes of `0x22`, and the payload was created at `2024-01-01T00:00:00Z` (`seconds` 1704067200, `nanos` 0) with
transactions `01` and `0203`, `totalGasWanted` 21000, and `totalGasEstimated` 300.

```
encodePayload = 0a08 088081c8ac06 1000
                320101
                32020203
                3888a401
                40ac02
payload_hash  = c5d7b958f7f6968694dc5b56438ec14a76dca8872c4d18fcfad4ee250805583a

encodeHeader  = 10ac02
                1a20 2222222222222222222222222222222222222222222222222222222222222222
                2220 c5d7b958f7f6968694dc5b56438ec14a76dca8872c4d18fcfad4ee250805583a
                2a26 0a220a20 1111111111111111111111111111111111111111111111111111111111111111 1007
blockHash     = e94492a4ef3a399de10a1a2e06da1a224ecc91860e10be9b875fd9fee38bc31d
```

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
defined by the [BUD specification](bud/bud_spec.md), which also defines the BUD proofs and BUD
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
