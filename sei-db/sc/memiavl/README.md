# MemIAVL

## Changelog
* Oct 11 2023:
  * Forked from Cronos MemIAVL(https://github.com/crypto-org-chain/cronos/tree/v1.1.0-rc4/memiavl)

## The Design
The idea of MemIAVL is to keep the whole chain state in memory as much as possible to speed up reads and writes.
- MemIAVL uses a write-ahead-log(WAL) to persist the changeset from transaction commit to speed up writes.
- Instead of updating and flushing nodes to disk, state changes at every height are actually only written to WAL file
- MemIAVL snapshots are taken periodically and written to disk to materialize the tree at some given height H
- Each snapshot is composed of 3 files per module, one for key/value pairs, one for leaf nodes and one for branch nodes
- After snapshot is taken, the snapshot files are then loaded with mmap for faster reads and lazy loading via page cache. At the same time, older WAL files will be truncated till the snapshot height
- Each MemIAVL tree is composed of 2 types of node: MemNode and Persistent Node
  - All nodes are persistent nodes to start with. Each persistent node maps to some data stored on file
  - During updates or insertion, persistent nodes will turn into MemNode
  - MemNodes are nodes stores only in memory for all future read and writes
- If a node crash in the middle of commit, it will be able to load from the last snapshot and replay the WAL file to catch up to the last committed height

### Advantages
- Better write amplification, we only need to write the change sets in real time which is much more compact than IAVL nodes, IAVL snapshot can be created in much lower frequency.
- Better read amplification, the IAVL snapshot is a plain file, the nodes are referenced with offset, the read amplification is simply 1.
- Better space amplification, the archived change sets are much more compact than current IAVL tree, in our test case, the ratio could be as large as 1:100. We don't need to keep too old IAVL snapshots, because versiondb will handle the historical key-value queries, IAVL tree only takes care of merkle proof generations for blocks within an unbonding period. In very rare cases that do need IAVL tree of very old version, you can always replay the change sets from the genesis.
- Facilitate async commit which improves commit latency by huge amount

### Trade-offs
- Performance can degrade when state size grows much larger than memory
- MemIAVL makes historical proof much slower
- Periodic snapshot creation is a very heavy operation and could become a bottleneck

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
