# Running a memIAVL reserve node for the FlatKV migration on mainnet

This guide is for mainnet, `pacific-1`. For testnet, `atlantic-2`, use the [testnet guide](memiavl_reserve_node.md).

The FlatKV migration moves chain state out of memIAVL and into FlatKV in phases, starting with the EVM module (see the [migration README](https://github.com/sei-protocol/sei-chain/blob/main/sei-db/state_db/sc/migration/README.md)). It only runs forward: once a node has moved data into FlatKV, its memIAVL no longer has that data.

A reserve node is a full node that never migrates. It keeps all state in memIAVL and still executes every block, so if the migration goes badly wrong across the fleet, the reserves hold a complete, current copy of the state in the old layout for the fleet to recover from.

Reserves run a special build of `seid`, compiled with the `mock_chain_validation` build tag from a `<release>-memiavl-reserve` branch. When the fleet starts migrating, the fleet's app hash changes and a reserve's no longer matches it. A normal binary would stop at the first mismatched block, but the reserve build counts the mismatch in a metric and keeps going.

To run one, build the reserve branch with `BUILD_TAGS=mock_chain_validation`, run it as a non-validator with `sc-write-mode = "memiavl_only"` and `sc-write-mode-enable-auto = false`, and have it synced before governance starts the migration. Mainnet gets the migration code with the `v6.7` chain upgrade, so a node you prepare before then runs the normal `v6.6` binary up to the upgrade height and the reserve build from there. After that, leave it alone apart from chain upgrades.

Paths below assume the default home directory, `~/.sei`.

## Before you start

The migration starts when a governance proposal raises the `NumKeysToMigratePerBlock` parameter above 0. Setting it back to 0 later only pauses the migration. To check the current value:

```bash
seid query params subspace migration NumKeysToMigratePerBlock
```

The parameter arrives with the `v6.7` upgrade. Until then the query fails with `migration: unknown subspace`, and the migration can't have started.

Your normal nodes also mark the start: they create their FlatKV directory, `~/.sei/data/state_commit/flatkv`, at that moment.

Have your reserves running and synced before then. Once the migration is under way, reserves can no longer state-sync, because snapshots from migrated nodes contain FlatKV data that a reserve refuses to import. From that point, a new reserve has to be copied from an existing one.

The reserve binary must match the release the network runs. Reserve branches are named `<release>-memiavl-reserve`, so for `v6.7.0` the branch is [`v6.7.0-memiavl-reserve`](https://github.com/sei-protocol/sei-chain/tree/v6.7.0-memiavl-reserve) (head `2bff7d5a9`). Its only code changes from `v6.7.0` are a startup guard and the reserve config defaults. If the network moves to a release that has no reserve branch yet, ask the Sei team.

`pacific-1` stays on `v6.6` (`v6.6.3` at the time of writing) until it takes the `v6.7` upgrade, which moves it to `v6.7.0`. There is no reserve branch for `v6.6`, and none is needed, because `v6.6` can't start the governance-driven migration. To see what a node runs, ask it with `curl -s localhost:26657/abci_info | jq -r .response.version`. `seid query upgrade plan` prints `no upgrade scheduled` until the upgrade proposal has passed, and the upgrade height after that.

Size the machine like your current RPC nodes, since a reserve stores state the same way they do today.

## 1. Build the binary

```bash
git clone https://github.com/sei-protocol/sei-chain.git
cd sei-chain
git checkout v6.7.0-memiavl-reserve
make build BUILD_TAGS=mock_chain_validation
./build/seid version --long | grep -E '^(version|commit|build_tags):'
```

`build_tags` must include `mock_chain_validation`:

```
version: v6.7.0-7-g2bff7d5a9
commit: 2bff7d5a9a53f37277de7645c86a4caf9f64d9e1
build_tags: netgo ledger mock_chain_validation,
```

Some versions of `make` separate the tags with commas instead, as in `netgo,ledger,mock_chain_validation`.

`make build` leaves the reserve build in `build/seid` and doesn't touch the `seid` your node runs. Step 3 says when to put it in place. Don't run `make install` on a machine where a node still runs `v6.6`: it overwrites `$(go env GOPATH)/bin/seid`, the binary a node installed with `make install` runs, and that node's next restart would start the reserve build before the upgrade height (see [Upgrades](#upgrades)).

`build/seid` loads its wasm libraries from the checkout, so leave the checkout unchanged for as long as a node runs the build, and build later releases in a separate clone.

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

Running `seid init` with the reserve binary writes all three values for you. On an existing home directory, including a `v6.6` node's, set them yourself. `v6.6` already defaults to `sc-write-mode = "memiavl_only"` and ignores `sc-write-mode-enable-auto`, so the node can carry these settings before the upgrade. The reserve binary treats a missing `sc-write-mode-enable-auto` as `false`, but write the key out anyway so the file says what the node does.

Also turn on Prometheus (`prometheus = true` under `[instrumentation]` in `config.toml`), because the checks below read a metric from `:26660/metrics`. Everything else can stay the way you run your normal RPC nodes.

The reserve binary refuses to start if the node is set up as a validator or has auto mode on. [Troubleshooting](#troubleshooting) has the exact messages.

## 3. Load the data

Installing the reserve build means copying `build/seid` over the binary the node's service starts, which `systemctl cat seid` shows. Under Cosmovisor that binary is `$DAEMON_HOME/cosmovisor/current/bin/seid`.

Before the `v6.7` upgrade, start from a `pacific-1` full node on the normal `v6.6` binary: an existing RPC node, or a new one set up with a normal state sync. Apply step 2 to it, but leave its `v6.6` binary in place. When it stops at the upgrade height with `UPGRADE "v6.7" NEEDED at height: <height>`, install the reserve build and start the node. With Cosmovisor, instead copy `build/seid` to `$DAEMON_HOME/cosmovisor/upgrades/v6.7/bin/seid` before the upgrade height, and Cosmovisor switches to it at the halt. The directory is named after the upgrade, `v6.7`, not the release.

After the upgrade but before the migration starts, do one of these:

- Convert an existing RPC node that runs `v6.7.0`. Stop it, install the reserve build, and apply step 2.
- Set up a new node with a normal state sync, with the reserve build installed and step 2 in place before the first start. The snapshot it restores has to be from after the upgrade height. If the node logs `BINARY UPDATED BEFORE TRIGGER` after the sync, the snapshot was older: wipe the node and sync it again later.

For a state sync at either point, the script in the [SeiDB migration guide](https://github.com/sei-protocol/sei-chain/blob/main/docs/migration/seidb_migration.md#step-3-state-sync) works with `CHAIN_ID="pacific-1"` and `PRIMARY_ENDPOINT` set to a `pacific-1` RPC node you trust. Sei's [state sync guide](https://docs.sei.io/node/statesync) lists public ones.

After the migration has started, copy `~/.sei/data` and `~/.sei/wasm` from a stopped reserve instead. Never copy data from a normal node.

In every case, confirm there is no FlatKV store before the reserve build's first start:

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

Before the migration starts, this must print nothing, because the reserve should still agree with the network on every block. Any output means it already disagrees, most likely because the binary was built from the wrong release or started before its upgrade height. Fix that before the migration starts.

## When the migration starts

The node logs this at ERROR level when the migration starts, and again after a restart or when governance sets a new batch size:

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

Every chain upgrade needs a reserve build of the new release, made from that release's `-memiavl-reserve` branch with `make build BUILD_TAGS=mock_chain_validation`, as in step 1.

Let the node stop at the upgrade height with `UPGRADE "<name>" NEEDED at height: <height>` as usual (the reserve build still halts there), then install the new reserve build and start it again.

Never install it early. A normal build of the new release panics if it starts before the upgrade height, but the reserve build only logs `BINARY UPDATED BEFORE TRIGGER` and carries on, so it executes blocks with the new code too soon. Before the upgrade proposal has passed there is no upgrade plan on chain, and the reserve build doesn't even log that. On `pacific-1` that includes `v6.7` itself: don't start the `v6.7.0` reserve build before the `v6.7` upgrade height.

With Cosmovisor, copy the reserve build to `$DAEMON_HOME/cosmovisor/upgrades/<name>/bin/seid` yourself, where `<name>` is the upgrade plan's name in lower case (`seid query upgrade plan` shows it), and leave `DAEMON_ALLOW_DOWNLOAD_BINARIES` off, so Cosmovisor doesn't fetch the normal release binary.

Once a node runs the reserve build, don't run the normal `seid` on it again. It stops at the first block whose app hash doesn't match.

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
seidb evm-logical-digest --backend composite --memiavl-open-mode changelog \
  --flatkv-dir ~/.sei/data/state_commit/flatkv \
  --memiavl-dir ~/.sei/data/state_commit/memiavl --height H
```

`--memiavl-open-mode changelog` reads the newest memIAVL snapshot at or below `H` plus the changelog above it, so the normal node doesn't need a snapshot at `H`. It gives the same result as `--memiavl-open-mode replay`, much faster and with far less memory: on a `pacific-1` memIAVL-only node 85,000 blocks past its snapshot, a digest took 405 seconds and peaked at 24 GB of memory this way, against 2,286 seconds and 142 GB with replay ([#4419](https://github.com/sei-protocol/sei-chain/pull/4419)). Add it to the reserve's command too if you pick a height the reserve has no snapshot for.

Once the EVM phase has finished, use `--backend flatkv --db-dir ~/.sei/data/state_commit/flatkv --height H` on the normal node instead. The two `FINAL_DIGEST` lines must match. The tool reads the entire EVM state, so run it at a quiet time.

## Don't

- Don't set `sc-write-mode-enable-auto = true` or change `sc-write-mode`. The reserve build won't start that way, and a normal binary would start migrating and stop being a usable reserve.
- Don't state-sync a reserve, restore a snapshot into it, or copy data into it from a normal node once the migration has started.
- Don't run it as a validator, because it would vote for blocks it disagrees with. The build refuses to start that way anyway.
- Don't try to turn a reserve back into a normal node in place. To retire one, wipe it and state-sync it as a normal node with the normal binary.
- Don't start the reserve build on `pacific-1` before the `v6.7` upgrade height, or install it anywhere a `v6.6` node's service would start it, as `make install` does. Until then the node runs the normal `v6.6` binary.

## If the migration fails

The Sei team coordinates recovery and will contact reserve operators. Until then, keep the reserve running and don't touch its binary, configuration, or data. If you're asked for its data, stop the node cleanly first, then archive `~/.sei/data` and `~/.sei/wasm`.

## Troubleshooting

| Message | Meaning |
|---|---|
| `mock_chain_validation builds must not run as a validator ...` | `mode` in `config.toml` is `validator`. Set it to `full`. |
| `mock_chain_validation builds must run "memiavl_only", got "auto" ...` | `app.toml` has `sc-write-mode-enable-auto = true` or `sc-write-mode = "auto"`. Apply step 2. |
| `migration: unknown subspace` | The chain, or the node you queried, is still on `v6.6`. The migration can't have started. |
| `snapshot contains a "flatkv" section but this store has no flatkv backend ...` | State sync picked a snapshot from a migrated node. It's too late to state-sync a reserve, so copy one instead (step 3). |
| `migration requested (batch size > 0) but the SC write mode is pinned ...` | Expected once the migration has started. It repeats after a restart and when governance sets a new batch size. |
| `BINARY UPDATED BEFORE TRIGGER! ...` | A release was started before its upgrade height, or a state sync restored a snapshot from before it. On a node that was already a reserve, stop it, put the previous build back, and tell the Sei team: blocks executed with the wrong binary may have left the reserve's state incorrect, and the app hash can't show it. On a node that wasn't a reserve yet, wipe it and redo step 3. |
