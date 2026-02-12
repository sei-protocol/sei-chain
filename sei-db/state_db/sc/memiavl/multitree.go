package memiavl

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/alitto/pond"
	"golang.org/x/exp/slices"
	"golang.org/x/time/rate"

	"github.com/sei-protocol/sei-chain/sei-db/common/errors"
	"github.com/sei-protocol/sei-chain/sei-db/common/logger"
	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/wal"
	"github.com/cosmos/iavl"
)

const (
	MetadataFileName = "__metadata"

	// Load time threshold for performance warning (300 seconds)
	slowLoadThreshold = 5 * time.Minute
)

type NamedTree struct {
	*Tree
	Name string
}

// MultiTree manages multiple memiavl tree together,
// all the trees share the same latest version, the snapshots are always created at the same version.
//
// The snapshot structure is like this:
// ```
// > snapshot-V
// >  metadata
// >  bank
// >   kvs
// >   nodes
// >   metadata
// >  acc
// >  other stores...
// ```
type MultiTree struct {
	// if the tree is start from genesis, it's the initial version of the chain,
	// if the tree is imported from snapshot, it's the imported version plus one,
	// it always corresponds to the rlog entry with index 1.
	// Use atomic for concurrent read/write safety
	initialVersion atomic.Uint32

	zeroCopy bool
	logger   logger.Logger

	trees          []NamedTree    // always ordered by tree name
	treesByName    map[string]int // index of the trees by name
	lastCommitInfo proto.CommitInfo

	// the initial metadata loaded from disk snapshot
	metadata proto.MultiTreeMetadata
}

func NewEmptyMultiTree(initialVersion uint32) *MultiTree {
	mt := &MultiTree{
		treesByName: make(map[string]int),
		zeroCopy:    true,
		logger:      logger.NewNopLogger(),
	}
	mt.initialVersion.Store(initialVersion)
	return mt
}

func LoadMultiTree(dir string, opts Options) (*MultiTree, error) {
	startTime := time.Now()
	log := opts.Logger
	if log == nil {
		log = logger.NewNopLogger()
	}
	metadata, err := readMetadata(dir)
	if err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	treeMap := make(map[string]*Tree, len(entries))
	treeNames := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		treeNames = append(treeNames, name)
		snapshot, err := OpenSnapshot(filepath.Join(dir, name), opts)
		if err != nil {
			return nil, err
		}
		treeMap[name] = NewFromSnapshot(snapshot, opts)
	}
	elapsed := time.Since(startTime)
	log.Info(fmt.Sprintf("All %d memIAVL trees loaded in %.1fs", len(treeNames), elapsed.Seconds()))

	if elapsed > slowLoadThreshold {
		log.Info("Loading MemIAVL tree from disk is too slow! Consider increasing the disk bandwidth to speed up the tree loading time within 300 seconds.")
	}
	slices.Sort(treeNames)

	trees := make([]NamedTree, len(treeNames))
	treesByName := make(map[string]int, len(trees))
	for i, name := range treeNames {
		tree := treeMap[name]
		trees[i] = NamedTree{Tree: tree, Name: name}
		treesByName[name] = i
	}

	mtree := &MultiTree{
		trees:          trees,
		treesByName:    treesByName,
		lastCommitInfo: *metadata.CommitInfo,
		metadata:       *metadata,
		zeroCopy:       opts.ZeroCopy,
		logger:         opts.Logger,
	}
	// initial version is necessary for rlog index conversion
	mtree.setInitialVersion(metadata.InitialVersion)
	return mtree, nil
}

// TreeByName returns the tree by name, returns nil if not found
func (t *MultiTree) TreeByName(name string) *Tree {
	if i, ok := t.treesByName[name]; ok {
		return t.trees[i].Tree
	}
	return nil
}

// Trees returns all the trees together with the name, ordered by name.
func (t *MultiTree) Trees() []NamedTree {
	return t.trees
}

