package memiavl

import (
	"fmt"
	"strconv"
	"testing"

	"github.com/cosmos/iavl"
	"github.com/stretchr/testify/require"
	db "github.com/tendermint/tm-db"
)

var (
	ChangeSets  []iavl.ChangeSet
	RefHashes   [][]byte
	ExpectItems [][]pair
)

func mockKVPairs(kvPairs ...string) []*iavl.KVPair {
	result := make([]*iavl.KVPair, len(kvPairs)/2)
	for i := 0; i < len(kvPairs); i += 2 {
		result[i/2] = &iavl.KVPair{
			Key:   []byte(kvPairs[i]),
			Value: []byte(kvPairs[i+1]),
		}
	}
	return result
}

func init() {
	ChangeSets = []iavl.ChangeSet{
		{Pairs: mockKVPairs("hello", "world")},
		{Pairs: mockKVPairs("hello", "world1", "hello1", "world1")},
		{Pairs: mockKVPairs("hello2", "world1", "hello3", "world1")},
	}

	changes := iavl.ChangeSet{}
	for i := 0; i < 1; i++ {
		changes.Pairs = append(changes.Pairs, &iavl.KVPair{Key: []byte(fmt.Sprintf("hello%02d", i)), Value: []byte("world1")})
	}

	ChangeSets = append(ChangeSets, changes)
	ChangeSets = append(ChangeSets, iavl.ChangeSet{Pairs: []*iavl.KVPair{{Key: []byte("hello"), Delete: true}, {Key: []byte("hello19"), Delete: true}}})

	changes = iavl.ChangeSet{}
	for i := 0; i < 21; i++ {
		changes.Pairs = append(changes.Pairs, &iavl.KVPair{Key: []byte(fmt.Sprintf("aello%02d", i)), Value: []byte("world1")})
	}
	ChangeSets = append(ChangeSets, changes)

	changes = iavl.ChangeSet{}
	for i := 0; i < 21; i++ {
		changes.Pairs = append(changes.Pairs, &iavl.KVPair{Key: []byte(fmt.Sprintf("aello%02d", i)), Delete: true})
	}
	for i := 0; i < 19; i++ {
		changes.Pairs = append(changes.Pairs, &iavl.KVPair{Key: []byte(fmt.Sprintf("hello%02d", i)), Delete: true})
	}
	ChangeSets = append(ChangeSets, changes)

	// generate ref hashes with ref impl
	d := db.NewMemDB()
	refTree, err := iavl.NewMutableTree(d, 0, true)
	if err != nil {
		panic(err)
	}
	for _, changes := range ChangeSets {
		if err := applyChangeSetRef(refTree, changes); err != nil {
			panic(err)
		}
		refHash, _, err := refTree.SaveVersion()
		if err != nil {
			panic(err)
		}
		RefHashes = append(RefHashes, refHash)
	}

	ExpectItems = [][]pair{
		{},
		{{[]byte("hello"), []byte("world")}},
		{
			{[]byte("hello"), []byte("world1")},
			{[]byte("hello1"), []byte("world1")},
		},
		{
			{[]byte("hello"), []byte("world1")},
			{[]byte("hello1"), []byte("world1")},
			{[]byte("hello2"), []byte("world1")},
			{[]byte("hello3"), []byte("world1")},
		},
		{
			{[]byte("hello"), []byte("world1")},
			{[]byte("hello00"), []byte("world1")},
			{[]byte("hello1"), []byte("world1")},
			{[]byte("hello2"), []byte("world1")},
			{[]byte("hello3"), []byte("world1")},
		},
		{
			{[]byte("hello00"), []byte("world1")},
			{[]byte("hello1"), []byte("world1")},
			{[]byte("hello2"), []byte("world1")},
			{[]byte("hello3"), []byte("world1")},
		},
		{
			{[]byte("aello00"), []byte("world1")},
			{[]byte("aello01"), []byte("world1")},
			{[]byte("aello02"), []byte("world1")},
			{[]byte("aello03"), []byte("world1")},
			{[]byte("aello04"), []byte("world1")},
			{[]byte("aello05"), []byte("world1")},
			{[]byte("aello06"), []byte("world1")},
			{[]byte("aello07"), []byte("world1")},
			{[]byte("aello08"), []byte("world1")},
			{[]byte("aello09"), []byte("world1")},
			{[]byte("aello10"), []byte("world1")},
			{[]byte("aello11"), []byte("world1")},
			{[]byte("aello12"), []byte("world1")},
			{[]byte("aello13"), []byte("world1")},
			{[]byte("aello14"), []byte("world1")},
			{[]byte("aello15"), []byte("world1")},
			{[]byte("aello16"), []byte("world1")},
			{[]byte("aello17"), []byte("world1")},
			{[]byte("aello18"), []byte("world1")},
			{[]byte("aello19"), []byte("world1")},
			{[]byte("aello20"), []byte("world1")},
			{[]byte("hello00"), []byte("world1")},
			{[]byte("hello1"), []byte("world1")},
			{[]byte("hello2"), []byte("world1")},
			{[]byte("hello3"), []byte("world1")},
		},
		{
			{[]byte("hello1"), []byte("world1")},
			{[]byte("hello2"), []byte("world1")},
			{[]byte("hello3"), []byte("world1")},
		},
	}
}

