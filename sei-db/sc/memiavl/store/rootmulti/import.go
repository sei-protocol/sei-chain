package rootmulti

import (
	"fmt"
	"io"
	"math"

	"cosmossdk.io/errors"
	snapshottypes "github.com/cosmos/cosmos-sdk/snapshots/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	protoio "github.com/gogo/protobuf/io"
	"github.com/sei-protocol/sei-db/memiavl/db"
)

// Implements interface Snapshotter
func (rs *Store) Restore(
	height uint64, format uint32, protoReader protoio.Reader,
) (snapshottypes.SnapshotItem, error) {
	if rs.db != nil {
		if err := rs.db.Close(); err != nil {
			return snapshottypes.SnapshotItem{}, fmt.Errorf("failed to close db: %w", err)
		}
		rs.db = nil
	}

	item, err := rs.restore(height, format, protoReader)
	if err != nil {
		return snapshottypes.SnapshotItem{}, err
	}

	return item, rs.LoadLatestVersion()
}

func (rs *Store) restore(
	height uint64, _ uint32, protoReader protoio.Reader,
) (snapshottypes.SnapshotItem, error) {
	importer, err := memiavl.NewMultiTreeImporter(rs.dir, height)
	if err != nil {
		return snapshottypes.SnapshotItem{}, err
	}
	defer importer.Close()

	var snapshotItem snapshottypes.SnapshotItem
loop:
	for {
		snapshotItem = snapshottypes.SnapshotItem{}
		err := protoReader.ReadMsg(&snapshotItem)
		if err == io.EOF {
			break
		} else if err != nil {
			return snapshottypes.SnapshotItem{}, errors.Wrap(err, "invalid protobuf message")
		}

		switch item := snapshotItem.Item.(type) {
		case *snapshottypes.SnapshotItem_Store:
			if err := importer.AddTree(item.Store.Name); err != nil {
				return snapshottypes.SnapshotItem{}, err
			}
		case *snapshottypes.SnapshotItem_IAVL:
			if item.IAVL.Height > math.MaxInt8 {
				return snapshottypes.SnapshotItem{}, errors.Wrapf(sdkerrors.ErrLogic, "node height %v cannot exceed %v",
					item.IAVL.Height, math.MaxInt8)
			}
			node := &memiavl.ExportNode{
				Key:     item.IAVL.Key,
				Value:   item.IAVL.Value,
				Height:  int8(item.IAVL.Height),
				Version: item.IAVL.Version,
			}
			// Protobuf does not differentiate between []byte{} as nil, but fortunately IAVL does
			// not allow nil keys nor nil values for leaf nodes, so we can always set them to empty.
			if node.Key == nil {
				node.Key = []byte{}
			}
			if node.Height == 0 && node.Value == nil {
				node.Value = []byte{}
			}
			importer.AddNode(node)
		default:
			// unknown element, could be an extension
			break loop
		}
	}

	if err := importer.Finalize(); err != nil {
		return snapshottypes.SnapshotItem{}, err
	}

	return snapshotItem, nil
}
