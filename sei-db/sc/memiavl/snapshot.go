package memiavl

import (
	"bufio"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/sei-protocol/sei-db/common/errors"
	"github.com/sei-protocol/sei-db/sc/types"
)

const (
	// SnapshotFileMagic is little endian encoded b"IAVL"
	SnapshotFileMagic = 1280721225

	// the initial snapshot format
	SnapshotFormat = 0

	// magic: uint32, format: uint32, version: uint32
	SizeMetadata = 12

	FileNameNodes    = "nodes"
	FileNameLeaves   = "leaves"
	FileNameKVs      = "kvs"
	FileNameMetadata = "metadata"
)

// Snapshot manage the lifecycle of mmap-ed files for the snapshot,
// it must out live the objects that derived from it.
type Snapshot struct {
	nodesMap  *MmapFile
	leavesMap *MmapFile
	kvsMap    *MmapFile

	nodes  []byte
	leaves []byte
	kvs    []byte

	// parsed from metadata file
	version uint32

	// wrapping the raw nodes buffer
	nodesLayout  Nodes
	leavesLayout Leaves

	// nil means empty snapshot
	root *PersistedNode
}

func NewEmptySnapshot(version uint32) *Snapshot {
	return &Snapshot{
		version: version,
	}
}

// OpenSnapshot parse the version number and the root node index from metadata file,
// and mmap the other files.
func OpenSnapshot(snapshotDir string) (*Snapshot, error) {
	// read metadata file
	bz, err := os.ReadFile(filepath.Join(snapshotDir, FileNameMetadata))
	if err != nil {
		return nil, err
	}
	if len(bz) != SizeMetadata {
		return nil, fmt.Errorf("wrong metadata file size, expcted: %d, found: %d", SizeMetadata, len(bz))
	}

	magic := binary.LittleEndian.Uint32(bz)
	if magic != SnapshotFileMagic {
		return nil, fmt.Errorf("invalid metadata file magic: %d", magic)
	}
	format := binary.LittleEndian.Uint32(bz[4:])
	if format != SnapshotFormat {
		return nil, fmt.Errorf("unknown snapshot format: %d", format)
	}
	version := binary.LittleEndian.Uint32(bz[8:])

	var nodesMap, leavesMap, kvsMap *MmapFile
	cleanupHandles := func(err error) error {
		errs := []error{err}
		if nodesMap != nil {
			errs = append(errs, nodesMap.Close())
		}
		if leavesMap != nil {
			errs = append(errs, leavesMap.Close())
		}
		if kvsMap != nil {
			errs = append(errs, kvsMap.Close())
		}
		return errors.Join(errs...)
	}

	if nodesMap, err = NewMmap(filepath.Join(snapshotDir, FileNameNodes)); err != nil {
		return nil, cleanupHandles(err)
	}
	if leavesMap, err = NewMmap(filepath.Join(snapshotDir, FileNameLeaves)); err != nil {
		return nil, cleanupHandles(err)
	}
	if kvsMap, err = NewMmap(filepath.Join(snapshotDir, FileNameKVs)); err != nil {
		return nil, cleanupHandles(err)
	}

	nodes := nodesMap.Data()
	leaves := leavesMap.Data()
	kvs := kvsMap.Data()

	// validate nodes length
	if len(nodes)%SizeNode != 0 {
		return nil, cleanupHandles(
			fmt.Errorf("corrupted snapshot, nodes file size %d is not a multiple of %d", len(nodes), SizeNode),
		)
	}
	if len(leaves)%SizeLeaf != 0 {
		return nil, cleanupHandles(
			fmt.Errorf("corrupted snapshot, leaves file size %d is not a multiple of %d", len(leaves), SizeLeaf),
		)
	}

	nodesLen := len(nodes) / SizeNode
	leavesLen := len(leaves) / SizeLeaf
	if (leavesLen > 0 && nodesLen+1 != leavesLen) || (leavesLen == 0 && nodesLen != 0) {
		return nil, cleanupHandles(
			fmt.Errorf("corrupted snapshot, branch nodes size %d don't match leaves size %d", nodesLen, leavesLen),
		)
	}

	nodesData, err := NewNodes(nodes)
	if err != nil {
		return nil, err
	}

	leavesData, err := NewLeaves(leaves)
	if err != nil {
		return nil, err
	}

	snapshot := &Snapshot{
		nodesMap:  nodesMap,
		leavesMap: leavesMap,
		kvsMap:    kvsMap,

		// cache the pointers
		nodes:  nodes,
		leaves: leaves,
		kvs:    kvs,

		version: version,

		nodesLayout:  nodesData,
		leavesLayout: leavesData,
	}

	if nodesLen > 0 {
		snapshot.root = &PersistedNode{
			snapshot: snapshot,
			isLeaf:   false,
			index:    uint32(nodesLen - 1),
		}
	} else if leavesLen > 0 {
		snapshot.root = &PersistedNode{
			snapshot: snapshot,
			isLeaf:   true,
			index:    0,
		}
	}

	return snapshot, nil
}

