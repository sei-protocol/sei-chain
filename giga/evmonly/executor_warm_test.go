package evmonly

import (
	"sync"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"

	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
	abevm "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/evm"
)

// readRecordingSnapshot records every account ReadAccount is asked for.
type readRecordingSnapshot struct {
	*memoryGigaSnapshot
	mu    sync.Mutex
	reads []common.Address
}

func (s *readRecordingSnapshot) ReadAccount(address gigatypes.Address) (gigatypes.Account, bool) {
	s.mu.Lock()
	s.reads = append(s.reads, address)
	s.mu.Unlock()
	return s.memoryGigaSnapshot.ReadAccount(address)
}

func TestWarmAccountsReadsEverySenderAndRecipient(t *testing.T) {
	snapshot := &readRecordingSnapshot{memoryGigaSnapshot: newMemoryGigaSnapshot(40)}
	executor := NewExecutor(Config{ParseWorkers: 2},
		withTestStores(&recordingGigaStore{snapshot: snapshot}, NewMemoryReceiptStore()))
	defer executor.Close()

	recipient := testAddress(0xc1)
	transferSender, creationSender := testAddress(0xa1), testAddress(0xa2)
	executor.WarmAccounts(t.Context(), []abevm.EvmTx{
		abevm.TestEvmTx(ethtypes.NewTx(&ethtypes.LegacyTx{To: &recipient}), transferSender),
		abevm.TestEvmTx(ethtypes.NewTx(&ethtypes.LegacyTx{}), creationSender),
	})

	require.ElementsMatch(t, []common.Address{transferSender, recipient, creationSender}, snapshot.reads)
}

func TestWarmAccountsSkipsANilView(t *testing.T) {
	executor := NewExecutor(Config{}, withTestStores(&recordingGigaStore{}, NewMemoryReceiptStore()))
	defer executor.Close()

	recipient := testAddress(0xc1)
	executor.WarmAccounts(t.Context(), []abevm.EvmTx{
		abevm.TestEvmTx(ethtypes.NewTx(&ethtypes.LegacyTx{To: &recipient}), testAddress(0xa1)),
	})
}
