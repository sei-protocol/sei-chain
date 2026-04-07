package memiavl

import (
	"context"
	"encoding/hex"
	"fmt"
	"strconv"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/stretchr/testify/require"
)

var (
	ChangeSets  []proto.ChangeSet
	RefHashes   [][]byte
	ExpectItems [][]pair
)

func mockKVPairs(kvPairs ...string) []*proto.KVPair {
	result := make([]*proto.KVPair, len(kvPairs)/2)
	for i := 0; i < len(kvPairs); i += 2 {
		result[i/2] = &proto.KVPair{
			Key:   []byte(kvPairs[i]),
			Value: []byte(kvPairs[i+1]),
		}
	}
	return result
}

func init() {
	ChangeSets = []proto.ChangeSet{
		{Pairs: mockKVPairs("hello", "world")},
		{Pairs: mockKVPairs("hello", "world1", "hello1", "world1")},
		{Pairs: mockKVPairs("hello2", "world1", "hello3", "world1")},
	}

	changes := proto.ChangeSet{}
	for i := 0; i < 1; i++ {
		changes.Pairs = append(changes.Pairs, &proto.KVPair{Key: []byte(fmt.Sprintf("hello%02d", i)), Value: []byte("world1")})
	}

	ChangeSets = append(ChangeSets, changes)
	ChangeSets = append(ChangeSets, proto.ChangeSet{Pairs: []*proto.KVPair{{Key: []byte("hello"), Delete: true}, {Key: []byte("hello19"), Delete: true}}})

	changes = proto.ChangeSet{}
	for i := 0; i < 21; i++ {
		changes.Pairs = append(changes.Pairs, &proto.KVPair{Key: []byte(fmt.Sprintf("aello%02d", i)), Value: []byte("world1")})
	}
	ChangeSets = append(ChangeSets, changes)

	changes = proto.ChangeSet{}
	for i := 0; i < 21; i++ {
		changes.Pairs = append(changes.Pairs, &proto.KVPair{Key: []byte(fmt.Sprintf("aello%02d", i)), Delete: true})
	}
	for i := 0; i < 19; i++ {
		changes.Pairs = append(changes.Pairs, &proto.KVPair{Key: []byte(fmt.Sprintf("hello%02d", i)), Delete: true})
	}
	ChangeSets = append(ChangeSets, changes)

	refHashHexes := []string{
		"6032661ab0d201132db7a8fa1da6a0afe427e6278bd122c301197680ab79ca02",
		"457d81f933f53e5cfb90d813b84981aa2604d69939e10c94304d18287ded31f7",
		"c7ab142752add0374992261536e502851ce555d243270d3c3c6b77cf31b7945d",
		"e54da9407cbca3570d04ad5c3296056a0726467cb06272ffd8ef1b4ae87fb99d",
		"8b04490800d6b54fa569715a754b5fafe24fd720f677cab819394cf7ccf8cdec",
		"38abd5268374923e6727b14ac5a9bb6611e591d7e316d0a612904062f244e72f",
		"d91cf6388eeff3204474bb07b853ab0d7d39163912ac1e610e92f9b178c76922",
	}
	for _, h := range refHashHexes {
		b, err := hex.DecodeString(h)
		if err != nil {
			panic(err)
		}
		RefHashes = append(RefHashes, b)
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

func TestRootHashes(t *testing.T) {
	tree := New(0)

	for i, changes := range ChangeSets {
		tree.ApplyChangeSet(changes)
		hash, v, err := tree.SaveVersion(true)
		require.NoError(t, err)
		require.Equal(t, i+1, int(v))
		require.Equal(t, RefHashes[i], hash)
	}
}

func TestNewKey(t *testing.T) {
	tree := New(0)

	for i := 0; i < 4; i++ {
		tree.Set([]byte(fmt.Sprintf("key-%d", i)), []byte{1})
	}
	_, _, err := tree.SaveVersion(true)
	require.NoError(t, err)

	// the smallest key in the right half of the tree
	require.Equal(t, tree.root.Key(), []byte("key-2"))

	// remove this key
	tree.Remove([]byte("key-2"))

	// check root node's key is changed
	require.Equal(t, []byte("key-3"), tree.root.Key())
}

func TestEmptyTree(t *testing.T) {
	tree := New(0)
	require.Equal(t, emptyHash, tree.RootHash())
}

func TestTreeCopy(t *testing.T) {
	tree := New(0)

	tree.ApplyChangeSet(proto.ChangeSet{Pairs: []*proto.KVPair{
		{Key: []byte("hello"), Value: []byte("world")},
	}})
	_, _, err := tree.SaveVersion(true)
	require.NoError(t, err)

	snapshot := tree.Copy()

	tree.ApplyChangeSet(proto.ChangeSet{Pairs: []*proto.KVPair{
		{Key: []byte("hello"), Value: []byte("world1")},
	}})
	_, _, err = tree.SaveVersion(true)
	require.NoError(t, err)

	require.Equal(t, []byte("world1"), tree.Get([]byte("hello")))
	require.Equal(t, []byte("world"), snapshot.Get([]byte("hello")))

	// check that normal copy don't work
	fakeSnapshot := *tree

	tree.ApplyChangeSet(proto.ChangeSet{Pairs: []*proto.KVPair{
		{Key: []byte("hello"), Value: []byte("world2")},
	}})
	_, _, err = tree.SaveVersion(true)
	require.NoError(t, err)

	// get modified in-place
	require.Equal(t, []byte("world2"), tree.Get([]byte("hello")))
	require.Equal(t, []byte("world2"), fakeSnapshot.Get([]byte("hello")))
}

func TestChangeSetMarshal(t *testing.T) {
	for _, changes := range ChangeSets {
		bz, err := changes.Marshal()
		require.NoError(t, err)

		var cs proto.ChangeSet
		require.NoError(t, cs.Unmarshal(bz))
		require.Equal(t, changes, cs)
	}
}

func TestGetByIndex(t *testing.T) {
	changes := proto.ChangeSet{}
	for i := 0; i < 20; i++ {
		changes.Pairs = append(changes.Pairs, &proto.KVPair{Key: []byte(fmt.Sprintf("hello%02d", i)), Value: []byte(strconv.Itoa(i))})
	}

	tree := New(0)
	tree.ApplyChangeSet(changes)
	_, _, err := tree.SaveVersion(true)
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
	require.NoError(t, tree.WriteSnapshot(context.Background(), dir))
	snapshot, err := OpenSnapshot(dir, Options{})
	require.NoError(t, err)
	ptree := NewFromSnapshot(snapshot, Options{ZeroCopy: true})
	t.Cleanup(func() { require.NoError(t, ptree.Close()) })

	for i, pair := range changes.Pairs {
		idx, v := ptree.GetWithIndex(pair.Key)
		require.Equal(t, pair.Value, v)
		require.Equal(t, int64(i), idx)

		k, v := ptree.GetByIndex(idx)
		require.Equal(t, pair.Key, k)
		require.Equal(t, pair.Value, v)
	}
}
