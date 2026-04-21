package composite

import (
	"errors"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
)

var _ types.Importer = (*SnapshotImporter)(nil)

type SnapshotImporter struct {
	cosmosImporter types.Importer
	flatkvImporter types.Importer
	currentModule  string
}

func NewImporter(cosmosImporter types.Importer, flatkvImporter types.Importer) *SnapshotImporter {
	return &SnapshotImporter{
		cosmosImporter: cosmosImporter,
		flatkvImporter: flatkvImporter,
	}
}

func (si *SnapshotImporter) AddModule(name string) error {
	si.currentModule = name
	if name == keys.FlatKVStoreKey {
		if si.flatkvImporter != nil {
			return si.flatkvImporter.AddModule(name)
		}
		return nil
	} else if si.cosmosImporter != nil {
		return si.cosmosImporter.AddModule(name)
	}
	return nil
}

func (si *SnapshotImporter) AddNode(node *types.SnapshotNode) {
	if si.currentModule == keys.FlatKVStoreKey {
		if si.flatkvImporter != nil {
			si.flatkvImporter.AddNode(node)
		}
		return
	}
	if si.cosmosImporter != nil {
		si.cosmosImporter.AddNode(node)
	}
}

func (si *SnapshotImporter) Close() error {
	var errCosmos, errFlatKV error
	if si.cosmosImporter != nil {
		errCosmos = si.cosmosImporter.Close()
	}
	if si.flatkvImporter != nil {
		errFlatKV = si.flatkvImporter.Close()
	}
	return errors.Join(errCosmos, errFlatKV)
}
