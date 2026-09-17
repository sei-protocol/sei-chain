package evmonly

import (
	"errors"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
)

// errTestCommitFailed is the failure a test store reports from its background commit.
var errTestCommitFailed = errors.New("commit failed")

// noopChangeSetEncoder stands in for a real encoder: the pipeline only needs something to hand the
// background commit.
func noopChangeSetEncoder(StateChangeSet) ([]*proto.NamedChangeSet, error) {
	return []*proto.NamedChangeSet{{Name: "encoded"}}, nil
}

// The recording store never applies a commit to its view, so a block can only see the block before
// it through the pending overlay. That is exactly the property the pipeline depends on: a block
// executes while its predecessor is still being written.
func TestPipelinedBlockSeesThePreviousBlocksUncommittedState(t *testing.T) {
	chainID := big.NewInt(testChainID)
	funderKey, err := crypto.GenerateKey()
	require.NoError(t, err)
	funder := crypto.PubkeyToAddress(funderKey.PublicKey)
	middleKey, err := crypto.GenerateKey()
	require.NoError(t, err)
	middle := crypto.PubkeyToAddress(middleKey.PublicKey)
	last := testAddress(0xc3)

	snapshot := newMemoryGigaSnapshot(40)
	// Enough that middle can cover gas on its own transaction out of what block 41 pays it.
	snapshot.setBalance(funder, new(big.Int).Mul(big.NewInt(testFundedBalanceWei), big.NewInt(100)))
	store := &recordingGigaStore{snapshot: snapshot}
	executor := NewExecutor(Config{}, withTestStores(store, NewMemoryReceiptStore(), noopChangeSetEncoder))
	defer executor.Close()

	// Block 41 funds middle. Its commit is left running.
	payment := new(big.Int).Mul(big.NewInt(testFundedBalanceWei), big.NewInt(10))
	first := executePipelinedBlock(t, executor, chainID, 41,
		signLegacyTx(t, funderKey, chainID, 0, &middle, payment, nil))
	require.Equal(t, uint64(1), first.Txs[0].Status, "funding transaction must succeed")
	require.Contains(t, first.ChangeSet.Balances, BalanceChange{Address: middle, Balance: payment})

	// Block 42 spends from middle. The store's view is still at genesis, so this can only succeed
	// if block 41's balance and nonce reached it through the overlay.
	forwarded := big.NewInt(1_000)
	second := executePipelinedBlock(t, executor, chainID, 42,
		signLegacyTx(t, middleKey, chainID, 0, &last, forwarded, nil))
	require.Equal(t, uint64(1), second.Txs[0].Status,
		"spending what the previous block paid must succeed, or the overlay is not being read")
	require.Contains(t, second.ChangeSet.Balances, BalanceChange{Address: last, Balance: forwarded})

	require.NoError(t, executor.AwaitCommits())
	require.Equal(t, []int64{41, 42}, store.commitBlock, "commits must land in block order")
}

// A commit that fails has to fail the run, even though the block that produced it already returned.
func TestFailedPipelinedCommitIsReportedToEveryWaiter(t *testing.T) {
	chainID := big.NewInt(testChainID)
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	sender := crypto.PubkeyToAddress(key.PublicKey)
	recipient := testAddress(0xa9)

	snapshot := newMemoryGigaSnapshot(40)
	snapshot.setBalance(sender, big.NewInt(testFundedBalanceWei))
	store := &recordingGigaStore{snapshot: snapshot, commitErr: errTestCommitFailed}
	executor := NewExecutor(Config{}, withTestStores(store, NewMemoryReceiptStore(), noopChangeSetEncoder))
	defer executor.Close()

	executePipelinedBlock(t, executor, chainID, 41,
		signLegacyTx(t, key, chainID, 0, &recipient, big.NewInt(7), nil))

	require.ErrorIs(t, executor.AwaitCommits(), errTestCommitFailed)
	// Latched rather than consumed, so a second caller cannot read the run as clean.
	require.ErrorIs(t, executor.AwaitCommits(), errTestCommitFailed)
}

// Close and the execute loop can both be waiting on the same commit; neither may be stranded.
func TestConcurrentWaitersOnOneCommitAreAllReleased(t *testing.T) {
	chainID := big.NewInt(testChainID)
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	sender := crypto.PubkeyToAddress(key.PublicKey)
	recipient := testAddress(0xa9)

	snapshot := newMemoryGigaSnapshot(40)
	snapshot.setBalance(sender, big.NewInt(testFundedBalanceWei))
	store := &recordingGigaStore{snapshot: snapshot}
	executor := NewExecutor(Config{}, withTestStores(store, NewMemoryReceiptStore(), noopChangeSetEncoder))
	defer executor.Close()

	executePipelinedBlock(t, executor, chainID, 41,
		signLegacyTx(t, key, chainID, 0, &recipient, big.NewInt(7), nil))

	const waiters = 8
	done := make(chan error, waiters)
	for range waiters {
		go func() { done <- executor.AwaitCommits() }()
	}
	for range waiters {
		select {
		case err := <-done:
			require.NoError(t, err)
		case <-t.Context().Done():
			t.Fatal("a waiter was never released by the commit")
		}
	}
}

// executePipelinedBlock runs one block through the pipelined path, which returns before the block's
// commit has landed.
func executePipelinedBlock(t *testing.T, executor *Executor, chainID *big.Int, number uint64, txs ...[]byte) *BlockResult {
	t.Helper()
	blockCtx := blockContext(chainID)
	blockCtx.Number = number
	prepared, err := executor.PrepareBlock(t.Context(), BlockRequest{Context: blockCtx, Txs: txs})
	require.NoError(t, err)
	result, err := executor.ExecutePreparedBlock(t.Context(), prepared)
	require.NoError(t, err)
	return result
}
