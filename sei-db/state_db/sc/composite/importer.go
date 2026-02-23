package composite

import (
	"errors"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
)

var _ types.Importer = (*StateImporter)(nil)

type StateImporter struct {
	cosmosImporter types.Importer
	evmImporter    types.Importer
	currentModule  string
}

func NewImporter(cosmosImporter types.Importer, evmImporter types.Importer) *StateImporter {
	return &StateImporter{
		cosmosImporter: cosmosImporter,
		evmImporter:    evmImporter,
	}
}

func (si *StateImporter) Close() error {
	var errCosmos, errEVM error
	if si.cosmosImporter != nil {
		errCosmos = si.cosmosImporter.Close()
	}
	if si.evmImporter != nil {
		errEVM = si.evmImporter.Close()
	}
	return errors.Join(errCosmos, errEVM)
}

func (si *StateImporter) AddModule(name string) error {
	si.currentModule = name
	var errCosmos, errEVM error
	if si.cosmosImporter != nil {
		errCosmos = si.cosmosImporter.AddModule(name)
	}
	if si.evmImporter != nil {
		errEVM = si.evmImporter.AddModule(name)
	}
	return errors.Join(errCosmos, errEVM)
}

func (si *StateImporter) AddNode(node *types.SnapshotNode) {
	if si.cosmosImporter != nil {
		si.cosmosImporter.AddNode(node)
	}
	if si.evmImporter != nil && si.currentModule == "evm" && node.Height == 0 {
		si.evmImporter.AddNode(node)
	}
}
