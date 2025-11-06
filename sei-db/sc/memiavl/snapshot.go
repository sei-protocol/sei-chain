package memiavl

import (
	"bufio"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/sei-protocol/sei-db/common/logger"
	"github.com/sei-protocol/sei-db/sc/types"
	"golang.org/x/sys/unix"

	"github.com/sei-protocol/sei-db/common/errors"
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

// monitoringWriter wraps an os.File to track write progress
type monitoringWriter struct {
	f       *os.File
	written int64
}

func (w *monitoringWriter) Write(p []byte) (n int, err error) {
	n, err = w.f.Write(p)
	if err != nil {
		return n, err
	}

	w.written += int64(n)
	return n, err
}

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
	root   *PersistedNode
	logger logger.Logger
}

func NewEmptySnapshot(version uint32) *Snapshot {
	return &Snapshot{
		version: version,
	}
}

// OpenSnapshot parse the version number and the root node index from metadata file,
// and mmap the other files.
func OpenSnapshot(snapshotDir string, opts Options) (*Snapshot, error) {
	// read metadata file
	bz, err := os.ReadFile(filepath.Join(filepath.Clean(snapshotDir), FileNameMetadata))
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
		return nil, cleanupHandles(err)
	}

	leavesData, err := NewLeaves(leaves)
	if err != nil {
		return nil, cleanupHandles(err)
	}

	snapshot := &Snapshot{
		logger: opts.Logger,

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
			index:    uint32(nodesLen - 1), //nolint:gosec
		}
	} else if leavesLen > 0 {
		snapshot.root = &PersistedNode{
			snapshot: snapshot,
			isLeaf:   true,
			index:    0,
		}
	}

	// Preload nodes + leaves into page cache using file I/O with SEQUENTIAL+WILLNEED
	// This eliminates random I/O during replay, relying on natural page cache for split keys
	if opts.PrefetchThreshold > 0 {
		snapshot.prefetchSnapshot(snapshotDir, opts.PrefetchThreshold)
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
		if err := callback(snapshot.Leaf(uint32(i))); err != nil { //nolint:gosec
			return err
		}
	}
	for i := 0; i < snapshot.nodesLen(); i++ {
		if err := callback(snapshot.Node(uint32(i))); err != nil { //nolint:gosec
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

// Export returns an Exporter for state sync
func (snapshot *Snapshot) Export() *Exporter {
	return newExporter(snapshot.export)
}

// export is the internal implementation that iterates through the snapshot in post-order
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
		hui8 := node.Height()
		if hui8 > math.MaxInt8 {
			panic("node height exceeds int8")
		}
		height := int8(hui8)
		enode := &types.SnapshotNode{
			Height:  height,
			Version: int64(node.Version()),
			Key:     snapshot.LeafKey(node.KeyLeaf()),
		}
		pendingTrees--

		if callback(enode) {
			return
		}
	}
}

func (t *Tree) WriteSnapshot(ctx context.Context, snapshotDir string) error {
	// Estimate tree size: root.Size() returns leaf count, total = leaves + branches ≈ 2x
	treeSize := int64(0)
	if t.root != nil {
		treeSize = t.root.Size() * 2 // Total nodes (leaves + branches)
	}

	// Use 256MB buffer for all trees (large buffer for better performance)
	bufSize := bufIOSize

	err := writeSnapshotWithBuffer(ctx, snapshotDir, t.version, bufSize, treeSize, func(w *snapshotWriter) (uint32, error) {
		if t.root == nil {
			return 0, nil
		}

		if err := w.writeRecursive(t.root); err != nil {
			return 0, err
		}
		return w.leafCounter, nil
	})

	if err != nil {
		return err
	}

	return nil
}

