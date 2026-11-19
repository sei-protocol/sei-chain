# State sync archive format, version 1

`seid statesync export` writes this archive and `seid statesync import` reads it. It holds the
FlatKV state of one checkpoint height `H` and the consensus proof of `H`.

The binding definition is the test vectors file
[`testdata/format_v1_vectors.json`](testdata/format_v1_vectors.json) and the tests in the table
below. This document is an overview of them. When the two differ, the tests are correct.

## Overview

```
archive = zstd( tar( manifest.json, proof/<H20>.bin, flatkv/rows/<I8>, ... ) )
record  = uvarint(len(key)) || key || uvarint(len(value)) || value
```

- zstd: one frame with the content checksum, window at most 8 MiB. No byte follows the end of the
  tar archive.
- tar: the entries are in the order above, and there is no other entry. Every entry is a regular file
  with a canonical relative path. `<H20>` is `H` padded to 20 decimal digits. `<I8>` is the chunk
  index 0, 1, 2, ... padded to 8 digits, with no gap. A state with no row has no chunk entry.
- `manifest.json`: a JSON object with exactly the fields `format_version`, `chain_id`,
  `checkpoint_height`, `created_at`, `producer_version`, `row_count`, `state_bytes` and `state_hash`.
- `proof/<H20>.bin`: the state sync proof file of `H`, as `sei-tendermint/autobahn/statesyncproof`
  defines it.
- `flatkv/rows/<I8>`: row records. `uvarint` is the Go `encoding/binary` varint in its shortest
  encoding. A record does not continue into the next chunk. The keys are in strictly ascending
  bytewise order across all chunks.
- Each record is one FlatKV row. The key and the value are the FlatKV bytes, unchanged, and
  [`sei-db/state_db/sc/flatkv/AGENTS.md`](../../../../sei-db/state_db/sc/flatkv/AGENTS.md) lists
  where they are defined. `state_hash` is the FlatKV state hash of all rows.

| Limit          | Maximum  |
|----------------|----------|
| manifest entry | 16 MiB   |
| proof entry    | 16 MiB   |
| row chunk      | 128 MiB  |
| key            | 64 KiB   |
| value          | 64 MiB   |

## Tests that pin the format

| Test                                                         | Pins                                                                 |
|--------------------------------------------------------------|----------------------------------------------------------------------|
| `TestFormatVectors`                                          | the reader accepts each valid vector and gets its rows; the writer makes the same bytes from those rows; the reader refuses each invalid vector |
| `TestFormatVectorRowsUseTheFlatKVEncoders`                   | the rows of the first valid vector are the output of the FlatKV encoders |
| `TestFormatUvarintExamples`                                  | the uvarint encoding                                                 |
| `TestOpenArchiveRejectsAWindowLargerThanTheArchiveWindow`    | the zstd window limit                                                |
| `TestArchiveRejects`                                         | the size limits                                                      |
| `TestArchiveRejectsDataAfterTheTarEnd`                       | the end of the zstd stream, and the content checksum                 |

Each vector entry has a `json` field (the manifest) or a `hex` field (the bytes of the entry). The
`error_contains` field of an invalid vector is the error text of this implementation. Another
implementation must refuse the vector, and can use a different error text.

Change the vectors only with a new `format_version`. To write the vectors file of a new version, run
`go test ./cmd/seid/cmd/statesync/ -run TestFormatVectors -format-vectors-record`. The test refuses
to record on CI, and refuses to overwrite the file of a version.

## Trust

The reader verifies the proof of `H` against the committee of `H` in the epoch registry of the
archived state, the row `autobahn/epoch_registry`. It does not bind that registry to the genesis of
the chain. It does not compare `state_hash` with the app hash of `H`, because the AppQC of `H` signs
only the app hash.

Thus the reader trusts the source of the archive for the committees:

- When the registry is correct and the state is wrong, the node finds the error when it verifies
  the AppQC of `H + 1`.
- When the registry is forged, import succeeds. The node verifies the blocks after `H` against the
  forged committees, so it refuses the blocks of the chain and makes no progress. It accepts wrong
  state only when every peer that it syncs from serves blocks that the forged committees signed.

A trusted app hash of `H` does not remove this trust, because a forged committee can sign the
correct app hash. Only a trusted value of the committees, such as a hash of the registry row, does.

## Versioning

A reader accepts exactly one `format_version`. The version increments when the archive bytes change
for the same state, or when a FlatKV encoding rule changes. A new manifest field also needs a new
version, because a reader refuses unknown fields.