func applyChangeSetRef(t *iavl.MutableTree, changes iavl.ChangeSet) error {
	for _, change := range changes.Pairs {
		if change.Delete {
			if _, _, err := t.Remove(change.Key); err != nil {
				return err
			}
		} else {
			if _, err := t.Set(change.Key, change.Value); err != nil {
				return err
			}
		}
	}
	return nil
}

func TestRootHashes(t *testing.T) {
	tree := New(0)

	for i, changes := range ChangeSets {
		hash, v, err := tree.ApplyChangeSet(changes, true)
		require.NoError(t, err)
		require.Equal(t, i+1, int(v))
		require.Equal(t, RefHashes[i], hash)
	}
}

func TestNewKey(t *testing.T) {
	tree := New(0)

	for i := 0; i < 4; i++ {
		tree.set([]byte(fmt.Sprintf("key-%d", i)), []byte{1})
	}
	_, _, err := tree.saveVersion(true)
	require.NoError(t, err)

	// the smallest key in the right half of the tree
	require.Equal(t, tree.root.Key(), []byte("key-2"))

	// remove this key
	tree.remove([]byte("key-2"))

	// check root node's key is changed
	require.Equal(t, []byte("key-3"), tree.root.Key())
}

func TestEmptyTree(t *testing.T) {
	tree := New(0)
	require.Equal(t, emptyHash, tree.RootHash())
}

func TestTreeCopy(t *testing.T) {
	tree := New(0)

	_, _, err := tree.ApplyChangeSet(iavl.ChangeSet{Pairs: []*iavl.KVPair{
		{Key: []byte("hello"), Value: []byte("world")},
	}}, true)
	require.NoError(t, err)

	snapshot := tree.Copy(0)

	_, _, err = tree.ApplyChangeSet(iavl.ChangeSet{Pairs: []*iavl.KVPair{
		{Key: []byte("hello"), Value: []byte("world1")},
	}}, true)
	require.NoError(t, err)

	require.Equal(t, []byte("world1"), tree.Get([]byte("hello")))
	require.Equal(t, []byte("world"), snapshot.Get([]byte("hello")))

	// check that normal copy don't work
	fakeSnapshot := *tree

	_, _, err = tree.ApplyChangeSet(iavl.ChangeSet{Pairs: []*iavl.KVPair{
		{Key: []byte("hello"), Value: []byte("world2")},
	}}, true)
	require.NoError(t, err)

	// get modified in-place
	require.Equal(t, []byte("world2"), tree.Get([]byte("hello")))
	require.Equal(t, []byte("world2"), fakeSnapshot.Get([]byte("hello")))
}

func TestChangeSetMarshal(t *testing.T) {
	for _, changes := range ChangeSets {
		bz, err := changes.Marshal()
		require.NoError(t, err)

		var cs iavl.ChangeSet
		require.NoError(t, cs.Unmarshal(bz))
		require.Equal(t, changes, cs)
	}
}

func TestGetByIndex(t *testing.T) {
	changes := iavl.ChangeSet{}
	for i := 0; i < 20; i++ {
		changes.Pairs = append(changes.Pairs, &iavl.KVPair{Key: []byte(fmt.Sprintf("hello%02d", i)), Value: []byte(strconv.Itoa(i))})
	}

	tree := New(0)
	_, _, err := tree.ApplyChangeSet(changes, true)
	require.NoError(t, err)

	for i, pair := range changes.Pairs {
		idx, v := tree.GetWithIndex(pair.Key)
		require.Equal(t, pair.Value, v)
		require.Equal(t, int64(i), idx)

		k, v := tree.GetByIndex(idx)
		require.Equal(t, pair.Key, k)
		require.Equal(t, pair.Value, v)
	}

	// test persisted tree
	dir := t.TempDir()
	require.NoError(t, tree.WriteSnapshot(dir))
	snapshot, err := OpenSnapshot(dir)
	require.NoError(t, err)
	ptree := NewFromSnapshot(snapshot, true, 0)
	defer ptree.Close()

	for i, pair := range changes.Pairs {
		idx, v := ptree.GetWithIndex(pair.Key)
		require.Equal(t, pair.Value, v)
		require.Equal(t, int64(i), idx)

		k, v := ptree.GetByIndex(idx)
		require.Equal(t, pair.Key, k)
		require.Equal(t, pair.Value, v)
	}
}
