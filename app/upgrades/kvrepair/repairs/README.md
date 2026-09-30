# KV repairs

Each `.json` file here is one repair that `kvrepair.Register` compiles into the
binary. A file names one chain, one height, and the entries to apply at the
start of that block:

```json
{
  "name": "arctic-1-evm-187000000",
  "chain_id": "arctic-1",
  "height": 187000000,
  "source": "memiavl-rpc-0-0 at 186999000",
  "entries": [
    {"store": "evm", "key": "03...", "value": "00...01", "expect": "de...ad"},
    {"store": "evm", "key": "03...", "value": null, "expect": "de...ad"}
  ]
}
```

- `key`, `value`, and `expect` are hex, with or without `0x`.
- `source` is free text for reviewers. It has no effect.
- A `null` value deletes the key.
- `expect` is the value the entry must find before it writes. Use
  `"expect_absent": true` when the key must not exist. With neither, the entry
  writes over any value.
- An entry that already holds its target value is skipped, so a node whose state
  is already correct can run the same binary.
- An entry that finds a value other than `expect` stops the node at that height.

Generate a file with `scripts/kvrepair-export.py`. Do not edit the values by hand.
