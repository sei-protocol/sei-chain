package memiavl

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	errorutils "github.com/sei-protocol/sei-db/common/errors"
	"github.com/sei-protocol/sei-db/common/logger"
	"github.com/sei-protocol/sei-db/config"
	"github.com/sei-protocol/sei-db/sc/types"
)

// exportBufferSize is the number of nodes to buffer in the exporter. It improves throughput by
// processing multiple nodes per context switch, but take care to avoid excessive memory usage,
// especially since callers may export several IAVL stores in parallel (e.g. the Cosmos SDK).
const exportBufferSize = 32

type MultiTreeExporter struct {
	// only one of them is non-nil
	db    *DB
	mtree *MultiTree

	iTree    int
	exporter *Exporter
}

func NewMultiTreeExporter(dir string, version uint32, onlyAllowExportOnSnapshotVersion bool) (exporter *MultiTreeExporter, err error) {
	var (
		db    *DB
		mtree *MultiTree
	)
	if !onlyAllowExportOnSnapshotVersion {
		db, err = OpenDB(logger.NewNopLogger(), int64(version), Options{
			Dir:                 dir,
			ZeroCopy:            true,
			ReadOnly:            true,
			SnapshotWriterLimit: config.DefaultSnapshotWriterLimit,
		})
		if err != nil {
			return nil, fmt.Errorf("invalid height: %d, %w", version, err)
		}
	} else {
		curVersion, err := currentVersion(dir)
		if err != nil {
			return nil, fmt.Errorf("failed to load current version: %w", err)
		}
		if int64(version) > curVersion {
			return nil, fmt.Errorf("export skipped because memiavl snapshot is not created yet for height: %d", version)
		}
		mtree, err = LoadMultiTree(filepath.Join(dir, snapshotName(int64(version))), true, 0)
		if err != nil {
			return nil, fmt.Errorf("memiavl snapshot don't exist for height: %d, %w", version, err)
		}
	}

	return &MultiTreeExporter{
		db:    db,
		mtree: mtree,
	}, nil
}

func (mte *MultiTreeExporter) trees() []NamedTree {
	if mte.db != nil {
		return mte.db.trees
	}
	return mte.mtree.trees
}

func (mte *MultiTreeExporter) Next() (interface{}, error) {
	if mte.exporter != nil {
		node, err := mte.exporter.Next()
		if err != nil {
			if errors.Is(err, errorutils.ErrorExportDone) {
				mte.exporter.Close()
				mte.exporter = nil
				return mte.Next()
			}
			return nil, err
		}
		return node, nil
	}

	trees := mte.trees()
	if mte.iTree >= len(trees) {
		return nil, errorutils.ErrorExportDone
	}
	tree := trees[mte.iTree]
	mte.exporter = tree.Export()
	mte.iTree++
	return tree.Name, nil
}

func (mte *MultiTreeExporter) Close() error {
	if mte.exporter != nil {
		mte.exporter.Close()
		mte.exporter = nil
	}

	if mte.db != nil {
		return mte.db.Close()
	}
	if mte.mtree != nil {
		return mte.mtree.Close()
	}

	return nil
}

type exportWorker func(callback func(*types.SnapshotNode) bool)

type Exporter struct {
	ch     <-chan *types.SnapshotNode
	cancel context.CancelFunc
}

func newExporter(worker exportWorker) *Exporter {
	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan *types.SnapshotNode, exportBufferSize)
	go func() {
		defer close(ch)
		worker(func(enode *types.SnapshotNode) bool {
			select {
			case ch <- enode:
			case <-ctx.Done():
				return true
			}
			return false
		})
	}()
	return &Exporter{ch, cancel}
}

func (e *Exporter) Next() (*types.SnapshotNode, error) {
	if exportNode, ok := <-e.ch; ok {
		return exportNode, nil
	}
	return nil, errorutils.ErrorExportDone
}

// Close closes the exporter. It is safe to call multiple times.
func (e *Exporter) Close() {
	e.cancel()
	for range e.ch {
		// drain channel
	}

}
