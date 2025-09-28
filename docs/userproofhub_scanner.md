# UserProofHub Scanner Suite

Two complementary scripts are available for identifying Zendity/Ava Labs
"UserProofHub" indicators across EVM networks:

- `scripts/userproofhub_scanner.py` – full featured scanner that supports bulk
  explorer queries with built-in Keccak selector hashing.
- `scripts/userproofhub_scanner_offline.py` – lightweight variant intended for
  analysts who want a simpler workflow that can operate from cached explorer
  responses while offline.

Both scripts compare contract source code against known function selectors,
event signatures, and keywords tied to the leaked UserProofHub implementation.
The indicator pack is documented in
`codex-attribution/ava_userproofhub_claim/proof_overlap_report.json`.

For analysts that need to *discover* where the violator deployed contracts, the
`scripts/userproofhub_locator.py` helper can query JSON-RPC endpoints for the
`ProofVerified(address,bytes32)` log emitted by the stolen implementation. The
utility collects the emitting contract addresses and, when requested, hydrates
them with the same explorer metadata gathered by the scanners. This makes it
straightforward to first locate the deployments and then feed the addresses into
either scanner for a deeper source-level diff.

## Offline scanner usage

The offline-friendly workflow accepts explicit addresses, text files, or JSON
lists of contracts grouped by chain:

```bash
python scripts/userproofhub_scanner_offline.py \
  --address ethereum:0x0000000000000000000000000000000000000000 \
  --address-file avalanche:avalanche_addresses.txt \
  --address-json contracts_to_scan.json \
  --output findings_userproofhub.json
```

Optional explorer API keys or custom endpoints are supplied per chain:

```bash
python scripts/userproofhub_scanner_offline.py \
  --api-key ethereum:$ETHERSCAN_API_KEY \
  --base-url sei:https://custom.sei.explorer/api
```

Add `--include-non-matches` to retain addresses that did not trigger an
indicator match in the generated JSON report.

The resulting JSON file contains an array of contract findings with metadata
(compiler, proxy configuration, verification timestamp) and the indicators that
were discovered for each address.

## Locating deployments through event logs

Use the locator to sweep ProofVerified logs across one or many JSON-RPC
endpoints. Example command:

```bash
python scripts/userproofhub_locator.py \
  --rpc avalanche:https://api.avax.network/ext/bc/C/rpc \
  --rpc ethereum:https://eth.llamarpc.com \
  --from-block 29300000 \
  --chunk-size 250000 \
  --include-metadata \
  --output userproofhub_deployments.json
```

By default the locator chunks log queries into 250k block windows. Increase or
decrease this value depending on the RPC's response limits. When
`--include-metadata` is supplied the script hydrates each discovered address via
the same explorer APIs used by `userproofhub_scanner.py`. Provide API keys or
custom explorer endpoints with the `--api-key` and `--explorer` flags if you are
working with non-default networks.
