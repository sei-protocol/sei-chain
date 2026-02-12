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

	"github.com/sei-protocol/sei-chain/sei-db/common/errors"
	"github.com/sei-protocol/sei-chain/sei-db/common/logger"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
	"golang.org/x/sys/unix"
	"golang.org/x/time/rate"
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

// rateLimitedWriter wraps an io.Writer with rate limiting to prevent
// page cache eviction on machines with limited RAM.
type rateLimitedWriter struct {
	w       io.Writer
	limiter *rate.Limiter
	ctx     context.Context
}

// NewGlobalRateLimiter creates a shared rate limiter for snapshot writes.
// rateMBps is the rate limit in MB/s. If <= 0, returns nil (no limit).
// This limiter should be shared across all files and trees in a single snapshot operation.
func NewGlobalRateLimiter(rateMBps int) *rate.Limiter {
	if rateMBps <= 0 {
		return nil
	}
	const mb = 1024 * 1024
	bytesPerSec := rate.Limit(rateMBps * mb)
	// Burst = 4MB: small enough to spread large bufio flushes (128MB) across
	// many smaller IO ops, preventing page cache eviction spikes.
	burstBytes := 4 * mb
	return rate.NewLimiter(bytesPerSec, burstBytes)
}

// newRateLimitedWriter creates a rate-limited writer with a shared limiter.
// If limiter is nil, returns the original writer (no limit).
func newRateLimitedWriter(ctx context.Context, w io.Writer, limiter *rate.Limiter) io.Writer {
	if limiter == nil {
		return w
	}
	return &rateLimitedWriter{
		w:       w,
		limiter: limiter,
		ctx:     ctx,
	}
}

