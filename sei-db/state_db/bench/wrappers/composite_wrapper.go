package wrappers

import (
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

func (c *compositeWrapper) ApplyChangeSets(cs []*proto.NamedChangeSet) error {
	return c.base.ApplyChangeSets(cs)
}

func (c *compositeWrapper) Commit() (int64, error) {
	return c.base.Commit()
}

func (c *compositeWrapper) LoadVersion(version int64) error {
	_, err := c.base.LoadVersion(version, false)
	return err
}

func (c *compositeWrapper) Version() int64 {
	return c.base.WorkingCommitInfo().Version
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
