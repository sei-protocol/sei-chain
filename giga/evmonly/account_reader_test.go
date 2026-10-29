package evmonly

import (
	"bytes"
	"math/big"
	"sync/atomic"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"
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
	require.Zero(t, account.CodeHash, "an account without code carries the zero hash, not the empty-code hash")
}

// An absent account is a real answer — zero balance, zero nonce, no code — so the combined read
// serves it. It declines only when something else has to answer instead.
func TestSnapshotReaderServesAnAbsentAccountAsEmpty(t *testing.T) {
	reading := &countingAccountView{memoryGigaSnapshot: newMemoryGigaSnapshot(7)}
	reader := gigaSnapshotStateReader{snapshot: reading}

	account, served := reader.ReadAccount(testAddress(0xb2))

	require.True(t, served)
	require.Equal(t, AccountState{}, account)
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
	require.Equal(t, crypto.Keccak256Hash([]byte{0x60, 0x00}), account.CodeHash, "the stored code hash comes with the code")
}

// stampedHashView reports hash as the code hash of every account that has code, whatever the code, so
// a code hash that reaches a changeset as hash was taken from the view rather than computed.
type stampedHashView struct {
	*memoryGigaSnapshot
	hash gigatypes.Hash
}

func (s stampedHashView) ReadAccount(addr gigatypes.Address) (gigatypes.Account, bool) {
	account, ok := s.memoryGigaSnapshot.ReadAccount(addr)
	if ok && account.CodeHash != gigatypes.EmptyCodeHash {
		account.CodeHash = s.hash
	}
	return account, ok
}

// A changeset takes the code hash of an account it changes from the row the account was read from,
// both read directly and through the prefix OCC reruns read, rather than hashing the code again.
func TestChangeSetTakesTheCodeHashFromTheSource(t *testing.T) {
	contract := testAddress(0xd4)
	snapshot := newMemoryGigaSnapshot(7)
	snapshot.setBalance(contract, big.NewInt(1))
	snapshot.code[contract] = []byte{0x60, 0x00}
	stamped := common.HexToHash("0x5eed")
	reader := gigaSnapshotStateReader{snapshot: stampedHashView{memoryGigaSnapshot: snapshot, hash: stamped}}

	for _, tc := range []struct {
		name   string
		source StateReader
	}{
		{"snapshot", reader},
		{"rerun prefix", newBlockSTMState(reader)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := newNativeStateDB(tc.source)
			db.AddBalance(contract, uint256.NewInt(5), tracing.BalanceChangeUnspecified)
			changes := db.ChangeSet()

			require.Len(t, changes.Accounts, 1)
			require.Equal(t, stamped, changes.Accounts[0].Prior.CodeHash)
			require.Equal(t, stamped, changes.Accounts[0].Account.CodeHash)
		})
	}
}

// The prefix serves an account a transaction ahead wrote from that write, code hash included, and
// hands out a balance its caller may change without changing the write.
func TestRerunPrefixServesAWrittenAccount(t *testing.T) {
	addr := testAddress(0xd5)
	state := newBlockSTMState(NewMemoryState())
	written := AccountState{Balance: big.NewInt(9), Nonce: 2, Code: []byte{0x60, 0x01}, CodeHash: crypto.Keccak256Hash([]byte{0x60, 0x01})}
	state.shard(addr).accounts[addr] = &AccountChange{Address: addr, Account: written}

	account, served := state.ReadAccount(addr)

	require.True(t, served)
	require.Equal(t, written, account)
	account.Balance.SetInt64(0)
	require.Equal(t, big.NewInt(9), state.shard(addr).accounts[addr].Account.Balance)
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
	require.Equal(t, state.ChangeSet(), changes, "the parallel merge must match the serial one")
	require.Zero(t, reading.reads.Load(), "the serial merge must read no row")
	require.Len(t, changes.Accounts, len(addrs))
	for i := 1; i < len(changes.Accounts); i++ {
		require.Negative(t, bytes.Compare(changes.Accounts[i-1].Address[:], changes.Accounts[i].Address[:]))
	}
}
