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
	"golang.org/x/sys/unix"

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
	for ; i < uint32(snapshot.nodesLen()); i++ { //nolint:gosec
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

	w.kvsOffset += 4 + 4 + uint64(keyLen) + uint64(valueLen)
	return nil
}

func (w *snapshotWriter) writeLeaf(version uint32, key, value, hash []byte) error {
	var buf [SizeLeafWithoutHash]byte
	binary.LittleEndian.PutUint32(buf[OffsetLeafVersion:], version)
	binary.LittleEndian.PutUint32(buf[OffsetLeafKeyLen:], uint32(len(key))) //nolint:gosec
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
	log.Info(fmt.Sprintf("Tree %s nodes page cache residency ratio is %f\n", treeName, residentNodes))
	log.Info(fmt.Sprintf("Tree %s leaves page cache residency ratio is %f\n", treeName, residentLeaves))
	if errNodes == nil && errLeaves == nil {
		if residentNodes >= prefetchThreshold && residentLeaves >= prefetchThreshold {
			log.Info(fmt.Sprintf("Skipped prefetching for tree %s\n", treeName))
			return
		}
	}

	if residentNodes < prefetchThreshold {
		_ = SequentialReadAndFillPageCache(filepath.Join(snapshotDir, FileNameNodes))
	}

	if residentLeaves < prefetchThreshold {
		_ = SequentialReadAndFillPageCache(filepath.Join(snapshotDir, FileNameLeaves))
	}
	log.Info(fmt.Sprintf("Prefetch all snapshot files completed in %fs\n", time.Since(startTime).Seconds()))
}

// shouldPreloadTree determines if a tree should be preloaded based on size and name
// Only large/active trees benefit from preload; small trees add overhead
func shouldPreloadTree(treeName string) bool {
	// Preload the 3 largest/most active trees
	// Parallel loading + madvise hints will maximize throughput even on slow disks
	activeTrees := map[string]bool{
		"evm":  true,
		"bank": true,
		"acc":  true,
	}

	return activeTrees[treeName]
}

func SequentialReadAndFillPageCache(filePath string) error {
	startTime := time.Now()
	fmt.Printf("[PREFETCH] Starting to prefetch file: %s\n", filePath)
	f, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer f.Close() // Ensure file handle is released for pruning

	fileInfo, err := f.Stat()
	if err != nil {
		return err
	}
	reportDone := make(chan struct{})
	var totalRead int64
	totalSize := fileInfo.Size()
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
