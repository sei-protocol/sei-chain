package evmonly

import (
	"bytes"
	"math/big"
	"sync/atomic"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
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

	account, _, served := reader.ReadAccount(addr)

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

	account, _, served := reader.ReadAccount(testAddress(0xb2))

	require.True(t, served)
	require.Equal(t, AccountState{}, account)
}

// With a missing-account reader configured, an account the view does not hold is that reader's to
// answer, so the combined read declines and the caller falls back per field.
func TestSnapshotReaderDeclinesWhenMissingStateOwnsTheAccount(t *testing.T) {
	reading := &countingAccountView{memoryGigaSnapshot: newMemoryGigaSnapshot(7)}
	reader := gigaSnapshotStateReader{snapshot: reading, missingState: NewMemoryState()}

	_, _, served := reader.ReadAccount(testAddress(0xb2))
	require.False(t, served)
}

func TestSnapshotReaderFetchesCodeOnlyWhenTheAccountHasSome(t *testing.T) {
	addr := testAddress(0xc3)
	snapshot := newMemoryGigaSnapshot(7)
	snapshot.setBalance(addr, big.NewInt(1))
	snapshot.code[addr] = []byte{0x60, 0x00}
	reading := &countingAccountView{memoryGigaSnapshot: snapshot}
	reader := gigaSnapshotStateReader{snapshot: reading}

	account, codeHash, served := reader.ReadAccount(addr)

	require.True(t, served)
	require.Equal(t, []byte{0x60, 0x00}, account.Code)
	require.Equal(t, crypto.Keccak256Hash(account.Code), codeHash, "the row's stored code hash comes with the code")
}

// A contract's code hash comes from its account row, so reading it does not hash the code again.
func TestStateDBTakesTheCodeHashFromTheAccountRow(t *testing.T) {
	addr := testAddress(0xc4)
	snapshot := newMemoryGigaSnapshot(7)
	snapshot.setBalance(addr, big.NewInt(1))
	snapshot.code[addr] = []byte{0x60, 0x00}
	stored := common.HexToHash("0x1234")
	reader := storedCodeHashReader{gigaSnapshotStateReader: gigaSnapshotStateReader{snapshot: snapshot}, codeHash: stored}
	stateDB := newNativeStateDB(reader)

	require.Equal(t, stored, stateDB.GetCodeHash(addr))
}

// storedCodeHashReader reports a fixed code hash, standing in for a row whose hash is not the one the
// code would hash to, so a test can tell whether the hash was read or recomputed.
type storedCodeHashReader struct {
	gigaSnapshotStateReader
	codeHash common.Hash
}

func (r storedCodeHashReader) ReadAccount(addr common.Address) (AccountState, common.Hash, bool) {
	account, _, served := r.gigaSnapshotStateReader.ReadAccount(addr)
	return account, r.codeHash, served
}

// The merge takes priors from the accepted writes, so it reads no account row, on the pool's workers
// or on the calling goroutine.
func TestParallelMergeReadsNoAccount(t *testing.T) {
	snapshot := newMemoryGigaSnapshot(7)
	addrs := make([]common.Address, 0, 512)
	for i := range 512 {
		addr := common.BigToAddress(new(big.Int).Lsh(big.NewInt(int64(i)+1), 150))
		snapshot.setBalance(addr, big.NewInt(int64(i)+1))
		snapshot.nonces[addr] = uint64(i) //nolint:gosec // i is non-negative.
		addrs = append(addrs, addr)
	}
	reading := &countingAccountView{memoryGigaSnapshot: snapshot}

	state := newBlockSTMState(gigaSnapshotStateReader{snapshot: reading})
	for i, addr := range addrs {
		state.shard(addr).accounts[addr] = &AccountChange{
			Address: addr,
			Account: AccountState{Balance: big.NewInt(int64(i) + 100), Nonce: uint64(i) + 1}, //nolint:gosec // i is non-negative.
		}
	}
	pool := newOCCWorkerPool(4)
	defer pool.Close()

	var changes StateChangeSet
	require.NoError(t, state.changeSetIntoParallel(t.Context(), pool, &changes))

	require.Zero(t, reading.reads.Load(), "the parallel merge must read no row")
	require.Equal(t, canonicalChangeSet(state.ChangeSet()), canonicalChangeSet(changes),
		"the parallel merge must match the serial one")
	require.Zero(t, reading.reads.Load(), "the serial merge must read no row")
	require.Len(t, changes.Accounts, len(addrs))
	for i := 1; i < len(changes.Accounts); i++ {
		require.Negative(t, bytes.Compare(changes.Accounts[i-1].Address[:], changes.Accounts[i].Address[:]))
	}
}
