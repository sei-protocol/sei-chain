package migration

import (
	"fmt"

	ics23 "github.com/confio/ics23/go"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	db "github.com/tendermint/tm-db"
)

var _ Router = (*TestOnlyDuplicatingRouter)(nil)

// A router that duplicates write traffic, sending it to both backends. Read requests, requests for proofs, and
// requests for iteration are not duplicated, and are instead sent to the primary backend.
//
// CRITICAL: this is a test-only router and should never be deployed to production machines.
type TestOnlyDuplicatingRouter struct {
	primary   *Route
	secondary DBWriter
}

// Create a new test-only duplicating router.
//
// CRITICAL: this is a test-only router and should never be deployed to production machines.
func NewTestOnlyDuplicatingRouter(
	// All traffic is routed here.
	primary *Route,
	// Write traffic is duplicated and also sent here. Reads, proofs, and iteration are not sent here.
	secondary DBWriter,
) (Router, error) {

	if primary == nil {
		return nil, fmt.Errorf("primary must not be nil")
	}
	if primary.proofBuilder == nil {
		return nil, fmt.Errorf("primary proof builder must not be nil")
	}
	if primary.iteratorBuilder == nil {
		return nil, fmt.Errorf("primary iterator builder must not be nil")
	}
	if secondary == nil {
		return nil, fmt.Errorf("secondary must not be nil")
	}

	return &TestOnlyDuplicatingRouter{primary: primary, secondary: secondary}, nil
}

func (t *TestOnlyDuplicatingRouter) ApplyChangeSets(changesets []*proto.NamedChangeSet) error {
	err := t.primary.writer(changesets)
	if err != nil {
		return fmt.Errorf("primary writer: %w", err)
	}

	err = t.secondary(changesets)
	if err != nil {
		return fmt.Errorf("secondary writer: %w", err)
	}

	return nil
}

func (t *TestOnlyDuplicatingRouter) GetProof(store string, key []byte) (*ics23.CommitmentProof, error) {
	proof, err := t.primary.proofBuilder(store, key)
	if err != nil {
		return nil, fmt.Errorf("primary proof builder: %w", err)
	}
	return proof, nil
}

func (t *TestOnlyDuplicatingRouter) Iterator(
	store string,
	start []byte,
	end []byte,
	ascending bool,
) (db.Iterator, error) {
	iterator, err := t.primary.iteratorBuilder(store, start, end, ascending)
	if err != nil {
		return nil, fmt.Errorf("primary iterator builder: %w", err)
	}
	return iterator, nil
}

func (t *TestOnlyDuplicatingRouter) Read(store string, key []byte) ([]byte, bool, error) {
	value, found, err := t.primary.reader(store, key)
	if err != nil {
		return nil, false, fmt.Errorf("primary reader: %w", err)
	}
	return value, found, nil
}