// writeSnapshotWithBuffer writes snapshot with specified buffer size
func writeSnapshotWithBuffer(
	ctx context.Context,
	dir string, version uint32,
	bufSize int,
	totalNodes int64,
	doWrite func(*snapshotWriter) (uint32, error),
) (returnErr error) {
	if err := os.MkdirAll(dir, os.ModePerm); err != nil { //nolint:gosec
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

	// Wrap files with monitoring writers for progress tracking
	nodesMonitor := &monitoringWriter{f: fpNodes}
	leavesMonitor := &monitoringWriter{f: fpLeaves}
	kvsMonitor := &monitoringWriter{f: fpKVs}

	// Create buffered writers with large buffers (2GB each for EVM tree)
	nodesWriter := bufio.NewWriterSize(nodesMonitor, bufSize)
	leavesWriter := bufio.NewWriterSize(leavesMonitor, bufSize)
	kvsWriter := bufio.NewWriterSize(kvsMonitor, bufSize)

	w := newSnapshotWriter(ctx, nodesWriter, leavesWriter, kvsWriter)
	w.treeName = filepath.Base(dir) // Set tree name for progress reporting
	w.totalNodes = totalNodes       // Set total nodes for progress percentage
	w.traversalStartTime = time.Now()

	leaves, err := doWrite(w)
	if err != nil {
		return err
	}
	// Wait for all pending writes to complete
	if err := w.waitForWrites(); err != nil {
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
		if err := fpNodes.Sync(); err != nil {
			return err
		}
		if err := fpLeaves.Sync(); err != nil {
			return err
		}
		if err := fpKVs.Sync(); err != nil {
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

// writeSnapshot is a compatibility wrapper that uses default buffer size
func writeSnapshot(
	ctx context.Context,
	dir string, version uint32,
	doWrite func(*snapshotWriter) (uint32, error),
) error {
	return writeSnapshotWithBuffer(ctx, dir, version, bufIOSize, 0, doWrite)
}

// kvWriteOp represents a key-value write operation
type kvWriteOp struct {
	key   []byte
	value []byte
}

// leafWriteOp represents a leaf write operation
type leafWriteOp struct {
	version   uint32
	keyLen    uint32
	keyOffset uint64
	hash      []byte
}

// branchWriteOp represents a branch write operation
type branchWriteOp struct {
	version  uint32
	size     uint32
	height   uint8
	preTrees uint8
	keyLeaf  uint32
	hash     []byte
}

type snapshotWriter struct {
	// context for cancel the writing process
	ctx context.Context

	nodesWriter, leavesWriter, kvWriter io.Writer

	// count how many nodes have been written
	branchCounter, leafCounter uint32

	// record the current writing offset in kvs file
	kvsOffset uint64

	// for progress reporting
	treeName               string
	totalNodes             int64 // Total nodes to write (for progress percentage)
	traversalStartTime     time.Time
	lastProgressReport     time.Time
	progressReportInterval time.Duration

	// Pipeline for async writes - separate channels for each file
	kvChan     chan kvWriteOp
	leafChan   chan leafWriteOp
	branchChan chan branchWriteOp

	writeErrors chan error
	wg          sync.WaitGroup // Wait for all writer goroutines

	// Pipeline metrics for each channel
	maxKvFill         int
	maxLeafFill       int
	maxBranchFill     int
	kvFillSum         int64
	leafFillSum       int64
	branchFillSum     int64
	kvFillCount       int64
	leafFillCount     int64
	branchFillCount   int64
	lastMetricsReport time.Time
}

// SetPipelineBufferSize allows configuring the pipeline buffer size
// Larger values provide more parallelism but use more memory
// Default is 10000. Recommended range: 1000-50000
func SetPipelineBufferSize(size int) {
	if size < 100 {
		size = 100 // Minimum to avoid deadlocks
	}
	if size > 100000 {
		size = 100000 // Maximum to avoid excessive memory usage
	}
	nodeChanSize = size
}

func newSnapshotWriter(ctx context.Context, nodesWriter, leavesWriter, kvsWriter io.Writer) *snapshotWriter {
	now := time.Now()

	// Create separate buffered channels for each file type
	// This allows parallel writes to all 3 files
	// Buffer size is configurable via SetPipelineBufferSize()
	kvChan := make(chan kvWriteOp, nodeChanSize)
	leafChan := make(chan leafWriteOp, nodeChanSize)
	branchChan := make(chan branchWriteOp, nodeChanSize)
	writeErrors := make(chan error, 3) // Buffer for errors from all 3 goroutines

	w := &snapshotWriter{
		ctx:                    ctx,
		nodesWriter:            nodesWriter,
		leavesWriter:           leavesWriter,
		kvWriter:               kvsWriter,
		lastProgressReport:     now,
		progressReportInterval: 30 * time.Second,
		kvChan:                 kvChan,
		leafChan:               leafChan,
		branchChan:             branchChan,
		writeErrors:            writeErrors,
		lastMetricsReport:      now,
	}

	// Start 3 parallel writer goroutines - one for each file
	w.wg.Add(3)
	go w.kvWriterLoop()
	go w.leafWriterLoop()
	go w.branchWriterLoop()

	return w
}

// kvWriterLoop processes KV write operations in parallel
func (w *snapshotWriter) kvWriterLoop() {
	defer w.wg.Done()

	for op := range w.kvChan {
		if err := w.writeKeyValueDirect(op.key, op.value); err != nil {
			select {
			case w.writeErrors <- fmt.Errorf("kv write error: %w", err):
			default:
			}
			return
		}
	}
}

// leafWriterLoop processes leaf write operations in parallel
func (w *snapshotWriter) leafWriterLoop() {
	defer w.wg.Done()

	for op := range w.leafChan {
		if err := w.writeLeafDirect(op.version, op.keyLen, op.keyOffset, op.hash); err != nil {
			select {
			case w.writeErrors <- fmt.Errorf("leaf write error: %w", err):
			default:
			}
			return
		}
	}
}

// branchWriterLoop processes branch write operations in parallel
func (w *snapshotWriter) branchWriterLoop() {
	defer w.wg.Done()

	for op := range w.branchChan {
		if err := w.writeBranchDirect(op.version, op.size, op.height, op.preTrees, op.keyLeaf, op.hash); err != nil {
			select {
			case w.writeErrors <- fmt.Errorf("branch write error: %w", err):
			default:
			}
			return
		}
	}
}

// waitForWrites waits for all pending writes to complete and returns any error
func (w *snapshotWriter) waitForWrites() error {
	// Close all channels to signal completion
	close(w.kvChan)
	close(w.leafChan)
	close(w.branchChan)

	// Wait for all writer goroutines to finish
	w.wg.Wait()

	// Check for any errors
	select {
	case err := <-w.writeErrors:
		return err
	default:
		return nil
	}
}

// writeKeyValueDirect writes key-value pair directly (called by writer goroutine)
func (w *snapshotWriter) writeKeyValueDirect(key, value []byte) error {
	var numBuf [4]byte

	keyLen := uint32(len(key))     //nolint:gosec
	valueLen := uint32(len(value)) //nolint:gosec

	binary.LittleEndian.PutUint32(numBuf[:], keyLen)
	if _, err := w.kvWriter.Write(numBuf[:]); err != nil {
		return err
	}
	if _, err := w.kvWriter.Write(key); err != nil {
		return err
	}

	binary.LittleEndian.PutUint32(numBuf[:], valueLen)
	if _, err := w.kvWriter.Write(numBuf[:]); err != nil {
		return err
	}
	if _, err := w.kvWriter.Write(value); err != nil {
		return err
	}

	return nil
}

// writeLeaf sends leaf and KV write operations to the pipeline
func (w *snapshotWriter) writeLeaf(version uint32, key, value, hash []byte) error {
	// Track channel fill metrics for all channels
	kvFill := len(w.kvChan)
	leafFill := len(w.leafChan)

	if kvFill > w.maxKvFill {
		w.maxKvFill = kvFill
	}
	if leafFill > w.maxLeafFill {
		w.maxLeafFill = leafFill
	}

	atomic.AddInt64(&w.kvFillSum, int64(kvFill))
	atomic.AddInt64(&w.kvFillCount, 1)
	atomic.AddInt64(&w.leafFillSum, int64(leafFill))
	atomic.AddInt64(&w.leafFillCount, 1)

	// Calculate key offset BEFORE sending to KV channel
	keyOffset := w.kvsOffset
	keyLen := uint32(len(key))
	valueLen := uint32(len(value))
	w.kvsOffset += 4 + 4 + uint64(keyLen) + uint64(valueLen)

	// Make copies since we're sending to another goroutine
	keyCopy := make([]byte, len(key))
	copy(keyCopy, key)
	valueCopy := make([]byte, len(value))
	copy(valueCopy, value)
	hashCopy := make([]byte, len(hash))
	copy(hashCopy, hash)

	// Send KV write operation
	kvOp := kvWriteOp{
		key:   keyCopy,
		value: valueCopy,
	}

	select {
	case w.kvChan <- kvOp:
	case <-w.ctx.Done():
		return w.ctx.Err()
	}

	// Send leaf write operation
	leafOp := leafWriteOp{
		version:   version,
		keyLen:    keyLen,
		keyOffset: keyOffset,
		hash:      hashCopy,
	}

	select {
	case w.leafChan <- leafOp:
		w.leafCounter++
		return nil
	case <-w.ctx.Done():
		return w.ctx.Err()
	}
}

// writeLeafDirect performs the actual leaf write (called by writer goroutine)
func (w *snapshotWriter) writeLeafDirect(version uint32, keyLen uint32, keyOffset uint64, hash []byte) error {
	var buf [SizeLeafWithoutHash]byte
	binary.LittleEndian.PutUint32(buf[OffsetLeafVersion:], version)
	binary.LittleEndian.PutUint32(buf[OffsetLeafKeyLen:], keyLen)
	binary.LittleEndian.PutUint64(buf[OffsetLeafKeyOffset:], keyOffset)

	if _, err := w.leavesWriter.Write(buf[:]); err != nil {
		return err
	}
	if _, err := w.leavesWriter.Write(hash); err != nil {
		return err
	}

	return nil
}

// writeBranch sends a branch write operation to the pipeline
func (w *snapshotWriter) writeBranch(version, size uint32, height, preTrees uint8, keyLeaf uint32, hash []byte) error {
	// Track channel fill metrics
	branchFill := len(w.branchChan)
	if branchFill > w.maxBranchFill {
		w.maxBranchFill = branchFill
	}
	atomic.AddInt64(&w.branchFillSum, int64(branchFill))
	atomic.AddInt64(&w.branchFillCount, 1)

	// Make copy of hash since we're sending to another goroutine
	hashCopy := make([]byte, len(hash))
	copy(hashCopy, hash)

	op := branchWriteOp{
		version:  version,
		size:     size,
		height:   height,
		preTrees: preTrees,
		keyLeaf:  keyLeaf,
		hash:     hashCopy,
	}

	select {
	case w.branchChan <- op:
		w.branchCounter++
		return nil
	case <-w.ctx.Done():
		return w.ctx.Err()
	}
}

// writeBranchDirect performs the actual branch write (called by writer goroutine)
func (w *snapshotWriter) writeBranchDirect(version, size uint32, height, preTrees uint8, keyLeaf uint32, hash []byte) error {
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

	return nil
}

// writeRecursive writes the node recursively in depth-first post-order
func (w *snapshotWriter) writeRecursive(node Node) error {
	select {
	case <-w.ctx.Done():
		return w.ctx.Err()
	default:
	}

	if node.IsLeaf() {
		return w.writeLeaf(node.Version(), node.Key(), node.Value(), node.Hash())
	}

	if w.leafCounter < w.branchCounter {
		return fmt.Errorf("leafCounter %d < branchCounter %d", w.leafCounter, w.branchCounter)
	}
	pt := w.leafCounter - w.branchCounter
	if pt > math.MaxUint8 {
		return fmt.Errorf("too many pending trees %d exceed %d", pt, math.MaxUint8)
	}

	// record the number of pending subtrees before the current one,
	// it's always positive and won't exceed the tree height, so we can use an uint8 to store it.
	preTrees := uint8(pt)

	if err := w.writeRecursive(node.Left()); err != nil {
		return err
	}
	keyLeaf := w.leafCounter
	if err := w.writeRecursive(node.Right()); err != nil {
		return err
	}

	size := node.Size()
	if size < 0 || size > math.MaxUint32 {
		return fmt.Errorf("node size %d out of range", size)
	}

	return w.writeBranch(node.Version(), uint32(size), node.Height(), preTrees, keyLeaf, node.Hash())
}

func createFile(name string) (*os.File, error) {
	return os.OpenFile(filepath.Clean(name), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
}

// prefetchSnapshot sequentially reads snapshot files into page cache
// This is critical for cold-start performance: eliminates 99% of random I/O during replay
func (snapshot *Snapshot) prefetchSnapshot(snapshotDir string, prefetchThreshold float64) {
	startTime := time.Now()
	if snapshot.nodes == nil && snapshot.leaves == nil {
		return // Empty snapshot
	}
	// Selective preload: only preload large and active trees
	// Small/inactive trees have minimal I/O during replay, not worth preloading
	treeName := filepath.Base(snapshotDir)
	needsPreload := shouldPreloadTree(treeName)
	if !needsPreload {
		return
	}
	log := snapshot.logger

	// If most pages are already in page cache, skip prefetch
	residentNodes, errNodes := residentRatio(snapshot.nodes)
	residentLeaves, errLeaves := residentRatio(snapshot.leaves)
	if errNodes == nil && errLeaves == nil {
		if residentNodes >= prefetchThreshold && residentLeaves >= prefetchThreshold {
			log.Debug(fmt.Sprintf("Skipped prefetching for tree %s\n", treeName))
			return
		}
	}

	if residentNodes < prefetchThreshold {
		log.Info(fmt.Sprintf("Tree %s nodes page cache residency ratio is %f, below threshold %f\n", treeName, residentNodes, prefetchThreshold))
		_ = SequentialReadAndFillPageCache(filepath.Join(snapshotDir, FileNameNodes))
	}

	if residentLeaves < prefetchThreshold {
		log.Info(fmt.Sprintf("Tree %s leaves page cache residency ratio is %f, below threshold %f\n", treeName, residentLeaves, prefetchThreshold))
		_ = SequentialReadAndFillPageCache(filepath.Join(snapshotDir, FileNameLeaves))
	}

	log.Info(fmt.Sprintf("Prefetch snapshot for %s completed in %fs. Consider adding more RAM for page cache to avoid preloading during restart.\n", treeName, time.Since(startTime).Seconds()))
}

// shouldPreloadTree determines if a tree should be preloaded based on size and name
// Only large/active trees benefit from preload; small trees add overhead
func shouldPreloadTree(treeName string) bool {
	// Preload the 4 largest/most active trees
	// Parallel loading + madvise hints will maximize throughput even on slow disks
	// evm (512M nodes), bank (278M nodes), acc (155M nodes), wasm (27M nodes)
	activeTrees := map[string]bool{
		"evm":  true,
		"bank": true,
		"acc":  true,
		"wasm": true, // Added: 27M nodes, worth prefetching in cold start
	}

	return activeTrees[treeName]
}

func SequentialReadAndFillPageCache(filePath string) error {
	startTime := time.Now()
	f, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer f.Close() // Ensure file handle is released for pruning

	fileInfo, err := f.Stat()
	if err != nil {
		return err
	}

	totalSize := fileInfo.Size()
	fmt.Printf("[PREFETCH] Starting to prefetch file: %s (size: %d MB)\n", filePath, totalSize/(1024*1024))

	// Mmap the file to apply madvise hints
	// This tells the kernel to:
	// 1. Read sequentially (MADV_SEQUENTIAL) - enables aggressive readahead
	// 2. Keep in cache (MADV_WILLNEED) - prioritize retention
	// This helps prevent eviction when write buffers compete for memory
	if totalSize > 0 {
		data, err := unix.Mmap(int(f.Fd()), 0, int(totalSize), unix.PROT_READ, unix.MAP_SHARED)
		if err == nil {
			// Tell kernel this will be read sequentially - enables aggressive readahead
			_ = unix.Madvise(data, unix.MADV_SEQUENTIAL)
			// Tell kernel we need this data soon - start readahead immediately
			_ = unix.Madvise(data, unix.MADV_WILLNEED)
			// Unmap after setting hints - the hints persist on the underlying pages
			defer unix.Munmap(data)
			fmt.Printf("[PREFETCH] Applied madvise hints (SEQUENTIAL + WILLNEED) to %s\n", filePath)
		} else {
			fmt.Printf("[PREFETCH] Warning: mmap failed for %s: %v (continuing with read)\n", filePath, err)
		}
	} else {
		fmt.Printf("[PREFETCH] Skipping empty file: %s\n", filePath)
		return nil
	}

	reportDone := make(chan struct{})
	var totalRead int64
	defer close(reportDone) // Stop progress reporter before returning

	startPrefetchProgressReporter(filePath, totalSize, &totalRead, startTime, reportDone)

	concurrency := runtime.NumCPU()
	var wg sync.WaitGroup
	jobs := make(chan [2]int64, concurrency)
	wg.Add(concurrency)
	const chunkSize = 16 * 1024 * 1024 // 16MB
	for w := 0; w < concurrency; w++ {
		go func() {
			defer wg.Done()
			buf := make([]byte, chunkSize)
			for job := range jobs {
				readChunkIntoCache(f, buf, job[0], int(job[1]), &totalRead)
			}
		}()
	}

	// Enqueue chunks sequentially to retain locality
	for offset := int64(0); offset < totalSize; offset += chunkSize {
		end := offset + chunkSize
		if end > totalSize {
			end = totalSize
		}
		jobs <- [2]int64{offset, end - offset}
	}
	close(jobs)
	wg.Wait()

	elapsed := time.Since(startTime).Seconds()
	avgSpeedMBps := float64(totalSize) / elapsed / (1024 * 1024)
	fmt.Printf("Completed prefetching %s: %d MB in %.1fs (%.1f MB/s)\n",
		filePath, totalSize/(1024*1024), elapsed, avgSpeedMBps)
	return nil
}

// startPrefetchProgressReporter periodically logs progress until done is closed.
func startPrefetchProgressReporter(filePath string, totalSize int64, totalRead *int64, startTime time.Time, done <-chan struct{}) {
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				tr := atomic.LoadInt64(totalRead)
				elapsed := time.Since(startTime).Seconds()
				if elapsed <= 0 {
					continue
				}
				speedMBps := float64(tr) / elapsed / (1024 * 1024)
				progressPct := float64(tr) * 100 / float64(totalSize)
				remaining := float64(totalSize-tr) / (speedMBps * 1024 * 1024)
				fmt.Printf("Prefetching file '%s': %d/%d MB (%.1f%%), speed: %.1f MB/s, ETA: %.0fs\n",
					filePath, tr/(1024*1024), totalSize/(1024*1024), progressPct, speedMBps, remaining)
			}
		}
	}()
}

// readChunkIntoCache reads n bytes starting at pos, updating totalRead.
func readChunkIntoCache(f *os.File, buf []byte, pos int64, n int, totalRead *int64) {
	remaining := n
	for remaining > 0 {
		readN, er := f.ReadAt(buf[:remaining], pos)
		if readN > 0 {
			pos += int64(readN)
			remaining -= readN
			atomic.AddInt64(totalRead, int64(readN))
		}
		if er == io.EOF {
			break
		}
		if er != nil && er != io.ErrUnexpectedEOF {
			// Best-effort warming; ignore transient errors
			break
		}
		if readN == 0 {
			break
		}
	}
}

// residentRatio returns fraction of pages resident in the page cache for data.
// Uses mincore on Linux; on other platforms returns an unsupported error.
func residentRatio(data []byte) (float64, error) {
	if len(data) == 0 {
		return 1, nil
	}
	if runtime.GOOS != "linux" {
		return 0, fmt.Errorf("residentRatio unsupported on %s", runtime.GOOS)
	}

	pageSize := unix.Getpagesize()
	numPages := (len(data) + pageSize - 1) / pageSize
	if numPages == 0 {
		return 1, nil
	}
	vec := make([]byte, numPages)

	addr := uintptr(unsafe.Pointer(&data[0]))
	length := uintptr(len(data))
	_, _, errno := unix.Syscall(unix.SYS_MINCORE, addr, length, uintptr(unsafe.Pointer(&vec[0])))
	if errno != 0 {
		return 0, errno
	}

	present := 0
	for _, v := range vec {
		if v&1 == 1 {
			present++
		}
	}
	return float64(present) / float64(len(vec)), nil
}