func (t *MultiTree) SetInitialVersion(initialVersion int64) error {
	if initialVersion >= math.MaxUint32 {
		return fmt.Errorf("version overflows uint32: %d", initialVersion)
	}

	if t.Version() != 0 {
		return fmt.Errorf("multi tree is not empty: %d", t.Version())
	}

	for _, entry := range t.trees {
		if !entry.IsEmpty() {
			return fmt.Errorf("tree is not empty: %s", entry.Name)
		}
	}

	t.setInitialVersion(initialVersion)
	return nil
}

func (t *MultiTree) setInitialVersion(initialVersion int64) {
	if initialVersion < 0 || initialVersion > math.MaxUint32 {
		panic(fmt.Sprintf("initial version %d is out of range", initialVersion))
	}
	iv := uint32(initialVersion)
	t.initialVersion.Store(iv)
	for _, entry := range t.trees {
		entry.initialVersion = iv
	}
}

func (t *MultiTree) SetZeroCopy(zeroCopy bool) {
	t.zeroCopy = zeroCopy
	for _, entry := range t.trees {
		entry.SetZeroCopy(zeroCopy)
	}
}

// Copy returns a snapshot of the tree which won't be corrupted by further modifications on the main tree.
func (t *MultiTree) Copy() *MultiTree {
	trees := make([]NamedTree, len(t.trees))
	treesByName := make(map[string]int, len(t.trees))
	for i, entry := range t.trees {
		tree := entry.Copy()
		trees[i] = NamedTree{Tree: tree, Name: entry.Name}
		treesByName[entry.Name] = i
	}

	clone := *t
	clone.trees = trees
	clone.treesByName = treesByName
	clone.logger = t.logger
	return &clone
}

func (t *MultiTree) Version() int64 {
	return t.lastCommitInfo.Version
}

func (t *MultiTree) SnapshotVersion() int64 {
	return t.metadata.CommitInfo.Version
}

func (t *MultiTree) LastCommitInfo() *proto.CommitInfo {
	return &t.lastCommitInfo
}

func (t *MultiTree) apply(entry proto.ChangelogEntry) error {
	if err := t.ApplyUpgrades(entry.Upgrades); err != nil {
		return err
	}
	return t.ApplyChangeSets(entry.Changesets)
}

// ApplyUpgrades store name upgrades
func (t *MultiTree) ApplyUpgrades(upgrades []*proto.TreeNameUpgrade) error {
	if len(upgrades) == 0 {
		return nil
	}

	t.treesByName = nil // rebuild in the end

	for _, upgrade := range upgrades {
		switch {
		case upgrade.Delete:
			i := slices.IndexFunc(t.trees, func(entry NamedTree) bool {
				return entry.Name == upgrade.Name
			})
			if i < 0 {
				return fmt.Errorf("unknown tree name %s", upgrade.Name)
			}
			// swap deletion
			t.trees[i], t.trees[len(t.trees)-1] = t.trees[len(t.trees)-1], t.trees[i]
			t.trees = t.trees[:len(t.trees)-1]
		case upgrade.RenameFrom != "":
			// rename tree
			i := slices.IndexFunc(t.trees, func(entry NamedTree) bool {
				return entry.Name == upgrade.RenameFrom
			})
			if i < 0 {
				return fmt.Errorf("unknown tree name %s", upgrade.RenameFrom)
			}
			t.trees[i].Name = upgrade.Name
		default:
			// add tree
			v := utils.NextVersion(t.Version(), t.initialVersion.Load())
			if v < 0 || v > math.MaxUint32 {
				return fmt.Errorf("version overflows uint32: %d", v)
			}
			version := uint32(v)
			tree := NewWithInitialVersion(version)
			t.trees = append(t.trees, NamedTree{Tree: tree, Name: upgrade.Name})
		}
	}

	sort.SliceStable(t.trees, func(i, j int) bool {
		return t.trees[i].Name < t.trees[j].Name
	})
	t.treesByName = make(map[string]int, len(t.trees))
	for i, tree := range t.trees {
		if _, ok := t.treesByName[tree.Name]; ok {
			return fmt.Errorf("memiavl tree name conflicts: %s", tree.Name)
		}
		t.treesByName[tree.Name] = i
	}

	return nil
}

