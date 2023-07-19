# Alternative IAVL Implementation

## Changelog

* 11 Jan 2023: Initial version
* 13 Jan 2023: Change changeset encoding from protobuf to plain one
* 17 Jan 2023:
  * Add delete field to change set to support empty value
  * Add section about compression on snapshot format
* 27 Jan 2023:
  * Update metadata file format
  * Encode key length with 4 bytes instead of 2.
* 24 Feb 2023:
  * Reduce node size without hash from 32bytes to 16bytes, leverage properties of post-order traversal.
  * Merge key-values into single kvs file, build optional MPHF hash table to index it.


## The Journey

It started for an use case of verifying the state change sets, we need to replay the change sets to rebuild IAVL tree and check the final IAVL root hash, compare the root hash with the on-chain hash to verify the integrity of the change sets.

The first implementation keeps the whole IAVL tree in memory, mutate nodes in-place, and don't update hashes for the intermediate versions, and one insight from the test run is it runs surprisingly fast. For the distribution store in our testnet, it can process from genesis to block `6698242` in 2 minutes, which is around `55818` blocks per second.

To support incremental replay, we further designed an IAVL snapshot format that's stored on disk, while supporting random access with mmap, which solves the memory usage issue, and reduce the time of replaying.

## New Design

So the new idea is we can put the snapshot and change sets together, the change sets is the write-ahead-log for the IAVL tree.

It also integrates well with versiondb, because versiondb can also be derived from change sets to provide query service. IAVL tree is only used for consensus state machine and merkle proof generations.

### Advantages

- Better write amplification, we only need to write the change sets in real time which is much more compact than IAVL nodes, IAVL snapshot can be created in much lower frequency.
- Better read amplification, the IAVL snapshot is a plain file, the nodes are referenced with offset, the read amplification is simply 1.
- Better space amplification, the archived change sets are much more compact than current IAVL tree, in our test case, the ratio could be as large as 1:100. We don't need to keep too old IAVL snapshots, because versiondb will handle the historical key-value queries, IAVL tree only takes care of merkle proof generations for blocks within an unbonding period. In very rare cases that do need IAVL tree of very old version, you can always replay the change sets from the genesis.

## File Formats

> NOTICE: the integers are always encoded with little endianness.

### Change Set File

```
version: 8
size:    8         // size of whole payload
payload:
  delete: 1
  keyLen: varint-uint64
  key
  [                 // if delete is false
    valueLen: varint-uint64
    value
  ]

repeat with next version
```

- Change set files can be splited with certain block ranges for incremental backup and restoration.

- Historical files can be compressed with zlib, because it doesn't need to support random access.

### IAVL Snapshot

IAVL snapshot is composed by four files:

- `metadata`, 16bytes:

  ```
  magic: 4
  format: 4
  version: 4
  root node index: 4
  ```

- `nodes`, array of fixed size(16+32bytes) nodes, the node format is like this:

  ```
  # branch
  height   : 1
  _padding : 3
  version  : 4
  size     : 4
  key node : 4
  hash     : [32]byte

  # leaf
  height      : 1
  _padding    : 3
  version     : 4
  key offset  : 8
  hash        : [32]byte
  ```
  The node has fixed length, can be indexed directly. The nodes references each other with the node index, nodes are written with post-order depth-first traversal, so the root node is always placed at the end.

  For branch node, the `key node` field reference the smallest leaf node in the right branch, the key slice is fetched from there indirectly, the leaf nodes stores the `offset` into the `kvs` file, where the key and value slices can be built.

  The branch node's left/child node indexes are inferenced from existing information and properties of post-order traversal:

  ```
  right child index = self index - 1
  left child index = key node - 1
  ```

  The version/size/node indexes are encoded with 4 bytes, should be enough in foreseeable future, but could be changed to more bytes in the future.

  The implementation will read the mmap-ed content in a zero-copy way, won't use extra node cache, it will only rely on the OS page cache.

- `kvs`, sequence of leaf node key-value pairs, the keys are ordered and no duplication.

  ```
  keyLen: varint-uint64
  key
  valueLen: varint-uint64
  value
  *repeat*
  ```

#### Compression

The items in snapshot reference with each other by file offsets, we can apply some block compression techniques to compress keys and values files while maintain random accessbility by uncompressed file offset, for example zstd's experimental seekable format[^1].

### VersionDB

[VersionDB](../README.md) is to support query and iterating historical versions of key-values pairs, currently implemented with rocksdb's experimental user-defined timestamp feature, support query and iterate key-value pairs by version, it's an alternative way to support grpc query service, and much more compact than IAVL trees, similar in size with the compressed change set files.

After versiondb is fully integrated, IAVL tree don't need to serve queries at all, it don't need to store the values at all, just store the value hashes would be enough.

[^1]: https://github.com/facebook/zstd/blob/dev/contrib/seekable_format/zstd_seekable_compression_format.md
