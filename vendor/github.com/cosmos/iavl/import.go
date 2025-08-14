package iavl

import (
	"bytes"
	"fmt"
	"sync"

	"github.com/pkg/errors"
	db "github.com/tendermint/tm-db"
)

// desiredBatchSize is the desired batch write size of the import batch before flushing it to the database.
// The actual batch write size could exceed this value when the previous batch is still flushing.
const defaultDesiredBatchSize = 20000

// If there's an ongoing pending batch write, we will keep batching more writes
// until the ongoing batch write completes or we reach maxBatchSize
const defaultMaxBatchSize = 400000

// ErrNoImport is returned when calling methods on a closed importer
var ErrNoImport = errors.New("no import in progress")

// Importer imports data into an empty MutableTree. It is created by MutableTree.Import(). Users
// must call Close() when done.
//
// ExportNodes must be imported in the order returned by Exporter, i.e. depth-first post-order (LRN).
//
// Importer is not concurrency-safe, it is the caller's responsibility to ensure the tree is not
// modified while performing an import.
type Importer struct {
	tree             *MutableTree
	version          int64
	batch            db.Batch
	batchSize        uint32
	stack            []*Node
	desiredBatchSize uint32
	maxBatchSize     uint32
	batchMtx         sync.RWMutex
	chNodeData       chan NodeData
	chNodeDataWg     sync.WaitGroup
	chBatch          chan db.Batch
	chBatchWg        sync.WaitGroup
	chError          chan error
	allChannelClosed bool
}

type NodeData struct {
	node *Node
	data []byte
}

// newImporter creates a new Importer for an empty MutableTree.
// Underneath it spawns three goroutines to process the data import flow.
//
// version should correspond to the version that was initially exported. It must be greater than
// or equal to the highest ExportNode version number given.
func newImporter(tree *MutableTree, version int64) (*Importer, error) {
	if version < 0 {
		return nil, errors.New("imported version cannot be negative")
	}
	if tree.ndb.latestVersion > 0 {
		return nil, errors.Errorf("found database at version %d, must be 0", tree.ndb.latestVersion)
	}
	if !tree.IsEmpty() {
		return nil, errors.New("tree must be empty")
	}

	importer := &Importer{
		tree:             tree,
		version:          version,
		batch:            tree.ndb.db.NewBatch(),
		stack:            make([]*Node, 0, 8),
		batchMtx:         sync.RWMutex{},
		desiredBatchSize: defaultDesiredBatchSize,
		maxBatchSize:     defaultMaxBatchSize,
		chNodeData:       make(chan NodeData, 2*defaultDesiredBatchSize),
		chNodeDataWg:     sync.WaitGroup{},
		chBatch:          make(chan db.Batch, 1),
		chBatchWg:        sync.WaitGroup{},
		chError:          make(chan error, 1),
		allChannelClosed: false,
	}

	importer.chNodeDataWg.Add(1)
	go setBatchData(importer)

	importer.chBatchWg.Add(1)
	go batchWrite(importer)

	return importer, nil

}

// WithDesiredBatchSize set the desired batch size for write
func (i *Importer) WithDesiredBatchSize(batchSize uint32) *Importer {
	i.desiredBatchSize = batchSize
	return i
}

// WithMaxBatchSize set the maximum allowed batch size for write, should be greater than desired batch size.
// Consider increase max batch size to reduce overall import time.
func (i *Importer) WithMaxBatchSize(batchSize uint32) *Importer {
	i.maxBatchSize = batchSize
	return i
}

// setBatchData get the next serialized node data from channel, and write the data to the current batch
func setBatchData(i *Importer) {
	for i.batch != nil {
		if nodeData, open := <-i.chNodeData; open {
			i.batchMtx.RLock()
			if i.batch != nil {
				err := i.batch.Set(i.tree.ndb.nodeKey(nodeData.node.GetHash()), nodeData.data)
				if err != nil {
					i.batchMtx.RUnlock()
					i.chError <- err
					break
				}
			}
			i.batchMtx.RUnlock()
			i.batchSize++
			// Only commit a new batch if size meet desiredBatchSize and there's no pending batch write
			if (i.batchSize >= i.desiredBatchSize && len(i.chBatch) < 1) || i.batchSize >= i.maxBatchSize {
				i.chBatch <- i.batch
				i.batch = i.tree.ndb.db.NewBatch()
				i.batchSize = 0
			}
		} else {
			break
		}
	}
	i.chNodeDataWg.Done()
}

// batchWrite get a new batch from the channel and execute the batch write to the underline DB.
func batchWrite(i *Importer) {
	for i.batch != nil {
		if nextBatch, open := <-i.chBatch; open {
			err := nextBatch.Write()
			if err != nil {
				i.chError <- err
				break
			}
			i.batchMtx.Lock()
			nextBatch.Close()
			i.batchMtx.Unlock()
		} else {
			break
		}
	}
	i.chBatchWg.Done()
}

// Close frees all resources. It is safe to call multiple times. Uncommitted nodes may already have
// been flushed to the database, but will not be visible. Errors are ignored in the close functions.
func (i *Importer) Close() {
	_ = i.waitAndCloseChannels()
	if i.batch != nil {
		_ = i.batch.Close()
	}
	i.batch = nil
	i.tree = nil
}