// ApplyChangeSet applies change set for a single tree.
func (t *MultiTree) ApplyChangeSet(name string, changeSet iavl.ChangeSet) error {
	i, found := t.treesByName[name]
	if !found {
		return fmt.Errorf("unknown tree name %s", name)
	}
	otelMetrics.NumOfKVPairs.Add(context.Background(), int64(len(changeSet.Pairs)))
	t.trees[i].ApplyChangeSet(changeSet)
	return nil
}

// ApplyChangeSets applies change sets for multiple trees.
func (t *MultiTree) ApplyChangeSets(changeSets []*proto.NamedChangeSet) error {
	for _, cs := range changeSets {
		if err := t.ApplyChangeSet(cs.Name, cs.Changeset); err != nil {
			return err
		}
	}
	return nil
}

// WorkingCommitInfo returns the commit info for the working tree
func (t *MultiTree) WorkingCommitInfo() *proto.CommitInfo {
	version := utils.NextVersion(t.lastCommitInfo.Version, t.initialVersion.Load())
	return t.buildCommitInfo(version)
}

// SaveVersion bumps the versions of all the stores and optionally returns the new app hash
func (t *MultiTree) SaveVersion(updateCommitInfo bool) (int64, error) {
	t.lastCommitInfo.Version = utils.NextVersion(t.lastCommitInfo.Version, t.initialVersion.Load())
	for _, entry := range t.trees {
		if _, _, err := entry.SaveVersion(updateCommitInfo); err != nil {
			return 0, err
		}
	}

	if updateCommitInfo {
		t.UpdateCommitInfo()
	} else {
		// clear the dirty informaton
		t.lastCommitInfo.StoreInfos = []proto.StoreInfo{}
	}

	return t.lastCommitInfo.Version, nil
}

func (t *MultiTree) buildCommitInfo(version int64) *proto.CommitInfo {
	var infos = make([]proto.StoreInfo, 0, len(t.trees))
	for _, entry := range t.trees {
		infos = append(infos, proto.StoreInfo{
			Name: entry.Name,
			CommitId: proto.CommitID{
				Version: entry.Version(),
				Hash:    entry.RootHash(),
			},
		})
	}

	return &proto.CommitInfo{
		Version:    version,
		StoreInfos: infos,
	}
}

// UpdateCommitInfo update lastCommitInfo based on current status of trees.
// it's needed if `updateCommitInfo` is set to `false` in `ApplyChangeSet`.
func (t *MultiTree) UpdateCommitInfo() {
	t.lastCommitInfo = *t.buildCommitInfo(t.lastCommitInfo.Version)
}

