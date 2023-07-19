package memiavl

import (
	stderrors "errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"

	"cosmossdk.io/errors"
	"github.com/cosmos/iavl"
	protoio "github.com/gogo/protobuf/io"

	snapshottypes "github.com/cosmos/cosmos-sdk/snapshots/types"
	storetypes "github.com/cosmos/cosmos-sdk/store/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

// Import restore memiavl db from state-sync snapshot stream
func Import(
	dir string, height uint64, format uint32, protoReader protoio.Reader,
) (snapshottypes.SnapshotItem, error) {
	if height > math.MaxUint32 {
		return snapshottypes.SnapshotItem{}, fmt.Errorf("version overflows uint32: %d", height)
	}
	snapshotDir := snapshotName(int64(height))
	tmpDir := snapshotDir + "-tmp"

	// Import nodes into stores. The first item is expected to be a SnapshotItem containing
	// a SnapshotStoreItem, telling us which store to import into. The following items will contain
	// SnapshotNodeItem (i.e. ExportNode) until we reach the next SnapshotStoreItem or EOF.
	var importer *TreeImporter
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
			if importer != nil {
				if err := importer.Close(); err != nil {
					return snapshottypes.SnapshotItem{}, err
				}
			}
			importer = NewTreeImporter(filepath.Join(dir, tmpDir, item.Store.Name), int64(height))
			defer importer.Close()
		case *snapshottypes.SnapshotItem_IAVL:
			if importer == nil {
				return snapshottypes.SnapshotItem{}, errors.Wrap(sdkerrors.ErrLogic, "received IAVL node item before store item")
			}
			if item.IAVL.Height > math.MaxInt8 {
				return snapshottypes.SnapshotItem{}, errors.Wrapf(sdkerrors.ErrLogic, "node height %v cannot exceed %v",
					item.IAVL.Height, math.MaxInt8)
			}
			node := &iavl.ExportNode{
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
			importer.Add(node)
		default:
			break loop
		}
	}

	if importer != nil {
		if err := importer.Close(); err != nil {
			return snapshottypes.SnapshotItem{}, err
		}
	}

	if err := updateMetadataFile(filepath.Join(dir, tmpDir), int64(height)); err != nil {
		return snapshottypes.SnapshotItem{}, err
	}

	if err := os.Rename(filepath.Join(dir, tmpDir), filepath.Join(dir, snapshotDir)); err != nil {
		return snapshottypes.SnapshotItem{}, err
	}

	if err := updateCurrentSymlink(dir, snapshotDir); err != nil {
		return snapshottypes.SnapshotItem{}, err
	}

	return snapshotItem, nil
}

// TreeImporter import a single memiavl tree from state-sync snapshot
type TreeImporter struct {
	nodesChan chan *iavl.ExportNode
	quitChan  chan error
}

func NewTreeImporter(dir string, version int64) *TreeImporter {
	nodesChan := make(chan *iavl.ExportNode)
	quitChan := make(chan error)
	go func() {
		defer close(quitChan)
		quitChan <- doImport(dir, version, nodesChan)
	}()
	return &TreeImporter{nodesChan, quitChan}
}

func (ai *TreeImporter) Add(node *iavl.ExportNode) {
	ai.nodesChan <- node
}

func (ai *TreeImporter) Close() error {
	var err error
	// tolerate double close
	if ai.nodesChan != nil {
		close(ai.nodesChan)
		err = <-ai.quitChan
	}
	ai.nodesChan = nil
	ai.quitChan = nil
	return err
}

// doImport a stream of `iavl.ExportNode`s into a new snapshot.
func doImport(dir string, version int64, nodes <-chan *iavl.ExportNode) (returnErr error) {
	if version > int64(math.MaxUint32) {
		return stderrors.New("version overflows uint32")
	}

	return writeSnapshot(dir, uint32(version), func(w *snapshotWriter) (uint32, error) {
		i := &importer{
			snapshotWriter: *w,
		}

		for node := range nodes {
			if err := i.Add(node); err != nil {
				return 0, err
			}
		}

		switch len(i.leavesStack) {
		case 0:
			return 0, nil
		case 1:
			return i.leafCounter, nil
		default:
			return 0, fmt.Errorf("invalid node structure, found stack size %v after imported", len(i.leavesStack))
		}
	})
}

type importer struct {
	snapshotWriter

	// keep track of how many leaves has been written before the pending nodes
	leavesStack []uint32
	// keep track of the pending nodes
	nodeStack []*MemNode
}

func (i *importer) Add(n *iavl.ExportNode) error {
	if n.Version > int64(math.MaxUint32) {
		return stderrors.New("version overflows uint32")
	}

	if n.Height == 0 {
		node := &MemNode{
			height:  0,
			size:    1,
			version: uint32(n.Version),
			key:     n.Key,
			value:   n.Value,
		}
		nodeHash := node.Hash()
		if err := i.writeLeaf(node.version, node.key, node.value, nodeHash); err != nil {
			return err
		}
		i.leavesStack = append(i.leavesStack, i.leafCounter)
		i.nodeStack = append(i.nodeStack, node)
		return nil
	}

	// branch node
	keyLeaf := i.leavesStack[len(i.leavesStack)-2]
	leftNode := i.nodeStack[len(i.nodeStack)-2]
	rightNode := i.nodeStack[len(i.nodeStack)-1]

	node := &MemNode{
		height:  uint8(n.Height),
		size:    leftNode.size + rightNode.size,
		version: uint32(n.Version),
		key:     n.Key,
		left:    leftNode,
		right:   rightNode,
	}
	nodeHash := node.Hash()

	// remove unnecessary reference to avoid memory leak
	node.left = nil
	node.right = nil

	preTrees := uint8(len(i.nodeStack) - 2)
	if err := i.writeBranch(node.version, uint32(node.size), node.height, preTrees, keyLeaf, nodeHash); err != nil {
		return err
	}

	i.leavesStack = i.leavesStack[:len(i.leavesStack)-2]
	i.leavesStack = append(i.leavesStack, i.leafCounter)

	i.nodeStack = i.nodeStack[:len(i.nodeStack)-2]
	i.nodeStack = append(i.nodeStack, node)
	return nil
}

func updateMetadataFile(dir string, height int64) (returnErr error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	storeInfos := make([]storetypes.StoreInfo, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		snapshot, err := OpenSnapshot(filepath.Join(dir, name))
		if err != nil {
			return err
		}
		defer func() {
			if err := snapshot.Close(); returnErr == nil {
				returnErr = err
			}
		}()
		storeInfos = append(storeInfos, storetypes.StoreInfo{
			Name: name,
			CommitId: storetypes.CommitID{
				Version: height,
				Hash:    snapshot.RootHash(),
			},
		})
	}
	metadata := MultiTreeMetadata{
		CommitInfo: &storetypes.CommitInfo{
			Version:    height,
			StoreInfos: storeInfos,
		},
		// initial version should correspond to the first wal entry
		InitialVersion: height + 1,
	}
	bz, err := metadata.Marshal()
	if err != nil {
		return err
	}
	return WriteFileSync(filepath.Join(dir, MetadataFileName), bz)
}
