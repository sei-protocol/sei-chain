State execution currently produces two "primitive" data types on disk:

- StateWAL data, i.e. a stream of key-value pairs that change each block
- StateSnapshots, i.e. on-disk pebble checkpoints that contain a copy of the enire state at a particular block height

This is a design scratchpad for Project WALRUS: "Write Ahead Log Reconstructing Unmaterialzed State"

The goal is to have an "SS" style query engine. That is, for any (key, block number) tuple, we should be able to
determine the value for the key at the end of the specified block number. Current SS imple is an MVCC style store
using pebble. That's ok, but it doesn't scale well. We are targeting 1 million+ updates per second with possibly
indefinate data retention. Keys will mostly be EVM things, so sizes will be EVM sizes, for the most part (but not 100%).

We will be changing the WAL format. Not changing existing code, just kind of writing our own for this prototype.
If we decide to adopt this schema in production, we might change the StateWAL format. But migration is out of scope for now.
It's ok to borrow StateWAL machinery for this task if we can do so in a way that makes the StateWAL either more generic
or expose extra features. Not clear if this is the right choice though.

The overall algorithm is straight forward. A user says "I want the data for key K at block B". A niave implementation
would be to take a state snapshot at or before block B, then replay the StateWAL until block K. A much better way:
search backwards through the WAL starting at block B. The first time you find an entry for key K, that's the value
we need to return for the query. Return it and stop. If while iterating backwards we don't find the key, if we hit
a block where we've taken a snapshot, just get the value from within the snapshot.

The whole challenge then becomes; how can we do an efficient search? Below describes an indexing schema to make
the search efficient.

Group blocks into "pods". A pod is a sequence of contiguous blocks. We will write the WAL data for a pod into a single
file in the WAL. Blocks will not span pods. Each pod MUST be 4gb or less on disk. (Note: we can prepend metadata that
pushes it over 4gb, but the data inside the pod must be indexable with a 32 bit unsigned integer.) When building pods,
we should keep appending blocks until we discover that the addition of the next block will cause it to be oversized.

For each pod, create a bloom fitler with a configurable false positive rate. This is the most course level of index.
When searching, we only need to open up pod WAL files and search them if we get a hit on the bloom filter.

Bloom filters make it so we don't need to search many pods. But searching a pod is still expensive at 4gb each. We should
create a "pod index" for each pod. The idea is to have an index that lets us find the entry we are looking for in a pod
in O(log n) time, where n is the number of entries in the pod. 


The entry point of a pod index is a sorted list of 4 byte "pointers". Not golang pointers, these are just 32 bit
integers that index into later positions in the pod index. Each of these pointers corresponds to a unique key
that appears in the pod, in lexographical order. That's what I mean by sorted... before creating this we must
first sort unique keys in the pod into lexographic order.

The pod entry pointers each point to the following data:

[key size in bytes][key bytes][sorted list of tuples (block number where key was changed, byte position of key in WAL file)]

Using this data, it's possible to find the value for the key in question in O(log n) time for a specific block, or
to determine that there is no appropriate key at the block in question.

To walk this index we are going to do two binary searches. The first is to find if the key is present in the pod,
and the second is to find where the value is for the block height requested. So we probably want an abstract
binary search algorithm that is generic enough to work for both traversals.





When building pod bloom filters and indexes, we don't need to worry about queries for data in the pod thats currently
under construction. Rather, we should have a return value that basically says "data not ready yet" or similar. Queries
for very recent history will be served via mechanisms that are out of scope of this current task.

Pod bloom filter and index creation may be freely parallelized, since they are functionally non-overlapping.