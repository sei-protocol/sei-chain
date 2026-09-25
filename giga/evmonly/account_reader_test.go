package evmonly

import (
	"math/big"
	"sync/atomic"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
)

// countingAccountView counts ReadAccount calls on the snapshot it wraps.
type countingAccountView struct {
	*memoryGigaSnapshot
	// Atomic because the prefetch reads the view from every pool worker at once.
	reads atomic.Int64
}

func (s *countingAccountView) ReadAccount(addr gigatypes.Address) (gigatypes.Account, bool) {
	s.reads.Add(1)
	return s.memoryGigaSnapshot.ReadAccount(addr)
}

var _ gigatypes.StateView = (*countingAccountView)(nil)

func TestSnapshotReaderServesAnAccountFromOneRead(t *testing.T) {
	addr := testAddress(0xa1)
	snapshot := newMemoryGigaSnapshot(7)
	snapshot.setBalance(addr, big.NewInt(1234))
	snapshot.nonces[addr] = 9
	reading := &countingAccountView{memoryGigaSnapshot: snapshot}
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
	reading := &countingAccountView{memoryGigaSnapshot: newMemoryGigaSnapshot(7)}
	reader := gigaSnapshotStateReader{snapshot: reading}

	account, served := reader.ReadAccount(testAddress(0xb2))

	require.True(t, served)
	require.Equal(t, baseAccount{}, account)
}

// With a missing-account reader configured, an account the view does not hold is that reader's to
// answer, so the combined read declines and the caller falls back per field.
func TestSnapshotReaderDeclinesWhenMissingStateOwnsTheAccount(t *testing.T) {
	reading := &countingAccountView{memoryGigaSnapshot: newMemoryGigaSnapshot(7)}
	reader := gigaSnapshotStateReader{snapshot: reading, missingState: NewMemoryState()}

	_, served := reader.ReadAccount(testAddress(0xb2))
	require.False(t, served)
}

func TestSnapshotReaderFetchesCodeOnlyWhenTheAccountHasSome(t *testing.T) {
	addr := testAddress(0xc3)
	snapshot := newMemoryGigaSnapshot(7)
	snapshot.setBalance(addr, big.NewInt(1))
	snapshot.code[addr] = []byte{0x60, 0x00}
	reading := &countingAccountView{memoryGigaSnapshot: snapshot}
	reader := gigaSnapshotStateReader{snapshot: reading}

	account, served := reader.ReadAccount(addr)

	require.True(t, served)
	require.Equal(t, []byte{0x60, 0x00}, account.Code)
}

// The merge reads every account it is about to compare against through the pool, then the serial
// comparison finds them already resolved.
func TestPrefetchResolvesEveryTouchedAccountOnce(t *testing.T) {
	snapshot := newMemoryGigaSnapshot(7)
	addrs := make([]common.Address, 0, minPrefetchedAccounts+8)
	for i := range minPrefetchedAccounts + 8 {
		addr := common.BigToAddress(big.NewInt(int64(i) + 1))
		snapshot.setBalance(addr, big.NewInt(int64(i)+1))
		addrs = append(addrs, addr)
	}
	reading := &countingAccountView{memoryGigaSnapshot: snapshot}

	state := newBlockSTMState(gigaSnapshotStateReader{snapshot: reading})
	for i, addr := range addrs {
		state.balances[addr] = big.NewInt(int64(i) + 100)
	}
	state.prefetchBaseAccounts(t.Context(), newOCCWorkerPool(4))

	require.Len(t, state.prefetched, len(addrs))
	for i, addr := range addrs {
		require.Equal(t, big.NewInt(int64(i)+1), state.prefetched[addr].Balance)
	}

	// The comparison that follows reads the prefetched rows rather than the view.
	before := reading.reads.Load()
	base := newBaseAccounts(state.source, state.prefetched)
	for _, addr := range addrs {
		base.balance(addr)
	}
	require.Equal(t, before, reading.reads.Load(), "the merge must not re-read what was prefetched")
}

// Below the threshold the pool costs more than the reads it saves, so the merge reads them itself.
func TestPrefetchIsSkippedForASmallBlock(t *testing.T) {
	snapshot := newMemoryGigaSnapshot(7)
	addr := testAddress(0xd4)
	snapshot.setBalance(addr, big.NewInt(5))
	reading := &countingAccountView{memoryGigaSnapshot: snapshot}

	state := newBlockSTMState(gigaSnapshotStateReader{snapshot: reading})
	state.balances[addr] = big.NewInt(6)
	state.prefetchBaseAccounts(t.Context(), newOCCWorkerPool(4))

	require.Nil(t, state.prefetched)
}
