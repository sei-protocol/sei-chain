package evmonly

import (
	"math/big"
	"sync/atomic"
	"testing"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"

	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
)

// countingCodeView counts GetCode calls on the snapshot it wraps.
type countingCodeView struct {
	*memoryGigaSnapshot
	codeReads atomic.Int64
}

func (s *countingCodeView) GetCode(addr gigatypes.Address) []byte {
	s.codeReads.Add(1)
	return s.memoryGigaSnapshot.GetCode(addr)
}

func TestSnapshotReaderReadsEachCodeHashFromTheStoreOnce(t *testing.T) {
	code := []byte{0x60, 0x01, 0x60, 0x00, 0x55}
	first, second := testAddress(0xd1), testAddress(0xd2)
	snapshot := newMemoryGigaSnapshot(7)
	for _, addr := range []gigatypes.Address{first, second} {
		snapshot.setBalance(addr, big.NewInt(1))
		snapshot.code[addr] = append([]byte(nil), code...)
	}
	reading := &countingCodeView{memoryGigaSnapshot: snapshot}
	codes := newCodeCache()

	// Two blocks' readers, each over its own view, share the executor's cache.
	for range 2 {
		reader := gigaSnapshotStateReader{snapshot: reading, codes: codes}
		for _, addr := range []gigatypes.Address{first, second} {
			account, served := reader.ReadAccount(addr)
			require.True(t, served)
			require.Equal(t, code, account.Code)
		}
	}
	require.Equal(t, int64(1), reading.codeReads.Load(), "code under one hash must be read from the store once")

	// The cache keeps its own copy, so a store reusing the slice it returned cannot change it.
	snapshot.code[first][0] = 0xff
	account, _ := gigaSnapshotStateReader{snapshot: reading, codes: codes}.ReadAccount(second)
	require.Equal(t, code, account.Code)
}

func TestCodeCacheStopsKeepingCodeWhenFull(t *testing.T) {
	codes := newCodeCache()
	codes.count.Store(maxCachedCodes)
	code := []byte{0x00}
	hash := crypto.Keccak256Hash(code)
	loads := 0
	load := func() []byte { loads++; return code }

	require.Equal(t, code, codes.get(hash, load))
	require.Equal(t, code, codes.get(hash, load))
	require.Equal(t, 2, loads, "a full cache serves misses without keeping them")
}