// Close closes the file and mmap handles, clears the buffers.
func (snapshot *Snapshot) Close() error {
	var errs []error

	if snapshot.nodesMap != nil {
		errs = append(errs, snapshot.nodesMap.Close())
	}
	if snapshot.leavesMap != nil {
		errs = append(errs, snapshot.leavesMap.Close())
	}
	if snapshot.kvsMap != nil {
		errs = append(errs, snapshot.kvsMap.Close())
	}

	// reset to an empty tree
	*snapshot = *NewEmptySnapshot(snapshot.version)
	return errors.Join(errs...)
}

// IsEmpty returns if the snapshot is an empty tree.
func (snapshot *Snapshot) IsEmpty() bool {
	return snapshot.root == nil
}

// Node returns the branch node by index
func (snapshot *Snapshot) Node(index uint32) PersistedNode {
	return PersistedNode{
		snapshot: snapshot,
		index:    index,
		isLeaf:   false,
	}
}

// Leaf returns the leaf node by index
func (snapshot *Snapshot) Leaf(index uint32) PersistedNode {
	return PersistedNode{
		snapshot: snapshot,
		index:    index,
		isLeaf:   true,
	}
}

// Version returns the version of the snapshot
func (snapshot *Snapshot) Version() uint32 {
	return snapshot.version
}

// RootNode returns the root node
func (snapshot *Snapshot) RootNode() PersistedNode {
	if snapshot.IsEmpty() {
		panic("RootNode not supported on an empty snapshot")
	}
	return *snapshot.root
}

func (snapshot *Snapshot) RootHash() []byte {
	if snapshot.IsEmpty() {
		return emptyHash
	}
	return snapshot.RootNode().Hash()
}

// nodesLen returns the number of nodes in the snapshot
func (snapshot *Snapshot) nodesLen() int {
	return len(snapshot.nodes) / SizeNode
}

// leavesLen returns the number of nodes in the snapshot
func (snapshot *Snapshot) leavesLen() int {
	return len(snapshot.leaves) / SizeLeaf
}

// ScanNodes iterate over the nodes in the snapshot order (depth-first post-order, leaf nodes before branch nodes)
func (snapshot *Snapshot) ScanNodes(callback func(node PersistedNode) error) error {
	for i := 0; i < snapshot.leavesLen(); i++ {
		if err := callback(snapshot.Leaf(uint32(i))); err != nil {
			return err
		}
	}
	for i := 0; i < snapshot.nodesLen(); i++ {
		if err := callback(snapshot.Node(uint32(i))); err != nil {
			return err
		}
	}
	return nil
}

// Key returns a zero-copy slice of key by offset
func (snapshot *Snapshot) Key(offset uint64) []byte {
	keyLen := binary.LittleEndian.Uint32(snapshot.kvs[offset:])
	offset += 4
	return snapshot.kvs[offset : offset+uint64(keyLen)]
}

// KeyValue returns a zero-copy slice of key/value pair by offset
func (snapshot *Snapshot) KeyValue(offset uint64) ([]byte, []byte) {
	len := uint64(binary.LittleEndian.Uint32(snapshot.kvs[offset:]))
	offset += 4
	key := snapshot.kvs[offset : offset+len]
	offset += len
	len = uint64(binary.LittleEndian.Uint32(snapshot.kvs[offset:]))
	offset += 4
	value := snapshot.kvs[offset : offset+len]
	return key, value
}

func (snapshot *Snapshot) LeafKey(index uint32) []byte {
	leaf := snapshot.leavesLayout.Leaf(index)
	offset := leaf.KeyOffset() + 4
	return snapshot.kvs[offset : offset+uint64(leaf.KeyLength())]
}

