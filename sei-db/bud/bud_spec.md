# Block Update Digest

**Status:** draft. **BUD version:** 1. **BUD proof version:** 1. **BUD state proof version:** 1.

This document defines the Block Update Digest (BUD): a 32-byte commitment to the key-value writes a block
makes. A block's BUD is the `bud` field of its [app hash](../../giga/apphash/apphash_spec.md). This document
also defines the serialization of a BUD tree; the BUD proof, which proves that one write is among those a BUD
commits to; and the BUD state proof, which combines one or two BUD proofs to prove a key's value over a range of
blocks. It does not define where a BUD tree is stored, or how a reader comes to trust an app hash.

## Notation

- `‖` is byte concatenation.
- `u8(x)` is `x` as a single byte.
- `u32be(x)` is `x` as a 4-byte unsigned big-endian integer.
- `u64be(x)` is `x` as an 8-byte unsigned big-endian integer.
- `keccak256(x)` is the original Keccak-256 with a 32-byte output, as used by Ethereum's `KECCAK256` opcode. It
  is not FIPS 202 SHA3-256, which pads differently. `keccak256` of the empty string is
  `c5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470`.
- `⊥` is the value of a deleted key. It is distinct from every byte string, including the empty one.
- A *hash* is exactly 32 bytes.
- Byte strings are ordered lexicographically by unsigned byte value, and a proper prefix orders before any
  string it is a prefix of.
- `⌊x / 2⌋` and `⌈x / 2⌉` are integer division rounded down and up.

## Terms

- **budlet:** the triple (`key`, `value`, `previousHeight`) for one key a block modified.
- **BUD tree:** the Merkle tree whose leaves are one block's budlets.
- **BUD:** the hash at the top of a BUD tree.
- **BUD proof:** a budlet together with the proof that it is a leaf of the BUD tree under a given BUD.
- **BUD state proof:** one or two BUD proofs of the same key, each paired with its block's app hash data,
  proving the key's value over a range of block heights.

## Budlets

A budlet is a triple (`key`, `value`, `previousHeight`).

| Element          | Type                    | Meaning                                                           |
|------------------|-------------------------|-------------------------------------------------------------------|
| `key`            | byte string             | The key modified. 1 to 2^32 − 1 bytes.                            |
| `value`          | byte string, or `⊥`     | The value written, 0 to 2^32 − 1 bytes, or `⊥` if deleted.        |
| `previousHeight` | 64-bit unsigned integer | The height of the block that last modified the key, or 0 if none. |

A block's budlets hold one budlet per key the block modified, in strictly increasing key order. An implementation
rejects, as an error, a sequence of budlets that is not strictly increasing by key or that holds a budlet
violating the bounds above.

A deletion (`value = ⊥`) and a write of the empty value are different budlets.

### Serialization

`serialize(b)` is the fields below, in order, with no padding or separators. `⊥` is serialized as a set deletion
flag and a value of length 0. The serialization is also the input to the budlet's leaf hash.

|         Size | Field            | Encoding                                  |
|-------------:|------------------|-------------------------------------------|
|            4 | key length       | `u32be(len(key))`                         |
|   `len(key)` | `key`            | raw bytes                                 |
|            1 | deletion flag    | `u8(1)` if `value = ⊥`, `u8(0)` otherwise |
|            4 | value length     | `u32be(len(value))`, 0 if `value = ⊥`     |
| `len(value)` | `value`          | raw bytes, none if `value = ⊥`            |
|            8 | `previousHeight` | `u64be(previousHeight)`                   |

This is Solidity's
`abi.encodePacked(uint32(key.length), key, deleted, uint32(value.length), value, uint64(previousHeight))`, with
`deleted` as the deletion flag.

The serialization carries no version of its own. It is defined by the BUD version of the BUD tree or BUD proof
that the budlet travels with.

A decoder rejects, as an error, input that ends before the end of a field, bytes after `previousHeight`, a
deletion flag other than `0` or `1`, a set deletion flag with a non-zero value length, and a key length of 0.

## BUD tree

The hashes of BUD tree nodes are

