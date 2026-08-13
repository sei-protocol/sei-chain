package wrappers

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/common/metrics"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/composite"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
)

var _ DBWrapper = (*compositeWrapper)(nil)

// compositeWrapper wraps a composite commit store to implement the DBWrapper interface.
type compositeWrapper struct {
	base *composite.CompositeCommitStore
}

// NewCompositeWrapper creates a new compositeWrapper with a given composite commit store.
func NewCompositeWrapper(store *composite.CompositeCommitStore) DBWrapper {
	return &compositeWrapper{
		base: store,
	}
}

func (c *compositeWrapper) ApplyChangeSets(entry *proto.ChangelogEntry) error {
	return c.base.ApplyChangeSets(entry.Changesets)
}

func (c *compositeWrapper) Commit() (int64, error) {
	return c.base.Commit()
}

func (c *compositeWrapper) LoadLatest() error {
	return c.base.LoadLatest()
}

func (c *compositeWrapper) Version() int64 {
	return c.base.Version()
}

func (c *compositeWrapper) Importer(version int64) (types.Importer, error) {
	return c.base.Importer(version)
}

func (c *compositeWrapper) Close() error {
	return c.base.Close()
}

func (c *compositeWrapper) Read(key []byte) (data []byte, found bool, err error) {
	store := c.base.GetChildStoreByName(EVMStoreName)
	data = store.Get(key)
	return data, data != nil, nil
}

// AwaitBlockHash asks the composite store for the version's lattice hash, which is what drains flatkv's hash
// stream. A composite store with no flatkv backend has no lattice hash and nothing to drain.
func (c *compositeWrapper) AwaitBlockHash(version int64) error {
	if !c.base.HasFlatKV() {
		return nil
	}
	if _, err := c.base.LatticeHash(version); err != nil {
		return fmt.Errorf("await composite lattice hash at version %d: %w", version, err)
	}
	return nil
}

func (c *compositeWrapper) GetPhaseTimer() *metrics.PhaseTimer {
	return nil
}