// Add adds an ExportNode to the import. ExportNodes must be added in the order returned by
// Exporter, i.e. depth-first post-order (LRN). Nodes are periodically flushed to the database,
// but the imported version is not visible until Commit() is called.
func (i *Importer) Add(exportNode *ExportNode) error {
	if i.tree == nil {
		return ErrNoImport
	}
	if exportNode == nil {
		return errors.New("node cannot be nil")
	}
	if exportNode.Version > i.version {
		return errors.Errorf("node version %v can't be greater than import version %v",
			exportNode.Version, i.version)
	}

	node := &Node{
		key:     exportNode.Key,
		value:   exportNode.Value,
		version: exportNode.Version,
		height:  exportNode.Height,
	}

	// We build the tree from the bottom-left up. The stack is used to store unresolved left
	// children while constructing right children. When all children are built, the parent can
	// be constructed and the resolved children can be discarded from the stack. Using a stack
	// ensures that we can handle additional unresolved left children while building a right branch.
	//
	// We don't modify the stack until we've verified the built node, to avoid leaving the
	// importer in an inconsistent state when we return an error.
	stackSize := len(i.stack)
	switch {
	case stackSize >= 2 && i.stack[stackSize-1].GetHeight() < node.GetHeight() && i.stack[stackSize-2].GetHeight() < node.GetHeight():
		node.SetLeftNode(i.stack[stackSize-2])
		node.SetLeftHash(node.GetLeftNode().GetHash())
		node.SetRightNode(i.stack[stackSize-1])
		node.SetRightHash(node.GetRightNode().GetHash())
	case stackSize >= 1 && i.stack[stackSize-1].GetHeight() < node.GetHeight():
		node.SetLeftNode(i.stack[stackSize-1])
		node.SetLeftHash(node.GetLeftNode().GetHash())
	}

	if node.GetHeight() == 0 {
		node.SetSize(1)
	}
	if node.GetLeftNode() != nil {
		node.SetSize(node.GetSize() + node.GetLeftNode().GetSize())
	}
	if node.GetRightNode() != nil {
		node.SetSize(node.GetSize() + node.GetRightNode().GetSize())
	}

	_, err := node._hash()
	if err != nil {
		return err
	}

	err = node.validate()
	if err != nil {
		return err
	}

	buf := bufPool.Get().(*bytes.Buffer)
	buf.Reset()
	if err := node.writeBytes(buf); err != nil {
		panic(err)
	}

	bytesCopy := make([]byte, buf.Len())
	copy(bytesCopy, buf.Bytes())
	bufPool.Put(buf)

	// Check errors
	select {
	case err := <-i.chError:
		return err
	default:
	}

	// Handle the remaining steps in a separate goroutine
	i.chNodeData <- NodeData{
		node: node,
		data: bytesCopy,
	}

	// Update the stack now that we know there were no errors
	switch {
	case node.GetLeftHash() != nil && node.GetRightHash() != nil:
		i.stack = i.stack[:stackSize-2]
	case node.GetLeftHash() != nil || node.GetRightHash() != nil:
		i.stack = i.stack[:stackSize-1]
	}
	// Only hash\height\size of the node will be used after it be pushed into the stack.
	i.stack = append(i.stack, &Node{hash: node.GetHash(), height: node.GetHeight(), size: node.GetSize()})

	return nil
}

// Commit finalizes the import by flushing any outstanding nodes to the database, making the
// version visible, and updating the tree metadata. It can only be called once, and calls Close()
// internally.
func (i *Importer) Commit() error {
	if i.tree == nil {
		return ErrNoImport
	}

	err := i.waitAndCloseChannels()
	if err != nil {
		return err
	}

	switch len(i.stack) {
	case 0:
		if err := i.batch.Set(i.tree.ndb.rootKey(i.version), []byte{}); err != nil {
			return err
		}
	case 1:
		if err := i.batch.Set(i.tree.ndb.rootKey(i.version), i.stack[0].GetHash()); err != nil {
			return err
		}
	default:
		return fmt.Errorf("invalid node structure, found stack size %v when committing",
			len(i.stack))
	}

	err = i.batch.WriteSync()
	if err != nil {
		return err
	}
	i.tree.ndb.resetLatestVersion(i.version)

	_, err = i.tree.LoadVersion(i.version)
	if err != nil {
		return err
	}

	i.Close()
	return nil
}

// waitAndCloseChannels will try to close all the channels for importer and wait for remaining work to be done.
// This function should only be called in the Commit or Close action. If any error happens when draining the remaining data in the channel,
// The error will be popped out and returned.
func (i *Importer) waitAndCloseChannels() error {
	// Make sure all pending works are drained and close the channels in order
	if !i.allChannelClosed {
		i.allChannelClosed = true
		close(i.chNodeData)
		i.chNodeDataWg.Wait()
		close(i.chBatch)
		i.chBatchWg.Wait()
		// Check errors
		select {
		case err := <-i.chError:
			return err
		default:
		}
	}
	return nil
}
