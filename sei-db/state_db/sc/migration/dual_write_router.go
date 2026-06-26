package migration

import (
	"fmt"

	ics23 "github.com/confio/ics23/go"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
)

var _ Router = (*TestOnlyDualWriteRouter)(nil)

// A router that dual-writes traffic, sending each batch of changesets to both backends. Read
// requests and requests for proofs are not dual-written, and are instead
// served exclusively by the primary backend.
//
// CRITICAL: this is a test-only router and should never be deployed to production machines.
type TestOnlyDualWriteRouter struct {
	primary   *Route
	secondary DBWriter
}

// Create a new test-only dual-write router.
//
// CRITICAL: this is a test-only router and should never be deployed to production machines.
func NewTestOnlyDualWriteRouter(
	// Read and proof traffic is served by this route, and writes are also sent here.
	// Module names associated with this route are ignored; this route forwards all regardless of the module names.
	primary *Route,
	// Write traffic is dual-written and also sent here. Reads and proofs are not sent here.
	secondary DBWriter,
) (*TestOnlyDualWriteRouter, error) {

	if primary == nil {
		return nil, fmt.Errorf("primary must not be nil")
	}
	if primary.proofBuilder == nil {
		return nil, fmt.Errorf("primary proof builder must not be nil")
	}
	if secondary == nil {
		return nil, fmt.Errorf("secondary must not be nil")
	}

	return &TestOnlyDualWriteRouter{primary: primary, secondary: secondary}, nil
}

func (t *TestOnlyDualWriteRouter) ApplyChangeSets(changesets []*proto.NamedChangeSet, firstBatchInBlock bool) error {
	err := t.primary.writer(changesets, firstBatchInBlock)
	if err != nil {
		return fmt.Errorf("primary writer: %w", err)
	}

	err = t.secondary(changesets, firstBatchInBlock)
	if err != nil {
		return fmt.Errorf("secondary writer: %w", err)
	}

	return nil
}

func (t *TestOnlyDualWriteRouter) GetProof(store string, key []byte) (*ics23.CommitmentProof, error) {
	proof, err := t.primary.proofBuilder(store, key)
	if err != nil {
		return nil, fmt.Errorf("primary proof builder: %w", err)
	}
	return proof, nil
}

func (t *TestOnlyDualWriteRouter) Read(store string, key []byte) ([]byte, bool, error) {
	value, found, err := t.primary.reader(store, key)
	if err != nil {
		return nil, false, fmt.Errorf("primary reader: %w", err)
	}
	return value, found, nil
}

// SetMigrationBatchSize is a no-op: the dual-write router duplicates
// traffic and performs no boundary-advancing data migration.
func (t *TestOnlyDualWriteRouter) SetMigrationBatchSize(int) {}

// BuildRoute returns a Route that dispatches the given module names to
// this DualWriteRouter. Reads, writes and proof requests for those
// modules will all flow through this dual-write router.
//
// Module names must be unique; NewRoute's validation rules apply. The
// returned Route may be passed to NewModuleRouter alongside other
// Routes to compose multi-database setups.
func (t *TestOnlyDualWriteRouter) BuildRoute(moduleNames ...string) (*Route, error) {
	return NewRoute(t.Read, t.ApplyChangeSets, t.GetProof, moduleNames...)
}
