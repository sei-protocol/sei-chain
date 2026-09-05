package walrus

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/util"
)

// The subdirectory of an instance's path that holds pods.
const podsDirName = "pods"

var _ Walrus = (*engine)(nil)

// engine is the pod lifecycle, the query walk, and retention.
type engine struct {
	config       *Config
	podDirectory string
	accumulator  *podAccumulator
	builder      *podBuilder
	catalog      *catalog

	// Bounds the number of pods being built at once. A build holds its blocks and its sort buffers in
	// memory, so the limit is memory rather than cores; a full channel blocks AppendBlock, which is the
	// honest response to falling behind.
	buildSlots chan struct{}

	// Completed builds on their way to the orderer.
	results chan buildResult

	// Counts builds that have been submitted but not yet added to the catalog, so Flush can wait for them.
	pending sync.WaitGroup

	// The sequence number of the next pod submitted, which is what the orderer restores order by.
	nextSequence uint64

	// Set when a build or an add fails. The engine stops accepting work after that: losing a pod means
	// losing a span of blocks, which nothing downstream can reconstruct.
	failureLock sync.Mutex
	failure     error

	// Pods submitted but not yet finished building, published as the build queue depth.
	queued atomic.Int64

	ordererDone chan struct{}
	closed      bool
}

// buildResult is one finished build on its way to the orderer.
type buildResult struct {
	sequence uint64
	pod      *Pod
	err      error
}

// AppendBlock records the changes a block made.
func (e *engine) AppendBlock(block Block) error {
	if err := e.checkFailure(); err != nil {
		return err
	}
	for _, changeSet := range block.ChangeSets {
		if changeSet.Name != e.config.StoreName {
			return fmt.Errorf("block %d carries a changeset named %q, but this instance indexes %q",
				block.Number, changeSet.Name, e.config.StoreName)
		}
	}

	pod, err := e.accumulator.Add(block)
	if err != nil {
		return fmt.Errorf("failed to accumulate block %d: %w", block.Number, err)
	}

	// Submitting can block while every build slot is busy. That wait is the write path being backpressured
	// rather than merely slow, which is a difference nothing else in the metrics would show.
	var blocked time.Duration
	if pod != nil {
		blocked = e.submit(pod)
	}

	blockSize := int(encodedBlockSize(block)) //nolint:gosec // G115 - far below the int ceiling
	recordAppend(e.config.Name, countPairs(block), blockSize, blocked)
	return nil
}

// Flush writes the pod being accumulated and waits for every submitted build to become queryable.
func (e *engine) Flush() error {
	if pod := e.accumulator.Drain(); pod != nil {
		e.submit(pod)
	}
	e.pending.Wait()
	return e.checkFailure()
}

// RetainSnapshot takes an independent reference to a state snapshot.
func (e *engine) RetainSnapshot(blockNumber uint64, directory string) error {
	if err := e.checkFailure(); err != nil {
		return err
	}
	snapshot, err := retainSnapshot(filepath.Join(e.config.Path, snapshotsDirName), blockNumber, directory)
	if err != nil {
		return err
	}
	e.catalog.AddSnapshot(snapshot)
	return nil
}

// Get returns the value key held at the end of blockNumber.
func (e *engine) Get(key []byte, blockNumber uint64) (value []byte, status ReadStatus, err error) {
	if err := e.checkFailure(); err != nil {
		return nil, ReadAbsent, err
	}

	ok, first, last := e.catalog.Bounds()
	if !ok || blockNumber > last {
		return nil, ReadTooNew, nil
	}
	if blockNumber < first {
		return nil, ReadTooOld, nil
	}

	query, admitted := e.catalog.Query(blockNumber)
	if !admitted {
		return nil, ReadTooOld, nil
	}
	defer query.Release()

	start := time.Now()
	result, err := e.walk(query, key, blockNumber)
	if err != nil {
		return nil, ReadAbsent, err
	}
	recordQuery(e.config.Name, start, result.status, result.probed, result.searched,
		result.readSnapshot, result.timing)
	return result.value, result.status, nil
}

// QueryableBounds reports the range of blocks Get can answer.
func (e *engine) QueryableBounds() (ok bool, first uint64, last uint64, err error) {
	if err := e.checkFailure(); err != nil {
		return false, 0, 0, err
	}
	ok, first, last = e.catalog.Bounds()
	return ok, first, last, nil
}

