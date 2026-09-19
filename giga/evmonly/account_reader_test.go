package evmonly

import (
	"bytes"
	"math/big"
	"sync/atomic"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
)

// accountReadingSnapshot answers an account's fields in one read, the capability the executor
// prefers over reading balance, nonce and code separately. Without a view that implements it, every
// caller of that path falls back and the path itself is never exercised.
type accountReadingSnapshot struct {
	*memoryGigaSnapshot
	// Atomic because the prefetch reads the view from every pool worker at once.
	reads atomic.Int64
}

func (s *accountReadingSnapshot) ReadAccount(addr gigatypes.Address) (gigatypes.AccountSnapshot, bool) {
	s.reads.Add(1)
	if !s.AccountExists(addr) {
		return gigatypes.AccountSnapshot{}, false
	}
	return gigatypes.AccountSnapshot{
		Balance:  s.balances[addr],
		Nonce:    s.nonces[addr],
		CodeHash: s.GetCodeHash(addr),
	}, true
}

var _ gigatypes.AccountReader = (*accountReadingSnapshot)(nil)

func TestSnapshotReaderServesAnAccountFromOneRead(t *testing.T) {
	addr := testAddress(0xa1)
	snapshot := newMemoryGigaSnapshot(7)
	snapshot.setBalance(addr, big.NewInt(1234))
	snapshot.nonces[addr] = 9
	reading := &accountReadingSnapshot{memoryGigaSnapshot: snapshot}
	reader := gigaSnapshotStateReader{snapshot: reading}

	account, served := reader.ReadAccount(addr)

	require.True(t, served)
	require.Equal(t, int64(1), reading.reads.Load(), "the row must be resolved once, not once per field")
	require.Equal(t, big.NewInt(1234), account.Balance)
	require.Equal(t, uint64(9), account.Nonce)
	require.Nil(t, account.Code, "an account with the empty-code hash must not ask the code store")
}

// An absent account is a real answer — zero balance, zero nonce, no code — so the combined read
// serves it. It declines only when something else has to answer instead.
func TestSnapshotReaderServesAnAbsentAccountAsEmpty(t *testing.T) {
	reading := &accountReadingSnapshot{memoryGigaSnapshot: newMemoryGigaSnapshot(7)}
	reader := gigaSnapshotStateReader{snapshot: reading}

	account, served := reader.ReadAccount(testAddress(0xb2))

	require.True(t, served)
	require.Equal(t, accountSnapshot{}, account)
}

// With a missing-account reader configured, an account the view does not hold is that reader's to
// answer, so the combined read declines and the caller falls back per field.
func TestSnapshotReaderDeclinesWhenMissingStateOwnsTheAccount(t *testing.T) {
	reading := &accountReadingSnapshot{memoryGigaSnapshot: newMemoryGigaSnapshot(7)}
	reader := gigaSnapshotStateReader{snapshot: reading, missingState: NewMemoryState()}

	_, served := reader.ReadAccount(testAddress(0xb2))
	require.False(t, served)
}

func TestSnapshotReaderFetchesCodeOnlyWhenTheAccountHasSome(t *testing.T) {
	addr := testAddress(0xc3)
	snapshot := newMemoryGigaSnapshot(7)
	snapshot.setBalance(addr, big.NewInt(1))
	snapshot.code[addr] = []byte{0x60, 0x00}
	reading := &accountReadingSnapshot{memoryGigaSnapshot: snapshot}
	reader := gigaSnapshotStateReader{snapshot: reading}

	account, served := reader.ReadAccount(addr)

	require.True(t, served)
	require.Equal(t, []byte{0x60, 0x00}, account.Code)
}

// The merge compares balance, nonce and code against the same row, and resolves that row once per
// touched account across the pool's workers.
func TestParallelMergeResolvesEveryTouchedAccountOnce(t *testing.T) {
	snapshot := newMemoryGigaSnapshot(7)
	addrs := make([]common.Address, 0, 512)
	for i := range 512 {
		addr := common.BigToAddress(new(big.Int).Lsh(big.NewInt(int64(i)+1), 150))
		snapshot.setBalance(addr, big.NewInt(int64(i)+1))
		snapshot.nonces[addr] = uint64(i) //nolint:gosec // i is non-negative.
		addrs = append(addrs, addr)
	}
	reading := &accountReadingSnapshot{memoryGigaSnapshot: snapshot}

	state := newBlockSTMState(gigaSnapshotStateReader{snapshot: reading})
	for i, addr := range addrs {
		state.shard(addr).balances[addr] = big.NewInt(int64(i) + 100)
		state.shard(addr).nonces[addr] = uint64(i) + 1 //nolint:gosec // i is non-negative.
	}
	pool := newOCCWorkerPool(4)
	defer pool.Close()

	var changes StateChangeSet
	require.NoError(t, state.changeSetIntoParallel(t.Context(), pool, &changes))

	require.Equal(t, int64(len(addrs)), reading.reads.Load(), "each row must be read once")
	require.Equal(t, state.ChangeSet(), changes, "the parallel merge must match the serial one")
	require.Equal(t, int64(2*len(addrs)), reading.reads.Load(), "the serial merge reads each row once too")
	require.Len(t, changes.Balances, len(addrs))
	require.Len(t, changes.Nonces, len(addrs))
	for i := 1; i < len(changes.Balances); i++ {
		require.Negative(t, bytes.Compare(changes.Balances[i-1].Address[:], changes.Balances[i].Address[:]))
	}
}
