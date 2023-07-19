package memiavl

import (
	"context"
	stderrors "errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"

	"cosmossdk.io/errors"

	storetypes "github.com/cosmos/cosmos-sdk/store/types"
	"github.com/tidwall/wal"
	"golang.org/x/exp/slices"
	"golang.org/x/sync/errgroup"
)

const MetadataFileName = "__metadata"

type namedTree struct {
	tree *Tree
	name string
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
	// it always corresponds to the wal entry with index 1.
	initialVersion uint32

	zeroCopy  bool
	cacheSize int

	trees          []namedTree
	treesByName    map[string]int // reversed index of the trees
	lastCommitInfo storetypes.CommitInfo

	// the initial metadata loaded from disk snapshot
	metadata MultiTreeMetadata
}

func NewEmptyMultiTree(initialVersion uint32, cacheSize int) *MultiTree {
	return &MultiTree{
		initialVersion: initialVersion,
		treesByName:    make(map[string]int),
		zeroCopy:       true,
		cacheSize:      cacheSize,
	}
}

func LoadMultiTree(dir string, zeroCopy bool, cacheSize int) (*MultiTree, error) {
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
		snapshot, err := OpenSnapshot(filepath.Join(dir, name))
		if err != nil {
			return nil, err
		}
		treeMap[name] = NewFromSnapshot(snapshot, zeroCopy, cacheSize)
	}

	sort.Strings(treeNames)

	trees := make([]namedTree, len(treeNames))
	treesByName := make(map[string]int, len(trees))
	for i, name := range treeNames {
		tree := treeMap[name]
		trees[i] = namedTree{tree: tree, name: name}
		treesByName[name] = i
	}

	mtree := &MultiTree{
		trees:          trees,
		treesByName:    treesByName,
		lastCommitInfo: *metadata.CommitInfo,
		metadata:       *metadata,
		zeroCopy:       zeroCopy,
		cacheSize:      cacheSize,
	}
	// initial version is nesserary for wal index conversion
	mtree.setInitialVersion(metadata.InitialVersion)
	return mtree, nil
}

// TreeByName returns the tree by name, returns nil if not found
func (t *MultiTree) TreeByName(name string) *Tree {
	if i, ok := t.treesByName[name]; ok {
		return t.trees[i].tree
	}
	return nil
}

func (t *MultiTree) SetInitialVersion(initialVersion int64) error {
	if initialVersion >= math.MaxUint32 {
		return fmt.Errorf("version overflows uint32: %d", initialVersion)
	}

	if t.Version() != 0 {
		return fmt.Errorf("multi tree is not empty: %d", t.Version())
	}

	for _, entry := range t.trees {
		if !entry.tree.IsEmpty() {
			return fmt.Errorf("tree is not empty: %s", entry.name)
		}
	}

	t.setInitialVersion(initialVersion)
	return nil
}

func (t *MultiTree) setInitialVersion(initialVersion int64) {
	t.initialVersion = uint32(initialVersion)
	for _, entry := range t.trees {
		entry.tree.initialVersion = t.initialVersion
	}
}

func (t *MultiTree) SetZeroCopy(zeroCopy bool) {
	t.zeroCopy = zeroCopy
	for _, entry := range t.trees {
		entry.tree.SetZeroCopy(zeroCopy)
	}
}

// Copy returns a snapshot of the tree which won't be corrupted by further modifications on the main tree.
func (t *MultiTree) Copy(cacheSize int) *MultiTree {
	trees := make([]namedTree, len(t.trees))
	treesByName := make(map[string]int, len(t.trees))
	for i, entry := range t.trees {
		tree := entry.tree.Copy(cacheSize)
		trees[i] = namedTree{tree: tree, name: entry.name}
		treesByName[entry.name] = i
	}

	clone := *t
	clone.trees = trees
	clone.treesByName = treesByName
	return &clone
}

func (t *MultiTree) Hash() []byte {
	return t.lastCommitInfo.Hash()
}

func (t *MultiTree) Version() int64 {
	return t.lastCommitInfo.Version
}

func (t *MultiTree) SnapshotVersion() int64 {
	return t.metadata.CommitInfo.Version
}

func (t *MultiTree) LastCommitInfo() *storetypes.CommitInfo {
	return &t.lastCommitInfo
}

// ApplyUpgrades store name upgrades
func (t *MultiTree) ApplyUpgrades(upgrades []*TreeNameUpgrade) error {
	if len(upgrades) == 0 {
		return nil
	}

	t.treesByName = nil // rebuild in the end

	for _, upgrade := range upgrades {
		switch {
		case upgrade.Delete:
			i := slices.IndexFunc(t.trees, func(entry namedTree) bool {
				return entry.name == upgrade.Name
			})
			if i < 0 {
				return fmt.Errorf("unknown tree name %s", upgrade.Name)
			}
			// swap deletion
			t.trees[i], t.trees[len(t.trees)-1] = t.trees[len(t.trees)-1], t.trees[i]
			t.trees = t.trees[:len(t.trees)-1]
		case upgrade.RenameFrom != "":
			// rename tree
			i := slices.IndexFunc(t.trees, func(entry namedTree) bool {
				return entry.name == upgrade.RenameFrom
			})
			if i < 0 {
				return fmt.Errorf("unknown tree name %s", upgrade.RenameFrom)
			}
			t.trees[i].name = upgrade.Name
		default:
			// add tree
			tree := NewWithInitialVersion(uint32(nextVersion(t.Version(), t.initialVersion)), t.cacheSize)
			t.trees = append(t.trees, namedTree{tree: tree, name: upgrade.Name})
		}
	}

	sort.SliceStable(t.trees, func(i, j int) bool {
		return t.trees[i].name < t.trees[j].name
	})
	t.treesByName = make(map[string]int, len(t.trees))
	for i, tree := range t.trees {
		if _, ok := t.treesByName[tree.name]; ok {
			return fmt.Errorf("memiavl tree name conflicts: %s", tree.name)
		}
		t.treesByName[tree.name] = i
	}

	return nil
}

