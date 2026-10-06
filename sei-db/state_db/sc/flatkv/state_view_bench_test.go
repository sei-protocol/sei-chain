package flatkv

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
)

// benchView opens a view over a store holding one account with a nonce, a balance, a storage slot and
// 8 KiB of code, so each benchmark measures a cache-hit read.
func benchView(b *testing.B) (gigatypes.StateView, gigatypes.Address) {
	b.Helper()
	cfg := config.DefaultTestConfig(&testing.T{})
	cfg.DataDir = filepath.Join(b.TempDir(), "flatkv")
	s, err := newCommitStoreWithWAL(b.Context(), cfg)
	require.NoError(b, err)
	require.NoError(b, s.LoadLatest())
	b.Cleanup(func() { require.NoError(b, s.Close()) })
	addr := addrN(1)
	code := make([]byte, 8*1024)
	for i := range code {
		code[i] = byte(i)
	}
	cs := namedCS(noncePair(addr, 7), storagePair(addr, slotN(1), padLeft32(0xaa)), codePair(addr, code))
	cs.Changeset.Pairs = append(cs.Changeset.Pairs, &proto.KVPair{
		Key:   keys.BuildEVMKey(keys.EVMKeyBalance, addr[:]),
		Value: padLeft32(0x05),
	})
	require.NoError(b, s.CommitStateChanges(1, []*proto.NamedChangeSet{cs}))
	v := s.OpenView()
	b.Cleanup(v.Close)
	return v, gigaAddr(addr)
}

func BenchmarkStateViewGetStorage(b *testing.B) {
	v, addr := benchView(b)
	slot := gigatypes.Hash(slotN(1))
	require.Equal(b, gigatypes.Hash(padLeft32(0xaa)), v.GetStorage(addr, slot))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		v.GetStorage(addr, slot)
	}
}

func BenchmarkStateViewGetBalance(b *testing.B) {
	v, addr := benchView(b)
	require.Equal(b, gigatypes.Hash(padLeft32(0x05)), v.GetBalance(addr))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		v.GetBalance(addr)
	}
}

func BenchmarkStateViewGetCode(b *testing.B) {
	v, addr := benchView(b)
	require.Len(b, v.GetCode(addr), 8*1024)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		v.GetCode(addr)
	}
}

func BenchmarkStateViewGetNonce(b *testing.B) {
	v, addr := benchView(b)
	require.Equal(b, uint64(7), v.GetNonce(addr))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		v.GetNonce(addr)
	}
}

func BenchmarkStateViewAccountExists(b *testing.B) {
	v, addr := benchView(b)
	require.True(b, v.AccountExists(addr))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		v.AccountExists(addr)
	}
}

func BenchmarkStateViewGetCodeHash(b *testing.B) {
	v, addr := benchView(b)
	require.Equal(b, gigatypes.EmptyCodeHash, v.GetCodeHash(addr))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		v.GetCodeHash(addr)
	}
}

func BenchmarkStateViewGetCodeSize(b *testing.B) {
	v, addr := benchView(b)
	require.Equal(b, 8*1024, v.GetCodeSize(addr))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		v.GetCodeSize(addr)
	}
}

func BenchmarkStateViewGetStorageMiss(b *testing.B) {
	v, addr := benchView(b)
	slot := gigatypes.Hash(slotN(2))
	require.Equal(b, gigatypes.Hash{}, v.GetStorage(addr, slot))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		v.GetStorage(addr, slot)
	}
}

func BenchmarkStateViewGetLogical(b *testing.B) {
	v, addr := benchView(b)
	slot := gigatypes.Hash(slotN(1))
	storageKey := append(append([]byte(nil), addr[:]...), slot[:]...)

	b.Run("nonce", func(b *testing.B) {
		key := keys.BuildEVMKey(keys.EVMKeyNonce, addr[:])
		_, found := v.Get(keys.EVMStoreKey, key)
		require.True(b, found)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			v.Get(keys.EVMStoreKey, key)
		}
	})

	b.Run("balance", func(b *testing.B) {
		key := keys.BuildEVMKey(keys.EVMKeyBalance, addr[:])
		_, found := v.Get(keys.EVMStoreKey, key)
		require.True(b, found)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			v.Get(keys.EVMStoreKey, key)
		}
	})

	b.Run("storage", func(b *testing.B) {
		key := keys.BuildEVMKey(keys.EVMKeyStorage, storageKey)
		_, found := v.Get(keys.EVMStoreKey, key)
		require.True(b, found)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			v.Get(keys.EVMStoreKey, key)
		}
	})

	b.Run("code", func(b *testing.B) {
		key := keys.BuildEVMKey(keys.EVMKeyCode, addr[:])
		_, found := v.Get(keys.EVMStoreKey, key)
		require.True(b, found)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			v.Get(keys.EVMStoreKey, key)
		}
	})
}
