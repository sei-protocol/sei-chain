package memiavl

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
)

var (
	// Pipeline channel size - controls how many operations can be queued
	// Memory footprint (worst case, all 3 channels full in snapshot.go):
	// - kvChan: 500k × ~140 bytes (key+value avg) = ~70 MiB
	// - leafChan: 500k × ~48 bytes (fixed size) = ~24 MiB
	// - branchChan: 500k × ~45 bytes (fixed size) = ~22.5 MiB
	// Total: ~116 MiB for channel buffers
	// Profiling shows peak usage of ~440k nodes with 1GB total overhead.
	// Smaller buffer causes frequent blocking and context switch overhead.
	nodeChanSize = 500000

	// bufio.Writer buffer size - 128 MiB balances performance and memory usage
	// Large buffer reduces syscalls and improves throughput for sequential writes
	bufIOSize = 128 << 20 // 128 MiB
)

type MultiTreeImporter struct {
	dir         string
	snapshotDir string
	height      int64
	importer    *TreeImporter
	fileLock    FileLock
	ctx         context.Context // Context for cancellation support
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
		ctx:         context.Background(), // Default to background context for backward compatibility
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
		return mti.AddModule(item)
	default:
		return fmt.Errorf("unknown item type: %T", item)
	}
}

func (mti *MultiTreeImporter) AddModule(name string) error {
	if mti.importer != nil {
		if err := mti.importer.Close(); err != nil {
			return err
		}
	}
	mti.importer = NewTreeImporter(mti.ctx, filepath.Join(mti.tmpDir(), name), mti.height)
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

	tmpDir := mti.tmpDir()
	if err := updateMetadataFile(tmpDir, mti.height); err != nil {
		return err
	}

	if err := os.Rename(tmpDir, filepath.Join(mti.dir, mti.snapshotDir)); err != nil {
		return err
	}

	if err := updateCurrentSymlink(mti.dir, mti.snapshotDir); err != nil {
		return err
	}
	return mti.fileLock.Unlock()
}

// TreeImporter import a single memiavl tree from state-sync snapshot
type TreeImporter struct {
	nodesChan chan *types.SnapshotNode
	quitChan  chan error
}

func NewTreeImporter(ctx context.Context, dir string, version int64) *TreeImporter {
	nodesChan := make(chan *types.SnapshotNode, nodeChanSize)
	quitChan := make(chan error)
	go func() {
		defer close(quitChan)
		quitChan <- doImport(ctx, dir, version, nodesChan)
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
func doImport(ctx context.Context, dir string, version int64, nodes <-chan *types.SnapshotNode) (returnErr error) {
	if version < 0 || version > int64(math.MaxUint32) {
		return fmt.Errorf("version under/overflows uint32: %d", version)
	}

	return writeSnapshot(ctx, dir, uint32(version), func(w *snapshotWriter) (uint32, error) {
		i := &importer{
			w:           w,
			leavesStack: make([]uint32, 0),
			nodeStack:   make([]*MemNode, 0),
		}

		// Check for context cancellation every 100k nodes or every 5 seconds to minimize overhead
		// This provides ~1 second response time for EVM tree (1B nodes / 100k = 10k checks at 1M nodes/s)
		// while guaranteeing max 5-second response time in case of slow imports
		const cancelCheckInterval = 100000
		const maxCheckInterval = 5 * time.Second
		nodeCount := 0
		lastCheck := time.Now()

		for node := range nodes {
			nodeCount++

			// Check for cancellation periodically (every 100k nodes or every 5 seconds)
			timeSinceLastCheck := time.Since(lastCheck)
			if nodeCount%cancelCheckInterval == 0 || timeSinceLastCheck >= maxCheckInterval {
				select {
				case <-ctx.Done():
					return 0, fmt.Errorf("import cancelled: %w", ctx.Err())
				default:
				}
				lastCheck = time.Now()
			}

			if err := i.Add(node); err != nil {
				return 0, err
			}
		}

		// Final check for context cancellation after loop completes
		// If context was cancelled, channel might be closed prematurely
		select {
		case <-ctx.Done():
			return 0, fmt.Errorf("import cancelled: %w", ctx.Err())
		default:
		}

		switch len(i.leavesStack) {
		case 0:
			return 0, nil
		case 1:
			return i.w.leafCounter, nil
		default:
			return 0, fmt.Errorf("invalid node structure, found stack size %v after imported", len(i.leavesStack))
		}
	})
}

type importer struct {
	w *snapshotWriter

	// keep track of how many leaves has been written before the pending nodes
	leavesStack []uint32
	// keep track of the pending nodes
	nodeStack []*MemNode
}

func (i *importer) Add(n *types.SnapshotNode) error {
	if n.Version < 0 || n.Version > math.MaxUint32 {
		return fmt.Errorf("node version under/overflows uint32: %d", n.Version)
	}
	version := uint32(n.Version)

	if n.Height == 0 {
		node := &MemNode{
			height:  0,
			size:    1,
			version: version,
			key:     n.Key,
			value:   n.Value,
		}
		nodeHash := node.Hash()
		if err := i.w.writeLeaf(node.version, node.key, node.value, nodeHash); err != nil {
			return err
		}
		i.leavesStack = append(i.leavesStack, i.w.leafCounter)
		i.nodeStack = append(i.nodeStack, node)
		return nil
	}

	// branch node
	keyLeaf := i.leavesStack[len(i.leavesStack)-2]
	leftNode := i.nodeStack[len(i.nodeStack)-2]
	rightNode := i.nodeStack[len(i.nodeStack)-1]

	if n.Height < 0 {
		return fmt.Errorf("node height under/overflows uint8: %d", n.Height)
	}

	node := &MemNode{
		height:  uint8(n.Height),
		size:    leftNode.size + rightNode.size,
		version: version,
		key:     n.Key,
		left:    leftNode,
		right:   rightNode,
	}
	nodeHash := node.Hash()

	// remove unnecessary reference to avoid memory leak
	node.left = nil
	node.right = nil

	pt := len(i.nodeStack) - 2
	if pt < 0 || pt > math.MaxUint8 {
		return fmt.Errorf("preTrees out of range: %d", pt)
	}
	preTrees := uint8(pt)
	if node.size < 0 || node.size > math.MaxUint32 {
		return fmt.Errorf("node size under/overflows uint32: %d", node.size)
	}
	if err := i.w.writeBranch(node.version, uint32(node.size), node.height, preTrees, keyLeaf, nodeHash); err != nil {
		return err
	}

	i.leavesStack = i.leavesStack[:len(i.leavesStack)-2]
	i.leavesStack = append(i.leavesStack, i.w.leafCounter)

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
	opts := Options{Config: Config{SnapshotPrefetchThreshold: 0}}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		snapshot, err := OpenSnapshot(filepath.Join(dir, name), opts)
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
