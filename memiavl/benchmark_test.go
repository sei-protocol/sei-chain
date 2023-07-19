package memiavl

import (
	"bytes"
	"encoding/binary"
	"math/rand"
	"sort"
	"testing"

	iavlcache "github.com/cosmos/iavl/cache"
	lru "github.com/hashicorp/golang-lru"
	"github.com/hashicorp/golang-lru/simplelru"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/btree"
)

func BenchmarkByteCompare(b *testing.B) {
	var x, y [32]byte
	for i := 0; i < b.N; i++ {
		_ = bytes.Compare(x[:], y[:])
	}
}

func BenchmarkRandomGet(b *testing.B) {
	amount := 1000000
	items := genRandItems(amount)
	targetKey := items[500].key
	targetValue := items[500].value
	targetItem := itemT{key: targetKey}

	tree := New(0)
	for _, item := range items {
		tree.set(item.key, item.value)
	}

	snapshotDir := b.TempDir()
	err := tree.WriteSnapshot(snapshotDir)
	require.NoError(b, err)
	snapshot, err := OpenSnapshot(snapshotDir)
	require.NoError(b, err)
	defer snapshot.Close()
	diskTree := NewFromSnapshot(snapshot, true, 0)

	require.Equal(b, targetValue, tree.Get(targetKey))
	require.Equal(b, targetValue, diskTree.Get(targetKey))

	b.ResetTimer()
	b.Run("memiavl", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = tree.Get(targetKey)
		}
	})
	b.Run("memiavl-disk", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = diskTree.Get(targetKey)
		}
	})
	b.Run("btree-degree-2", func(b *testing.B) {
		bt2 := btree.NewBTreeGOptions(lessG, btree.Options{
			NoLocks: true,
			Degree:  2,
		})
		for _, item := range items {
			bt2.Set(item)
		}
		v, _ := bt2.Get(targetItem)
		require.Equal(b, targetValue, v.value)

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _ = bt2.Get(targetItem)
		}
	})
	b.Run("btree-degree-32", func(b *testing.B) {
		bt32 := btree.NewBTreeGOptions(lessG, btree.Options{
			NoLocks: true,
			Degree:  32,
		})
		for _, item := range items {
			bt32.Set(item)
		}
		v, _ := bt32.Get(targetItem)
		require.Equal(b, targetValue, v.value)

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _ = bt32.Get(targetItem)
		}
	})
	b.Run("lru-cache", func(b *testing.B) {
		cache, err := lru.NewARC(amount)
		require.NoError(b, err)
		for _, item := range items {
			cache.Add(string(item.key), item.value)
		}
		v, _ := cache.Get(string(targetItem.key))
		require.Equal(b, targetValue, v.([]byte))

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _ = cache.Get(string(targetKey))
		}
	})
	b.Run("simplelru", func(b *testing.B) {
		cache, err := simplelru.NewLRU(amount, nil)
		require.NoError(b, err)
		for _, item := range items {
			cache.Add(string(item.key), item.value)
		}
		v, _ := cache.Get(string(targetItem.key))
		require.Equal(b, targetValue, v.([]byte))

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _ = cache.Get(string(targetKey))
		}
	})
	b.Run("iavl-lru", func(b *testing.B) {
		cache := iavlcache.New(amount)
		for _, item := range items {
			cache.Add(NewIavlCacheNode(item.key, item.value))
		}
		v := cache.Get(targetItem.key).(iavlCacheNode).value
		require.Equal(b, targetValue, v)

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = cache.Get(targetKey).(iavlCacheNode).value
		}
	})
	b.Run("go-map", func(b *testing.B) {
		m := make(map[string][]byte, amount)
		for _, item := range items {
			m[string(item.key)] = item.value
		}
		v := m[string(targetItem.key)]
		require.Equal(b, targetValue, v)

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = m[string(targetKey)]
		}
	})

	b.Run("binary-search", func(b *testing.B) {
		// the last benchmark sort the items in place
		sort.Slice(items, func(i, j int) bool {
			return bytes.Compare(items[i].key, items[j].key) < 0
		})
		cmp := func(i int) bool { return bytes.Compare(items[i].key, targetKey) != -1 }
		i := sort.Search(len(items), cmp)
		require.Equal(b, targetValue, items[i].value)

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			n := sort.Search(len(items), cmp)
			_ = items[n].value
		}
	})
}

func BenchmarkRandomSet(b *testing.B) {
	items := genRandItems(1000000)
	b.ResetTimer()
	b.Run("memiavl", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			tree := New(0)
			for _, item := range items {
				tree.set(item.key, item.value)
			}
		}
	})
	b.Run("tree2", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			bt := btree.NewBTreeGOptions(lessG, btree.Options{
				NoLocks: true,
				Degree:  2,
			})
			for _, item := range items {
				bt.Set(item)
			}
		}
	})
	b.Run("tree32", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			bt := btree.NewBTreeGOptions(lessG, btree.Options{
				NoLocks: true,
				Degree:  32,
			})
			for _, item := range items {
				bt.Set(item)
			}
		}
	})
}

type itemT struct {
	key, value []byte
}

func lessG(a, b itemT) bool {
	return bytes.Compare(a.key, b.key) == -1
}

func int64ToItemT(n uint64) itemT {
	var key, value [8]byte
	binary.BigEndian.PutUint64(key[:], n)
	binary.LittleEndian.PutUint64(value[:], n)
	return itemT{
		key:   key[:],
		value: value[:],
	}
}

func genRandItems(n int) []itemT {
	r := rand.New(rand.NewSource(0))
	items := make([]itemT, n)
	itemsM := make(map[uint64]bool)
	for i := 0; i < n; i++ {
		for {
			key := uint64(r.Int63n(10000000000000000))
			if !itemsM[key] {
				itemsM[key] = true
				items[i] = int64ToItemT(key)
				break
			}
		}
	}
	return items
}

type iavlCacheNode struct {
	key   []byte
	value []byte
}

func NewIavlCacheNode(key, value []byte) iavlCacheNode {
	return iavlCacheNode{key, value}
}

func (n iavlCacheNode) GetKey() []byte {
	return n.key
}
