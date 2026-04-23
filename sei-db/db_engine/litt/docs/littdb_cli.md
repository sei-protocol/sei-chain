# Installation

The LittDB CLI is not currently distributed as a pre-built binary. This may change in the future, but for now,
you will need to build it from source.

## Building from source

Make sure you have the latest version of Go installed. You can find instructions for installing Go
[here](https://go.dev/doc/install).

Clone the EigenDA repository:

```bash
git clone https://github.com/Layr-Labs/eigenda.git
```
Build the LittDB CLI:

```bash
cd eigenda/litt
make build
```

The LittDB CLI binary will be located at `eigenda/litt/bin/litt`.

### Optional: Shortcuts

If you want to be able to run the LittDB CLI from anywhere, you can do one of the following:

Create an alias in your shell configuration file (e.g. `.bashrc`, `.zshrc`, etc.):

```bash
alias litt='path/to/eigenda/litt/bin/litt'
```

Or, you can add the `eigenda/litt/bin` directory to your `PATH` environment variable:

```bash
export PATH="$PATH:path/to/eigenda/litt/bin"
```

Or you can just copy the `litt` binary to a directory that is already in your `PATH`, such as `/usr/local/bin`:

```bash
cp eigenda/litt/bin/litt /usr/local/bin/
```

A symlink can also be created to the `litt` binary in a directory that is already in your `PATH`:

```bash
ln -s path/to/eigenda/litt/bin/litt /usr/local/bin/litt
```

### Help! I'm trying to run on Windows!

Heh, good luck!

# Sources and Destinations

Many LittDB commands operate on the concept of "sources" and "destinations". A source/destination is a path where
LittDB data is stored. For commands that require source directories, those directories can be specified using the
`--src` or `-s` flag. For commands that require a destination directory, the `--dst` or `-d` flag is used.

LittDB can be configured to store data in just a single directory, or it can be configured to store data across
multiple directories. This can be useful if you want to spread data between multiple physical drives. When
using the LittDB CLI, it is important to always provide ALL source directories. If you do not do this, the CLI will
detect the problem and abort the operation.

## EigenDA Validator: Source Directories

If you are running an EigenDA validator node, the source directories are determined by the following flags:

### Recommended: `NODE_LITT_DB_STORAGE_PATHS`

If `NODE_LITT_DB_STORAGE_PATHS` is set, then the source directories will be the paths specified in that variable.

Example:
```
export NODE_LITT_DB_STORAGE_PATHS="/data0,/data1,/data2"

litt ls --src /data0 --src /data1 --src /data2
```

### Deprecated: `NODE_DB_PATH`

If `NODE_LITT_DB_STORAGE_PATHS` is not set, then the source directory will be determined by the value of
`NODE_DB_PATH`. The source directory will be `$NODE_DB_PATH/chunk_v2_litt`.

Note that this pattern is deprecated. It is suggested that you use the LittDB CLI to refactor your DB as described
in the "bonus example" [here](#litt-rebase).

Example:
```
export NODE_DB_PATH=/data

litt ls --src /data/chunk_v2_litt
```

# Subcommands

## `littdb --help`

Prints a help message.


## `litt ls`

A utility for listing the names of all tables in a LittDB instance.

For documentation on command flags and configuration, run `litt ls --help`.

Example:

Suppose you have a LittDB instance with data stored in `/data0`, `/data1`, and `/data2`, and suppose you have
tables named `tableA`, `tableB`, and `tableC`. You can list the tables in the instance by running:

```
$ litt ls --src /data0 --src /data1 --src /data2

Jun 18 11:28:59.732 INF cli/ls.go:47 Tables found:
tableA
tableB
tableC
```

## `litt table-info`

This utility provides information about the data contained in a LittDB table.

For documentation on command flags and configuration, run `litt table-info --help`.

Example:

Suppose you have a LittDB instance with data stored in `/data0`, `/data1`, and `/data2`, and want to get information
about the `tableA` table. You can run:

```
$ litt table-info --src /data0 --src /data1 --src /data2 tableA

Jun 18 11:32:11.236 INF cli/table_info.go:76 Table:                       tableA
Jun 18 11:32:11.236 INF cli/table_info.go:77 Key count:                   95
Jun 18 11:32:11.236 INF cli/table_info.go:78 Size:                        190.01 MiB
Jun 18 11:32:11.236 INF cli/table_info.go:79 Is snapshot:                 false
Jun 18 11:32:11.236 INF cli/table_info.go:80 Oldest segment age:          1.05 hours
Jun 18 11:32:11.236 INF cli/table_info.go:81 Oldest segment seal time:    2025-06-18T10:29:02-05:00
Jun 18 11:32:11.236 INF cli/table_info.go:82 Newest segment age:          50.88 minutes
Jun 18 11:32:11.236 INF cli/table_info.go:83 Newest segment seal time:    2025-06-18T10:41:18-05:00
Jun 18 11:32:11.236 INF cli/table_info.go:84 Segment span:                12.27 minutes
Jun 18 11:32:11.236 INF cli/table_info.go:85 Lowest segment index:        0
Jun 18 11:32:11.236 INF cli/table_info.go:86 Highest segment index:       95
Jun 18 11:32:11.236 INF cli/table_info.go:87 Key map type:                LevelDBKeymap
```

## `litt rebase`

LittDB can store data in multiple directories. Changing the number of directories after data has been written into 
the DB is possible, but not easy to do by hand. The `litt rebase` utility automates this workflow.

For documentation on command flags and configuration, run `litt rebase --help`.

Before rebasing, you must know two things:

- the list of directories where the DB is currently storing its data (called the "source directories")
- the list of directories where you want the DB to store its data after the rebase (called the "destination directories")

If your destination directories are a superset of the source directories, then the rebase will be a no-op. Adding a new
directory to LittDB does not require a rebase, since LittDB can dynamically add new directories as needed.

A rebase operation is idempotent. That is to say, running it more than once has the same effect as running it exactly 
once. If your computer crashes half way though a rebase, simply run the same command again, and the rebase utility will
pick up where it left off.

Example:

Suppose you have a LittDB instance with data stored in `/data0`, `/data1`, and `/data2`, and you want to rebase to the
directories `/data2`, `/data3`, and `/data4`. (Notice there is overlap between the sources and destinations, this is 
ok!)

You can run the following command:

```
litt rebase --src /data0 --src /data1 --src /data2 --dst /data2 --dst /data3 --dst /data4
```

Bonus example:

Suppose you are running an EigenDA validator node and want to change from using the deprecated `NODE_DB_PATH` flag
to instead using the recommended `NODE_LITT_DB_STORAGE_PATHS` flag. Suppose your old path for `NODE_DB_PATH` was
`/data` (meaning the LittDB source directory is `/data/chunk_v2_litt`), and you instead use 
`NODE_LITT_DB_STORAGE_PATHS="/data0,/data1,/data2"`. This can be done with the following command:

```
litt rebase --src /data/chunk_v2_litt --dst /data0 --dst /data1 --dst /data2
```

## `litt benchmark`

The LittDB benchmark can be launched using the `litt benchmark` command. This may be useful for determining the
capability of hardware in various configurations, or for testing the performance of LittDB itself.

The LittDB benchmark accepts a single argument, which is a path to a configuration file. An example configuration file
is shown below:

```json
{
  "LittConfig": {
    "Paths": ["~/benchmark/volume1", "~/benchmark/volume2", "~/benchmark/volume3"],
  },
  "MaximumWriteThroughputMB": 1024,
  "MetricsLoggingPeriodSeconds": 1
}
```

For more documentation on possible configuration options, see the 
[benchmark_config.go](../benchmark/config/benchmark_config.go). 

## `litt prune`

The `litt prune` command is used to delete data from a LittDB database or snapshot. LittDB snapshots are not
automatically pruned, so if no action is taken, then the size of the snapshot on disk will grow indefinitely 
(at least until you fill up your disk).

For documentation on command flags and configuration, run `litt prune --help`.

The `--max-age` flag is used to specify the maximum age of data to keep, and is specified in seconds.

Example:

Suppose you have a LittDB instance with data stored in `/data0`, `/data1`, and `/data2`, and you want to prune all
data that is older than 1 hour. You can run the following command:

```
litt prune --src /data0 --src /data1 --src /data2 --max-age 3600
```

## `litt push`

Although it is perfectly safe from a concurrency perspective to make copies of the data in the LittDB snapshot
directory, there are some nuances involved in doing so. The `litt push` command is a utility that can be used to
push data from a LittDB snapshot to a remote location using `ssh` and `rsync`. The `litt push` utility also deletes
data from the snapshot directory after it has been successfully pushed to the remote location.

For documentation on command flags and configuration, run `litt push --help`.

Similar to the LittDB's capability to store data in multiple directories, the `litt push` command can also push data to
multiple remote directories (on the same machine). This may be convenient if your data size is sufficiently large that
it is difficult to provision a single disk that is large enough to hold the entire data set.

`litt push` makes incremental/rolling backups. That is to say, if you make a backup at time T1, and then make a backup
at time T2, then `litt push` will only copy data written into the DB between T1 and T2.

As long as you are working from a snapshot directory, there is no need to stop the LittDB instance while you are
making a backup. Backups made with `litt push` are fully consistent. If a backup fails for some reason 
(e.g. a network issue or a computer crash), running the same command again will pick up where it left off.

Suppose your LittDB instance is storing snapshot data in `/snapshot`, and you want to push that data to directories
`/backup1`, `/backup2`, and `/backup3` on a remote machine with the username `user` and hostname `host`. You can run
the following command:

```
litt push --src /snapshot --dst /backup1 --dst /backup2 --dst /backup3 user@host
```

This command will copy over all data since the previous backup, and will delete data from the snapshot directory
once it has been successfully transferred.

### Restoring from a Backup

To restore data from a backup, simply use `litt push` on the backup machine to push the data where it needs to go.
`litt push` can push from multiple source directories if that is how it is being stored.

### Backup Garbage Collection

If you are using the patterns described above to back up data, then the size of your backup will grow indefinitely.
In order to prune the data you keep, use `litt prune` on the backup machine to delete old data. You should not run
`litt prune` concurrently with `litt push`, as there are race conditions that can occur if you do so.