func (snapshot *Snapshot) LeafKeyValue(index uint32) ([]byte, []byte) {
	leaf := snapshot.leavesLayout.Leaf(index)
	offset := leaf.KeyOffset() + 4
	length := uint64(leaf.KeyLength())
	key := snapshot.kvs[offset : offset+length]
	offset += length
	length = uint64(binary.LittleEndian.Uint32(snapshot.kvs[offset:]))
	offset += 4
	return key, snapshot.kvs[offset : offset+length]
}

// Export exports the nodes from snapshot file sequentially, more efficient than a post-order traversal.
func (snapshot *Snapshot) Export() *Exporter {
	return newExporter(snapshot.export)
}

func (snapshot *Snapshot) export(callback func(*types.SnapshotNode) bool) {
	if snapshot.leavesLen() == 0 {
		return
	}

	if snapshot.leavesLen() == 1 {
		leaf := snapshot.Leaf(0)
		callback(&types.SnapshotNode{
			Height:  0,
			Version: int64(leaf.Version()),
			Key:     leaf.Key(),
			Value:   leaf.Value(),
		})
		return
	}

	var pendingTrees int
	var i, j uint32
	for ; i < uint32(snapshot.nodesLen()); i++ {
		// pending branch node
		node := snapshot.nodesLayout.Node(i)
		for pendingTrees < int(node.PreTrees())+2 {
			// add more leaf nodes
			leaf := snapshot.leavesLayout.Leaf(j)
			key, value := snapshot.KeyValue(leaf.KeyOffset())
			enode := &types.SnapshotNode{
				Height:  0,
				Version: int64(leaf.Version()),
				Key:     key,
				Value:   value,
			}
			j++
			pendingTrees++

			if callback(enode) {
				return
			}
		}
		enode := &types.SnapshotNode{
			Height:  int8(node.Height()),
			Version: int64(node.Version()),
			Key:     snapshot.LeafKey(node.KeyLeaf()),
		}
		pendingTrees--

		if callback(enode) {
			return
		}
	}
}

// WriteSnapshot save the IAVL tree to a new snapshot directory.
func (t *Tree) WriteSnapshot(ctx context.Context, snapshotDir string) error {
	return writeSnapshot(ctx, snapshotDir, t.version, func(w *snapshotWriter) (uint32, error) {
		if t.root == nil {
			return 0, nil
		}

		if err := w.writeRecursive(t.root); err != nil {
			return 0, err
		}
		return w.leafCounter, nil
	})
}

func writeSnapshot(
	ctx context.Context,
	dir string, version uint32,
	doWrite func(*snapshotWriter) (uint32, error),
) (returnErr error) {
	if err := os.MkdirAll(dir, os.ModePerm); err != nil {
		return err
	}

	nodesFile := filepath.Join(dir, FileNameNodes)
	leavesFile := filepath.Join(dir, FileNameLeaves)
	kvsFile := filepath.Join(dir, FileNameKVs)

	fpNodes, err := createFile(nodesFile)
	if err != nil {
		return err
	}
	defer func() {
		if err := fpNodes.Close(); returnErr == nil {
			returnErr = err
		}
	}()

	fpLeaves, err := createFile(leavesFile)
	if err != nil {
		return err
	}
	defer func() {
		if err := fpLeaves.Close(); returnErr == nil {
			returnErr = err
		}
	}()

	fpKVs, err := createFile(kvsFile)
	if err != nil {
		return err
	}
	defer func() {
		if err := fpKVs.Close(); returnErr == nil {
			returnErr = err
		}
	}()

	nodesWriter := bufio.NewWriterSize(fpNodes, bufIOSize)
	leavesWriter := bufio.NewWriterSize(fpLeaves, bufIOSize)
	kvsWriter := bufio.NewWriterSize(fpKVs, bufIOSize)

	w := newSnapshotWriter(ctx, nodesWriter, leavesWriter, kvsWriter)
	leaves, err := doWrite(w)
	if err != nil {
		return err
	}

	if leaves > 0 {
		if err := nodesWriter.Flush(); err != nil {
			return err
		}
		if err := leavesWriter.Flush(); err != nil {
			return err
		}
		if err := kvsWriter.Flush(); err != nil {
			return err
		}

		if err := fpKVs.Sync(); err != nil {
			return err
		}
		if err := fpLeaves.Sync(); err != nil {
			return err
		}
		if err := fpNodes.Sync(); err != nil {
			return err
		}
	}

	// write metadata
	var metadataBuf [SizeMetadata]byte
	binary.LittleEndian.PutUint32(metadataBuf[:], SnapshotFileMagic)
	binary.LittleEndian.PutUint32(metadataBuf[4:], SnapshotFormat)
	binary.LittleEndian.PutUint32(metadataBuf[8:], version)

	metadataFile := filepath.Join(dir, FileNameMetadata)
	fpMetadata, err := createFile(metadataFile)
	if err != nil {
		return err
	}
	defer func() {
		if err := fpMetadata.Close(); returnErr == nil {
			returnErr = err
		}
	}()

	if _, err := fpMetadata.Write(metadataBuf[:]); err != nil {
		return err
	}

	return fpMetadata.Sync()
}