// Close writes the pod being accumulated and releases resources.
func (e *engine) Close() error {
	if e.closed {
		return nil
	}
	e.closed = true

	flushErr := e.Flush()
	close(e.results)
	<-e.ordererDone
	return flushErr
}

// walkResult is one completed walk: what it found and what it cost.
type walkResult struct {
	value        []byte
	status       ReadStatus
	probed       int
	searched     int
	readSnapshot bool
	timing       walkTiming
}

// walk searches pods newest first and terminates at the floor snapshot.
//
// Each phase is timed separately and accumulated across the pods visited, because the interesting question
// is not how long a read took but which part of it was expensive: ruling pods out with bloom filters,
// searching the indexes of the pods that were not ruled out, or reading the entry once it was located.
func (e *engine) walk(query Query, key []byte, blockNumber uint64) (walkResult, error) {
	floorBlock := query.Floor().BlockNumber()
	result := walkResult{status: ReadAbsent}

	for _, pod := range query.Pods() {
		result.probed++

		bloomStart := time.Now()
		admitted := pod.Bloom.MayContain(key)
		result.timing.bloom += time.Since(bloomStart)
		if !admitted {
			recordBloomProbe(e.config.Name, bloomRuledOut)
			continue
		}
		result.searched++

		indexStart := time.Now()
		offset, _, found, present, err := pod.Index.FindNewest(key, floorBlock, blockNumber)
		result.timing.index += time.Since(indexStart)
		if err != nil {
			return result, fmt.Errorf("failed to search %s: %w", pod.Info, err)
		}

		if present {
			recordBloomProbe(e.config.Name, bloomTruePositive)
		} else {
			recordBloomProbe(e.config.Name, bloomFalsePositive)
		}
		if !found {
			continue
		}

		dataStart := time.Now()
		entry, deleted, err := pod.Data.ReadEntry(offset)
		result.timing.data += time.Since(dataStart)
		if err != nil {
			return result, fmt.Errorf("failed to read %s: %w", pod.Info, err)
		}
		if deleted {
			return result, nil
		}
		result.value = entry
		result.status = ReadFound
		return result, nil
	}

	// Every pod above the floor has been ruled out, so whatever the key held is whatever the floor holds.
	result.readSnapshot = true
	snapshotStart := time.Now()
	stored, found, err := query.Floor().Get(key)
	result.timing.snapshot = time.Since(snapshotStart)
	if err != nil {
		return result, fmt.Errorf("failed to read the floor snapshot: %w", err)
	}
	if found {
		result.value = stored
		result.status = ReadFound
	}
	return result, nil
}

// submit hands a completed pod to the build pipeline and reports how long it waited for a slot.
//
// The wait is the backpressure: when every slot is busy there is nowhere to put the pod, and blocking here
// is the honest response. The alternative is unbounded memory growth that ends in a kill.
func (e *engine) submit(blocks []Block) time.Duration {
	sequence := e.nextSequence
	e.nextSequence++
	e.pending.Add(1)
	recordBuildQueueDepth(e.config.Name, int(e.queued.Add(1)))

	start := time.Now()
	e.buildSlots <- struct{}{}
	blocked := time.Since(start)

	go func() {
		pod, err := e.builder.Build(blocks)
		<-e.buildSlots
		recordBuildQueueDepth(e.config.Name, int(e.queued.Add(-1)))
		e.results <- buildResult{sequence: sequence, pod: pod, err: err}
	}()
	return blocked
}

// runOrderer releases finished builds to the catalog in the order they were submitted.
//
// Builds run concurrently and finish out of order. A pod added out of order would leave a gap that a walk
// falls through, answering from the floor rather than failing, so an early finisher waits here for the pod
// before it.
func (e *engine) runOrderer() {
	defer close(e.ordererDone)

	waiting := map[uint64]buildResult{}
	next := uint64(0)

	for result := range e.results {
		waiting[result.sequence] = result
		for {
			ready, ok := waiting[next]
			if !ok {
				break
			}
			delete(waiting, next)
			next++

			if ready.err != nil {
				e.setFailure(fmt.Errorf("failed to build a pod: %w", ready.err))
			} else {
				e.catalog.AddPod(ready.pod)
				e.applyRetention(ready.pod.Info.LastBlock)
			}
			e.pending.Done()
		}
	}
}