// ApplyChangeSet applies change sets for all trees.
// if `updateCommitInfo` is `false`, the `lastCommitInfo.StoreInfos` is dirty.
func (t *MultiTree) ApplyChangeSet(changeSets []*NamedChangeSet, updateCommitInfo bool) ([]byte, int64, error) {
	version := nextVersion(t.lastCommitInfo.Version, t.initialVersion)

	for _, cs := range changeSets {
		tree := t.trees[t.treesByName[cs.Name]].tree

		_, v, err := tree.ApplyChangeSet(cs.Changeset, updateCommitInfo)
		if err != nil {
			return nil, 0, err
		}
		if v != version {
			return nil, 0, fmt.Errorf("multi tree version don't match(%d != %d)", v, version)
		}
	}

	t.lastCommitInfo.Version = version

	var hash []byte
	if updateCommitInfo {
		hash = t.UpdateCommitInfo()
	} else {
		t.lastCommitInfo.StoreInfos = []storetypes.StoreInfo{}
	}

	return hash, version, nil
}

// UpdateCommitInfo update lastCommitInfo based on current status of trees.
// it's needed if `updateCommitInfo` is set to `false` in `ApplyChangeSet`.
func (t *MultiTree) UpdateCommitInfo() []byte {
	var infos []storetypes.StoreInfo
	for _, entry := range t.trees {
		infos = append(infos, storetypes.StoreInfo{
			Name: entry.name,
			CommitId: storetypes.CommitID{
				Version: entry.tree.Version(),
				Hash:    entry.tree.RootHash(),
			},
		})
	}

	t.lastCommitInfo.StoreInfos = infos
	return t.lastCommitInfo.Hash()
}

// CatchupWAL replay the new entries in the WAL on the tree to catch-up to the target or latest version.
func (t *MultiTree) CatchupWAL(wal *wal.Log, endVersion int64) error {
	lastIndex, err := wal.LastIndex()
	if err != nil {
		return errors.Wrap(err, "read wal last index failed")
	}

	firstIndex := walIndex(nextVersion(t.Version(), t.initialVersion), t.initialVersion)
	if firstIndex > lastIndex {
		// already up-to-date
		return nil
	}

	endIndex := lastIndex
	if endVersion != 0 {
		endIndex = walIndex(endVersion, t.initialVersion)
	}

	if endIndex < firstIndex {
		return fmt.Errorf("target index %d is pruned", endIndex)
	}

	if endIndex > lastIndex {
		return fmt.Errorf("target index %d is in the future, latest index: %d", endIndex, lastIndex)
	}

	for i := firstIndex; i <= endIndex; i++ {
		bz, err := wal.Read(i)
		if err != nil {
			return errors.Wrap(err, "read wal log failed")
		}
		var entry WALEntry
		if err := entry.Unmarshal(bz); err != nil {
			return errors.Wrap(err, "unmarshal wal log failed")
		}
		if err := t.ApplyUpgrades(entry.Upgrades); err != nil {
			return errors.Wrap(err, "replay store upgrades failed")
		}
		if _, _, err := t.ApplyChangeSet(entry.Changesets, false); err != nil {
			return errors.Wrap(err, "replay change set failed")
		}
	}
	t.UpdateCommitInfo()
	return nil
}

func (t *MultiTree) WriteSnapshot(dir string) error {
	if err := os.MkdirAll(dir, os.ModePerm); err != nil {
		return err
	}

	// write the snapshots in parallel
	g, _ := errgroup.WithContext(context.Background())
	for _, entry := range t.trees {
		tree, name := entry.tree, entry.name // https://github.com/golang/go/wiki/CommonMistakes#using-goroutines-on-loop-iterator-variables
		g.Go(func() error {
			return tree.WriteSnapshot(filepath.Join(dir, name))
		})
	}
	if err := g.Wait(); err != nil {
		return err
	}

	// write commit info
	metadata := MultiTreeMetadata{
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
	f, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, os.ModePerm)
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
		errs = append(errs, entry.tree.Close())
	}
	t.trees = nil
	t.treesByName = nil
	t.lastCommitInfo = storetypes.CommitInfo{}
	return stderrors.Join(errs...)
}

func nextVersion(v int64, initialVersion uint32) int64 {
	if v == 0 && initialVersion > 1 {
		return int64(initialVersion)
	}
	return v + 1
}

// walIndex converts version to wal index based on initial version
func walIndex(v int64, initialVersion uint32) uint64 {
	if initialVersion > 1 {
		return uint64(v) - uint64(initialVersion) + 1
	}
	return uint64(v)
}

// walVersion converts wal index to version, reverse of walIndex
func walVersion(index uint64, initialVersion uint32) int64 {
	if initialVersion > 1 {
		return int64(index) + int64(initialVersion) - 1
	}
	return int64(index)
}

func readMetadata(dir string) (*MultiTreeMetadata, error) {
	// load commit info
	bz, err := os.ReadFile(filepath.Join(dir, MetadataFileName))
	if err != nil {
		return nil, err
	}
	var metadata MultiTreeMetadata
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