type snapshotWriter struct {
	// context for cancel the writing process
	ctx context.Context

	nodesWriter, leavesWriter, kvWriter io.Writer

	// count how many nodes have been written
	branchCounter, leafCounter uint32

	// record the current writing offset in kvs file
	kvsOffset uint64
}

func newSnapshotWriter(ctx context.Context, nodesWriter, leavesWriter, kvsWriter io.Writer) *snapshotWriter {
	return &snapshotWriter{
		ctx:          ctx,
		nodesWriter:  nodesWriter,
		leavesWriter: leavesWriter,
		kvWriter:     kvsWriter,
	}
}

// writeKeyValue append key-value pair to kvs file and record the offset
func (w *snapshotWriter) writeKeyValue(key, value []byte) error {
	var numBuf [4]byte

	binary.LittleEndian.PutUint32(numBuf[:], uint32(len(key)))
	if _, err := w.kvWriter.Write(numBuf[:]); err != nil {
		return err
	}
	if _, err := w.kvWriter.Write(key); err != nil {
		return err
	}

	binary.LittleEndian.PutUint32(numBuf[:], uint32(len(value)))
	if _, err := w.kvWriter.Write(numBuf[:]); err != nil {
		return err
	}
	if _, err := w.kvWriter.Write(value); err != nil {
		return err
	}

	w.kvsOffset += 4 + 4 + uint64(len(key)) + uint64(len(value))
	return nil
}

func (w *snapshotWriter) writeLeaf(version uint32, key, value, hash []byte) error {
	var buf [SizeLeafWithoutHash]byte
	binary.LittleEndian.PutUint32(buf[OffsetLeafVersion:], version)
	binary.LittleEndian.PutUint32(buf[OffsetLeafKeyLen:], uint32(len(key)))
	binary.LittleEndian.PutUint64(buf[OffsetLeafKeyOffset:], w.kvsOffset)

	if err := w.writeKeyValue(key, value); err != nil {
		return err
	}

	if _, err := w.leavesWriter.Write(buf[:]); err != nil {
		return err
	}
	if _, err := w.leavesWriter.Write(hash); err != nil {
		return err
	}

	w.leafCounter++
	return nil
}

func (w *snapshotWriter) writeBranch(version, size uint32, height, preTrees uint8, keyLeaf uint32, hash []byte) error {
	var buf [SizeNodeWithoutHash]byte
	buf[OffsetHeight] = height
	buf[OffsetPreTrees] = preTrees
	binary.LittleEndian.PutUint32(buf[OffsetVersion:], version)
	binary.LittleEndian.PutUint32(buf[OffsetSize:], size)
	binary.LittleEndian.PutUint32(buf[OffsetKeyLeaf:], keyLeaf)

	if _, err := w.nodesWriter.Write(buf[:]); err != nil {
		return err
	}
	if _, err := w.nodesWriter.Write(hash); err != nil {
		return err
	}

	w.branchCounter++
	return nil
}

// writeRecursive write the node recursively in depth-first post-order,
// returns `(nodeIndex, err)`.
func (w *snapshotWriter) writeRecursive(node Node) error {
	select {
	case <-w.ctx.Done():
		return w.ctx.Err()
	default:
	}
	if node.IsLeaf() {
		return w.writeLeaf(node.Version(), node.Key(), node.Value(), node.Hash())
	}

	// record the number of pending subtrees before the current one,
	// it's always positive and won't exceed the tree height, so we can use an uint8 to store it.
	preTrees := uint8(w.leafCounter - w.branchCounter)

	if err := w.writeRecursive(node.Left()); err != nil {
		return err
	}
	keyLeaf := w.leafCounter
	if err := w.writeRecursive(node.Right()); err != nil {
		return err
	}

	return w.writeBranch(node.Version(), uint32(node.Size()), node.Height(), preTrees, keyLeaf, node.Hash())
}

func createFile(name string) (*os.File, error) {
	return os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
}
