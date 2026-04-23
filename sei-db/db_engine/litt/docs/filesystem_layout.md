# Filesystem Layout

This document provides an overview of how LittDB stores data on disk.

## Root Directories

LittDB spreads its data across N root directories. In practice, each root directory will probably be on its
own physical drive, but that's not a hard requirement.

In the example below, the root directories are `root/root0`, `root/root1`, and `root/root2`.

## Table Directories

LittDB supports multiple tables, each with its own namespace. Each table is stored within its own
subdirectory. 

The name of the table's subdirectory is the name of the table (hence the restrictions on characters allowed in 
table names). Each table will have one subdirectory per root.

In the example below, there are three tables: `tableA`, `tableB`, and `tableC`. The full paths to the table directories
in the example below are as follows:

- for `tableA`:
    - `root/root0/tableA`
    - `root/root1/tableA`
    - `root/root2/tableA`
for `tableB`:
    - `root/root0/tableB`
    - `root/root1/tableB`
    - `root/root2/tableB`
for `tableC`:
    - `root/root0/tableC`
    - `root/root1/tableC`
    - `root/root2/tableC`

## Keymap Directory

All keymap data appears in the directory named `keymap`. There is one keymap per table, so if there are multiple
tables in a DB then there may be multiple keymap directories.

- The file `keymap/keymap-type.txt` contains the name of the keymap implementation. 
- The file `keymap/initialized` is a marker file used to indicate if a keymap has been fully initialized or not 
  (relevant if the process crashes during keymap initialization). 
- If the keymap writes data to disk (e.g. levelDB, as pictured below), then the data will be stored in the 
  `keymap/data` directory.

Even if there are multiple root paths, each table only has a single keymap directory. The directory will be located
inside the table directory in exactly one of the root directories. It doesn't matter which root directory contains the
keymap directory.

In the example below, keymap directories are located at the following paths:

- `root/root0/tableA/keymap`
- `root/root0/tableB/keymap`
- `root/root0/tableC/keymap`

If the DB is shut down, it's safe to delete the entire `keymap` directory. On the next startup, LittDB will
recreate the keymap directory and reinitialize the keymap.

## Segment Files

There are three types of files that contain data for a segment

- metadata: these files take the form `N.metadata`, where `N` is the segment number. These files contain a small amount
  of metadata about the segment.
- keys: these files take the form `N.keys`, where `N` is the segment number. These files contain the keys for the
  segment.
- values: these files take the form `N-M.values`, where `N` is the segment number and `M` is the shard number.
  These files contain the values for the segment.

Segment files appear in the `segments` subdirectory of a table directory. Segments for a table may be spread across
different root directories. It's unimportant which root directory contains each segment file. It's perfectly ok
to move a segment file from one root directory to another while the DB is not running.

In the example below, segment files can be found in the following paths:

- `root/root0/tableA/segments`
- `root/root1/tableA/segments`
- `root/root2/tableA/segments`
- `root/root0/tableB/segments`
- `root/root1/tableB/segments`
- `root/root2/tableB/segments`
- `root/root0/tableC/segments`
- `root/root1/tableC/segments`
- `root/root2/tableC/segments`

## Snapshot Files

If enabled, LittDB will periodically capture a rolling snapshot of its data. This snapshot can be used to make backups.
In the example below, the rolling snapshot is stored in the `root/rolling_snapshot` directory (this is configurable).

The data in the rolling snapshot directory are symlinks. This is needed since LittDB data may be spread across
multiple physical volumes, and we really don't want to do a deep copy of the data in order to create a snapshot.
LittDB files are immutable, so there is no risk of the data being "pulled out from under" the snapshot.

The snapshot files point to hard linked copies of the segment files. For each volume, there is a directory named
`snapshot` that contains these hard linked files. The reason for this is to protect the snapshot data from being
deleted by the LittDB garbage collector. LittDB links the snapshot files, and it is the responsibility of the
external user/tooling to delete the snapshot files when they are no longer needed (both the symlinks and the hard 
links).

Within the snapshot directory, there are also files named `lower-bound.txt` and `upper-bound.txt`. These files
are used for communication between the DB and tooling that manages LittDB snapshots.

## Lock Files

LittDB writes lock files to each root directory it operates on. This acts as a sanity check to ensure that multiple
processes do not attempt to access/modify the same file tree in an unsafe way. The lock file is named `litt.lock`.

If a LittDB process crashes before cleaning up its lock files, no action is needed. LittDB will automatically
remove the lock files on the next startup as long as the old process is no longer running. If the old process
is hanging, then it will be necessary to kill the process before starting a new one.

