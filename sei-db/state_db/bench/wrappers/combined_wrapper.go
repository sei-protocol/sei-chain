package wrappers

import (
	"sync/atomic"

	dbTypes "github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	scTypes "github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
)

var _ DBWrapper = (*combinedWrapper)(nil)

// combinedWrapper drives both a State Commit (SC) and State Store (SS) backend
// from the same changeset stream, mirroring production where SC and SS receive
// identical writes.
type combinedWrapper struct {
	sc        DBWrapper
	ss        dbTypes.StateStore
	ssVersion atomic.Int64
}

func NewCombinedWrapper(sc DBWrapper, ss dbTypes.StateStore) DBWrapper {
	w := &combinedWrapper{sc: sc, ss: ss}
	w.ssVersion.Store(ss.GetLatestVersion())
	return w
}

func (c *combinedWrapper) ApplyChangeSets(cs []*proto.NamedChangeSet) error {
	if err := c.sc.ApplyChangeSets(cs); err != nil {
		return err
	}
	nextVersion := c.ssVersion.Add(1)
	return c.ss.ApplyChangesetSync(nextVersion, cs)
}

func (c *combinedWrapper) Read(key []byte) (data []byte, found bool, err error) {
	return c.sc.Read(key)
}

func (c *combinedWrapper) Commit() (int64, error) {
	return c.sc.Commit()
}

func (c *combinedWrapper) Close() error {
	scErr := c.sc.Close()
	ssErr := c.ss.Close()
	if scErr != nil {
		return scErr
	}
	return ssErr
}

func (c *combinedWrapper) Version() int64 {
	return c.sc.Version()
}

func (c *combinedWrapper) LoadVersion(version int64) error {
	return c.sc.LoadVersion(version)
}

func (c *combinedWrapper) Importer(version int64) (scTypes.Importer, error) {
	return c.sc.Importer(version)
}
