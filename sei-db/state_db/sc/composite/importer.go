package composite

import (
	"errors"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
)

var _ types.Importer = (*SnapshotImporter)(nil)

type SnapshotImporter struct {
	cosmosImporter types.Importer
	evmImporter    types.Importer
	currentModule  string
}

func NewImporter(cosmosImporter types.Importer, evmImporter types.Importer) *SnapshotImporter {
	return &SnapshotImporter{
		cosmosImporter: cosmosImporter,
		evmImporter:    evmImporter,
	}
}

func (si *SnapshotImporter) Close() error {
	var errCosmos, errEVM error
	if si.cosmosImporter != nil {
		errCosmos = si.cosmosImporter.Close()
	}
	if si.evmImporter != nil {
		errEVM = si.evmImporter.Close()
	}
	return errors.Join(errCosmos, errEVM)
}

func (si *SnapshotImporter) AddModule(name string) error {
	si.currentModule = name
	if name == EVMFlatKVStoreName {
		if si.evmImporter != nil {
			return si.evmImporter.AddModule(name)
		}
		return nil
	}
	if si.cosmosImporter != nil {
		return si.cosmosImporter.AddModule(name)
	}
	return nil
}

func (si *SnapshotImporter) AddNode(node *types.SnapshotNode) {
	if si.currentModule == EVMFlatKVStoreName {
		if si.evmImporter != nil {
			si.evmImporter.AddNode(node)
		}
		return
	}
	if si.cosmosImporter != nil {
		si.cosmosImporter.AddNode(node)
	}
}
