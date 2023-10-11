package memiavl

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
)

// ErrorExportDone is returned by Exporter.Next() when all items have been exported.
var ErrorExportDone = errors.New("export is complete")

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

func NewMultiTreeExporter(dir string, version uint32, supportExportNonSnapshotVersion bool) (exporter *MultiTreeExporter, err error) {
	var (
		db    *DB
		mtree *MultiTree
	)
	if supportExportNonSnapshotVersion {
		db, err = Load(dir, Options{
			TargetVersion:       version,
			ZeroCopy:            true,
			ReadOnly:            true,
			SnapshotWriterLimit: DefaultSnapshotWriterLimit,
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
			return nil, fmt.Errorf("snapshot is not created yet: height: %d", version)
		}
		mtree, err = LoadMultiTree(filepath.Join(dir, snapshotName(int64(version))), true, 0)
		if err != nil {
			return nil, fmt.Errorf("snapshot don't exists: height: %d, %w", version, err)
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
			if err == ErrorExportDone {
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
		return nil, ErrorExportDone
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

type exportWorker func(callback func(*ExportNode) bool)

type Exporter struct {
	ch     <-chan *ExportNode
	cancel context.CancelFunc
}

func newExporter(worker exportWorker) *Exporter {
	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan *ExportNode, exportBufferSize)
	go func() {
		defer close(ch)
		worker(func(enode *ExportNode) bool {
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

func (e *Exporter) Next() (*ExportNode, error) {
	if exportNode, ok := <-e.ch; ok {
		return exportNode, nil
	}
	return nil, ErrorExportDone
}

// Close closes the exporter. It is safe to call multiple times.
func (e *Exporter) Close() {
	e.cancel()
	for range e.ch { // drain channel
	}
}