// applyRetention raises the query floor to the retention window below head and collects what that releases.
func (e *engine) applyRetention(head uint64) {
	if head > e.config.RetentionBlocks {
		e.catalog.SetQueryFloor(head - e.config.RetentionBlocks)
	}
	if _, _, err := e.catalog.Collect(); err != nil {
		e.setFailure(fmt.Errorf("failed to collect: %w", err))
	}
}

// setFailure latches the first failure, after which the engine refuses further work.
func (e *engine) setFailure(err error) {
	e.failureLock.Lock()
	defer e.failureLock.Unlock()
	if e.failure == nil {
		e.failure = err
	}
}

// checkFailure reports the latched failure, if any.
func (e *engine) checkFailure() error {
	e.failureLock.Lock()
	defer e.failureLock.Unlock()
	return e.failure
}

// New opens a Walrus instance in the configured directory.
//
// Opening recovers what a previous session left behind: files from an interrupted build are deleted, the
// pods on disk are truncated to an unbroken run, and retained snapshots are relisted. Recovery is deletion
// rather than repair, because a pod is written whole or not at all.
func New(config *Config) (Walrus, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	podDirectory := filepath.Join(config.Path, podsDirName)
	snapshotDirectory := filepath.Join(config.Path, snapshotsDirName)
	for _, directory := range []string{podDirectory, snapshotDirectory} {
		if err := os.MkdirAll(directory, 0o750); err != nil {
			return nil, fmt.Errorf("failed to create %s: %w", directory, err)
		}
	}

	pods, err := recoverPods(podDirectory)
	if err != nil {
		return nil, err
	}
	retained, err := openRetainedSnapshots(snapshotDirectory)
	if err != nil {
		return nil, err
	}
	snapshots := make([]Snapshot, 0, len(retained))
	for _, snapshot := range retained {
		snapshots = append(snapshots, snapshot)
	}

	created := &engine{
		config:       config,
		podDirectory: podDirectory,
		accumulator:  newPodAccumulator(config),
		builder:      newPodBuilder(podDirectory, config),
		catalog:      newCatalog(config, pods, snapshots),
		buildSlots:   make(chan struct{}, config.PodBuildConcurrency),
		results:      make(chan buildResult, config.PodBuildConcurrency+1),
		ordererDone:  make(chan struct{}),
	}
	if len(pods) > 0 {
		created.accumulator.lastBlock = pods[len(pods)-1].Info.LastBlock
		created.accumulator.started = true
	}

	go created.runOrderer()
	return created, nil
}

// recoverPods opens the pods in a directory, deleting the wreckage of an interrupted build and truncating to
// an unbroken run of blocks.
//
// A pod above a gap is deleted rather than kept, because a walk that spans the gap would fall through it and
// answer from the floor. The blocks it held are gone and the caller resumes appending from the gap, which is
// how replaying a log behaves anyway.
func recoverPods(directory string) ([]*Pod, error) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", directory, err)
	}

	present := map[string]bool{}
	infos := make([]*PodInfo, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasSuffix(name, podPartialExtension) {
			if err := os.Remove(filepath.Join(directory, name)); err != nil {
				return nil, fmt.Errorf("failed to clear the interrupted build %s: %w", name, err)
			}
			continue
		}
		present[name] = true
		if info, ok := ParsePodName(name); ok {
			infos = append(infos, info)
		}
	}
	sort.Slice(infos, func(a int, b int) bool { return infos[a].FirstBlock < infos[b].FirstBlock })

	keep := 0
	for index, info := range infos {
		complete := present[filepath.Base(info.IndexPath(""))] && present[filepath.Base(info.BloomPath(""))]
		contiguous := index == 0 || info.FirstBlock == infos[index-1].LastBlock+1
		if !complete || !contiguous {
			break
		}
		keep++
	}

	for _, info := range infos[keep:] {
		paths := []string{info.DataPath(directory), info.IndexPath(directory), info.BloomPath(directory)}
		for _, path := range paths {
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				return nil, fmt.Errorf("failed to truncate %s: %w", path, err)
			}
		}
	}
	if keep < len(infos) {
		if err := util.SyncPath(directory); err != nil {
			return nil, fmt.Errorf("failed to sync %s after truncating: %w", directory, err)
		}
	}

	pods := make([]*Pod, 0, keep)
	for _, info := range infos[:keep] {
		pod, err := openPod(directory, info)
		if err != nil {
			return nil, err
		}
		pods = append(pods, pod)
	}
	return pods, nil
}