func (w *rateLimitedWriter) Write(p []byte) (n int, err error) {
	// Wait for rate limiter before writing
	// For large writes, we may need to wait multiple times
	remaining := len(p)
	written := 0
	for remaining > 0 {
		// Limit each wait to burst size to avoid very long waits
		toWrite := remaining
		if toWrite > w.limiter.Burst() {
			toWrite = w.limiter.Burst()
		}
		if err := w.limiter.WaitN(w.ctx, toWrite); err != nil {
			return written, err
		}
		n, err := w.w.Write(p[written : written+toWrite])
		written += n
		remaining -= n
		if err != nil {
			return written, err
		}
	}
	return written, nil
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

	// Load snapshot mmap files with MADV_RANDOM.
	// Snapshot prefetch is handled separately by prefetchSnapshot() at the end of this function.
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
	if opts.SnapshotPrefetchThreshold > 0 {
		snapshot.prefetchSnapshot(snapshotDir, opts.SnapshotPrefetchThreshold)
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
	return t.WriteSnapshotWithRateLimit(ctx, snapshotDir, nil)
}

// WriteSnapshotWithRateLimit writes snapshot with optional rate limiting.
// limiter is a shared rate limiter. nil means unlimited.
func (t *Tree) WriteSnapshotWithRateLimit(ctx context.Context, snapshotDir string, limiter *rate.Limiter) error {
	// Estimate tree size: root.Size() returns leaf count, total = leaves + branches ≈ 2x
	treeSize := int64(0)
	if t.root != nil {
		treeSize = t.root.Size() * 2 // Total nodes (leaves + branches)
	}

	// Use 128MB buffer for all trees (large buffer for better performance)
	bufSize := bufIOSize

	err := writeSnapshotWithBuffer(ctx, snapshotDir, t.version, bufSize, treeSize, limiter, t.logger, func(w *snapshotWriter) (uint32, error) {
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

// writeSnapshotWithBuffer writes snapshot with specified buffer size and optional rate limiting.
// limiter is a shared rate limiter. nil means unlimited.
func writeSnapshotWithBuffer(
	ctx context.Context,
	dir string, version uint32,
	bufSize int,
	totalNodes int64,
	limiter *rate.Limiter,
	log logger.Logger,
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

	// Apply rate limiting if configured (shared limiter across all files)
	// This ensures total write rate is capped regardless of file count
	var nodesRateLimited, leavesRateLimited, kvsRateLimited io.Writer
	nodesRateLimited = newRateLimitedWriter(ctx, nodesMonitor, limiter)
	leavesRateLimited = newRateLimitedWriter(ctx, leavesMonitor, limiter)
	kvsRateLimited = newRateLimitedWriter(ctx, kvsMonitor, limiter)

	// Create buffered writers with buffers
	nodesWriter := bufio.NewWriterSize(nodesRateLimited, bufSize)
	leavesWriter := bufio.NewWriterSize(leavesRateLimited, bufSize)
	kvsWriter := bufio.NewWriterSize(kvsRateLimited, bufSize)

	w := newSnapshotWriter(ctx, nodesWriter, leavesWriter, kvsWriter, log)
	w.treeName = filepath.Base(dir) // Set tree name for progress reporting
	w.totalNodes = totalNodes       // Set total nodes for progress percentage
	w.traversalStartTime = time.Now()

	leaves, err := doWrite(w)
	// Always wait for writer goroutines to finish
	waitErr := w.waitForWrites()

	// Handle errors with priority to waitErr (the underlying I/O error)
	if err != nil {
		// If doWrite failed due to context cancellation, return the real I/O error
		if err == context.Canceled && waitErr != nil {
			return waitErr
		}
		return err
	}
	if waitErr != nil {
		return waitErr
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
	// Use nop logger and no rate limit for backward compatibility
	return writeSnapshotWithBuffer(ctx, dir, version, bufIOSize, 0, nil, logger.NewNopLogger(), doWrite)
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
	ctx    context.Context
	cancel context.CancelFunc

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
	logger                 logger.Logger

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
	// Clamp size between 100 (minimum to avoid deadlocks) and 100000 (maximum to avoid excessive memory)
	nodeChanSize = max(100, min(size, 100000))
}

func newSnapshotWriter(ctx context.Context, nodesWriter, leavesWriter, kvsWriter io.Writer, log logger.Logger) *snapshotWriter {
	// Create a cancelable context so we can stop producers on error
	ctx, cancel := context.WithCancel(ctx)
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
		cancel:                 cancel,
		nodesWriter:            nodesWriter,
		leavesWriter:           leavesWriter,
		kvWriter:               kvsWriter,
		lastProgressReport:     now,
		progressReportInterval: 30 * time.Second,
		logger:                 log,
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
			w.fail(fmt.Errorf("kv write error: %w", err))
			return
		}
	}
}

// leafWriterLoop processes leaf write operations in parallel
func (w *snapshotWriter) leafWriterLoop() {
	defer w.wg.Done()

	for op := range w.leafChan {
		if err := w.writeLeafDirect(op.version, op.keyLen, op.keyOffset, op.hash); err != nil {
			w.fail(fmt.Errorf("leaf write error: %w", err))
			return
		}
	}
}

// branchWriterLoop processes branch write operations in parallel
func (w *snapshotWriter) branchWriterLoop() {
	defer w.wg.Done()

	for op := range w.branchChan {
		if err := w.writeBranchDirect(op.version, op.size, op.height, op.preTrees, op.keyLeaf, op.hash); err != nil {
			w.fail(fmt.Errorf("branch write error: %w", err))
			return
		}
	}
}

// fail records an error and cancels the context to stop producers
func (w *snapshotWriter) fail(err error) {
	// Log the error immediately for debugging
	if w.logger != nil {
		w.logger.Error("snapshot writer failed, canceling operation",
			"tree", w.treeName,
			"error", err.Error(),
			"branches_written", w.branchCounter,
			"leaves_written", w.leafCounter,
		)
	}

	select {
	case w.writeErrors <- err:
	default:
		// Channel full, error already recorded
	}
	w.cancel()
}

// waitForWrites waits for all pending writes to complete and returns any error
func (w *snapshotWriter) waitForWrites() error {
	// Close all channels to signal completion
	close(w.kvChan)
	close(w.leafChan)
	close(w.branchChan)

	if w.logger != nil {
		w.logger.Info("waiting for async writers to complete",
			"tree", w.treeName,
			"branches_queued", w.branchCounter,
			"leaves_queued", w.leafCounter,
		)
	}

	// Wait for all writer goroutines to finish
	w.wg.Wait()

	// Check for any errors
	select {
	case err := <-w.writeErrors:
		if w.logger != nil {
			w.logger.Error("async writer reported error after completion",
				"tree", w.treeName,
				"error", err.Error(),
			)
		}
		return err
	default:
		if w.logger != nil {
			w.logger.Info("all async writers completed successfully",
				"tree", w.treeName,
				"total_branches", w.branchCounter,
				"total_leaves", w.leafCounter,
			)
		}
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
			log.Info(fmt.Sprintf("Skipped prefetching for tree %s (nodes: %.2f%%, leaves: %.2f%%, threshold: %.2f%%)",
				treeName, residentNodes*100, residentLeaves*100, prefetchThreshold*100))
			return
		}
	}

	if residentNodes < prefetchThreshold {
		log.Info(fmt.Sprintf("Tree %s nodes page cache residency ratio is %f, below threshold %f", treeName, residentNodes, prefetchThreshold))
		_ = SequentialReadAndFillPageCache(log, filepath.Join(snapshotDir, FileNameNodes))
	}

	if residentLeaves < prefetchThreshold {
		log.Info(fmt.Sprintf("Tree %s leaves page cache residency ratio is %f, below threshold %f", treeName, residentLeaves, prefetchThreshold))
		_ = SequentialReadAndFillPageCache(log, filepath.Join(snapshotDir, FileNameLeaves))
	}

	log.Info(fmt.Sprintf("Prefetch snapshot for %s completed in %fs. Consider adding more RAM for page cache to avoid preloading during restart.", treeName, time.Since(startTime).Seconds()))
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

func SequentialReadAndFillPageCache(log logger.Logger, filePath string) error {
	filePath = filepath.Clean(filePath)
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
	if totalSize == 0 {
		log.Debug("skipping empty file for prefetch", "path", filePath)
		return nil
	}

	sizeMiB := float64(totalSize) / (1024 * 1024)
	log.Debug("starting to prefetch file", "path", filePath, "size_mib", sizeMiB)

	reportDone := make(chan struct{})
	var totalRead int64
	defer close(reportDone) // Stop progress reporter before returning

	startPrefetchProgressReporter(log, filePath, totalSize, &totalRead, startTime, reportDone)

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
	completedSizeMiB := float64(totalSize) / (1024 * 1024)
	avgSpeedMiBps := float64(totalSize) / elapsed / (1024 * 1024)
	log.Debug("completed prefetching file",
		"path", filePath,
		"size_mib", completedSizeMiB,
		"elapsed_sec", elapsed,
		"speed_mib_per_sec", avgSpeedMiBps)
	return nil
}

// startPrefetchProgressReporter periodically logs progress until done is closed.
func startPrefetchProgressReporter(log logger.Logger, filePath string, totalSize int64, totalRead *int64, startTime time.Time, done <-chan struct{}) {
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
				readMiB := float64(tr) / (1024 * 1024)
				totalMiB := float64(totalSize) / (1024 * 1024)
				speedMiBps := float64(tr) / elapsed / (1024 * 1024)
				progressPct := float64(tr) * 100 / float64(totalSize)
				remaining := float64(totalSize-tr) / (speedMiBps * 1024 * 1024)
				log.Debug("prefetching file progress",
					"path", filePath,
					"read_mib", readMiB,
					"total_mib", totalMiB,
					"progress_pct", progressPct,
					"speed_mib_per_sec", speedMiBps,
					"eta_sec", remaining)
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