The LittDB CLI also uses lock files in the same way. This ensures that the CLI does not attempt to operate on LittDB
files in unsafe ways, such as deleting files that are currently being managed by a running LittDB process.

In the example below, lock files can be found at the following paths:

- `root/root0/litt.lock`
- `root/root1/litt.lock`
- `root/root2/litt.lock`

## Example Layout

The following is an example file tree for a simple LittDB instance.
(This example file tree was generated using generate_example_tree_test.go.)

### Root Directories

There are three directories into which data is written. In theory, these could be located on three separate
physical drives. Those directories are

- `root/root0`
- `root/root1`
- `root/root2`

The table is configured to have four shards. That's one more shard than root directory, meaning that one of the
root directories will have two shards, and all the others will have one shard.

### Tables

There are three tables, each with its own namespace. The tables are

- `tableA`
- `tableB`
- `tableC`

### Segments

A little data has been written to the DB.

- `tableA` has enough data to have three segments
- `tableB` has enough data to have two segments
- `tableC` has enough data to have one segment

### Keymap

The keymap is implemented using levelDB.

### Snapshot

The DB has been configured to take a rolling snapshot, and the target directory is `root/rolling_snapshot`.

### File Tree

```text
root
├── rolling_snapshot
│   ├── tableA
│   │   ├── lower-bound.txt
│   │   ├── segments
│   │   │   ├── 0-0.values -> root/root1/tableA/snapshot/0-0.values
│   │   │   ├── 0-1.values -> root/root2/tableA/snapshot/0-1.values
│   │   │   ├── 0-2.values -> root/root0/tableA/snapshot/0-2.values
│   │   │   ├── 0-3.values -> root/root1/tableA/snapshot/0-3.values
│   │   │   ├── 0.keys -> root/root0/tableA/snapshot/0.keys
│   │   │   ├── 0.metadata -> root/root0/tableA/snapshot/0.metadata
│   │   │   ├── 1-0.values -> root/root1/tableA/snapshot/1-0.values
│   │   │   ├── 1-1.values -> root/root2/tableA/snapshot/1-1.values
│   │   │   ├── 1-2.values -> root/root0/tableA/snapshot/1-2.values
│   │   │   ├── 1-3.values -> root/root1/tableA/snapshot/1-3.values
│   │   │   ├── 1.keys -> root/root0/tableA/snapshot/1.keys
│   │   │   ├── 1.metadata -> root/root0/tableA/snapshot/1.metadata
│   │   │   ├── 2-0.values -> root/root1/tableA/snapshot/2-0.values
│   │   │   ├── 2-1.values -> root/root2/tableA/snapshot/2-1.values
│   │   │   ├── 2-2.values -> root/root0/tableA/snapshot/2-2.values
│   │   │   ├── 2-3.values -> root/root1/tableA/snapshot/2-3.values
│   │   │   ├── 2.keys -> root/root0/tableA/snapshot/2.keys
│   │   │   └── 2.metadata -> root/root0/tableA/snapshot/2.metadata
│   │   └── upper-bound.txt
│   ├── tableB
│   │   ├── lower-bound.txt
│   │   ├── segments
│   │   │   ├── 0-0.values -> root/root1/tableB/snapshot/0-0.values
│   │   │   ├── 0-1.values -> root/root2/tableB/snapshot/0-1.values
│   │   │   ├── 0-2.values -> root/root0/tableB/snapshot/0-2.values
│   │   │   ├── 0-3.values -> root/root1/tableB/snapshot/0-3.values
│   │   │   ├── 0.keys -> root/root0/tableB/snapshot/0.keys
│   │   │   ├── 0.metadata -> root/root0/tableB/snapshot/0.metadata
│   │   │   ├── 1-0.values -> root/root1/tableB/snapshot/1-0.values
│   │   │   ├── 1-1.values -> root/root2/tableB/snapshot/1-1.values
│   │   │   ├── 1-2.values -> root/root0/tableB/snapshot/1-2.values
│   │   │   ├── 1-3.values -> root/root1/tableB/snapshot/1-3.values
│   │   │   ├── 1.keys -> root/root0/tableB/snapshot/1.keys
│   │   │   └── 1.metadata -> root/root0/tableB/snapshot/1.metadata
│   │   └── upper-bound.txt
│   └── tableC
│       ├── lower-bound.txt
│       └── segments
├── root0
│   ├── litt.lock
│   ├── tableA
│   │   ├── keymap
│   │   │   ├── data
│   │   │   │   ├── 000001.log
│   │   │   │   ├── CURRENT
│   │   │   │   ├── LOCK
│   │   │   │   ├── LOG
│   │   │   │   └── MANIFEST-000000
│   │   │   ├── initialized
│   │   │   └── keymap-type.txt
│   │   ├── segments
│   │   │   ├── 0-2.values
│   │   │   ├── 0.keys
│   │   │   ├── 0.metadata
│   │   │   ├── 1-2.values
│   │   │   ├── 1.keys
│   │   │   ├── 1.metadata
│   │   │   ├── 2-2.values
│   │   │   ├── 2.keys
│   │   │   ├── 2.metadata
│   │   │   ├── 3-2.values
│   │   │   ├── 3.keys
│   │   │   └── 3.metadata
│   │   ├── snapshot
│   │   │   ├── 0-2.values
│   │   │   ├── 0.keys
│   │   │   ├── 0.metadata
│   │   │   ├── 1-2.values
│   │   │   ├── 1.keys
│   │   │   ├── 1.metadata
│   │   │   ├── 2-2.values
│   │   │   ├── 2.keys
│   │   │   └── 2.metadata
│   │   └── table.metadata
│   ├── tableB
│   │   ├── keymap
│   │   │   ├── data
│   │   │   │   ├── 000001.log
│   │   │   │   ├── CURRENT
│   │   │   │   ├── LOCK
│   │   │   │   ├── LOG
│   │   │   │   └── MANIFEST-000000
│   │   │   ├── initialized
│   │   │   └── keymap-type.txt
│   │   ├── segments
│   │   │   ├── 0-2.values
│   │   │   ├── 0.keys
│   │   │   ├── 0.metadata
│   │   │   ├── 1-2.values
│   │   │   ├── 1.keys
│   │   │   ├── 1.metadata
│   │   │   ├── 2-2.values
│   │   │   ├── 2.keys
│   │   │   └── 2.metadata
│   │   ├── snapshot
│   │   │   ├── 0-2.values
│   │   │   ├── 0.keys
│   │   │   ├── 0.metadata
│   │   │   ├── 1-2.values
│   │   │   ├── 1.keys
│   │   │   └── 1.metadata
│   │   └── table.metadata
│   └── tableC
│       ├── keymap
│       │   ├── data
│       │   │   ├── 000001.log
│       │   │   ├── CURRENT
│       │   │   ├── LOCK
│       │   │   ├── LOG
│       │   │   └── MANIFEST-000000
│       │   ├── initialized
│       │   └── keymap-type.txt
│       ├── segments
│       │   ├── 0-2.values
│       │   ├── 0.keys
│       │   └── 0.metadata
│       ├── snapshot
│       └── table.metadata
├── root1
│   ├── litt.lock
│   ├── tableA
│   │   ├── segments
│   │   │   ├── 0-0.values
│   │   │   ├── 0-3.values
│   │   │   ├── 1-0.values
│   │   │   ├── 1-3.values
│   │   │   ├── 2-0.values
│   │   │   ├── 2-3.values
│   │   │   ├── 3-0.values
│   │   │   └── 3-3.values
│   │   └── snapshot
│   │       ├── 0-0.values
│   │       ├── 0-3.values
│   │       ├── 1-0.values
│   │       ├── 1-3.values
│   │       ├── 2-0.values
│   │       └── 2-3.values
│   ├── tableB
│   │   ├── segments
│   │   │   ├── 0-0.values
│   │   │   ├── 0-3.values
│   │   │   ├── 1-0.values
│   │   │   ├── 1-3.values
│   │   │   ├── 2-0.values
│   │   │   └── 2-3.values
│   │   └── snapshot
│   │       ├── 0-0.values
│   │       ├── 0-3.values
│   │       ├── 1-0.values
│   │       └── 1-3.values
│   └── tableC
│       ├── segments
│       │   ├── 0-0.values
│       │   └── 0-3.values
│       └── snapshot
└── root2
    ├── litt.lock
    ├── tableA
    │   ├── segments
    │   │   ├── 0-1.values
    │   │   ├── 1-1.values
    │   │   ├── 2-1.values
    │   │   └── 3-1.values
    │   └── snapshot
    │       ├── 0-1.values
    │       ├── 1-1.values
    │       └── 2-1.values
    ├── tableB
    │   ├── segments
    │   │   ├── 0-1.values
    │   │   ├── 1-1.values
    │   │   └── 2-1.values
    │   └── snapshot
    │       ├── 0-1.values
    │       └── 1-1.values
    └── tableC
        ├── segments
        │   └── 0-1.values
        └── snapshot
```
