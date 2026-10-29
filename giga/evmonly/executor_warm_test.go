package evmonly

import (
	"sync"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"

	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
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
		withTestStores(&recordingGigaStore{snapshot: snapshot}, NewMemoryReceiptStore(), noopChangeSetEncoder))
	defer executor.Close()

	recipient := testAddress(0xc1)
	transferSender, creationSender := testAddress(0xa1), testAddress(0xa2)
	executor.WarmAccounts(t.Context(), PreparedBlock{Txs: []PreparedTx{
		{Tx: ethtypes.NewTx(&ethtypes.LegacyTx{To: &recipient}), Sender: transferSender},
		{Tx: ethtypes.NewTx(&ethtypes.LegacyTx{}), Sender: creationSender},
	}})

	require.ElementsMatch(t, []common.Address{transferSender, recipient, creationSender}, snapshot.reads)
}