// Catchup replays WAL entries to catch up the tree to the target or latest version.
// delta is the difference between version and WAL index (version = walIndex + delta).
// endVersion specifies the target version (0 means catch up to latest).
func (t *MultiTree) Catchup(ctx context.Context, stream wal.ChangelogWAL, delta int64, endVersion int64) error {
	startTime := time.Now()

	// Get actual WAL index range
	firstIndex, err := stream.FirstOffset()
	if err != nil {
		return fmt.Errorf("read rlog first index failed, %w", err)
	}
	lastIndex, err := stream.LastOffset()
	if err != nil {
		return fmt.Errorf("read rlog last index failed, %w", err)
	}

	// Empty WAL - nothing to replay
	if lastIndex == 0 || firstIndex > lastIndex {
		return nil
	}

	currentVersion := t.Version()

	// Calculate start index: walIndex = version - delta
	// We want to start from currentVersion + 1
	startIndexSigned := currentVersion + 1 - delta

	// Ensure startIndex is within valid range (handle negative case before uint64 conversion)
	var startIndex uint64
	if startIndexSigned <= 0 || uint64(startIndexSigned) < firstIndex {
		startIndex = firstIndex
	} else {
		startIndex = uint64(startIndexSigned)
	}
	if startIndex > lastIndex {
		// Nothing to replay - tree is already caught up
		return nil
	}

	var replayCount = 0
	err = stream.Replay(startIndex, lastIndex, func(index uint64, entry proto.ChangelogEntry) error {
		// Check for cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Safety check: skip entries we already have (should not happen with correct startIndex)
		if entry.Version <= currentVersion {
			return nil
		}

		// If endVersion is specified, stop at that version
		if endVersion != 0 && entry.Version > endVersion {
			return nil
		}

		if err := t.ApplyUpgrades(entry.Upgrades); err != nil {
			return err
		}
		updatedTrees := make(map[string]bool)
		for _, cs := range entry.Changesets {
			treeName := cs.Name
			tree := t.TreeByName(treeName)
			if tree == nil {
				return fmt.Errorf("unknown tree name %s during WAL replay (missing initial stores / upgrades)", treeName)
			}
			tree.ApplyChangeSetAsync(cs.Changeset)
			updatedTrees[treeName] = true
		}
		for _, tree := range t.trees {
			if _, found := updatedTrees[tree.Name]; !found {
				tree.ApplyChangeSetAsync(iavl.ChangeSet{})
			}
		}
		t.lastCommitInfo.Version = entry.Version
		t.lastCommitInfo.StoreInfos = []proto.StoreInfo{}
		replayCount++
		if replayCount%1000 == 0 {
			t.logger.Info(fmt.Sprintf("Replayed %d changelog entries", replayCount))
		}
		return nil
	})

	for _, tree := range t.trees {
		tree.WaitToCompleteAsyncWrite()
	}

	if err != nil {
		return err
	}

	if replayCount > 0 {
		t.UpdateCommitInfo()
		replayElapsed := time.Since(startTime).Seconds()
		t.logger.Info(fmt.Sprintf("Total replayed %d entries in %.1fs (%.1f entries/sec).",
			replayCount, replayElapsed, float64(replayCount)/replayElapsed))
	}

	return nil
}

func (t *MultiTree) WriteSnapshot(ctx context.Context, dir string, wp *pond.WorkerPool) error {
	return t.WriteSnapshotWithRateLimit(ctx, dir, wp, 0)
}

// WriteSnapshotWithRateLimit writes snapshot with optional rate limiting.
// rateMBps is the rate limit in MB/s. 0 means unlimited.
// A single global limiter is shared across ALL trees and files to ensure
// the total write rate is capped at the configured value.
func (t *MultiTree) WriteSnapshotWithRateLimit(ctx context.Context, dir string, wp *pond.WorkerPool, rateMBps int) error {
	t.logger.Info("starting snapshot write", "trees", len(t.trees), "rate_limit_mbps", rateMBps)

	if err := os.MkdirAll(dir, os.ModePerm); err != nil { //nolint:gosec
		return err
	}

	// Create a single global limiter shared by all trees and files
	// This ensures total write rate is capped regardless of parallelism
	limiter := NewGlobalRateLimiter(rateMBps)
	if limiter != nil {
		t.logger.Info("global rate limiting enabled", "rate_mbps", rateMBps)
	}

	// Write EVM first to avoid disk I/O contention, then parallel
	return t.writeSnapshotPriorityEVM(ctx, dir, wp, limiter)
}