```
leafHash(b)            = keccak256(u8(0x00) ‖ serialize(b))
innerHash(left, right) = keccak256(u8(0x01) ‖ left ‖ right)
```

The leading byte separates leaves from inner nodes, so that no leaf can be presented as an inner node or the
reverse.

The root `R` of the BUD tree over a block's `n` budlets `b[0] … b[n−1]` is computed level by level:

1. If `n = 0`, `R` is 32 zero bytes.
2. Level 0 is `leafHash(b[0]) … leafHash(b[n−1])`.
3. The level above a level of `s > 1` nodes is formed by replacing each pair at positions `(0, 1)`, `(2, 3)`,
   … with `innerHash` of the pair, in order. When `s` is odd, the last node has no pair and is carried up to
   the level above unchanged: it is neither hashed nor duplicated. The level above has `⌈s / 2⌉` nodes.
4. Repeat step 3 until a level has one node. That node is `R`.

When `n = 1`, `R` is the single leaf hash.

*Non-normative:* this is the Merkle Tree Hash of RFC 9162 §2.1.1 (RFC 6962 §2.1), with `leafHash` and
`innerHash` as its leaf and node hashes and a different value for the empty tree.

### Serialization

A serialized BUD tree is the fields below, in order, with no padding or separators. It holds the budlets only;
the root and inner nodes are recomputed from them.

| Offset |     Size | Field     | Encoding                                     |
|-------:|---------:|-----------|----------------------------------------------|
|      0 |        1 | `version` | `u8`, the BUD version, `1` for this document |
|      1 |        8 | `n`       | `u64be`, the number of budlets               |
|      9 | variable | budlets   | `serialize(b[0]) ‖ … ‖ serialize(b[n−1])`    |

Each budlet's serialization declares its own length, so the budlets need no separators.

A decoder reads `version` from the first byte and rejects any version it does not support. It then rejects, as
an error, input shorter than 9 bytes, input that ends before the `n`th budlet does, bytes after it, any budlet
the budlet decoder rejects, and budlets whose keys are not strictly increasing.

## BUD

```
BUD = keccak256("sei-bud" ‖ u8(version) ‖ u64be(n) ‖ R)
```

| Size | Field     | Value                                                                             |
|-----:|-----------|-----------------------------------------------------------------------------------|
|    7 | domain    | the ASCII bytes `sei-bud` (`7365692d627564`), with no terminator or length prefix |
|    1 | `version` | `1`, the BUD version this document defines                                        |
|    8 | `n`       | `u64be` of the number of budlets                                                  |
|   32 | `R`       | the BUD tree root                                                                 |

The domain separates a BUD from every BUD tree node, whose preimages begin with `0x00` or `0x01`, and from
objects of other kinds that commit to a tree of this shape, which use other domains. The BUD commits to `n` so
that a BUD proof cannot claim a different `count` and `index` for the same root.

## BUD proof

A BUD proof of budlet `b[i]` in a block of `n` budlets holds:

- `budlet`: `b[i]`.
- `count`: `n`.
- `index`: `i`, the budlet's position in key order.
- `siblings`: the sibling hashes on the way from the budlet's leaf to the root, leaf end first.

The BUD is not part of the proof; the verifier must obtain it from a source it trusts. The serialized proof
names the BUD version it was built under, which also defines how its budlet is serialized and hashed.

### Siblings

The siblings are taken from the levels of the BUD tree. Start with position `p = i` at level 0, whose size is
`s = n`. While `s > 1`:

1. If `p` is odd, append the node at position `p − 1`.
2. Otherwise, if `p + 1 < s`, append the node at position `p + 1`.
3. Otherwise the node at `p` is carried up unpaired, and nothing is appended.
4. Move to the level above: `p ← ⌊p / 2⌋`, `s ← ⌈s / 2⌉`.

The number of siblings depends only on `count` and `index`. `siblingCount(count, index)` is the number of
iterations of the loop above, run on `p = index` and `s = count`, that append a node.

### Computing the BUD

