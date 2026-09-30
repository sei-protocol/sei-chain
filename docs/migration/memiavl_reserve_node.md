# Running a memIAVL reserve node for the FlatKV migration

The FlatKV migration moves chain state out of memIAVL and into FlatKV in phases, starting with the EVM module (see the [migration README](https://github.com/sei-protocol/sei-chain/blob/main/sei-db/state_db/sc/migration/README.md)). It only runs forward: once a node has moved data into FlatKV, its memIAVL no longer has that data.

A reserve node is a full node that never migrates. It keeps all state in memIAVL and still executes every block, so if the migration goes badly wrong across the fleet, the reserves hold a complete, current copy of the state in the old layout for the fleet to recover from.

Reserves run a special build of `seid`, compiled with the `mock_chain_validation` build tag from a `<release>-memiavl-reserve` branch. When the fleet starts migrating, the fleet's app hash changes and a reserve's no longer matches it. A normal binary would stop at the first mismatched block, but the reserve build counts the mismatch in a metric and keeps going.

To run one, build the reserve branch with `BUILD_TAGS=mock_chain_validation`, run it as a non-validator with `sc-write-mode = "memiavl_only"` and `sc-write-mode-enable-auto = false`, and have it synced before governance starts the migration. After that, leave it alone apart from chain upgrades.

Paths below assume the default home directory, `~/.sei`.

## Before you start

The migration starts when a governance proposal raises the `NumKeysToMigratePerBlock` parameter above 0. Setting it back to 0 later only pauses the migration. To check the current value:

```bash
seid query params subspace migration NumKeysToMigratePerBlock
```

Your normal nodes also mark the start: they create their FlatKV directory, `~/.sei/data/state_commit/flatkv`, at that moment.

Have your reserves running and synced before then. Once the migration is under way, reserves can no longer state-sync, because snapshots from migrated nodes contain FlatKV data that a reserve refuses to import. From that point, a new reserve has to be copied from an existing one.

The reserve binary must match the release the network runs. Reserve branches are named `<release>-memiavl-reserve`, so for `v6.7.0-rc3` the branch is [`v6.7.0-rc3-memiavl-reserve`](https://github.com/sei-protocol/sei-chain/tree/v6.7.0-rc3-memiavl-reserve) (head `235c2cf95`). Its only code changes from rc3 are a startup guard and the reserve config defaults. If the network moves to a release that has no reserve branch yet, ask the Sei team.

Size the machine like your current RPC nodes, since a reserve stores state the same way they do today.

## 1. Build the binary

```bash
git clone https://github.com/sei-protocol/sei-chain.git
cd sei-chain
git checkout v6.7.0-rc3-memiavl-reserve
make install BUILD_TAGS=mock_chain_validation
seid version --long | grep -E '^(version|commit|build_tags):'
```

`build_tags` must include `mock_chain_validation`:

```
version: v6.7.0-rc3-7-g235c2cf95
commit: 235c2cf95ba8489b16d124a7464f0b41c469cebb
build_tags: netgo,ledger,mock_chain_validation
```

The branch needs Go 1.25.6 or newer. To build a container image instead, run `docker build --build-arg GO_BUILD_TAGS=mock_chain_validation -t seid-reserve .` from the same checkout.

## 2. Configure it

In `config.toml`:

```toml
mode = "full"
```

In the `[state-commit]` section of `app.toml`:

```toml
sc-write-mode = "memiavl_only"
sc-write-mode-enable-auto = false
```

Running `seid init` with the reserve binary writes all three values for you. On an existing home directory, set them yourself. The reserve binary treats a missing `sc-write-mode-enable-auto` as `false`, but write the key out anyway so the file says what the node does.

Also turn on Prometheus (`prometheus = true` under `[instrumentation]` in `config.toml`), because the checks below read a metric from `:26660/metrics`. Everything else can stay the way you run your normal RPC nodes.

The reserve binary refuses to start if the node is set up as a validator or has auto mode on. [Troubleshooting](#troubleshooting) has the exact messages.

## 3. Load the data

Before the migration starts, do one of these:

- Convert an existing RPC node that runs the same release. Stop it, install the reserve binary, and apply step 2.
- Set up a new node with a normal state sync. The script in the [SeiDB migration guide](https://github.com/sei-protocol/sei-chain/blob/main/docs/migration/seidb_migration.md#step-3-state-sync) works, as long as the reserve binary and step 2 are in place before the first start.

After the migration has started, copy `~/.sei/data` and `~/.sei/wasm` from a stopped reserve instead. Never copy data from a normal node.

In every case, confirm there is no FlatKV store before the first start:

```bash
ls -d ~/.sei/data/state_commit/flatkv ~/.sei/data/flatkv
```

Both paths must come back "No such file or directory". (If `sc-directory` is set in `app.toml`, look under that directory instead of `~/.sei`.) A reserve never opens FlatKV, so if some EVM data had already moved there, the node would run on incomplete state and this build would not notice.

## 4. Start it and check it

Start the node the way you normally do, then check three things.

The startup log shows the pinned write mode:

```bash
journalctl -u seid | grep 'SeiDB SC is enabled now' | grep -o 'WriteMode:[a-z_]* WriteModeEnableAuto:[a-z]*'
```

```
WriteMode:memiavl_only WriteModeEnableAuto:false
```

The node reaches the tip, meaning `curl -s localhost:26657/status | jq .sync_info.catching_up` returns `false`.

No validation failure has been swallowed yet:

```bash
curl -s localhost:26660/metrics | grep unsafe_validation_skipped | grep -v '^#'
```

Before the migration starts, this must print nothing, because the reserve should still agree with the network on every block. Any output means it already disagrees, most likely because the binary was built from the wrong release. Fix that before the migration starts.

## When the migration starts

Every block now logs this at ERROR level:

```
migration requested (batch size > 0) but the SC write mode is pinned to fixed memiavl_only by configuration; skipping migration kick-off.
```

On a reserve that line is expected. The message goes on to suggest setting `sc-write-mode-enable-auto = true`, which you should not do.

The metric check from step 4 also starts returning lines with `validation_error="app_hash"`, and the count keeps rising. This is the reserve disagreeing with the migrated fleet's app hash, which is what it is there to do.

Tell the Sei team if you see any of these:

- A `validation_error` label other than `app_hash`. Only the storage layout differs from the network, and that only changes the app hash, so another label means the reserve's execution has drifted.
- A FlatKV directory appearing on the reserve.
- The node falling behind and not catching up.

## Running it

Keep reserves out of your public RPC pool. Once the migration has started, a reserve keeps running even when its state disagrees with the network, so if that state went wrong, it would go on answering queries and nobody would notice.

Monitor the same things as in step 4: the node is at the tip, `app_hash` is the only label in the metric, and there is no FlatKV directory.

### Upgrades

Every chain upgrade needs a reserve build of the new release, made from that release's `-memiavl-reserve` branch with `BUILD_TAGS=mock_chain_validation`.

Let the node stop at the upgrade height with `UPGRADE NEEDED` as usual (the reserve build still halts there), then install the new reserve build and start it again.

Never install it early. A normal build of the new release panics if it starts before the upgrade height, but the reserve build only logs `BINARY UPDATED BEFORE TRIGGER` and carries on, so it executes blocks with the new code too soon.

With Cosmovisor, put the reserve build in `upgrades/<name>/bin/` yourself and leave `DAEMON_ALLOW_DOWNLOAD_BINARIES` off, so Cosmovisor doesn't fetch the normal release binary.

Don't run the normal `seid` on a reserve at any point. It stops at the first block whose app hash doesn't match.

### Comparing EVM state with a migrated node (optional)

Once the migration has started, the app hash can't tell you whether a reserve's state is right, but `seidb evm-logical-digest` can. It computes a digest of the EVM state that comes out the same on memIAVL and on FlatKV at the same height. Build it from the reserve checkout:

```bash
go build -o build/seidb ./sei-db/tools/cmd/seidb
```

Pick a height `H` that the reserve has a memIAVL snapshot for. Snapshot directories are named after the height padded to 20 digits, so `snapshot-00000000000213200000` is height `213200000`:

```bash
ls ~/.sei/data/state_commit/memiavl | grep '^snapshot-'
```

(Older nodes keep memIAVL in `~/.sei/data/committer.db` and FlatKV in `~/.sei/data/flatkv`.)

On the reserve:

```bash
seidb evm-logical-digest --backend memiavl \
  --db-dir ~/.sei/data/state_commit/memiavl --height H
```

On one of your normal nodes, while the EVM phase is still running:

```bash
seidb evm-logical-digest --backend composite --memiavl-open-mode replay \
  --flatkv-dir ~/.sei/data/state_commit/flatkv \
  --memiavl-dir ~/.sei/data/state_commit/memiavl --height H
```

Once the EVM phase has finished, use `--backend flatkv --db-dir ~/.sei/data/state_commit/flatkv --height H` on the normal node instead. The two `FINAL_DIGEST` lines must match. The tool reads the entire EVM state, so run it at a quiet time.

## Don't

- Don't set `sc-write-mode-enable-auto = true` or change `sc-write-mode`. The reserve build won't start that way, and a normal binary would start migrating and stop being a usable reserve.
- Don't state-sync a reserve, restore a snapshot into it, or copy data into it from a normal node once the migration has started.
- Don't run it as a validator, because it would vote for blocks it disagrees with. The build refuses to start that way anyway.
- Don't try to turn a reserve back into a normal node in place. To retire one, wipe it and state-sync it as a normal node with the normal binary.

## If the migration fails

The Sei team coordinates recovery and will contact reserve operators. Until then, keep the reserve running and don't touch its binary, configuration, or data. If you're asked for its data, stop the node cleanly first, then archive `~/.sei/data` and `~/.sei/wasm`.

## Troubleshooting

| Message | Meaning |
|---|---|
| `mock_chain_validation builds must not run as a validator ...` | `mode` in `config.toml` is `validator`. Set it to `full`. |
| `mock_chain_validation builds must run "memiavl_only", got "auto" ...` | `app.toml` has `sc-write-mode-enable-auto = true` or `sc-write-mode = "auto"`. Apply step 2. |
| `snapshot contains a "flatkv" section but this store has no flatkv backend ...` | State sync picked a snapshot from a migrated node. It's too late to state-sync a reserve, so copy one instead (step 3). |
| `migration requested (batch size > 0) but the SC write mode is pinned ...` | Expected on every block once the migration has started. |
| `BINARY UPDATED BEFORE TRIGGER! ...` | The next release was installed before its upgrade height. Stop the node, put the current reserve build back, and tell the Sei team. Blocks executed with the wrong binary may have left the reserve's state incorrect, and the app hash can't show it. |
