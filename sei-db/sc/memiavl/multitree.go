package memiavl

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/alitto/pond"
	"golang.org/x/exp/slices"

	"github.com/cosmos/iavl"
	"github.com/sei-protocol/sei-db/common/errors"
	"github.com/sei-protocol/sei-db/common/logger"
	"github.com/sei-protocol/sei-db/common/metrics"
	"github.com/sei-protocol/sei-db/common/utils"
	"github.com/sei-protocol/sei-db/proto"
	"github.com/sei-protocol/sei-db/stream/types"
)

const MetadataFileName = "__metadata"

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
	initialVersion uint32

	zeroCopy bool
	logger   logger.Logger

	trees          []NamedTree    // always ordered by tree name
	treesByName    map[string]int // index of the trees by name
	lastCommitInfo proto.CommitInfo

	// the initial metadata loaded from disk snapshot
	metadata proto.MultiTreeMetadata
}

func NewEmptyMultiTree(initialVersion uint32) *MultiTree {
	return &MultiTree{
		initialVersion: initialVersion,
		treesByName:    make(map[string]int),
		zeroCopy:       true,
		logger:         logger.NewNopLogger(),
	}
}

func LoadMultiTree(dir string, opts Options) (*MultiTree, error) {
	startTime := time.Now()
	log := opts.Logger
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
	log.Info(fmt.Sprintf("All %d memIAVL trees loaded in %.1fs\n", len(treeNames), time.Since(startTime).Seconds()))
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
	t.initialVersion = iv
	for _, entry := range t.trees {
		entry.initialVersion = t.initialVersion
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
			v := utils.NextVersion(t.Version(), t.initialVersion)
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
	metrics.SeiDBMetrics.NumOfKVPairs.Add(context.Background(), int64(len(changeSet.Pairs)))
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
	version := utils.NextVersion(t.lastCommitInfo.Version, t.initialVersion)
	return t.buildCommitInfo(version)
}

// SaveVersion bumps the versions of all the stores and optionally returns the new app hash
func (t *MultiTree) SaveVersion(updateCommitInfo bool) (int64, error) {
	t.lastCommitInfo.Version = utils.NextVersion(t.lastCommitInfo.Version, t.initialVersion)
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

// Catchup replay the new entries in the Rlog file on the tree to catch up to the target or latest version.
func (t *MultiTree) Catchup(stream types.Stream[proto.ChangelogEntry], endVersion int64) error {
	startTime := time.Now()
	lastIndex, err := stream.LastOffset()
	if err != nil {
		return fmt.Errorf("read rlog last index failed, %w", err)
	}

	firstIndex := utils.VersionToIndex(utils.NextVersion(t.Version(), t.initialVersion), t.initialVersion)
	if firstIndex > lastIndex {
		// already up-to-date
		return nil
	}

	endIndex := lastIndex
	if endVersion != 0 {
		endIndex = utils.VersionToIndex(endVersion, t.initialVersion)
	}

	if endIndex < firstIndex {
		return fmt.Errorf("target index %d is pruned", endIndex)
	}

	if endIndex > lastIndex {
		return fmt.Errorf("target index %d is in the future, latest index: %d", endIndex, lastIndex)
	}

	var replayCount = 0
	err = stream.Replay(firstIndex, endIndex, func(index uint64, entry proto.ChangelogEntry) error {
		if err := t.ApplyUpgrades(entry.Upgrades); err != nil {
			return err
		}
		updatedTrees := make(map[string]bool)
		for _, cs := range entry.Changesets {
			treeName := cs.Name
			t.TreeByName(treeName).ApplyChangeSetAsync(cs.Changeset)
			updatedTrees[treeName] = true
		}
		for _, tree := range t.trees {
			if _, found := updatedTrees[tree.Name]; !found {
				tree.ApplyChangeSetAsync(iavl.ChangeSet{})
			}
		}
		t.lastCommitInfo.Version = utils.NextVersion(t.lastCommitInfo.Version, t.initialVersion)
		t.lastCommitInfo.StoreInfos = []proto.StoreInfo{}
		replayCount++
		if replayCount%1000 == 0 {
			t.logger.Info(fmt.Sprintf("Replayed %d changelog entries\n", replayCount))
		}
		return nil
	})

	for _, tree := range t.trees {
		tree.WaitToCompleteAsyncWrite()
	}

	if err != nil {
		return err
	}
	t.UpdateCommitInfo()

	replayElapsed := time.Since(startTime).Seconds()
	t.logger.Info(fmt.Sprintf("Total replayed %d entries in %.1fs (%.1f entries/sec).\n",
		replayCount, replayElapsed, float64(replayCount)/replayElapsed))
	return nil
}

func (t *MultiTree) WriteSnapshot(ctx context.Context, dir string, wp *pond.WorkerPool) error {
	if err := os.MkdirAll(dir, os.ModePerm); err != nil { //nolint:gosec
		return err
	}

	// write the snapshots in parallel and wait all jobs done
	group, _ := wp.GroupContext(ctx)

	for _, entry := range t.trees {
		tree, name := entry.Tree, entry.Name
		group.Submit(func() error {
			return tree.WriteSnapshot(ctx, filepath.Join(dir, name))
		})
	}

	if err := group.Wait(); err != nil {
		return err
	}

	// write commit info
	metadata := proto.MultiTreeMetadata{
		CommitInfo:     &t.lastCommitInfo,
		InitialVersion: int64(t.initialVersion),
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