A BUD proof (`budlet`, `count`, `index`, `siblings`) determines a BUD. The decoder has already ensured that
`budlet` satisfies the bounds in [Budlets](#budlets), that `index < count`, and that `siblings` holds exactly
`siblingCount(count, index)` hashes.

1. Let `h = leafHash(budlet)`, `p = index`, `s = count`, and `k = 0`. While `s > 1`:
   - If `p` is odd: `h ← innerHash(siblings[k], h)`, and `k ← k + 1`.
   - Otherwise, if `p + 1 < s`: `h ← innerHash(h, siblings[k])`, and `k ← k + 1`.
   - Then `p ← ⌊p / 2⌋` and `s ← ⌈s / 2⌉`.
2. The computed BUD is `keccak256("sei-bud" ‖ u8(1) ‖ u64be(count) ‖ h)`.

The proof shows that a block wrote `budlet` when the computed BUD equals that block's BUD, obtained from a source
the verifier trusts. A mismatch is a failed proof, not an error.

### Serialization

A serialized BUD proof is the fields below, in order, with no padding or separators.

|                              Size | Field        | Encoding                                           |
|----------------------------------:|--------------|----------------------------------------------------|
|                                 1 | `version`    | `u8`, the BUD proof version, `1` for this document |
|                                 1 | `budVersion` | `u8`, the BUD version, `1` for this document       |
|                          variable | `budlet`     | `serialize(budlet)`                                |
|                                 8 | `count`      | `u64be`                                            |
|                                 8 | `index`      | `u64be`                                            |
| `32 × siblingCount(count, index)` | `siblings`   | each hash's 32 raw bytes, in order                 |

The budlet declares its own length and `count` and `index` determine the number of siblings, so a serialized
BUD proof needs no length prefix to be embedded in a larger format.

A decoder reads `version` from the first byte and rejects any version it does not support. It then rejects, in
order, input that ends before `budVersion`, a `budVersion` it does not support, a budlet the budlet decoder
rejects, input that ends before the end of `index`, an `index` not less than `count`, and input that ends before
the last sibling. A decoder of a standalone BUD proof also rejects bytes after the last sibling. Each rejection
is an error, distinct from a proof that decodes but computes the wrong BUD.

## BUD state proof

A BUD state proof proves the value of one key over a range of block heights. It holds one or two pairs, each
of a block's app hash data and a BUD proof against the BUD in it. App hash data and its serialization are
defined by the [Giga app hash specification](../../giga/apphash/apphash_spec.md); its `blockHeight` field is
the height of the block and its `bud` field the block's BUD.

- **One pair**, for budlet `a` in the block at height `hA`: the key `a.key` held `a.value` at `hA`.
- **Two pairs**, for budlet `a` at height `hA` followed by budlet `b` at height `hB`: the key `a.key` held
  `a.value` at every height from `hA` up to but not including `hB`, and `b.value` at `hB`.

An implementation rejects, as an error, a BUD state proof that does not satisfy every condition below.

- For each pair, the BUD [computed](#computing-the-bud) from its BUD proof equals the `bud` of its app hash data.
- With two pairs:
  - `a.key = b.key`.
  - `hA < hB`.
  - `b.previousHeight = hA`, so that no block between `hA` and `hB` modified the key.
  - Both app hash data have the same `chainID` and the same `version`.

### Trust

A BUD state proof that satisfies these conditions is consistent, which alone proves nothing. The verifier must
also authenticate the app hash of each pair's app hash data through a protocol outside this document.

### Serialization

A serialized BUD state proof is a header followed by `n` pairs, with no padding or separators.

| Size | Field     | Encoding                                                 |
|-----:|-----------|----------------------------------------------------------|
|    1 | `version` | `u8`, the BUD state proof version, `1` for this document |
|    1 | `n`       | `u8`, the number of pairs, `1` or `2`                    |

Each pair, in height order:

|                Size | Field               | Encoding                               |
|--------------------:|---------------------|----------------------------------------|
|                   4 | `appHashDataLength` | `u32be` of the length of `appHashData` |
| `appHashDataLength` | `appHashData`       | the app hash data serialization        |
|            variable | `budProof`          | the BUD proof serialization            |

`appHashDataLength` exists only in this format. It is not part of any hashed or signed bytes, and lets a
decoder find the end of `appHashData` without knowing its size for each app hash version.

A decoder reads `version` from the first byte and rejects any version it does not support. It then rejects, as
an error, input that ends before `n`, an `n` other than `1` or `2`, input that ends before a length prefix or
before the end of the app hash data it declares, app hash data the app hash decoder rejects, a BUD proof the
BUD proof decoder rejects, bytes after the `n`th pair, and pairs that violate the conditions above.

## Versioning

There are three versions, and they change independently.

- The BUD version is bumped on any change to the budlet fields, the budlet serialization, the BUD tree, the BUD
  tree serialization, or the BUD preimage. It leads the serialized BUD tree, appears in the serialized BUD proof,
  and is part of the BUD preimage, so BUDs of different versions never coincide.
- The BUD proof version is bumped on any change to the layout of the serialized BUD proof.
- The BUD state proof version is bumped on any change to the contents, conditions, or serialization of the BUD
  state proof.

## Open design work

| Item           | Needed                                                                     |
|----------------|----------------------------------------------------------------------------|
| Inherited keys | How `previousHeight` marks a key last written before BUDs were introduced. |

## Test vector

A block of three budlets:

| `b[i]` | `key` (ASCII) | `key` (hex)  | `value`        | `previousHeight`     |
|-------:|---------------|--------------|----------------|----------------------|
|      0 | `evm/a`       | `65766d2f61` | `aabb`         | `0`                  |
|      1 | `evm/b`       | `65766d2f62` | `⊥` (deletion) | `7`                  |
|      2 | `evm/c`       | `65766d2f63` | `cc`           | `0x0102030405060708` |

Budlet serializations:

```
serialize(b[0]) = 00000005 65766d2f61 00 00000002 aabb 0000000000000000
serialize(b[1]) = 00000005 65766d2f62 01 00000000      0000000000000007
serialize(b[2]) = 00000005 65766d2f63 00 00000001 cc   0102030405060708
```

Leaf hashes:

```
L0 = leafHash(b[0]) = 2c04f8ef01f65fcf00309d0eb3b513fbd67f963c84bdced043becfa555a81984
L1 = leafHash(b[1]) = a9ffe6eeb707bdc90e75cedc80ddd41cea6b6d99b56a00b8deb832942f11406d
L2 = leafHash(b[2]) = e9d985609a1790ef720997688285b872e4e3b5b0767c7a55c8eb339bbd9e6a56
```

Level 0 is `L0 L1 L2`. Level 1 is `N = innerHash(L0, L1)` followed by `L2`, carried up unpaired. Level 2 is
the root.

```
N = innerHash(L0, L1) = c61a514b106ecf34b8371ad4af73a1d8d0c00962f9979128da71682bbb211ca2
R = innerHash(N, L2)  = aa81ff328275dcf86485ec05be9a5a6eb47ebb242e3492e3be0d91fe0188f368
```

BUD, i.e. `keccak256` of `7365692d627564 01 0000000000000003 ‖ R`:

```
a60f1c2ddcd436250a3634bd33732a485610bfd1047283d6c5d63cad526cf86d
```

Serialized BUD tree (hex, wrapped at field boundaries):

```
01 0000000000000003
serialize(b[0])
serialize(b[1])
serialize(b[2])
```

Serialized BUD proofs (hex, wrapped at field boundaries):

```
b[0]: 01 01
      serialize(b[0])
      0000000000000003 0000000000000000
      L1
      L2

b[1]: 01 01
      serialize(b[1])
      0000000000000003 0000000000000001
      L0
      L2

b[2]: 01 01
      serialize(b[2])
      0000000000000003 0000000000000002
      N
```

where `serialize(b[i])`, `L0`, `L1`, `L2`, and `N` stand for the bytes listed above. The proof of `b[2]` has
one sibling, because `L2` is carried up unpaired from level 0.

More vectors, including BUD state proofs, are in `testdata/golden/`. The implementation's tests require it to
reproduce every one.
