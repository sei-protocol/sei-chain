package flatkv

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
	"github.com/cosmos/iavl"
)

const importBatchSize = 20000

var _ types.Importer = (*KVImporter)(nil)

type KVImporter struct {
	store   *CommitStore
	version int64
	batch   []*iavl.KVPair
	err     error
}

func NewKVImporter(store *CommitStore, version int64) types.Importer {
	return &KVImporter{
		store:   store,
		version: version,
		batch:   make([]*iavl.KVPair, 0, importBatchSize),
	}
}

func (imp *KVImporter) AddModule(_ string) error {
	return nil
}

func (imp *KVImporter) AddNode(node *types.SnapshotNode) {
	if imp.err != nil || node.Height != 0 || node.Key == nil {
		return
	}

	imp.batch = append(imp.batch, &iavl.KVPair{Key: node.Key, Value: node.Value})
	if len(imp.batch) >= importBatchSize {
		imp.flush()
	}
}

func (imp *KVImporter) flush() {
	if len(imp.batch) == 0 {
		return
	}

	cs := []*proto.NamedChangeSet{{
		Name:      "evm",
		Changeset: iavl.ChangeSet{Pairs: imp.batch},
	}}
	if err := imp.store.ApplyChangeSets(cs); err != nil {
		imp.err = fmt.Errorf("import apply changesets: %w", err)
		imp.store.log.Error("import flush failed when apply changesets", "err", err)
		return
	}
	if err := imp.store.commitBatches(imp.version); err != nil {
		imp.err = fmt.Errorf("import commit batches: %w", err)
		imp.store.log.Error("import flush failed when commit batches", "err", err)
		return
	}
	imp.store.clearPendingWrites()
	imp.batch = make([]*iavl.KVPair, 0, importBatchSize)
}

func (imp *KVImporter) Close() error {
	if imp.err != nil {
		return imp.err
	}

	imp.flush()
	if imp.err != nil {
		return imp.err
	}

	imp.store.committedVersion = imp.version
	imp.store.committedLtHash = imp.store.workingLtHash.Clone()
	if err := imp.store.commitGlobalMetadata(imp.version, imp.store.committedLtHash); err != nil {
		return fmt.Errorf("import global metadata: %w", err)
	}

	return nil
}
