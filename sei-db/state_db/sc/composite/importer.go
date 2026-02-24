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
	if si.cosmosImporter != nil {
		return si.cosmosImporter.AddModule(name)
	}
	return nil
}

func (si *SnapshotImporter) AddNode(node *types.SnapshotNode) {
	if si.cosmosImporter != nil {
		si.cosmosImporter.AddNode(node)
	}
	if si.evmImporter != nil && si.currentModule == "evm" && node.Height == 0 {
		si.evmImporter.AddNode(node)
	}
}
