# KV repairs

Each `.json` file here is one repair that `kvrepair.Register` compiles into the
binary. A file names one chain, one height, the height its values were read at,
and the entries to apply at the start of that block:

```json
{
  "name": "arctic-1-evm-187000001",
  "chain_id": "arctic-1",
  "height": 187000001,
  "read_height": 187000000,
  "source": "reserve memiavl /.sei/data/state_commit/memiavl and production composite ... at 187000000",
  "entries": [
    {"store":"evm","key":"03...","new":"00...01","old":"de...ad"},
    {"store":"evm","key":"03...","new":null,"old_absent":true}
  ]
}
```

- `key`, `new`, and `old` are hex, with or without `0x`.
- Field names are lowercase. A field name that appears twice in one object is
  refused.
- `read_height` must be below `height`.
- `source` is free text for reviewers. It has no effect.
- `new` is the correct value, read at `read_height`. Every entry must have
  `new`. A `null` new deletes the key.
- `old` is the incorrect value the key holds before the repair. Use
  `"old_absent": true` when the key must not exist before the repair. A `null`
  old is refused.
- An entry with neither `old` nor `old_absent` writes over any value. It is
  allowed only when `read_height` is `height - 1`, because a key can change
  between the two heights.
- Every entry is written, and each write is read back. An entry that already
  holds `new` skips the `old` check, so a reserve whose state is correct runs
  the same binary and commits the same changeset as a repaired node.
- An entry that finds neither `new` nor `old` stops the node at that height.
- In the `evm` store, an all-zero storage slot, nonce, code hash, or balance,
  and an empty code value, compare as equal to an absent key. Every other value
  compares exactly, so `""` differs from an absent key.

## Generate a file

Stop the chain at `H`. Then, for each bucket and key prefix that the digest
reports as different, list the keys on a memiavl reserve and on a production
node at `H`. Both runs need the same bucket, `--key-offset`, and `--key-prefix`,
and `--list-limit 0`:

```bash
seidb evm-logical-digest --backend memiavl --memiavl-open-mode replay \
  -d <reserve memiavl dir> --height H --inspect-bucket storage \
  --key-offset 4 --key-prefix 03AB --list --list-limit 0 --json > reserve.json
seidb evm-logical-digest --backend composite --memiavl-open-mode replay \
  --flatkv-dir <flatkv dir> --memiavl-dir <memiavl dir> --height H \
  --inspect-bucket storage --key-offset 4 --key-prefix 03AB \
  --list --list-limit 0 --json > prod.json
seidb kvrepair-export --reserve reserve.json --prod prod.json \
  --chain-id arctic-1 --name arctic-1-evm-<H+1> --repair-height <H+1> \
  -o app/upgrades/kvrepair/repairs/arctic-1-evm-<H+1>.json
```

`kvrepair-export` gives every entry an `old` value, and refuses two reports
that differ in height, bucket, offset, or prefix, a list that `--list-limit`
cut off, and a reserve read with `--memiavl-normalization translator`. It
checks that a node loads the file it writes. Do not edit the values by hand.
