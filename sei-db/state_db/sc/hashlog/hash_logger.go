package hashlog

import "github.com/sei-protocol/sei-chain/sei-db/proto"

// Logs the hash of each block.
//
// This is, first and foremost, a debugging tool. It produces an easy to consume record of block hashes that
// can be used to study, analyze, and verify block hashes as computed by a node.
//
// The set of hash "types" a logger records is fixed at construction time (see HashLoggerConfig.HashTypes).
// All types except the changeset are supplied by the caller via ReportHash; the changeset is the one type the logger
// computes itself from the raw change sets (see ReportChangeset). A block is considered complete, and is written to
// disk, once a hash has been reported for every configured type.
//
// Slice ownership: every slice handed to this logger — the hash passed to ReportHash, and the change set (and
// all of its nested keys and values) passed to ReportChangeset — is retained and read asynchronously on background
// goroutines after the call returns. The caller is free to keep reading these slices, but MUST NOT mutate them
// (or reuse/recycle their underlying buffers) afterwards, or it risks a data race and a corrupted hash. Pass
// freshly allocated slices, or copies, if the underlying buffers may otherwise be mutated.
type HashLogger interface {

	// Report the changeset for a block's state. The logger hashes the changeset itself, on a background thread, and
	// records the result under the configured changeset hash type.
	//
	// Passing a nil cs is supported: it records a nil changeset hash for the block without hashing anything. This is
	// the way to skip changeset hashing for a particular block while changeset hashing is otherwise enabled. It is
	// distinct from disabling changeset hashing globally via HashLoggerConfig.DisableChangesetHashing, which stops the
	// hasher thread, makes ReportChangeset a complete no-op, and records no changeset column at all.
	//
	// An empty (non-nil) cs is a legitimate change set for a block that made no state changes: it is hashed
	// normally, yielding the stable hash of the empty change set (not a nil hash). nil and empty are therefore
	// treated differently.
	ReportChangeset(blockNumber uint64, cs []*proto.NamedChangeSet)

	// Register an additional caller-reported hash type (CSV column). This supplements the types supplied at
	// construction via HashLoggerConfig.HashTypes, letting a caller (e.g. a database that owns its own hash
	// categories) populate the column set without knowing every type up front.
	//
	// It may be called at any time, including after blocks have been logged. Because a hash log file's
	// header is fixed, changing the column set rotates the file: the logger flushes all complete blocks to
	// the current file, seals it, and opens a fresh file whose header includes the new column. Registering a
	// type that is already present is a no-op (no rotation). The reserved changeset type is rejected, as are
	// names containing characters outside the legal allow-list. Returns nil once the change has been applied
	// (the call blocks until then), so a subsequent ReportHash for the new column is accepted.
	//
	// Callers must not invoke the Register/Unregister/Report methods concurrently from multiple goroutines.
	RegisterHashType(hashType string) error

	// Unregister a previously registered caller-reported hash type, removing its column. Like
	// RegisterHashType this rotates to a fresh file whose header omits the column. Removing a type that is
	// not present is a no-op; the reserved changeset column cannot be removed.
	UnregisterHashType(hashType string) error

	// Report a hash for a block under the given type. The type must be one of the types this logger was
	// configured to record (via HashLoggerConfig.HashTypes or RegisterHashType), otherwise an error is
	// returned. The changeset hash type is reserved for the
	// logger-computed changeset column (use ReportChangeset) and is also rejected when changeset hashing is enabled. A
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
