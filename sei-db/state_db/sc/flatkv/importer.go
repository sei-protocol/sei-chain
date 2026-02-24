package flatkv

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
	iavl "github.com/sei-protocol/sei-chain/sei-iavl/proto"
)

const importBatchSize = 10000

var _ Importer = (*KVImporter)(nil)

type KVImporter struct {
	store   *CommitStore
	version int64
	batch   []*iavl.KVPair
}

func NewKVImporter(store *CommitStore, version int64) Importer {
	return &KVImporter{
		store:   store,
		version: version,
		batch:   make([]*iavl.KVPair, 0, importBatchSize),
	}
}

func (imp *KVImporter) AddNode(node *types.SnapshotNode) {
	if node.Height != 0 || node.Key == nil {
		return
	}

	key := node.Key
	value := node.Value
	imp.batch = append(imp.batch, &iavl.KVPair{Key: key, Value: value})
	if len(imp.batch) >= importBatchSize {
		if err := imp.flush(); err != nil {
			panic(err)
		}
	}
	imp.batch = make([]*iavl.KVPair, 0, importBatchSize)
}

func (imp *KVImporter) flush() error {
	if len(imp.batch) == 0 {
		return nil
	}

	cs := []*proto.NamedChangeSet{{
		Name:      "evm",
		Changeset: iavl.ChangeSet{Pairs: imp.batch},
	}}
	if err := imp.store.ApplyChangeSets(cs); err != nil {
		return err
	}
	if err := imp.store.commitBatches(imp.version); err != nil {
		return fmt.Errorf("import commit batches: %w", err)
	}
	imp.store.clearPendingWrites()
	return nil
}

func (imp *KVImporter) Close() error {
	err := imp.flush()
	if err != nil {
		return err
	}

	imp.store.committedVersion = imp.version
	imp.store.committedLtHash = imp.store.workingLtHash.Clone()
	if err = imp.store.commitGlobalMetadata(imp.version, imp.store.committedLtHash); err != nil {
		return fmt.Errorf("import global metadata: %w", err)
	}

	return nil
}
