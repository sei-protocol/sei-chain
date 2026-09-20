package evmonly

import (
	"errors"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"
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

// errTestReceiptWriteFailed is the failure a test receipt store reports from SetReceipts.
var errTestReceiptWriteFailed = errors.New("receipt write failed")

// gatedCommitStore holds every state commit until the test releases it, so receipts can be
// observed landing while the state commit is still in flight.
type gatedCommitStore struct {
	*recordingGigaStore
	release chan struct{}
}

func (s *gatedCommitStore) CommitStateChanges(blockNum int64, changeset []*proto.NamedChangeSet) error {
	<-s.release
	return s.recordingGigaStore.CommitStateChanges(blockNum, changeset)
}

// A block's receipts are readable once AwaitReceipts returns, whether or not its state commit has
// landed: that is what lets the RPC head advance behind receipts alone.
func TestAwaitReceiptsReturnsBeforeTheStateCommitLands(t *testing.T) {
	chainID := big.NewInt(testChainID)
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	sender := crypto.PubkeyToAddress(key.PublicKey)
	recipient := testAddress(0xa9)

	snapshot := newMemoryGigaSnapshot(40)
	snapshot.setBalance(sender, big.NewInt(testFundedBalanceWei))
	store := &gatedCommitStore{recordingGigaStore: &recordingGigaStore{snapshot: snapshot}, release: make(chan struct{})}
	receipts := NewMemoryReceiptStore()
	executor := NewExecutor(Config{BlockResultPoolSize: 1}, withTestStores(store, receipts, noopChangeSetEncoder))
	defer executor.Close()

	rawTx := signLegacyTx(t, key, chainID, 0, &recipient, big.NewInt(7), nil)
	result := executePipelinedBlock(t, executor, chainID, 41, rawTx)
	txHash := result.Txs[0].Hash
	result.Release()

	require.NoError(t, executor.AwaitReceipts())
	stored, err := receipts.GetReceipt(newReceiptContext(t.Context(), 41), txHash)
	require.NoError(t, err)
	require.Equal(t, uint64(41), stored.BlockNumber)
	require.Empty(t, store.commits, "the state commit is still held")
	// The result is needed only until its receipts are encoded, so the pool has it back while
	// the commit is still running.
	require.Equal(t, BlockResultPoolStats{Capacity: 1, Available: 1}, executor.ResultPoolStats())

	close(store.release)
	require.NoError(t, executor.AwaitCommits())
	require.Equal(t, []int64{41}, store.commitBlock)
}

// An executor without a receipt store commits state only: AwaitReceipts has nothing to wait on
// and the result goes back to the pool as soon as the block returns.
func TestNoReceiptStoreCommitsStateWithoutWaitingOnReceipts(t *testing.T) {
	chainID := big.NewInt(testChainID)
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	sender := crypto.PubkeyToAddress(key.PublicKey)
	recipient := testAddress(0xa9)

	snapshot := newMemoryGigaSnapshot(40)
	snapshot.setBalance(sender, big.NewInt(testFundedBalanceWei))
	store := &gatedCommitStore{recordingGigaStore: &recordingGigaStore{snapshot: snapshot}, release: make(chan struct{})}
	executor := NewExecutor(Config{BlockResultPoolSize: 1}, withTestStores(store, nil, noopChangeSetEncoder))
	defer executor.Close()

	rawTx := signLegacyTx(t, key, chainID, 0, &recipient, big.NewInt(7), nil)
	result := executePipelinedBlock(t, executor, chainID, 41, rawTx)
	require.Equal(t, uint64(1), result.Receipts[0].Status, "receipts are still produced for the block result")
	result.Release()

	require.NoError(t, executor.AwaitReceipts())
	require.Empty(t, store.commits, "the state commit is still held")
	require.Equal(t, BlockResultPoolStats{Capacity: 1, Available: 1}, executor.ResultPoolStats())

	close(store.release)
	require.NoError(t, executor.AwaitCommits())
	require.Equal(t, []int64{41}, store.commitBlock)
}

// signallingReceiptStore reports each block whose receipts it was handed on written.
type signallingReceiptStore struct {
	*MemoryReceiptStore
	written chan int64
}

func (s *signallingReceiptStore) SetReceipts(ctx sdk.Context, records []receipt.ReceiptRecord) error {
	if err := s.MemoryReceiptStore.SetReceipts(ctx, records); err != nil {
		return err
	}
	s.written <- ctx.BlockHeight()
	return nil
}

// A block's receipts are written while the loop is still waiting on the previous block's commit,
// rather than after it: the write needs only the result.
func TestReceiptWriteStartsBeforeThePreviousCommitLands(t *testing.T) {
	chainID := big.NewInt(testChainID)
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	sender := crypto.PubkeyToAddress(key.PublicKey)
	recipient := testAddress(0xa9)

	snapshot := newMemoryGigaSnapshot(40)
	snapshot.setBalance(sender, big.NewInt(testFundedBalanceWei))
	store := &gatedCommitStore{recordingGigaStore: &recordingGigaStore{snapshot: snapshot}, release: make(chan struct{})}
	receipts := &signallingReceiptStore{MemoryReceiptStore: NewMemoryReceiptStore(), written: make(chan int64, 2)}
	// A store-reading block encoder makes the loop wait for the previous commit before it runs.
	blockEncoder := func(BlockContext, *BlockResult) ([]*proto.NamedChangeSet, error) { return nil, nil }
	executor := NewExecutor(Config{},
		withTestStores(store, receipts, noopChangeSetEncoder),
		WithBlockChangeSetEncoder(blockEncoder))
	defer executor.Close()

	executePipelinedBlock(t, executor, chainID, 41,
		signLegacyTx(t, key, chainID, 0, &recipient, big.NewInt(7), nil)).Release()
	require.NoError(t, executor.AwaitReceipts())
	require.Equal(t, int64(41), <-receipts.written)

	// Block 42 blocks on the loop until 41's commit is released.
	blockCtx := blockContext(chainID)
	blockCtx.Number = 42
	prepared, err := executor.PrepareBlock(t.Context(), BlockRequest{Context: blockCtx,
		Txs: [][]byte{signLegacyTx(t, key, chainID, 1, &recipient, big.NewInt(9), nil)}})
	require.NoError(t, err)
	executed := make(chan error, 1)
	go func() {
		result, err := executor.ExecutePreparedBlock(t.Context(), prepared)
		if err == nil {
			result.Release()
		}
		executed <- err
	}()

	require.Equal(t, int64(42), <-receipts.written, "block 42's receipts are written while its loop waits")
	require.Empty(t, store.commits, "the previous commit is still held")
	select {
	case err := <-executed:
		t.Fatalf("block 42 returned before the previous commit landed: %v", err)
	default:
	}

	close(store.release)
	require.NoError(t, <-executed)
	require.NoError(t, executor.AwaitCommits())
	require.Equal(t, []int64{41, 42}, store.commitBlock)
}

// A receipt write that fails is a failed block: the state commit is not attempted and both waiters
// report it.
func TestFailedReceiptWriteFailsTheBlockBeforeItsStateCommit(t *testing.T) {
	chainID := big.NewInt(testChainID)
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	sender := crypto.PubkeyToAddress(key.PublicKey)
	recipient := testAddress(0xa9)

	snapshot := newMemoryGigaSnapshot(40)
	snapshot.setBalance(sender, big.NewInt(testFundedBalanceWei))
	store := &recordingGigaStore{snapshot: snapshot}
	executor := NewExecutor(Config{}, withTestStores(store, &failingReceiptStore{MemoryReceiptStore: NewMemoryReceiptStore(), err: errTestReceiptWriteFailed}, noopChangeSetEncoder))
	defer executor.Close()

	executePipelinedBlock(t, executor, chainID, 41,
		signLegacyTx(t, key, chainID, 0, &recipient, big.NewInt(7), nil))

	require.ErrorIs(t, executor.AwaitReceipts(), errTestReceiptWriteFailed)
	require.ErrorIs(t, executor.AwaitCommits(), errTestReceiptWriteFailed)
	require.Empty(t, store.commits, "state must not be committed for a block whose receipts were not")
}

// The block after a failed receipt write fails too, before its own receipts are attempted: a store
// whose version has moved past a block whose receipts it never got would claim them as written.
func TestReceiptWriteAfterAFailedOneIsNotAttempted(t *testing.T) {
	chainID := big.NewInt(testChainID)
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	sender := crypto.PubkeyToAddress(key.PublicKey)
	recipient := testAddress(0xa9)

	snapshot := newMemoryGigaSnapshot(40)
	snapshot.setBalance(sender, big.NewInt(testFundedBalanceWei))
	store := &recordingGigaStore{snapshot: snapshot}
	receipts := &failingReceiptStore{MemoryReceiptStore: NewMemoryReceiptStore(), err: errTestReceiptWriteFailed}
	executor := NewExecutor(Config{}, withTestStores(store, receipts, noopChangeSetEncoder))
	defer executor.Close()

	executePipelinedBlock(t, executor, chainID, 41,
		signLegacyTx(t, key, chainID, 0, &recipient, big.NewInt(7), nil))
	require.ErrorIs(t, executor.AwaitReceipts(), errTestReceiptWriteFailed)

	// The store would accept block 42's receipts now; the executor must not offer them.
	receipts.err = nil
	blockCtx := blockContext(chainID)
	blockCtx.Number = 42
	rawTx := signLegacyTx(t, key, chainID, 1, &recipient, big.NewInt(9), nil)
	prepared, err := executor.PrepareBlock(t.Context(), BlockRequest{Context: blockCtx, Txs: [][]byte{rawTx}})
	require.NoError(t, err)
	_, err = executor.ExecutePreparedBlock(t.Context(), prepared)
	require.ErrorIs(t, err, errTestReceiptWriteFailed)

	require.ErrorIs(t, executor.AwaitReceipts(), errTestReceiptWriteFailed)
	_, err = receipts.GetReceipt(newReceiptContext(t.Context(), 42), decodeTx(t, rawTx).Hash())
	require.ErrorIs(t, err, receipt.ErrNotFound, "block 42's receipts must not land over the hole at 41")
	require.Empty(t, store.commits)
}

// droppingReceiptStore accepts every write and applies none of them, the way a queued store behaves
// once an earlier write has failed: it takes the block, waits out its queue, and its version never
// reaches it.
type droppingReceiptStore struct {
	*MemoryReceiptStore
}

func (s *droppingReceiptStore) SetReceipts(sdk.Context, []receipt.ReceiptRecord) error { return nil }

func (s *droppingReceiptStore) WaitForPendingWrites() {}

// A write the store accepted but never applied is this block's failure, not the next one's.
func TestReceiptWriteThatNeverLandsFailsTheBlock(t *testing.T) {
	chainID := big.NewInt(testChainID)
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	sender := crypto.PubkeyToAddress(key.PublicKey)
	recipient := testAddress(0xa9)

	snapshot := newMemoryGigaSnapshot(40)
	snapshot.setBalance(sender, big.NewInt(testFundedBalanceWei))
	store := &recordingGigaStore{snapshot: snapshot}
	receipts := &droppingReceiptStore{MemoryReceiptStore: NewMemoryReceiptStore()}
	executor := NewExecutor(Config{}, withTestStores(store, receipts, noopChangeSetEncoder))
	defer executor.Close()

	executePipelinedBlock(t, executor, chainID, 41,
		signLegacyTx(t, key, chainID, 0, &recipient, big.NewInt(7), nil))

	err = executor.AwaitReceipts()
	require.ErrorContains(t, err, "receipts for block 41 did not land")
	require.ErrorIs(t, executor.AwaitCommits(), err)
	require.Empty(t, store.commits, "state must not be committed for a block whose receipts were not")
}

// The state encoder runs after the block result may have been reused, so it must be handed the
// block's own changes and nothing else.
func TestBackgroundEncoderSeesTheBlocksOwnChanges(t *testing.T) {
	chainID := big.NewInt(testChainID)
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	sender := crypto.PubkeyToAddress(key.PublicKey)
	recipient := testAddress(0xa9)

	snapshot := newMemoryGigaSnapshot(40)
	snapshot.setBalance(sender, big.NewInt(testFundedBalanceWei))
	store := &recordingGigaStore{snapshot: snapshot}
	var encoded []StateChangeSet
	encoder := func(changes StateChangeSet) ([]*proto.NamedChangeSet, error) {
		encoded = append(encoded, changes)
		return noopChangeSetEncoder(changes)
	}
	executor := NewExecutor(Config{BlockResultPoolSize: 1}, withTestStores(store, NewMemoryReceiptStore(), encoder))
	defer executor.Close()

	first := executePipelinedBlock(t, executor, chainID, 41,
		signLegacyTx(t, key, chainID, 0, &recipient, big.NewInt(7), nil))
	want := *first.ChangeSet.clone()
	first.Release()
	require.NoError(t, executor.AwaitReceipts())
	// Takes the pooled result back while block 41's encoder may still be running.
	second := executePipelinedBlock(t, executor, chainID, 42,
		signLegacyTx(t, key, chainID, 1, &recipient, big.NewInt(9), nil))
	second.Release()

	require.NoError(t, executor.AwaitCommits())
	require.Len(t, encoded, 2)
	require.Equal(t, want, encoded[0])
	require.Equal(t, []int64{41, 42}, store.commitBlock)
}

// errTestEncoderFailed is the failure a test block encoder reports.
var errTestEncoderFailed = errors.New("block encoder failed")

// gatedReceiptStore holds every receipt write until release is closed.
type gatedReceiptStore struct {
	*MemoryReceiptStore
	release chan struct{}
}

func (s *gatedReceiptStore) SetReceipts(ctx sdk.Context, records []receipt.ReceiptRecord) error {
	<-s.release
	return s.MemoryReceiptStore.SetReceipts(ctx, records)
}

// A block that fails on the loop after its receipts were handed off has no state commit for Close to
// drain, so Close waits for the receipt write itself and the receipts are readable once it returns.
func TestCloseWaitsForTheReceiptsOfABlockThatFailedAfterHandingThemOff(t *testing.T) {
	chainID := big.NewInt(testChainID)
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	sender := crypto.PubkeyToAddress(key.PublicKey)
	recipient := testAddress(0xa9)

	snapshot := newMemoryGigaSnapshot(40)
	snapshot.setBalance(sender, big.NewInt(testFundedBalanceWei))
	store := &recordingGigaStore{snapshot: snapshot}
	receipts := &gatedReceiptStore{MemoryReceiptStore: NewMemoryReceiptStore(), release: make(chan struct{})}
	blockEncoder := func(BlockContext, *BlockResult) ([]*proto.NamedChangeSet, error) {
		return nil, errTestEncoderFailed
	}
	executor := NewExecutor(Config{},
		withTestStores(store, receipts, noopChangeSetEncoder),
		WithBlockChangeSetEncoder(blockEncoder))

	rawTx := signLegacyTx(t, key, chainID, 0, &recipient, big.NewInt(7), nil)
	blockCtx := blockContext(chainID)
	blockCtx.Number = 41
	prepared, err := executor.PrepareBlock(t.Context(), BlockRequest{Context: blockCtx, Txs: [][]byte{rawTx}})
	require.NoError(t, err)
	_, err = executor.ExecutePreparedBlock(t.Context(), prepared)
	require.ErrorIs(t, err, errTestEncoderFailed)

	closed := make(chan struct{})
	go func() {
		executor.Close()
		close(closed)
	}()
	select {
	case <-closed:
		t.Fatal("Close returned while the receipt write was still held")
	case <-time.After(50 * time.Millisecond):
	}

	close(receipts.release)
	<-closed
	stored, err := receipts.GetReceipt(newReceiptContext(t.Context(), 41), decodeTx(t, rawTx).Hash())
	require.NoError(t, err)
	require.Equal(t, uint64(41), stored.BlockNumber)
	require.Empty(t, store.commits, "a block that failed on the loop commits no state")
}
