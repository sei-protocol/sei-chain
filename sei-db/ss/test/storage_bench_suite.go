package sstest

import (
	"bytes"
	"math/rand"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cosmos/iavl"
	"github.com/sei-protocol/sei-db/proto"
	"github.com/sei-protocol/sei-db/ss/types"
)

// StorageBenchSuite defines a reusable benchmark suite for all storage backends.
type StorageBenchSuite struct {
	BenchBackendName string
	NewDB            func(dir string) (types.StateStore, error)
}

var rng = rand.New(rand.NewSource(567320))

func (s *StorageBenchSuite) BenchmarkGet(b *testing.B) {
	numKeyVals := 10000
	keys := make([][]byte, numKeyVals)
	vals := make([][]byte, numKeyVals)
	for i := 0; i < numKeyVals; i++ {
		key := make([]byte, 128)
		val := make([]byte, 128)

		_, err := rng.Read(key)
		require.NoError(b, err)
		_, err = rng.Read(val)
		require.NoError(b, err)

		keys[i] = key
		vals[i] = val
	}

	db, err := s.NewDB(b.TempDir())
	require.NoError(b, err)
	defer db.Close()

	cs := &iavl.ChangeSet{}
	cs.Pairs = []*iavl.KVPair{}

	for i := 0; i < numKeyVals; i++ {
		cs.Pairs = append(cs.Pairs, &iavl.KVPair{Key: keys[i], Value: vals[i]})
	}

	ncs := &proto.NamedChangeSet{
		Name:      storeKey1,
		Changeset: *cs,
	}

	require.NoError(b, db.ApplyChangeset(1, ncs))

	b.Run(s.BenchBackendName, func(b *testing.B) {
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			b.StopTimer()
			key := keys[rng.Intn(len(keys))]

			b.StartTimer()
			_, err = db.Get(storeKey1, 1, key)
			require.NoError(b, err)
		}
	})
}

func (s *StorageBenchSuite) BenchmarkApplyChangeset(b *testing.B) {
	db, err := s.NewDB(b.TempDir())
	require.NoError(b, err)
	defer db.Close()

	b.Run(s.BenchBackendName, func(b *testing.B) {
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			b.StopTimer()

			cs := &iavl.ChangeSet{}
			cs.Pairs = []*iavl.KVPair{}

			for j := 0; j < 1000; j++ {
				key := make([]byte, 128)
				val := make([]byte, 128)

				_, err = rng.Read(key)
				require.NoError(b, err)
				_, err = rng.Read(val)
				require.NoError(b, err)

				cs.Pairs = append(cs.Pairs, &iavl.KVPair{Key: key, Value: val})
			}

			ncs := &proto.NamedChangeSet{
				Name:      storeKey1,
				Changeset: *cs,
			}
			b.StartTimer()
			require.NoError(b, db.ApplyChangeset(int64(b.N+1), ncs))
		}
	})
}

func (s *StorageBenchSuite) BenchmarkIterate(b *testing.B) {
	numKeyVals := 1_000_000
	keys := make([][]byte, numKeyVals)
	vals := make([][]byte, numKeyVals)
	for i := 0; i < numKeyVals; i++ {
		key := make([]byte, 128)
		val := make([]byte, 128)

		_, err := rng.Read(key)
		require.NoError(b, err)
		_, err = rng.Read(val)
		require.NoError(b, err)

		keys[i] = key
		vals[i] = val

	}

	db, err := s.NewDB(b.TempDir())
	require.NoError(b, err)
	defer db.Close()

	b.StopTimer()

	cs := &iavl.ChangeSet{}
	cs.Pairs = []*iavl.KVPair{}
	for i := 0; i < numKeyVals; i++ {
		cs.Pairs = append(cs.Pairs, &iavl.KVPair{Key: keys[i], Value: vals[i]})
	}
	ncs := &proto.NamedChangeSet{
		Name:      storeKey1,
		Changeset: *cs,
	}

	require.NoError(b, db.ApplyChangeset(1, ncs))

	sort.Slice(keys, func(i, j int) bool {
		return bytes.Compare(keys[i], keys[j]) < 0
	})

	b.Run(s.BenchBackendName, func(b *testing.B) {
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			b.StopTimer()

			itr, err := db.Iterator(storeKey1, 1, keys[0], nil)
			require.NoError(b, err)

			b.StartTimer()

			for ; itr.Valid(); itr.Next() {
				_ = itr.Key()
				_ = itr.Value()
			}

			require.NoError(b, itr.Error())
		}
	})
}
