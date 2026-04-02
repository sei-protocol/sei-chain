package flatkv

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
)

const importBatchSize = 20000

var _ types.Importer = (*KVImporter)(nil)

type KVImporter struct {
	store   *CommitStore
	version int64
	batch   []*proto.KVPair
	err     error
}

func NewKVImporter(store *CommitStore, version int64) types.Importer {
	return &KVImporter{
		store:   store,
		version: version,
		batch:   make([]*proto.KVPair, 0, importBatchSize),
	}
}

func (imp *KVImporter) AddModule(_ string) error {
	return nil
}

func (imp *KVImporter) AddNode(node *types.SnapshotNode) {
	if imp.err != nil || node.Height != 0 || node.Key == nil {
		return
	}

	imp.batch = append(imp.batch, &proto.KVPair{Key: node.Key, Value: node.Value})
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
		Changeset: proto.ChangeSet{Pairs: imp.batch},
	}}
	if err := imp.store.ApplyChangeSets(cs); err != nil {
		imp.err = fmt.Errorf("import apply changesets: %w", err)
		logger.Error("import flush failed when apply changesets", "err", err)
		return
	}
	if err := imp.store.commitBatches(imp.version); err != nil {
		imp.err = fmt.Errorf("import commit batches: %w", err)
		logger.Error("import flush failed when commit batches", "err", err)
		return
	}
	imp.store.clearPendingWrites()
	imp.batch = make([]*proto.KVPair, 0, importBatchSize)
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

	// Write a snapshot so the imported data survives store reopen / restart.
	// Import bypasses the WAL, so without a snapshot the next LoadVersion
	// would clone from the pre-import snapshot and lose all imported data.
	if err := imp.store.WriteSnapshot(""); err != nil {
		return fmt.Errorf("import snapshot: %w", err)
	}

	return nil
}
