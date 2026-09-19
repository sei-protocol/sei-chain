package evmonly

import (
	"errors"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
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

// retiringOnOpenStore retires the in-flight commit while a block is opening its view, which is the
// exact window a concurrent AwaitCommits or Close occupies.
type retiringOnOpenStore struct {
	*recordingGigaStore
	retire func()
}

func (s *retiringOnOpenStore) OpenView() gigatypes.StateView {
	if s.retire != nil {
		s.retire()
	}
	return s.recordingGigaStore.OpenView()
}

// Retiring the previous commit must never cost the next block the state its predecessor produced.
// The block reads the pending changes before opening its view, so a retire landing in between
// cannot leave it with a view that predates those changes and no overlay to supply them.
//
// The store here never applies a commit to its view, so the overlay is the only source of block
// 41's payment: if it were read after the view, this block would execute against nothing.
func TestRetiringACommitWhileOpeningAViewKeepsThePreviousBlocksState(t *testing.T) {
	chainID := big.NewInt(testChainID)
	funderKey, err := crypto.GenerateKey()
	require.NoError(t, err)
	funder := crypto.PubkeyToAddress(funderKey.PublicKey)
	middleKey, err := crypto.GenerateKey()
	require.NoError(t, err)
	middle := crypto.PubkeyToAddress(middleKey.PublicKey)
	last := testAddress(0xc3)

	snapshot := newMemoryGigaSnapshot(40)
	snapshot.setBalance(funder, new(big.Int).Mul(big.NewInt(testFundedBalanceWei), big.NewInt(100)))
	store := &retiringOnOpenStore{recordingGigaStore: &recordingGigaStore{snapshot: snapshot}}
	executor := NewExecutor(Config{}, withTestStores(store, NewMemoryReceiptStore(), noopChangeSetEncoder))
	defer executor.Close()

	payment := new(big.Int).Mul(big.NewInt(testFundedBalanceWei), big.NewInt(10))
	executePipelinedBlock(t, executor, chainID, 41,
		signLegacyTx(t, funderKey, chainID, 0, &middle, payment, nil))

	retires := 0
	store.retire = func() {
		retires++
		require.NoError(t, executor.AwaitCommits())
	}
	second := executePipelinedBlock(t, executor, chainID, 42,
		signLegacyTx(t, middleKey, chainID, 0, &last, big.NewInt(1_000), nil))

	require.Equal(t, 1, retires, "the retire must land while the view is being opened")
	require.Equal(t, uint64(1), second.Txs[0].Status,
		"block 42 lost what block 41 paid it when the commit was retired underneath it")
	require.Contains(t, second.ChangeSet.Balances, BalanceChange{Address: last, Balance: big.NewInt(1_000)})
}

// The store's view never advances, so the account state a block produced is only reachable through
// the pending overlay until AwaitCommits. A latest-account read must report it without settling.
func TestReadLatestAccountSeesTheBlockWhoseCommitIsInFlight(t *testing.T) {
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

	before, err := executor.ReadLatestAccount(sender)
	require.NoError(t, err)
	require.Equal(t, LatestAccount{Balance: big.NewInt(testFundedBalanceWei)}, before)

	result := executePipelinedBlock(t, executor, chainID, 41,
		signLegacyTx(t, key, chainID, 0, &recipient, big.NewInt(7), nil))
	require.Equal(t, uint64(1), result.Txs[0].Status)

	got, err := executor.ReadLatestAccount(sender)
	require.NoError(t, err)
	require.Equal(t, uint64(1), got.Nonce)
	require.Equal(t, -1, got.Balance.Cmp(big.NewInt(testFundedBalanceWei)), "gas and value must be deducted")
	paid, err := executor.ReadLatestAccount(recipient)
	require.NoError(t, err)
	require.Equal(t, LatestAccount{Balance: big.NewInt(7)}, paid)

	require.NoError(t, executor.AwaitCommits())
}

// A commit that failed leaves the store behind the run, so a latest-account read reports the
// failure rather than state that omits the failed block.
func TestReadLatestAccountReportsAFailedCommit(t *testing.T) {
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

	_, err = executor.ReadLatestAccount(sender)
	require.ErrorIs(t, err, errTestCommitFailed)
}

// A failed commit is reported as soon as the commit has returned, before any waiter has retired it.
func TestReadLatestAccountReportsAFailedCommitNobodyHasAwaited(t *testing.T) {
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
	executor.pipelineMu.Lock()
	done := executor.pipelineDone
	executor.pipelineMu.Unlock()
	require.NotNil(t, done)
	<-done

	_, err = executor.ReadLatestAccount(sender)
	require.ErrorIs(t, err, errTestCommitFailed)
}

// A block that lands its commit and starts the next one between a reader's pending read and its
// view must not leave the reader replaying the older block over the newer state; the read starts
// over instead.
func TestReadLatestAccountRestartsWhenABlockLandsUnderIt(t *testing.T) {
	chainID := big.NewInt(testChainID)
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	sender := crypto.PubkeyToAddress(key.PublicKey)
	recipient := testAddress(0xa9)

	snapshot := newMemoryGigaSnapshot(40)
	snapshot.setBalance(sender, big.NewInt(testFundedBalanceWei))
	store := &retiringOnOpenStore{recordingGigaStore: &recordingGigaStore{snapshot: snapshot}}
	executor := NewExecutor(Config{}, withTestStores(store, NewMemoryReceiptStore(), noopChangeSetEncoder))
	defer executor.Close()

	executePipelinedBlock(t, executor, chainID, 41,
		signLegacyTx(t, key, chainID, 0, &recipient, big.NewInt(7), nil))

	// The reader has block 41 in hand as pending; block 42 lands underneath while its view opens.
	opens := 0
	store.retire = func() {
		opens++
		if opens == 1 {
			store.retire = nil
			executePipelinedBlock(t, executor, chainID, 42,
				signLegacyTx(t, key, chainID, 1, &recipient, big.NewInt(7), nil))
		}
	}
	got, err := executor.ReadLatestAccount(sender)
	require.NoError(t, err)
	require.Equal(t, uint64(2), got.Nonce, "the read must reflect block 42, not replay block 41 over it")
	require.NoError(t, executor.AwaitCommits())
}

// A reader whose pending block is retired, and then followed by further blocks that land, between
// its pending read and its view must not replay that retired block over the newer state, even though
// nothing is pending any more by the time it looks again.
func TestReadLatestAccountRestartsWhenItsPendingBlockRetiresUnderIt(t *testing.T) {
	chainID := big.NewInt(testChainID)
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	sender := crypto.PubkeyToAddress(key.PublicKey)
	recipient := testAddress(0xa9)

	snapshot := newMemoryGigaSnapshot(40)
	snapshot.setBalance(sender, big.NewInt(testFundedBalanceWei))
	store := &retiringOnOpenStore{recordingGigaStore: &recordingGigaStore{snapshot: snapshot}}
	executor := NewExecutor(Config{}, withTestStores(store, NewMemoryReceiptStore(), noopChangeSetEncoder))
	defer executor.Close()

	executePipelinedBlock(t, executor, chainID, 41,
		signLegacyTx(t, key, chainID, 0, &recipient, big.NewInt(7), nil))

	// The reader has block 41 in hand as pending; while its view opens, block 42 runs and both
	// blocks land, leaving nothing pending and a store view that already holds them.
	opens := 0
	store.retire = func() {
		opens++
		if opens == 1 {
			store.retire = nil
			executePipelinedBlock(t, executor, chainID, 42,
				signLegacyTx(t, key, chainID, 1, &recipient, big.NewInt(7), nil))
			require.NoError(t, executor.AwaitCommits())
			snapshot.nonces[sender] = 2
		}
	}
	got, err := executor.ReadLatestAccount(sender)
	require.NoError(t, err)
	require.Equal(t, uint64(2), got.Nonce, "the read must not replay retired block 41 over the landed state")
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
