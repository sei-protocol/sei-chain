package rootmulti

import (
	"errors"
	"fmt"
	"math"

	snapshottypes "github.com/cosmos/cosmos-sdk/snapshots/types"
	protoio "github.com/gogo/protobuf/io"
	memiavl "github.com/sei-protocol/sei-db/sc/memiavl/db"
)

// Implements interface Snapshotter
func (rs *Store) Snapshot(height uint64, protoWriter protoio.Writer) error {
	if height > math.MaxUint32 {
		return fmt.Errorf("height overflows uint32: %d", height)
	}
	version := uint32(height)

	exporter, err := memiavl.NewMultiTreeExporter(rs.dir, version, rs.opts.ExportNonSnapshotVersion)
	if err != nil {
		return err
	}

	defer exporter.Close()

	for {
		item, err := exporter.Next()
		if err != nil {
			if errors.Is(err, memiavl.ErrorExportDone) {
				break
			}

			return err
		}

		switch item := item.(type) {
		case *memiavl.ExportNode:
			if err := protoWriter.WriteMsg(&snapshottypes.SnapshotItem{
				Item: &snapshottypes.SnapshotItem_IAVL{
					IAVL: &snapshottypes.SnapshotIAVLItem{
						Key:     item.Key,
						Value:   item.Value,
						Height:  int32(item.Height),
						Version: item.Version,
					},
				},
			}); err != nil {
				return err
			}
		case string:
			if err := protoWriter.WriteMsg(&snapshottypes.SnapshotItem{
				Item: &snapshottypes.SnapshotItem_Store{
					Store: &snapshottypes.SnapshotStoreItem{
						Name: item,
					},
				},
			}); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unknown item type %T", item)
		}
	}

	return nil
}
