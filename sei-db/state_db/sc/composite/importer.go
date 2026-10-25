package composite

import (
	"errors"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
)

var _ types.Importer = (*SnapshotImporter)(nil)

type SnapshotImporter struct {
	cosmosImporter types.Importer
	flatkvImporter types.Importer
	// flatkvFactory lazily creates the flatkv importer the first time the
	// stream presents a keys.FlatKVStoreKey section and no importer was
	// built up front. Under types.Auto on a fresh node flatkv does not
	// exist yet; the section's presence in the snapshot is the proof that
	// it should, so the import is what materializes it. Nil when the
	// store's configuration cannot accept a flatkv section, in which case
	// such a section is rejected loudly.
	flatkvFactory func() (types.Importer, error)
	currentModule string
}

func NewImporter(
	cosmosImporter types.Importer,
	flatkvImporter types.Importer,
	flatkvFactory func() (types.Importer, error),
) *SnapshotImporter {
	return &SnapshotImporter{
		cosmosImporter: cosmosImporter,
		flatkvImporter: flatkvImporter,
		flatkvFactory:  flatkvFactory,
	}
}

func (si *SnapshotImporter) AddModule(name string) error {
	si.currentModule = name
	if name == keys.FlatKVStoreKey {
		if si.flatkvImporter == nil && si.flatkvFactory != nil {
			imp, err := si.flatkvFactory()
			if err != nil {
				return fmt.Errorf("failed to create flatkv importer for section %q: %w", name, err)
			}
			si.flatkvImporter = imp
		}
		if si.flatkvImporter == nil {
			// Silently dropping the section would restore a state tree
			// that is missing data the snapshot's AppHash commits to.
			return fmt.Errorf(
				"snapshot contains a %q section but this store has no flatkv backend "+
					"(restoring a flatkv-bearing snapshot onto a memiavl-only configuration?)",
				name)
		}
		return si.flatkvImporter.AddModule(name)
	} else if si.cosmosImporter != nil {
		return si.cosmosImporter.AddModule(name)
	}
	return nil
}

func (si *SnapshotImporter) AddNode(node *types.SnapshotNode) error {
	if si.currentModule == keys.FlatKVStoreKey {
		if si.flatkvImporter != nil {
			return si.flatkvImporter.AddNode(node)
		}
		return nil
	}
	if si.cosmosImporter != nil {
		return si.cosmosImporter.AddNode(node)
	}
	return nil
}

// Close publishes both backends' imports. When the cosmos import fails, the flatkv import is discarded
// rather than published, so a failed import does not leave flatkv ahead of memiavl.
func (si *SnapshotImporter) Close() error {
	if si.cosmosImporter != nil {
		if err := si.cosmosImporter.Close(); err != nil {
			if si.flatkvImporter != nil {
				_ = si.flatkvImporter.Abort(err)
			}
			return err
		}
	}
	if si.flatkvImporter != nil {
		return si.flatkvImporter.Close()
	}
	return nil
}

// Abort discards both backends' imports, publishing neither.
func (si *SnapshotImporter) Abort(reason error) error {
	var errCosmos, errFlatKV error
	if si.cosmosImporter != nil {
		errCosmos = si.cosmosImporter.Abort(reason)
	}
	if si.flatkvImporter != nil {
		errFlatKV = si.flatkvImporter.Abort(reason)
	}
	return errors.Join(errCosmos, errFlatKV)
}
