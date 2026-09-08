package wrappers

import (
	"github.com/sei-protocol/sei-chain/sei-db/common/metrics"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/giga"
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
	// The benchmark wrapper interface carries no height, so the next one is derived here. That is
	// sound only because nothing in the benchmark path takes a block's hash before committing it.
	return c.base.Commit(c.base.Version() + 1)
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

// RegisterHashListener reports that this DB publishes no block hashes. The composite store consumes
// flatKV's hashes itself, in order to answer Cosmos synchronously, so it admits no second consumer.
func (c *compositeWrapper) RegisterHashListener(_ giga.HashListener) (bool, error) {
	return false, nil
}

func (c *compositeWrapper) GetPhaseTimer() *metrics.PhaseTimer {
	return nil
}
