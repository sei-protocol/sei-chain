package hashlog

import "github.com/sei-protocol/sei-chain/sei-db/proto"

// Logs the hash of each block.
//
// This is, first and foremost, a debugging tool. It produces an easy to consume record of block hashes that
// can be used to study, analize, and verify block hashes as computed by a node.
type HashLogger interface {

	// Report the diff for a block's state.
	ReportDiff(blockNumber uint64, cs []*proto.NamedChangeSet)

	// Report the hash of the FlatKV state after processing a block. If FlatKV is not enabled, this should
	// be called with a nil hash.
	ReportFlatKVHash(blockNumber uint64, hash []byte) error

	// Report the hash of the MemIAVL state after processing a block. If MemIAVL is not enabled, this should
	// be called with a nil hash.
	ReportMemIAVLHash(blockNumber uint64, hash []byte) error

	// Report the root hash of the block, which should be the hash of the block header.
	ReportRootHash(blockNumber uint64, hash []byte) error

	// Shut down the HashLogger and release any resources. Flushes pending writes before returning.
	Close() error
}
