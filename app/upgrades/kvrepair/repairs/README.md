# KV repairs

Each `.json` file here is one repair that `kvrepair.Register` compiles into the
binary. A file names one chain, one height, the height its values were read at,
and the entries to apply at the start of that block:

```json
{
  "name": "arctic-1-evm-187000000",
  "chain_id": "arctic-1",
  "height": 187000000,
  "read_height": 186999000,
  "source": "memiavl-rpc-0-0 at 186999000",
  "entries": [
    {"store": "evm", "key": "03...", "new": "00...01", "old": "de...ad"},
    {"store": "evm", "key": "03...", "new": null, "old": "de...ad"}
  ]
}
```

- `key`, `new`, and `old` are hex, with or without `0x`.
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

Generate a file with `scripts/kvrepair-export.py`. Do not edit the values by hand.
