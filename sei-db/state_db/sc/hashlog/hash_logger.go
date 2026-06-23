package hashlog

import "github.com/sei-protocol/sei-chain/sei-db/proto"

// Logs the hash of each block.
//
// This is, first and foremost, a debugging tool. It produces an easy to consume record of block hashes that
// can be used to study, analyze, and verify block hashes as computed by a node.
//
// The set of hash "types" a logger records is fixed at construction time (see HashLoggerConfig.HashTypes).
// All types except the diff are supplied by the caller via ReportHash; the diff is the one type the logger
// computes itself from the raw change sets (see ReportDiff). A block is considered complete, and is written to
// disk, once a hash has been reported for every configured type.
//
// Slice ownership: every slice handed to this logger — the hash passed to ReportHash, and the change set (and
// all of its nested keys and values) passed to ReportDiff — is retained and read asynchronously on background
// goroutines after the call returns. The caller is free to keep reading these slices, but MUST NOT mutate them
// (or reuse/recycle their underlying buffers) afterwards, or it risks a data race and a corrupted hash. Pass
// freshly allocated slices, or copies, if the underlying buffers may otherwise be mutated.
type HashLogger interface {

	// Report the diff for a block's state. The logger hashes the diff itself, on a background thread, and
	// records the result under the configured diff hash type.
	//
	// Passing a nil cs is supported: it records a nil diff hash for the block without hashing anything. This is
	// the way to skip diff hashing for a particular block while diff hashing is otherwise enabled. It is
	// distinct from disabling diff hashing globally via HashLoggerConfig.DisableDiffHashing, which stops the
	// hasher thread, makes ReportDiff a complete no-op, and records no diff column at all.
	//
	// An empty (non-nil) cs is a legitimate change set for a block that made no state changes: it is hashed
	// normally, yielding the stable hash of the empty change set (not a nil hash). nil and empty are therefore
	// treated differently.
	ReportDiff(blockNumber uint64, cs []*proto.NamedChangeSet)

	// Report a hash for a block under the given type. The type must be one of the types this logger was
	// configured to record, otherwise an error is returned. The diff hash type is reserved for the
	// logger-computed diff column (use ReportDiff) and is also rejected when diff hashing is enabled. A
	// subsystem that is disabled should report a nil hash for its type rather than skipping the call, so that
	// the block can still be completed.
	ReportHash(blockNumber uint64, hashType string, hash []byte) error

	// Shut down the HashLogger and release any resources. Flushes pending writes before returning. Only blocks
	// that are complete (a hash has been reported for every configured type) are written; a block still missing a
	// hash type at shutdown is discarded rather than written as a partial record.
	//
	// To roll back — re-execute blocks at heights that have already been logged — close the logger and open a
	// new one. A reopened logger starts with nothing flushed, so it logs the re-executed blocks into a fresh
	// file even though their numbers no longer advance; the prior records remain on disk alongside them.
	// Reports for already-flushed blocks made without reopening are silently discarded.
	Close() error
}
