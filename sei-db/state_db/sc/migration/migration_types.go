package migration

import (
	"fmt"

	ics23 "github.com/confio/ics23/go"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
)

// MigrationStatus is the lifecycle status of a migration.
type MigrationStatus int

const (
	// MigrationNotStarted means the migration has not yet started. All keys are considered unmigrated.
	MigrationNotStarted MigrationStatus = 0
	// MigrationInProgress means the migration is in progress. Some keys are migrated, some are not.
	MigrationInProgress MigrationStatus = 1
	// MigrationComplete means the migration is complete. All keys are considered migrated.
	MigrationComplete MigrationStatus = 2
)

func (s MigrationStatus) String() string {
	switch s {
	case MigrationNotStarted:
		return "not_started"
	case MigrationInProgress:
		return "in_progress"
	case MigrationComplete:
		return "complete"
	default:
		return fmt.Sprintf("unknown(%d)", int(s))
	}
}

// Write a batch of values to the database.
//
// May not be atomic. If not atomic, then the caller must provide crash safe atomicity.
//
// firstBatchInBlock is true when this write is the first ApplyChangeSets
// call in the caller's current block-commit cycle. Leaf-level writers
// (memiavl, flatkv) should ignore it; only the MigrationManager
// consumes it, to advance the migration boundary at most once per
// block. The parameter is plumbed through DBWriter (rather than only
// living on Router) because MigrationManager.BuildRoute exposes its
// ApplyChangeSets as a DBWriter, so a leaf writer and a router writer
// must share the same shape.
type DBWriter func(changesets []*proto.NamedChangeSet, firstBatchInBlock bool) error

// Read a value from the database.
type DBReader func(store string, key []byte) ([]byte, bool, error)

// Builds a proof of the value for a key in a store.
type DBProofBuilder func(store string, key []byte) (*ics23.CommitmentProof, error)

// An object capable of routing/splitting reads and writes between databases.
type Router interface {
	// Read a value from the database.
	Read(store string, key []byte) ([]byte, bool, error)

	// Apply a batch of change sets to the database.
	//
	// Not atomic. Caller is responsible for providing crash safe atomicity.
	//
	// If this method returns an error, it is not safe to attempt to retry. An error should be considered
	// fatal, and should result in any managed databases being shut down and crash recovered.
	//
	// firstBatchInBlock is true when this is the first ApplyChangeSets call in
	// the caller's current block-commit cycle. Non-migration routers should
	// ignore it; migration routers use it to advance the migration boundary at
	// most once per block.
	ApplyChangeSets(changesets []*proto.NamedChangeSet, firstBatchInBlock bool) error

	// Get a proof of the value for a key in a store. Some stores may not support proofs,
	// and this method will return an error in that case.
	GetProof(store string, key []byte) (*ics23.CommitmentProof, error)

	// SetMigrationBatchSize updates the number of keys migrated per block.
	//
	// Only routers that perform data migration act on this; every other
	// router treats it as a no-op. Composite/wrapper routers forward it to
	// the underlying migration manager. A value of 0 pauses the migration
	// (caller writes still route normally; only the background key transfer
	// stops advancing).
	//
	// Must be called between blocks, on the same goroutine that drives
	// ApplyChangeSets. threadSafeRouter additionally serializes it against
	// concurrent reads.
	SetMigrationBatchSize(batchSize int)
}