// writeSnapshotPriorityEVM writes EVM tree first, then others in parallel
// Best strategy: reduces disk I/O contention for the largest tree
// limiter is a shared rate limiter. nil means unlimited.
func (t *MultiTree) writeSnapshotPriorityEVM(ctx context.Context, dir string, wp *pond.WorkerPool, limiter *rate.Limiter) error {
	startTime := time.Now()

	// Phase 1: Write EVM tree first (if it exists)
	var evmTree *Tree
	var evmName string
	otherTrees := make([]NamedTree, 0, len(t.trees))

	for _, entry := range t.trees {
		if entry.Name == "evm" {
			evmTree = entry.Tree
			evmName = entry.Name
		} else {
			otherTrees = append(otherTrees, entry)
		}
	}

	if evmTree != nil {
		t.logger.Info("writing evm tree", "phase", "1/2")
		evmStart := time.Now()
		if err := evmTree.WriteSnapshotWithRateLimit(ctx, filepath.Join(dir, evmName), limiter); err != nil {
			return err
		}
		evmElapsed := time.Since(evmStart).Seconds()
		t.logger.Info("evm tree completed", "duration_sec", evmElapsed)
	}

	// Phase 2: Write all other trees in parallel
	if len(otherTrees) > 0 {
		t.logger.Info("writing remaining trees", "phase", "2/2", "count", len(otherTrees))
		phase2Start := time.Now()

		// NOTE: We use explicit WaitGroup instead of pond.GroupContext because
		// GroupContext.Wait() returns immediately on context cancellation without
		// waiting for workers to finish. This causes data races when DB.Close()
		// destroys mmap resources that background writers are still accessing.
		// The WaitGroup ensures all goroutines fully exit before we return.
		var wg sync.WaitGroup
		var mu sync.Mutex
		var errs []error

		for _, entry := range otherTrees {
			// Capture loop variables for goroutine closure to avoid data race
			entry := entry // Create new variable for closure capture
			wg.Add(1)
			wp.Submit(func() {
				defer wg.Done()
				if err := entry.WriteSnapshotWithRateLimit(ctx, filepath.Join(dir, entry.Name), limiter); err != nil {
					mu.Lock()
					errs = append(errs, fmt.Errorf("tree %s: %w", entry.Name, err))
					mu.Unlock()
				}
			})
		}

		wg.Wait()
		if len(errs) > 0 {
			return errors.Join(errs...)
		}

		phase2Elapsed := time.Since(phase2Start).Seconds()
		t.logger.Info("remaining trees completed", "duration_sec", phase2Elapsed, "count", len(otherTrees))
	}

	elapsed := time.Since(startTime).Seconds()
	t.logger.Info("all trees completed", "duration_sec", elapsed, "trees", len(t.trees))

	// write commit info
	metadata := proto.MultiTreeMetadata{
		CommitInfo:     &t.lastCommitInfo,
		InitialVersion: int64(t.initialVersion.Load()),
	}
	bz, err := metadata.Marshal()
	if err != nil {
		return err
	}
	return WriteFileSync(filepath.Join(dir, MetadataFileName), bz)
}

// WriteFileSync calls `f.Sync` after before closing the file
func WriteFileSync(name string, data []byte) error {
	f, err := os.OpenFile(filepath.Clean(name), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, os.ModePerm) //nolint:gosec
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	if err1 := f.Close(); err1 != nil && err == nil {
		err = err1
	}
	return err
}

func (t *MultiTree) Close() error {
	errs := make([]error, 0, len(t.trees))
	for _, entry := range t.trees {
		errs = append(errs, entry.Close())
	}
	t.trees = nil
	t.treesByName = nil
	t.lastCommitInfo = proto.CommitInfo{}
	return errors.Join(errs...)
}

func (t *MultiTree) ReplaceWith(other *MultiTree) error {
	errs := make([]error, 0, len(t.trees))
	for _, entry := range t.trees {
		errs = append(errs, entry.ReplaceWith(other.TreeByName(entry.Name)))
	}
	t.treesByName = other.treesByName
	t.lastCommitInfo = other.lastCommitInfo
	t.metadata = other.metadata
	return errors.Join(errs...)
}

func readMetadata(dir string) (*proto.MultiTreeMetadata, error) {
	// load commit info
	bz, err := os.ReadFile(filepath.Join(filepath.Clean(dir), MetadataFileName))
	if err != nil {
		return nil, err
	}
	var metadata proto.MultiTreeMetadata
	if err := metadata.Unmarshal(bz); err != nil {
		return nil, err
	}
	if metadata.CommitInfo.Version > math.MaxUint32 {
		return nil, fmt.Errorf("commit info version overflows uint32: %d", metadata.CommitInfo.Version)
	}
	if metadata.InitialVersion > math.MaxUint32 {
		return nil, fmt.Errorf("initial version overflows uint32: %d", metadata.InitialVersion)
	}

	return &metadata, nil
}
