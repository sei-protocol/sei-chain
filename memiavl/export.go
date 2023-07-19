package memiavl

import (
	"context"
	"fmt"
	"math"
	"path/filepath"

	"cosmossdk.io/errors"
	snapshottypes "github.com/cosmos/cosmos-sdk/snapshots/types"
	"github.com/cosmos/iavl"
	protoio "github.com/gogo/protobuf/io"
)

// exportBufferSize is the number of nodes to buffer in the exporter. It improves throughput by
// processing multiple nodes per context switch, but take care to avoid excessive memory usage,
// especially since callers may export several IAVL stores in parallel (e.g. the Cosmos SDK).
const exportBufferSize = 32

func (db *DB) Snapshot(height uint64, protoWriter protoio.Writer) (returnErr error) {
	if height > math.MaxUint32 {
		return fmt.Errorf("height overflows uint32: %d", height)
	}
	version := uint32(height)

	var mtree *MultiTree
	if db.supportExportNonSnapshotVersion {
		db, err := Load(db.dir, Options{
			TargetVersion: version,
			ZeroCopy:      true,
			ReadOnly:      true,
		})
		if err != nil {
			return errors.Wrapf(err, "invalid height: %d", height)
		}
		defer func() {
			if err := db.Close(); returnErr == nil {
				returnErr = err
			}
		}()

		mtree = &db.MultiTree
	} else {
		curVersion, err := currentVersion(db.dir)
		if err != nil {
			return fmt.Errorf("failed to load current version: %w", err)
		}
		if int64(version) > curVersion {
			return fmt.Errorf("snapshot is not created yet: height: %d", version)
		}
		mtree, err = LoadMultiTree(filepath.Join(db.dir, snapshotName(int64(version))), true, 0)
		if err != nil {
			return errors.Wrapf(err, "snapshot don't exists: height: %d", version)
		}
		defer func() {
			if err := mtree.Close(); returnErr == nil {
				returnErr = err
			}
		}()
	}

	for _, tree := range mtree.trees {
		if err := protoWriter.WriteMsg(&snapshottypes.SnapshotItem{
			Item: &snapshottypes.SnapshotItem_Store{
				Store: &snapshottypes.SnapshotStoreItem{
					Name: tree.name,
				},
			},
		}); err != nil {
			return err
		}

		exporter := tree.tree.Export()
		for {
			node, err := exporter.Next()
			if err == iavl.ExportDone {
				break
			} else if err != nil {
				return err
			}
			if err := protoWriter.WriteMsg(&snapshottypes.SnapshotItem{
				Item: &snapshottypes.SnapshotItem_IAVL{
					IAVL: &snapshottypes.SnapshotIAVLItem{
						Key:     node.Key,
						Value:   node.Value,
						Height:  int32(node.Height),
						Version: node.Version,
					},
				},
			}); err != nil {
				return err
			}
		}
	}

	return nil
}

type exportWorker func(callback func(*iavl.ExportNode) bool)

type Exporter struct {
	ch     <-chan *iavl.ExportNode
	cancel context.CancelFunc
}

func newExporter(worker exportWorker) *Exporter {
	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan *iavl.ExportNode, exportBufferSize)
	go func() {
		defer close(ch)
		worker(func(enode *iavl.ExportNode) bool {
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

func (e *Exporter) Next() (*iavl.ExportNode, error) {
	if exportNode, ok := <-e.ch; ok {
		return exportNode, nil
	}
	return nil, iavl.ExportDone
}

// Close closes the exporter. It is safe to call multiple times.
func (e *Exporter) Close() {
	e.cancel()
	for range e.ch { // drain channel
	}
}
