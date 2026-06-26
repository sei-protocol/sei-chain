package migration

import (
	"fmt"

	ics23 "github.com/confio/ics23/go"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
)

var _ Router = (*PassthroughRouter)(nil)

// PassthroughRouter implements Router for single-backend modes where
// every operation goes to the same destination regardless of store
// name. Unlike ModuleRouter it holds no name -> backend map and
// performs no per-call name lookup: each method forwards its arguments
// straight to the supplied accessor. The wrapped accessors are
// themselves responsible for surfacing "unknown store" errors when the
// backend does not recognize the name.
//
// Used by MemiavlOnly mode where every store lives on memiavl and
// memiavl already reports unknown child stores from
// GetChildStoreByName.
type PassthroughRouter struct {
	reader       DBReader
	writer       DBWriter
	proofBuilder DBProofBuilder
}

// NewPassthroughRouter builds a router that forwards every operation
// to the supplied accessors. The reader and writer are required.
// proofBuilder is optional: when nil, GetProof returns an error
// describing the missing capability (e.g. flatkv has no proof builder).
func NewPassthroughRouter(
	reader DBReader,
	writer DBWriter,
	proofBuilder DBProofBuilder,
) (*PassthroughRouter, error) {
	if reader == nil {
		return nil, fmt.Errorf("reader must not be nil")
	}
	if writer == nil {
		return nil, fmt.Errorf("writer must not be nil")
	}
	return &PassthroughRouter{
		reader:       reader,
		writer:       writer,
		proofBuilder: proofBuilder,
	}, nil
}

// Read forwards directly to the wrapped reader.
func (p *PassthroughRouter) Read(store string, key []byte) ([]byte, bool, error) {
	return p.reader(store, key)
}

// ApplyChangeSets forwards directly to the wrapped writer. The router
// performs no per-changeset name validation; the writer (and its
// backing store) is the sole authority on which names it accepts.
func (p *PassthroughRouter) ApplyChangeSets(changesets []*proto.NamedChangeSet, firstBatchInBlock bool) error {
	return p.writer(changesets, firstBatchInBlock)
}

// GetProof forwards to the wrapped proof builder. If no proof builder
// was supplied, returns an error describing the limitation.
func (p *PassthroughRouter) GetProof(store string, key []byte) (*ics23.CommitmentProof, error) {
	if p.proofBuilder == nil {
		return nil, fmt.Errorf("proofs not supported by passthrough router (store=%q)", store)
	}
	return p.proofBuilder(store, key)
}

// SetMigrationBatchSize is a no-op: a passthrough router targets a single
// backend and performs no data migration.
func (p *PassthroughRouter) SetMigrationBatchSize(int) {}
