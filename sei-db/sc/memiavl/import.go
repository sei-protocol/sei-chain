package memiavl

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"

	"github.com/sei-protocol/sei-db/proto"
	"github.com/sei-protocol/sei-db/sc/types"
)

var (
	nodeChanSize = 10000
	bufIOSize    = 64 * 1024 * 1024
)

type MultiTreeImporter struct {
	dir         string
	snapshotDir string
	height      int64
	importer    *TreeImporter
	fileLock    FileLock
}

func NewMultiTreeImporter(dir string, height uint64) (*MultiTreeImporter, error) {
	if height > math.MaxUint32 {
		return nil, fmt.Errorf("version overflows uint32: %d", height)
	}

	var fileLock FileLock
	fileLock, err := LockFile(filepath.Join(dir, LockFileName))
	if err != nil {
		return nil, fmt.Errorf("fail to lock db: %w", err)
	}

	return &MultiTreeImporter{
		dir:         dir,
		height:      int64(height),
		snapshotDir: snapshotName(int64(height)),
		fileLock:    fileLock,
	}, nil
}

func (mti *MultiTreeImporter) tmpDir() string {
	return filepath.Join(mti.dir, mti.snapshotDir+"-tmp")
}

func (mti *MultiTreeImporter) Add(item interface{}) error {
	switch item := item.(type) {
	case *types.SnapshotNode:
		mti.AddNode(item)
		return nil
	case string:
		return mti.AddTree(item)
	default:
		return fmt.Errorf("unknown item type: %T", item)
	}
}

func (mti *MultiTreeImporter) AddTree(name string) error {
	if mti.importer != nil {
		if err := mti.importer.Close(); err != nil {
			return err
		}
	}
	mti.importer = NewTreeImporter(filepath.Join(mti.tmpDir(), name), mti.height)
	return nil
}

func (mti *MultiTreeImporter) AddNode(node *types.SnapshotNode) {
	mti.importer.Add(node)
}

func (mti *MultiTreeImporter) Close() error {
	if mti.importer != nil {
		if err := mti.importer.Close(); err != nil {
			return err
		}
		mti.importer = nil
	}
	defer mti.fileLock.Unlock()
	tmpDir := mti.tmpDir()
	if err := updateMetadataFile(tmpDir, mti.height); err != nil {
		return err
	}

	if err := os.Rename(tmpDir, filepath.Join(mti.dir, mti.snapshotDir)); err != nil {
		return err
	}

	return updateCurrentSymlink(mti.dir, mti.snapshotDir)
}

// TreeImporter import a single memiavl tree from state-sync snapshot
type TreeImporter struct {
	nodesChan chan *types.SnapshotNode
	quitChan  chan error
}

func NewTreeImporter(dir string, version int64) *TreeImporter {
	nodesChan := make(chan *types.SnapshotNode, nodeChanSize)
	quitChan := make(chan error)
	go func() {
		defer close(quitChan)
		quitChan <- doImport(dir, version, nodesChan)
	}()
	return &TreeImporter{nodesChan, quitChan}
}

func (ai *TreeImporter) Add(node *types.SnapshotNode) {
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

// doImport a stream of `types.SnapshotNode`s into a new snapshot.
func doImport(dir string, version int64, nodes <-chan *types.SnapshotNode) (returnErr error) {
	if version > int64(math.MaxUint32) {
		return errors.New("version overflows uint32")
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

func (i *importer) Add(n *types.SnapshotNode) error {
	if n.Version > int64(math.MaxUint32) {
		return errors.New("version overflows uint32")
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
	storeInfos := make([]proto.StoreInfo, 0, len(entries))
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
		storeInfos = append(storeInfos, proto.StoreInfo{
			Name: name,
			CommitId: proto.CommitID{
				Version: height,
				Hash:    snapshot.RootHash(),
			},
		})
	}
	metadata := proto.MultiTreeMetadata{
		CommitInfo: &proto.CommitInfo{
			Version:    height,
			StoreInfos: storeInfos,
		},
		// initial version should correspond to the first rlog entry
		InitialVersion: height + 1,
	}
	bz, err := metadata.Marshal()
	if err != nil {
		return err
	}
	return WriteFileSync(filepath.Join(dir, MetadataFileName), bz)
}
