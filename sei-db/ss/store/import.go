package store

import (
	"errors"
	"fmt"
	"io"
	"math"

	snapshottypes "github.com/cosmos/cosmos-sdk/snapshots/types"
	protoio "github.com/gogo/protobuf/io"
	"github.com/sei-protocol/sei-db/ss/types"
)

func (s *MultiStore) Restore(height uint64, _ uint32, protoReader protoio.Reader) (snapshottypes.SnapshotItem, error) {
	ch := make(chan types.ImportEntry, 1000)
	go func() {
		err := s.stateStore.Import(int64(height), ch)
		if err != nil {
			panic(err)
		}
	}()
	return restoreSnapshotEntries(protoReader, ch)
}

// processSnapshotEntries reads key-value entries from protobuf reader and feed to the channel
func restoreSnapshotEntries(protoReader protoio.Reader, ch chan types.ImportEntry) (snapshottypes.SnapshotItem, error) {
	var (
		snapshotItem snapshottypes.SnapshotItem
		storeKey     string
	)
	defer close(ch)
loop:
	for {
		snapshotItem = snapshottypes.SnapshotItem{}
		err := protoReader.ReadMsg(&snapshotItem)
		if err == io.EOF {
			break
		} else if err != nil {
			return snapshottypes.SnapshotItem{}, err
		}

		switch item := snapshotItem.Item.(type) {
		case *snapshottypes.SnapshotItem_Store:
			storeKey = item.Store.Name
		case *snapshottypes.SnapshotItem_IAVL:
			if storeKey == "" {
				return snapshottypes.SnapshotItem{}, errors.New("invalid protobuf message, store name is empty")
			}
			if item.IAVL.Height > math.MaxInt8 {
				return snapshottypes.SnapshotItem{}, fmt.Errorf("node height %v cannot exceed %v",
					item.IAVL.Height, math.MaxInt8)
			}
			if item.IAVL.Height == 0 {
				value := []byte{}
				if item.IAVL.Value != nil {
					value = item.IAVL.Value
				}
				ch <- types.ImportEntry{
					StoreKey: storeKey,
					Key:      item.IAVL.Key,
					Value:    value,
				}
			}
		default:
			break loop
		}
	}
	return snapshotItem, nil
}
