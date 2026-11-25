# Giga state sync

A new Autobahn fullnode starts from a FlatKV checkpoint in S3.

## Upload snapshots

On one or more fullnodes, add the following section to `config/app.toml`:

```toml
[giga.state_sync]
# Writes a state sync proof file for each FlatKV checkpoint (default false)
enabled = true
# S3 bucket of the snapshots
snapshot_bucket = "my-sei-snapshots"
# AWS region of the bucket; empty uses the AWS SDK default
snapshot_region = "us-east-2"
# Uploads snapshots to the bucket; a validator refuses to start with it (default false)
snapshot_upload = true
# Least time between two uploads of the chain, by any node (default "1h")
snapshot_upload_interval = "1h"
```

Check the access to the bucket:

```sh
seid statesync check --write
```

## Start a new node

On the new node, add the following section to `config/app.toml`:

```toml
[giga.state_sync]
enabled = true
snapshot_bucket = "my-sei-snapshots"
snapshot_region = "us-east-2"
```

On a new home, after `seid init` and before the first `seid start`:

```sh
seid statesync import
seid start
```

If the home is not `~/.sei`, add `--home <dir>` to both commands.

`seid statesync import` takes the newest snapshot. The uploading nodes write its height to the
object `latest.txt` in the bucket. To take another snapshot:

```sh
seid statesync import --height 120000
seid statesync import --from s3://my-sei-snapshots/giga-state-sync/my-chain-1/00000000000000120000.tar
```

## If the node stays at the snapshot height

After `seid start`, the node gets the blocks above the snapshot height from its peers. When the
snapshot is too old, the peers no longer keep those blocks. The node then stays at the snapshot
height. It does not stop, and it logs this warning each time a peer that no longer keeps the
blocks skips them:

```text
A peer skipped FullCommitQCs that this node needs
```

The warning names the peer. One warning is not a failure, because another peer can still send the
blocks. If the height does not increase and every peer gives the warning, no peer keeps them.

To recover, stop the node and delete the contents of the persistent state dir. This is
`persistent_state_dir` in the Autobahn config file. `seid statesync import` refuses a persistent
state dir that holds the data of a started node. Then take a newer snapshot:

```sh
seid statesync import
seid start
```
