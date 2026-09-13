package memiavl

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/stretchr/testify/require"
)

// Copy hands a tree to readers that run concurrently with the live tree: the
// background snapshot rewrite, which serializes every reachable node, and the
// DB.Copy consumers that read a copy while consensus keeps committing. For that
// to be safe the copy must be frozen: every MemNode it can reach is already
// hashed, and sits at or below cowVersion so a live write clones it instead of
// mutating it in place. The tests below pin both halves of that invariant.

const copyTestSeedKeys = 3000

// seedTree returns a tree holding keys 0..keys-1, saved and fully hashed.
func seedTree(t *testing.T, keys int) *Tree {
	t.Helper()
	tree := NewEmptyTree(0, 0)
	tree.ApplyChangeSet(changeSet(0, keys, "seed"))
	_, _, err := tree.SaveVersion(true)
	require.NoError(t, err)
	return tree
}

// changeSet writes count values over keys offset..offset+count-1, tagged so
// repeated rounds over the same keys produce distinct values.
func changeSet(offset, count int, tag string) proto.ChangeSet {
	cs := proto.ChangeSet{Pairs: make([]*proto.KVPair, 0, count)}
	for i := offset; i < offset+count; i++ {
		cs.Pairs = append(cs.Pairs, &proto.KVPair{
			Key:   []byte(fmt.Sprintf("key%08d", i)),
			Value: []byte(fmt.Sprintf("%s%08d", tag, i)),
		})
	}
	return cs
}

// frozenViolations counts the MemNodes reachable from tree's root that break the
// freeze invariant: unhashed, so a reader fills MemNode.hash in place on a node
// the live tree also holds; or above cowVersion, so a live write mutates the node
// rather than cloning it. Either lets a live commit change a node out from under
// the snapshot writer mid-traversal.
func frozenViolations(tree *Tree) (unhashed, mutable int) {
	var walk func(node Node)
	walk = func(node Node) {
		// PersistedNode is backed by a read-only mmap and is never mutated.
		mem, ok := node.(*MemNode)
		if !ok {
			return
		}
		if mem.hash == nil {
			unhashed++
		}
		if mem.version > tree.cowVersion {
			mutable++
		}
		if !mem.IsLeaf() {
			walk(mem.left)
			walk(mem.right)
		}
	}
	if tree.root != nil {
		walk(tree.root)
	}
	return unhashed, mutable
}

// TestCopyFreezesNodesWrittenThisVersion pins the copy-on-write floor. Set and
// Remove stamp new nodes at version+1, so between a changeset and SaveVersion the
// tree holds nodes above t.version. Deriving the floor from t.version alone
// leaves exactly those nodes mutable in the copy.
func TestCopyFreezesNodesWrittenThisVersion(t *testing.T) {
	tree := seedTree(t, copyTestSeedKeys)

	// Mid-version: the changeset is applied but not yet saved.
	tree.ApplyChangeSet(changeSet(0, 500, "mid"))

	frozen := tree.Copy()

	unhashed, mutable := frozenViolations(frozen)
	require.Zerof(t, mutable,
		"copy shares %d MemNode(s) above cowVersion %d; a live write will mutate them in place", mutable, frozen.cowVersion)
	require.Zerof(t, unhashed,
		"copy shares %d unhashed MemNode(s); a reader will fill MemNode.hash in place on nodes the live tree also holds", unhashed)
}

// TestCopyFreezesAfterSaveVersion is the same invariant at the other point a copy
// is taken, immediately after a commit. This one holds even without the fix, so
// it guards against a fix that only moves the window rather than closing it.
func TestCopyFreezesAfterSaveVersion(t *testing.T) {
	tree := seedTree(t, copyTestSeedKeys)
	tree.ApplyChangeSet(changeSet(0, 500, "committed"))
	_, _, err := tree.SaveVersion(true)
	require.NoError(t, err)

	frozen := tree.Copy()

	unhashed, mutable := frozenViolations(frozen)
	require.Zero(t, mutable, "copy shares %d MemNode(s) above cowVersion %d", mutable, frozen.cowVersion)
	require.Zero(t, unhashed, "copy shares %d unhashed MemNode(s)", unhashed)
}

// TestCopyIsStableWhileLiveTreeAdvances is the behavioural form of the invariant:
// the tree handed to the snapshot writer must serialize identically no matter how
// far the live tree advances underneath it. A copy taken mid-version fails this
// when the live commits mutate its nodes in place.
func TestCopyIsStableWhileLiveTreeAdvances(t *testing.T) {
	tree := seedTree(t, copyTestSeedKeys)
	tree.ApplyChangeSet(changeSet(0, 500, "mid"))

	frozen := tree.Copy()
	want := frozen.RootHash()
	wantVersion := frozen.Version()

	// Finish the in-flight version, then keep committing.
	_, _, err := tree.SaveVersion(true)
	require.NoError(t, err)
	for round := 0; round < 20; round++ {
		tree.ApplyChangeSet(changeSet(round*100, 500, fmt.Sprintf("adv%02d", round)))
		_, _, err := tree.SaveVersion(true)
		require.NoError(t, err)
	}

	require.Equal(t, wantVersion, frozen.Version(), "copy's version moved with the live tree")
	require.Equal(t, want, frozen.RootHash(), "live-tree commits mutated nodes the copy still references")
}

// TestSnapshotFromCopyIgnoresLaterCommits carries the freeze invariant through
// the snapshot writer to what lands on disk: serializing a copy must reproduce
// the tree as it stood when copied, however far the live tree has moved on.
//
// This is the deterministic form of the corruption. The concurrent version needs
// -race to be caught reliably, because at test scale the writer finishes before
// the live tree does much damage; here the commits land before the writer runs
// at all, so an unfrozen copy drifts every time.
func TestSnapshotFromCopyIgnoresLaterCommits(t *testing.T) {
	tree := seedTree(t, copyTestSeedKeys)
	tree.ApplyChangeSet(changeSet(0, 500, "mid"))

	frozen := tree.Copy()
	want := frozen.RootHash()

	_, _, err := tree.SaveVersion(true)
	require.NoError(t, err)
	for round := 0; round < 20; round++ {
		tree.ApplyChangeSet(changeSet(round*100, 500, fmt.Sprintf("adv%02d", round)))
		_, _, err := tree.SaveVersion(true)
		require.NoError(t, err)
	}

	snapshotDir := filepath.Join(t.TempDir(), "snapshot")
	require.NoError(t, frozen.WriteSnapshot(context.Background(), snapshotDir))

	snapshot, err := OpenSnapshot(snapshotDir, Options{})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, snapshot.Close()) })

	require.Equal(t, want, snapshot.RootHash(),
		"snapshot of the copy picked up commits made after Copy returned")
}

// TestCopyDoesNotInheritBackgroundWriteChannel covers the other way a copy
// reaches back into the live tree. Close closes pendingChanges, so a copy that
// inherited the channel closes the live tree's background writer: the next
// ApplyChangeSetAsync sends on a closed channel and panics, and
// WaitToCompleteAsyncWrite closes it a second time.
func TestCopyDoesNotInheritBackgroundWriteChannel(t *testing.T) {
	tree := seedTree(t, 100)
	tree.StartBackgroundWrite()
	defer tree.WaitToCompleteAsyncWrite()

	frozen := tree.Copy()

	require.Nil(t, frozen.pendingChanges, "copy shares the live tree's background-write channel")
	require.NotSame(t, tree.pendingWg, frozen.pendingWg, "copy shares the live tree's background-write WaitGroup")
}
