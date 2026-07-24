package evmonly

import (
	"context"
	"crypto/ecdsa"
	"errors"
	"math/big"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
)

const testGasPriceWei = 1_000_000_000

type recordingResultSink struct {
	heights  []uint64
	results  []*BlockResult
	releases []func()
}

func (s *recordingResultSink) StoreBlockResult(_ context.Context, height uint64, result *BlockResult, release func()) error {
	s.heights = append(s.heights, height)
	s.results = append(s.results, result)
	s.releases = append(s.releases, release)
	return nil
}

func TestExecutorEmptyBlock(t *testing.T) {
	executor := NewExecutor(Config{})

	result, err := executor.ExecuteBlock(context.Background(), BlockRequest{
		Context: blockContext(big.NewInt(713715)),
	})

	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestExecutorTransferTx(t *testing.T) {
	chainID := big.NewInt(713715)
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	sender := crypto.PubkeyToAddress(key.PublicKey)
	recipient := common.HexToAddress("0x00000000000000000000000000000000000000a1")

	state := NewMemoryState()
	state.SetBalance(sender, big.NewInt(200_000_000_000_000))

	rawTx := signLegacyTx(t, key, chainID, 0, &recipient, big.NewInt(7), nil)
	executor := NewExecutor(Config{}, WithState(state))

	result, err := executor.ExecuteBlock(context.Background(), BlockRequest{
		Context: blockContext(chainID),
		Txs:     [][]byte{rawTx},
	})

	require.NoError(t, err)
	require.Len(t, result.Txs, 1)
	require.Len(t, result.Receipts, 1)
	require.Equal(t, ethtypes.ReceiptStatusSuccessful, result.Txs[0].Status)
	require.Equal(t, uint64(21_000), result.GasUsed)
	require.NotEmpty(t, result.ChangeSet.Balances)

	state.ApplyChangeSet(result.ChangeSet)
	require.Equal(t, big.NewInt(7), state.GetBalance(recipient))
	require.Equal(t, uint64(1), state.GetNonce(sender))
}

func TestExecutorInvokesResultSink(t *testing.T) {
	chainID := big.NewInt(713715)
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	sender := crypto.PubkeyToAddress(key.PublicKey)
	recipient := common.HexToAddress("0x00000000000000000000000000000000000000a7")

	state := NewMemoryState()
	state.SetBalance(sender, big.NewInt(200_000_000_000_000))
	sink := &recordingResultSink{}

	rawTx := signLegacyTx(t, key, chainID, 0, &recipient, big.NewInt(7), nil)
	executor := NewExecutor(Config{}, WithState(state), WithResultSink(sink))
	ctx := blockContext(chainID)
	ctx.Number = 77

	result, err := executor.ExecuteBlock(context.Background(), BlockRequest{
		Context: ctx,
		Txs:     [][]byte{rawTx},
	})

	require.NoError(t, err)
	require.Len(t, sink.results, 1)
	require.Len(t, sink.releases, 1)
	require.Equal(t, []uint64{ctx.Number}, sink.heights)
	require.Same(t, result, sink.results[0])
	sink.releases[0]()
}

func TestExecutorPooledResultRelease(t *testing.T) {
	chainID := big.NewInt(713715)
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	sender := crypto.PubkeyToAddress(key.PublicKey)
	recipient := common.HexToAddress("0x00000000000000000000000000000000000000a8")

	state := NewMemoryState()
	state.SetBalance(sender, big.NewInt(200_000_000_000_000))
	sink := &recordingResultSink{}
	executor := NewExecutor(Config{BlockResultPoolSize: 1}, WithState(state), WithResultSink(sink))
	rawTx := signLegacyTx(t, key, chainID, 0, &recipient, big.NewInt(7), nil)
	req := BlockRequest{Context: blockContext(chainID), Txs: [][]byte{rawTx}}

	first, err := executor.ExecuteBlock(context.Background(), req)
	require.NoError(t, err)
	require.Same(t, first, sink.results[0])
	require.NotNil(t, sink.releases[0])
	sink.releases[0]()
	first.Release()

	second, err := executor.ExecuteBlock(context.Background(), req)
	require.NoError(t, err)
	require.Same(t, first, second)
	sink.releases[1]()
	second.Release()
}

func TestExecutorPooledResultReleaseIsConcurrentIdempotent(t *testing.T) {
	chainID := big.NewInt(713715)
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	sender := crypto.PubkeyToAddress(key.PublicKey)
	recipient := testAddress(0xd1)

	state := NewMemoryState()
	state.SetBalance(sender, big.NewInt(200_000_000_000_000))
	executor := NewExecutor(Config{BlockResultPoolSize: 1}, WithState(state))
	rawTx := signLegacyTx(t, key, chainID, 0, &recipient, big.NewInt(7), nil)
	req := BlockRequest{Context: blockContext(chainID), Txs: [][]byte{rawTx}}

	result, err := executor.ExecuteBlock(context.Background(), req)
	require.NoError(t, err)

	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result.Release()
		}()
	}
	wg.Wait()

	next, err := executor.ExecuteBlock(context.Background(), req)
	require.NoError(t, err)
	require.Same(t, result, next)
	next.Release()
}

func TestExecutorCloseDisablesOCC(t *testing.T) {
	chainID := big.NewInt(713715)
	rawTxs := make([][]byte, 0, 2)
	state := NewMemoryState()
	for i := range 2 {
		key, err := crypto.GenerateKey()
		require.NoError(t, err)
		sender := crypto.PubkeyToAddress(key.PublicKey)
		recipient := testAddress(byte(0xd2 + i))
		state.SetBalance(sender, big.NewInt(1_000_000_000))
		rawTxs = append(rawTxs, signLegacyTxWithGasPrice(t, key, chainID, 0, &recipient, big.NewInt(1), nil, 100_000, big.NewInt(0)))
	}
	executor := NewExecutor(Config{MinGasPrice: big.NewInt(0), OCCWorkers: 2}, WithState(state))
	executor.Close()

	result, err := executor.ExecuteBlock(context.Background(), BlockRequest{
		Context: blockContext(chainID),
		Txs:     rawTxs,
	})

	require.NoError(t, err)
	require.False(t, result.OCCStats.Attempted)
}

func TestOCCWorkerPoolCloseWaitsForInFlightRun(t *testing.T) {
	pool := newOCCWorkerPool(2)
	started := make(chan struct{})
	release := make(chan struct{})
	runDone := make(chan error, 1)

	go func() {
		runDone <- pool.Run(context.Background(), []occTxRange{{start: 0, end: 1}}, func(context.Context, occTxRange) error {
			close(started)
			<-release
			return nil
		})
	}()

	<-started
	closeDone := make(chan struct{})
	go func() {
		pool.Close()
		close(closeDone)
	}()

	select {
	case <-closeDone:
		t.Fatal("Close returned before the in-flight Run completed")
	default:
	}

	close(release)
	require.NoError(t, <-runDone)
	select {
	case <-closeDone:
	case <-time.After(time.Second):
		t.Fatal("Close did not return after Run completed")
	}
	require.ErrorIs(t, pool.Run(context.Background(), []occTxRange{{start: 0, end: 1}}, func(context.Context, occTxRange) error {
		return nil
	}), errOCCWorkerPoolClosed)
}

func TestPrepareBlockParallelParsePreservesOrderAndFirstError(t *testing.T) {
	chainID := big.NewInt(713715)
	validRaw := make([][]byte, 4)
	expectedSenders := make([]common.Address, 4)
	for i := range validRaw {
		key, err := crypto.GenerateKey()
		require.NoError(t, err)
		expectedSenders[i] = crypto.PubkeyToAddress(key.PublicKey)
		recipient := testAddress(byte(0xe0 + i))
		validRaw[i] = signLegacyTx(t, key, chainID, uint64(i), &recipient, big.NewInt(1), nil)
	}

	executor := NewExecutor(Config{ParseWorkers: 4})
	prepared, err := executor.PrepareBlock(context.Background(), BlockRequest{
		Context: blockContext(chainID),
		Txs:     validRaw,
	})
	require.NoError(t, err)
	for i := range expectedSenders {
		require.Equal(t, expectedSenders[i], prepared.Txs[i].Sender)
	}

	malformed := append([][]byte(nil), validRaw...)
	malformed[1] = []byte{0x01}
	malformed[3] = []byte{0x02}
	_, err = executor.PrepareBlock(context.Background(), BlockRequest{
		Context: blockContext(chainID),
		Txs:     malformed,
	})
	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), "parse tx 1"), err.Error())
}

func TestExecutorDynamicFeeTx(t *testing.T) {
	chainID := big.NewInt(713715)
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	sender := crypto.PubkeyToAddress(key.PublicKey)
	recipient := common.HexToAddress("0x00000000000000000000000000000000000000a2")

	state := NewMemoryState()
	state.SetBalance(sender, big.NewInt(200_000_000_000_000))

	rawTx := signDynamicFeeTx(t, key, chainID, 0, &recipient, big.NewInt(11), nil)
	executor := NewExecutor(Config{}, WithState(state))

	result, err := executor.ExecuteBlock(context.Background(), BlockRequest{
		Context: blockContext(chainID),
		Txs:     [][]byte{rawTx},
	})

	require.NoError(t, err)
	require.Len(t, result.Txs, 1)
	require.Equal(t, uint8(ethtypes.DynamicFeeTxType), result.Receipts[0].Type)

	state.ApplyChangeSet(result.ChangeSet)
	require.Equal(t, big.NewInt(11), state.GetBalance(recipient))
}

func TestExecutorRejectsBlobTxUntilBlockAccountingIsWired(t *testing.T) {
	chainID := big.NewInt(713715)
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	sender := crypto.PubkeyToAddress(key.PublicKey)
	recipient := testAddress(0xb5)
	blobBaseFee := big.NewInt(3)
	blobHash := common.Hash{0x01}

	state := NewMemoryState()
	state.SetBalance(sender, big.NewInt(1_000_000_000_000_000))
	rawTx := signBlobTxWithFees(
		t,
		key,
		chainID,
		0,
		recipient,
		big.NewInt(1),
		nil,
		big.NewInt(1),
		big.NewInt(3),
		big.NewInt(3),
		100_000,
		[]common.Hash{blobHash},
	)
	ctx := blockContext(chainID)
	ctx.BaseFee = big.NewInt(2)
	ctx.BlobBaseFee = blobBaseFee

	executor := NewExecutor(Config{MinGasPrice: big.NewInt(0)}, WithState(state))
	result, err := executor.ExecuteBlock(context.Background(), BlockRequest{
		Context: ctx,
		Txs:     [][]byte{rawTx},
	})

	require.ErrorIs(t, err, errUnsupportedBlobTx)
	require.Nil(t, result)
	require.Equal(t, big.NewInt(0), state.GetBalance(recipient))

	tx, sender, err := parseTx(rawTx, ethtypes.LatestSignerForChainID(chainID))
	require.NoError(t, err)
	result, err = executor.ExecutePreparedBlock(context.Background(), PreparedBlock{
		Context: ctx,
		Txs: []PreparedTx{{
			Tx:     tx,
			Sender: sender,
		}},
	})

	require.ErrorIs(t, err, errUnsupportedBlobTx)
	require.Nil(t, result)
	require.Equal(t, big.NewInt(0), state.GetBalance(recipient))
}

func TestExecutorRequiresBaseFeeAfterLondon(t *testing.T) {
	chainID := big.NewInt(713715)
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	sender := crypto.PubkeyToAddress(key.PublicKey)
	recipient := testAddress(0xb4)

	state := NewMemoryState()
	state.SetBalance(sender, big.NewInt(200_000_000_000_000))
	rawTx := signLegacyTx(t, key, chainID, 0, &recipient, big.NewInt(1), nil)
	ctx := blockContext(chainID)
	ctx.BaseFee = nil

	result, err := NewExecutor(Config{}, WithState(state)).ExecuteBlock(context.Background(), BlockRequest{
		Context: ctx,
		Txs:     [][]byte{rawTx},
	})

	require.ErrorIs(t, err, errMissingBaseFee)
	require.Nil(t, result)
	require.Equal(t, big.NewInt(0), state.GetBalance(recipient))
}

func TestExecutorRequiresBlobBaseFeeAfterCancun(t *testing.T) {
	chainID := big.NewInt(713715)
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	sender := crypto.PubkeyToAddress(key.PublicKey)
	recipient := testAddress(0xb6)

	state := NewMemoryState()
	state.SetBalance(sender, big.NewInt(200_000_000_000_000))
	rawTx := signLegacyTx(t, key, chainID, 0, &recipient, big.NewInt(1), nil)
	ctx := blockContext(chainID)
	ctx.BlobBaseFee = nil

	result, err := NewExecutor(Config{}, WithState(state)).ExecuteBlock(context.Background(), BlockRequest{
		Context: ctx,
		Txs:     [][]byte{rawTx},
	})

	require.ErrorIs(t, err, errMissingBlobBaseFee)
	require.Nil(t, result)
	require.Equal(t, big.NewInt(0), state.GetBalance(recipient))
}

func TestExecutorOCCNonConflictingTransfersMatchSequential(t *testing.T) {
	chainID := big.NewInt(713715)
	txCount := 12
	rawTxs := make([][]byte, 0, txCount)
	senders := make([]common.Address, 0, txCount)
	recipients := make([]common.Address, 0, txCount)
	seqState := NewMemoryState()
	occState := NewMemoryState()

	for i := 0; i < txCount; i++ {
		key, err := crypto.GenerateKey()
		require.NoError(t, err)
		sender := crypto.PubkeyToAddress(key.PublicKey)
		recipient := common.BigToAddress(big.NewInt(int64(10_000 + i)))
		senders = append(senders, sender)
		recipients = append(recipients, recipient)
		seqState.SetBalance(sender, big.NewInt(1_000_000))
		occState.SetBalance(sender, big.NewInt(1_000_000))
		rawTxs = append(rawTxs, signLegacyTxWithGasPrice(t, key, chainID, 0, &recipient, big.NewInt(7), nil, 100_000, big.NewInt(0)))
	}

	cfg := Config{MinGasPrice: big.NewInt(0)}
	seqExecutor := NewExecutor(cfg, WithState(seqState))
	occExecutor := NewExecutor(Config{MinGasPrice: big.NewInt(0), OCCWorkers: 4}, WithState(occState))
	req := BlockRequest{Context: blockContext(chainID), Txs: rawTxs}

	seqResult, err := seqExecutor.ExecuteBlock(context.Background(), req)
	require.NoError(t, err)
	occResult, err := occExecutor.ExecuteBlock(context.Background(), req)
	require.NoError(t, err)

	require.Equal(t, seqResult.GasUsed, occResult.GasUsed)
	require.Len(t, occResult.Txs, txCount)
	require.Len(t, occResult.Receipts, txCount)
	require.True(t, occResult.OCCStats.Attempted)
	require.False(t, occResult.OCCStats.Fallback)
	require.Zero(t, occResult.OCCStats.ConflictCount)
	for i := range txCount {
		require.Equal(t, seqResult.Txs[i].Hash, occResult.Txs[i].Hash)
		require.Equal(t, seqResult.Txs[i].Status, occResult.Txs[i].Status)
		require.Equal(t, seqResult.Receipts[i].CumulativeGasUsed, occResult.Receipts[i].CumulativeGasUsed)
	}

	seqState.ApplyChangeSet(seqResult.ChangeSet)
	occState.ApplyChangeSet(occResult.ChangeSet)
	for i := range txCount {
		require.Equal(t, seqState.GetBalance(senders[i]), occState.GetBalance(senders[i]))
		require.Equal(t, seqState.GetNonce(senders[i]), occState.GetNonce(senders[i]))
		require.Equal(t, seqState.GetBalance(recipients[i]), occState.GetBalance(recipients[i]))
	}
}

func TestExecutorOCCConflictingTransfersMatchSequential(t *testing.T) {
	chainID := big.NewInt(713715)
	txCount := 8
	recipient := testAddress(0xdd)
	rawTxs := make([][]byte, 0, txCount)
	seqState := NewMemoryState()
	occState := NewMemoryState()

	for i := 0; i < txCount; i++ {
		key, err := crypto.GenerateKey()
		require.NoError(t, err)
		sender := crypto.PubkeyToAddress(key.PublicKey)
		seqState.SetBalance(sender, big.NewInt(1_000_000))
		occState.SetBalance(sender, big.NewInt(1_000_000))
		rawTxs = append(rawTxs, signLegacyTxWithGasPrice(t, key, chainID, 0, &recipient, big.NewInt(3), nil, 100_000, big.NewInt(0)))
	}

	req := BlockRequest{Context: blockContext(chainID), Txs: rawTxs}
	seqResult, err := NewExecutor(Config{MinGasPrice: big.NewInt(0)}, WithState(seqState)).ExecuteBlock(context.Background(), req)
	require.NoError(t, err)
	occResult, err := NewExecutor(Config{MinGasPrice: big.NewInt(0), OCCWorkers: 4}, WithState(occState)).ExecuteBlock(context.Background(), req)
	require.NoError(t, err)

	seqState.ApplyChangeSet(seqResult.ChangeSet)
	occState.ApplyChangeSet(occResult.ChangeSet)
	require.Equal(t, seqResult.GasUsed, occResult.GasUsed)
	require.True(t, occResult.OCCStats.Attempted)
	require.False(t, occResult.OCCStats.Fallback)
	require.Equal(t, "conflict", occResult.OCCStats.FallbackReason)
	require.Greater(t, occResult.OCCStats.RerunCount, uint64(0))
	require.Greater(t, occResult.OCCStats.ConflictCount, uint64(0))
	require.NotEmpty(t, occResult.OCCStats.ConflictSamples)
	foundRecipientBalanceConflict := false
	for _, conflict := range occResult.OCCStats.ConflictSamples {
		if conflict.Kind == "balance" && conflict.Address == recipient {
			foundRecipientBalanceConflict = true
			require.Greater(t, conflict.Count, uint64(0))
		}
	}
	require.True(t, foundRecipientBalanceConflict)
	require.Equal(t, seqState.GetBalance(recipient), occState.GetBalance(recipient))
	require.Equal(t, big.NewInt(int64(txCount*3)), occState.GetBalance(recipient))
}

func TestExecutorOCCFeePayingTransfersDoNotConflictOnCoinbase(t *testing.T) {
	chainID := big.NewInt(713715)
	txCount := 4
	rawTxs := make([][]byte, 0, txCount)
	senders := make([]common.Address, 0, txCount)
	recipients := make([]common.Address, 0, txCount)
	seqState := NewMemoryState()
	occState := NewMemoryState()

	for i := 0; i < txCount; i++ {
		key, err := crypto.GenerateKey()
		require.NoError(t, err)
		sender := crypto.PubkeyToAddress(key.PublicKey)
		recipient := common.BigToAddress(big.NewInt(int64(30_000 + i)))
		senders = append(senders, sender)
		recipients = append(recipients, recipient)
		seqState.SetBalance(sender, big.NewInt(1_000_000_000))
		occState.SetBalance(sender, big.NewInt(1_000_000_000))
		rawTxs = append(rawTxs, signLegacyTxWithGasPrice(t, key, chainID, 0, &recipient, big.NewInt(7), nil, 100_000, big.NewInt(1)))
	}

	cfg := Config{MinGasPrice: big.NewInt(0)}
	req := BlockRequest{Context: blockContext(chainID), Txs: rawTxs}
	seqResult, err := NewExecutor(cfg, WithState(seqState)).ExecuteBlock(context.Background(), req)
	require.NoError(t, err)
	occResult, err := NewExecutor(Config{MinGasPrice: big.NewInt(0), OCCWorkers: 4}, WithState(occState)).ExecuteBlock(context.Background(), req)
	require.NoError(t, err)

	require.True(t, occResult.OCCStats.Attempted)
	require.False(t, occResult.OCCStats.Fallback)
	require.Equal(t, seqResult.GasUsed, occResult.GasUsed)

	seqState.ApplyChangeSet(seqResult.ChangeSet)
	occState.ApplyChangeSet(occResult.ChangeSet)
	require.Equal(t, seqState.GetBalance(req.Context.Coinbase), occState.GetBalance(req.Context.Coinbase))
	for i := range txCount {
		require.Equal(t, seqState.GetBalance(senders[i]), occState.GetBalance(senders[i]))
		require.Equal(t, seqState.GetBalance(recipients[i]), occState.GetBalance(recipients[i]))
	}
}

func TestExecutorOCCRerunsWhenLaterTxReadsFeeCreditedCoinbase(t *testing.T) {
	chainID := big.NewInt(713715)
	feePayerKey, err := crypto.GenerateKey()
	require.NoError(t, err)
	readerKey, err := crypto.GenerateKey()
	require.NoError(t, err)
	feePayer := crypto.PubkeyToAddress(feePayerKey.PublicKey)
	reader := crypto.PubkeyToAddress(readerKey.PublicKey)
	recipient := testAddress(0xbe)
	contract := testAddress(0xcf)
	storageKey := testHash(0x0b)

	seqState := NewMemoryState()
	occState := NewMemoryState()
	for _, state := range []*MemoryState{seqState, occState} {
		state.SetBalance(feePayer, big.NewInt(1_000_000_000))
		state.SetBalance(reader, big.NewInt(1_000_000_000))
		state.SetCode(contract, balanceStoreCode(blockContext(chainID).Coinbase, storageKey))
	}

	feeTx := signLegacyTxWithGasPrice(t, feePayerKey, chainID, 0, &recipient, big.NewInt(1), nil, 100_000, big.NewInt(1))
	readCoinbaseTx := signLegacyTxWithGasPrice(t, readerKey, chainID, 0, &contract, big.NewInt(0), nil, 100_000, big.NewInt(0))
	req := BlockRequest{Context: blockContext(chainID), Txs: [][]byte{feeTx, readCoinbaseTx}}
	seqResult, err := NewExecutor(Config{MinGasPrice: big.NewInt(0)}, WithState(seqState)).ExecuteBlock(context.Background(), req)
	require.NoError(t, err)
	occResult, err := NewExecutor(Config{MinGasPrice: big.NewInt(0), OCCWorkers: 2}, WithState(occState)).ExecuteBlock(context.Background(), req)
	require.NoError(t, err)

	require.True(t, occResult.OCCStats.Attempted)
	require.False(t, occResult.OCCStats.Fallback)
	require.Equal(t, occFallbackReasonConflict, occResult.OCCStats.FallbackReason)
	require.Equal(t, uint64(1), occResult.OCCStats.RerunCount)
	foundCoinbaseBalanceRead := false
	for _, conflict := range occResult.OCCStats.ConflictSamples {
		if conflict.Access == "read" && conflict.Kind == "balance" && conflict.Address == req.Context.Coinbase {
			foundCoinbaseBalanceRead = true
		}
	}
	require.True(t, foundCoinbaseBalanceRead)

	seqState.ApplyChangeSet(seqResult.ChangeSet)
	occState.ApplyChangeSet(occResult.ChangeSet)
	require.Equal(t, seqState.GetState(contract, storageKey), occState.GetState(contract, storageKey))
	require.Equal(t, common.BigToHash(big.NewInt(21_000)), occState.GetState(contract, storageKey))
}

func TestExecutorOCCRerunsWhenLaterTxWritesFeeCreditedCoinbase(t *testing.T) {
	chainID := big.NewInt(713715)
	feePayerKey, err := crypto.GenerateKey()
	require.NoError(t, err)
	transferKey, err := crypto.GenerateKey()
	require.NoError(t, err)
	feePayer := crypto.PubkeyToAddress(feePayerKey.PublicKey)
	transferSender := crypto.PubkeyToAddress(transferKey.PublicKey)
	coinbase := testAddress(0xd6)
	feeRecipient := testAddress(0xd7)

	seqState := NewMemoryState()
	occState := NewMemoryState()
	for _, state := range []*MemoryState{seqState, occState} {
		state.SetBalance(feePayer, big.NewInt(1_000_000_000))
		state.SetBalance(transferSender, big.NewInt(1_000_000_000))
	}

	feeTx := signLegacyTxWithGasPrice(t, feePayerKey, chainID, 0, &feeRecipient, big.NewInt(0), nil, 100_000, big.NewInt(1))
	transferToCoinbaseTx := signLegacyTxWithGasPrice(t, transferKey, chainID, 0, &coinbase, big.NewInt(5), nil, 100_000, big.NewInt(0))
	ctx := blockContext(chainID)
	ctx.Coinbase = coinbase
	req := BlockRequest{Context: ctx, Txs: [][]byte{feeTx, transferToCoinbaseTx}}

	seqResult, err := NewExecutor(Config{MinGasPrice: big.NewInt(0)}, WithState(seqState)).ExecuteBlock(context.Background(), req)
	require.NoError(t, err)
	occResult, err := NewExecutor(Config{MinGasPrice: big.NewInt(0), OCCWorkers: 2}, WithState(occState)).ExecuteBlock(context.Background(), req)
	require.NoError(t, err)

	require.True(t, occResult.OCCStats.Attempted)
	require.False(t, occResult.OCCStats.Fallback)
	require.Equal(t, uint64(1), occResult.OCCStats.RerunCount)
	foundCoinbaseBalanceWrite := false
	for _, conflict := range occResult.OCCStats.ConflictSamples {
		if conflict.Access == "write" && conflict.Kind == "balance" && conflict.Address == coinbase {
			foundCoinbaseBalanceWrite = true
		}
	}
	require.True(t, foundCoinbaseBalanceWrite)

	seqState.ApplyChangeSet(seqResult.ChangeSet)
	occState.ApplyChangeSet(occResult.ChangeSet)
	require.Equal(t, seqState.GetBalance(coinbase), occState.GetBalance(coinbase))
	require.Equal(t, big.NewInt(21_005), occState.GetBalance(coinbase))
}

func TestExecutorOCCRerunsCoinbaseSpendFundedByPriorFeeCredit(t *testing.T) {
	chainID := big.NewInt(713715)
	feePayerKey, err := crypto.GenerateKey()
	require.NoError(t, err)
	coinbaseKey, err := crypto.GenerateKey()
	require.NoError(t, err)
	feePayer := crypto.PubkeyToAddress(feePayerKey.PublicKey)
	coinbase := crypto.PubkeyToAddress(coinbaseKey.PublicKey)
	feeRecipient := testAddress(0xd8)
	spendRecipient := testAddress(0xd9)

	seqState := NewMemoryState()
	occState := NewMemoryState()
	for _, state := range []*MemoryState{seqState, occState} {
		state.SetBalance(feePayer, big.NewInt(1_000_000_000))
	}

	feeTx := signLegacyTxWithGasPrice(t, feePayerKey, chainID, 0, &feeRecipient, big.NewInt(0), nil, 100_000, big.NewInt(1))
	spendFeeCreditTx := signLegacyTxWithGasPrice(t, coinbaseKey, chainID, 0, &spendRecipient, big.NewInt(21_000), nil, 100_000, big.NewInt(0))
	ctx := blockContext(chainID)
	ctx.Coinbase = coinbase
	req := BlockRequest{Context: ctx, Txs: [][]byte{feeTx, spendFeeCreditTx}}

	seqResult, err := NewExecutor(Config{MinGasPrice: big.NewInt(0)}, WithState(seqState)).ExecuteBlock(context.Background(), req)
	require.NoError(t, err)
	occResult, err := NewExecutor(Config{MinGasPrice: big.NewInt(0), OCCWorkers: 2}, WithState(occState)).ExecuteBlock(context.Background(), req)
	require.NoError(t, err)

	require.True(t, occResult.OCCStats.Attempted)
	require.False(t, occResult.OCCStats.Fallback)
	require.Equal(t, uint64(1), occResult.OCCStats.RerunCount)

	seqState.ApplyChangeSet(seqResult.ChangeSet)
	occState.ApplyChangeSet(occResult.ChangeSet)
	require.Equal(t, seqState.GetBalance(coinbase), occState.GetBalance(coinbase))
	require.Equal(t, big.NewInt(0), occState.GetBalance(coinbase))
	require.Equal(t, big.NewInt(21_000), occState.GetBalance(spendRecipient))
}

func TestExecutorOCCRerunsCoinbaseReadAfterNormalAndCommutativeWrite(t *testing.T) {
	chainID := big.NewInt(713715)
	coinbaseKey, err := crypto.GenerateKey()
	require.NoError(t, err)
	readerKey, err := crypto.GenerateKey()
	require.NoError(t, err)
	coinbase := crypto.PubkeyToAddress(coinbaseKey.PublicKey)
	reader := crypto.PubkeyToAddress(readerKey.PublicKey)
	recipient := testAddress(0xda)
	contract := testAddress(0xdb)
	storageKey := testHash(0x3b)
	initialCoinbaseBalance := big.NewInt(1_000_000_000)

	seqState := NewMemoryState()
	occState := NewMemoryState()
	for _, state := range []*MemoryState{seqState, occState} {
		state.SetBalance(coinbase, initialCoinbaseBalance)
		state.SetBalance(reader, big.NewInt(1_000_000_000))
		state.SetCode(contract, balanceStoreCode(coinbase, storageKey))
	}

	coinbaseTransferTx := signLegacyTxWithGasPrice(t, coinbaseKey, chainID, 0, &recipient, big.NewInt(7), nil, 100_000, big.NewInt(1))
	readCoinbaseTx := signLegacyTxWithGasPrice(t, readerKey, chainID, 0, &contract, big.NewInt(0), nil, 100_000, big.NewInt(0))
	ctx := blockContext(chainID)
	ctx.Coinbase = coinbase
	req := BlockRequest{Context: ctx, Txs: [][]byte{coinbaseTransferTx, readCoinbaseTx}}

	seqResult, err := NewExecutor(Config{MinGasPrice: big.NewInt(0)}, WithState(seqState)).ExecuteBlock(context.Background(), req)
	require.NoError(t, err)
	occResult, err := NewExecutor(Config{MinGasPrice: big.NewInt(0), OCCWorkers: 2}, WithState(occState)).ExecuteBlock(context.Background(), req)
	require.NoError(t, err)

	require.True(t, occResult.OCCStats.Attempted)
	require.False(t, occResult.OCCStats.Fallback)
	require.Equal(t, uint64(1), occResult.OCCStats.RerunCount)

	seqState.ApplyChangeSet(seqResult.ChangeSet)
	occState.ApplyChangeSet(occResult.ChangeSet)
	expectedReadBalance := new(big.Int).Sub(initialCoinbaseBalance, big.NewInt(7))
	require.Equal(t, common.BigToHash(expectedReadBalance), occState.GetState(contract, storageKey))
	require.Equal(t, seqState.GetState(contract, storageKey), occState.GetState(contract, storageKey))
	require.Equal(t, seqState.GetBalance(coinbase), occState.GetBalance(coinbase))
}

func TestExecutorOCCMergesCoinbaseSenderFeeWithoutDoubleCount(t *testing.T) {
	chainID := big.NewInt(713715)
	coinbaseKey, err := crypto.GenerateKey()
	require.NoError(t, err)
	otherKey, err := crypto.GenerateKey()
	require.NoError(t, err)
	coinbase := crypto.PubkeyToAddress(coinbaseKey.PublicKey)
	other := crypto.PubkeyToAddress(otherKey.PublicKey)
	coinbaseRecipient := testAddress(0xba)
	otherRecipient := testAddress(0xbb)
	initialCoinbaseBalance := big.NewInt(1_000_000_000)

	seqState := NewMemoryState()
	occState := NewMemoryState()
	for _, state := range []*MemoryState{seqState, occState} {
		state.SetBalance(coinbase, initialCoinbaseBalance)
		state.SetBalance(other, big.NewInt(1_000_000_000))
	}

	coinbaseTx := signLegacyTxWithGasPrice(t, coinbaseKey, chainID, 0, &coinbaseRecipient, big.NewInt(0), nil, 100_000, big.NewInt(1))
	otherTx := signLegacyTxWithGasPrice(t, otherKey, chainID, 0, &otherRecipient, big.NewInt(0), nil, 100_000, big.NewInt(1))
	ctx := blockContext(chainID)
	ctx.Coinbase = coinbase
	req := BlockRequest{Context: ctx, Txs: [][]byte{coinbaseTx, otherTx}}
	seqResult, err := NewExecutor(Config{MinGasPrice: big.NewInt(0)}, WithState(seqState)).ExecuteBlock(context.Background(), req)
	require.NoError(t, err)
	occResult, err := NewExecutor(Config{MinGasPrice: big.NewInt(0), OCCWorkers: 2}, WithState(occState)).ExecuteBlock(context.Background(), req)
	require.NoError(t, err)

	require.True(t, occResult.OCCStats.Attempted)
	require.False(t, occResult.OCCStats.Fallback)

	seqState.ApplyChangeSet(seqResult.ChangeSet)
	occState.ApplyChangeSet(occResult.ChangeSet)
	require.Equal(t, seqState.GetBalance(coinbase), occState.GetBalance(coinbase))
	require.Equal(t, new(big.Int).Add(initialCoinbaseBalance, big.NewInt(21_000)), occState.GetBalance(coinbase))
}

func TestExecutorOCCRerunsSameSenderNonceChain(t *testing.T) {
	chainID := big.NewInt(713715)
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	sender := crypto.PubkeyToAddress(key.PublicKey)
	firstRecipient := testAddress(0xb6)
	secondRecipient := testAddress(0xb7)

	state := NewMemoryState()
	state.SetBalance(sender, big.NewInt(1_000_000))
	firstTx := signLegacyTxWithGasPrice(t, key, chainID, 0, &firstRecipient, big.NewInt(1), nil, 100_000, big.NewInt(0))
	secondTx := signLegacyTxWithGasPrice(t, key, chainID, 1, &secondRecipient, big.NewInt(1), nil, 100_000, big.NewInt(0))
	executor := NewExecutor(Config{MinGasPrice: big.NewInt(0), OCCWorkers: 2}, WithState(state))

	result, err := executor.ExecuteBlock(context.Background(), BlockRequest{
		Context: blockContext(chainID),
		Txs:     [][]byte{firstTx, secondTx},
	})

	require.NoError(t, err)
	require.True(t, result.OCCStats.Attempted)
	require.False(t, result.OCCStats.Fallback)
	require.Equal(t, uint64(1), result.OCCStats.RerunCount)

	state.ApplyChangeSet(result.ChangeSet)
	require.Equal(t, uint64(2), state.GetNonce(sender))
	require.Equal(t, big.NewInt(1), state.GetBalance(firstRecipient))
	require.Equal(t, big.NewInt(1), state.GetBalance(secondRecipient))
}

func TestExecutorOCCFallsBackWhenDeclaredGasExceedsBlockLimit(t *testing.T) {
	chainID := big.NewInt(713715)
	rawTxs := make([][]byte, 0, 2)
	state := NewMemoryState()

	for i := 0; i < 2; i++ {
		key, err := crypto.GenerateKey()
		require.NoError(t, err)
		sender := crypto.PubkeyToAddress(key.PublicKey)
		recipient := common.BigToAddress(big.NewInt(int64(20_000 + i)))
		state.SetBalance(sender, big.NewInt(1_000_000))
		rawTxs = append(rawTxs, signLegacyTxWithGasPrice(t, key, chainID, 0, &recipient, big.NewInt(1), nil, 90_000, big.NewInt(0)))
	}

	ctx := blockContext(chainID)
	ctx.GasLimit = 100_000
	executor := NewExecutor(Config{MinGasPrice: big.NewInt(0), OCCWorkers: 2}, WithState(state))

	result, err := executor.ExecuteBlock(context.Background(), BlockRequest{
		Context: ctx,
		Txs:     rawTxs,
	})

	require.Error(t, err)
	require.True(t, errors.Is(err, core.ErrGasLimitReached))
	require.Nil(t, result)
}

func TestExecutorOCCAllowsDeclaredGasSumAboveBlockLimitWhenUsedGasFits(t *testing.T) {
	chainID := big.NewInt(713715)
	rawTxs := make([][]byte, 0, 2)
	state := NewMemoryState()

	for i := 0; i < 2; i++ {
		key, err := crypto.GenerateKey()
		require.NoError(t, err)
		sender := crypto.PubkeyToAddress(key.PublicKey)
		recipient := common.BigToAddress(big.NewInt(int64(21_000 + i)))
		state.SetBalance(sender, big.NewInt(1_000_000))
		rawTxs = append(rawTxs, signLegacyTxWithGasPrice(t, key, chainID, 0, &recipient, big.NewInt(1), nil, 60_000, big.NewInt(0)))
	}

	ctx := blockContext(chainID)
	ctx.GasLimit = 100_000
	executor := NewExecutor(Config{MinGasPrice: big.NewInt(0), OCCWorkers: 2}, WithState(state))

	result, err := executor.ExecuteBlock(context.Background(), BlockRequest{
		Context: ctx,
		Txs:     rawTxs,
	})

	require.NoError(t, err)
	require.True(t, result.OCCStats.Attempted)
	require.False(t, result.OCCStats.Fallback)
	require.Equal(t, uint64(42_000), result.GasUsed)
}

func TestExecutorOCCCreateThenCallRerunsDependentTx(t *testing.T) {
	chainID := big.NewInt(713715)
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	sender := crypto.PubkeyToAddress(key.PublicKey)
	storageKey := testHash(0x31)
	storageValue := testHash(0x32)
	runtime := storeCode(storageKey, storageValue)
	contractAddr := crypto.CreateAddress(sender, 0)

	seqState := NewMemoryState()
	occState := NewMemoryState()
	for _, state := range []*MemoryState{seqState, occState} {
		state.SetBalance(sender, big.NewInt(2_000_000_000_000_000))
	}

	createContract := signLegacyTxWithGas(t, key, chainID, 0, nil, big.NewInt(0), initCode(runtime), 300_000)
	callContract := signLegacyTx(t, key, chainID, 1, &contractAddr, big.NewInt(0), nil)
	req := BlockRequest{Context: blockContext(chainID), Txs: [][]byte{createContract, callContract}}

	seqResult, err := NewExecutor(Config{}, WithState(seqState)).ExecuteBlock(context.Background(), req)
	require.NoError(t, err)
	occResult, err := NewExecutor(Config{OCCWorkers: 2}, WithState(occState)).ExecuteBlock(context.Background(), req)
	require.NoError(t, err)

	require.True(t, occResult.OCCStats.Attempted)
	require.False(t, occResult.OCCStats.Fallback)
	require.GreaterOrEqual(t, occResult.OCCStats.RerunCount, uint64(1))
	require.Equal(t, seqResult.GasUsed, occResult.GasUsed)

	seqState.ApplyChangeSet(seqResult.ChangeSet)
	occState.ApplyChangeSet(occResult.ChangeSet)
	require.Equal(t, seqState.GetCode(contractAddr), occState.GetCode(contractAddr))
	require.Equal(t, storageValue, occState.GetState(contractAddr, storageKey))
	require.Equal(t, seqState.GetNonce(sender), occState.GetNonce(sender))
}

func TestExecutorReceiptAndLogMetadata(t *testing.T) {
	chainID := big.NewInt(713715)
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	sender := crypto.PubkeyToAddress(key.PublicKey)
	recipient := testAddress(0xa5)
	logContract := testAddress(0xc2)

	state := NewMemoryState()
	state.SetBalance(sender, big.NewInt(1_000_000_000_000_000))
	state.SetCode(logContract, log0Code())

	transfer := signLegacyTx(t, key, chainID, 0, &recipient, big.NewInt(3), nil)
	emitLog := signLegacyTx(t, key, chainID, 1, &logContract, big.NewInt(0), nil)
	transferTx := decodeTx(t, transfer)
	emitLogTx := decodeTx(t, emitLog)
	ctx := blockContext(chainID)
	ctx.Number = 42
	ctx.BlockHash = testHash(0x42)
	executor := NewExecutor(Config{}, WithState(state))

	result, err := executor.ExecuteBlock(context.Background(), BlockRequest{
		Context: ctx,
		Txs:     [][]byte{transfer, emitLog},
	})

	require.NoError(t, err)
	require.Len(t, result.Txs, 2)
	require.Len(t, result.Receipts, 2)

	require.Equal(t, transferTx.Hash(), result.Receipts[0].TxHash)
	require.Equal(t, uint(0), result.Receipts[0].TransactionIndex)
	require.Equal(t, ctx.BlockHash, result.Receipts[0].BlockHash)
	require.Equal(t, new(big.Int).SetUint64(ctx.Number), result.Receipts[0].BlockNumber)
	require.Equal(t, result.Txs[0].GasUsed, result.Receipts[0].CumulativeGasUsed)

	require.Equal(t, emitLogTx.Hash(), result.Receipts[1].TxHash)
	require.Equal(t, uint(1), result.Receipts[1].TransactionIndex)
	require.Equal(t, result.GasUsed, result.Receipts[1].CumulativeGasUsed)
	require.Len(t, result.Receipts[1].Logs, 1)
	log := result.Receipts[1].Logs[0]
	require.Equal(t, logContract, log.Address)
	require.Equal(t, ctx.Number, log.BlockNumber)
	require.Equal(t, ctx.BlockHash, log.BlockHash)
	require.Equal(t, emitLogTx.Hash(), log.TxHash)
	require.Equal(t, uint(1), log.TxIndex)
	require.Equal(t, uint(0), log.Index)

	state.ApplyChangeSet(result.ChangeSet)
	require.Equal(t, big.NewInt(3), state.GetBalance(recipient))
	require.Equal(t, uint64(2), state.GetNonce(sender))
}

func TestExecutorEVMFailureProducesReceiptAndContinues(t *testing.T) {
	chainID := big.NewInt(713715)
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	sender := crypto.PubkeyToAddress(key.PublicKey)
	oogContract := testAddress(0xc3)
	recipient := testAddress(0xa6)
	keySlot := testHash(0x01)
	value := testHash(0x02)

	state := NewMemoryState()
	state.SetBalance(sender, big.NewInt(1_000_000_000_000_000))
	state.SetCode(oogContract, storeCode(keySlot, value))

	oogCall := signLegacyTxWithGas(t, key, chainID, 0, &oogContract, big.NewInt(0), nil, 22_000)
	laterTransfer := signLegacyTx(t, key, chainID, 1, &recipient, big.NewInt(5), nil)
	executor := NewExecutor(Config{}, WithState(state))

	result, err := executor.ExecuteBlock(context.Background(), BlockRequest{
		Context: blockContext(chainID),
		Txs:     [][]byte{oogCall, laterTransfer},
	})

	require.NoError(t, err)
	require.Len(t, result.Txs, 2)
	require.Equal(t, ethtypes.ReceiptStatusFailed, result.Txs[0].Status)
	require.True(t, errors.Is(result.Txs[0].Err, vm.ErrOutOfGas))
	require.Equal(t, uint64(22_000), result.Txs[0].GasUsed)
	require.Equal(t, ethtypes.ReceiptStatusSuccessful, result.Txs[1].Status)
	require.Equal(t, result.GasUsed, result.Receipts[1].CumulativeGasUsed)

	state.ApplyChangeSet(result.ChangeSet)
	require.Equal(t, common.Hash{}, state.GetState(oogContract, keySlot))
	require.Equal(t, big.NewInt(5), state.GetBalance(recipient))
	require.Equal(t, uint64(2), state.GetNonce(sender))
}

func TestExecutorValidationFailuresAbortBlock(t *testing.T) {
	chainID := big.NewInt(713715)
	recipient := testAddress(0xa7)

	t.Run("nonce too high", func(t *testing.T) {
		key, err := crypto.GenerateKey()
		require.NoError(t, err)
		sender := crypto.PubkeyToAddress(key.PublicKey)

		state := NewMemoryState()
		state.SetBalance(sender, big.NewInt(1_000_000_000_000_000))
		rawTx := signLegacyTx(t, key, chainID, 1, &recipient, big.NewInt(1), nil)
		executor := NewExecutor(Config{}, WithState(state))

		result, err := executor.ExecuteBlock(context.Background(), BlockRequest{
			Context: blockContext(chainID),
			Txs:     [][]byte{rawTx},
		})

		require.Error(t, err)
		require.True(t, errors.Is(err, core.ErrNonceTooHigh))
		require.Nil(t, result)
		require.Equal(t, uint64(0), state.GetNonce(sender))
		require.Equal(t, big.NewInt(0), state.GetBalance(recipient))
	})

	t.Run("nonce too low", func(t *testing.T) {
		key, err := crypto.GenerateKey()
		require.NoError(t, err)
		sender := crypto.PubkeyToAddress(key.PublicKey)

		state := NewMemoryState()
		state.SetBalance(sender, big.NewInt(1_000_000_000_000_000))
		state.SetNonce(sender, 1)
		rawTx := signLegacyTx(t, key, chainID, 0, &recipient, big.NewInt(1), nil)
		executor := NewExecutor(Config{}, WithState(state))

		result, err := executor.ExecuteBlock(context.Background(), BlockRequest{
			Context: blockContext(chainID),
			Txs:     [][]byte{rawTx},
		})

		require.Error(t, err)
		require.True(t, errors.Is(err, core.ErrNonceTooLow))
		require.Nil(t, result)
		require.Equal(t, uint64(1), state.GetNonce(sender))
		require.Equal(t, big.NewInt(0), state.GetBalance(recipient))
	})

	t.Run("insufficient balance", func(t *testing.T) {
		key, err := crypto.GenerateKey()
		require.NoError(t, err)
		sender := crypto.PubkeyToAddress(key.PublicKey)

		state := NewMemoryState()
		state.SetBalance(sender, big.NewInt(1))
		rawTx := signLegacyTx(t, key, chainID, 0, &recipient, big.NewInt(1), nil)
		executor := NewExecutor(Config{}, WithState(state))

		result, err := executor.ExecuteBlock(context.Background(), BlockRequest{
			Context: blockContext(chainID),
			Txs:     [][]byte{rawTx},
		})

		require.Error(t, err)
		require.True(t, errors.Is(err, core.ErrInsufficientFunds))
		require.Nil(t, result)
		require.Equal(t, uint64(0), state.GetNonce(sender))
		require.Equal(t, big.NewInt(0), state.GetBalance(recipient))
	})

	t.Run("min gas price", func(t *testing.T) {
		key, err := crypto.GenerateKey()
		require.NoError(t, err)
		sender := crypto.PubkeyToAddress(key.PublicKey)

		state := NewMemoryState()
		state.SetBalance(sender, big.NewInt(1_000_000_000_000_000))
		rawTx := signLegacyTxWithGasPrice(t, key, chainID, 0, &recipient, big.NewInt(1), nil, 100_000, big.NewInt(1))
		executor := NewExecutor(Config{
			MinGasPrice: big.NewInt(2),
		}, WithState(state))

		result, err := executor.ExecuteBlock(context.Background(), BlockRequest{
			Context: blockContext(chainID),
			Txs:     [][]byte{rawTx},
		})

		require.Error(t, err)
		require.True(t, errors.Is(err, errInsufficientGasPrice))
		require.Nil(t, result)
		require.Equal(t, uint64(0), state.GetNonce(sender))
		require.Equal(t, big.NewInt(0), state.GetBalance(recipient))
	})

	t.Run("fee cap below base fee", func(t *testing.T) {
		key, err := crypto.GenerateKey()
		require.NoError(t, err)
		sender := crypto.PubkeyToAddress(key.PublicKey)

		state := NewMemoryState()
		state.SetBalance(sender, big.NewInt(1_000_000_000_000_000))
		rawTx := signDynamicFeeTxWithFees(
			t,
			key,
			chainID,
			0,
			&recipient,
			big.NewInt(1),
			nil,
			big.NewInt(testGasPriceWei),
			big.NewInt(testGasPriceWei),
			100_000,
		)
		executor := NewExecutor(Config{
			DisableGasPriceCheck: true,
		}, WithState(state))
		ctx := blockContext(chainID)
		ctx.BaseFee = big.NewInt(2 * testGasPriceWei)

		result, err := executor.ExecuteBlock(context.Background(), BlockRequest{
			Context: ctx,
			Txs:     [][]byte{rawTx},
		})

		require.Error(t, err)
		require.True(t, errors.Is(err, core.ErrFeeCapTooLow))
		require.Nil(t, result)
		require.Equal(t, uint64(0), state.GetNonce(sender))
		require.Equal(t, big.NewInt(0), state.GetBalance(recipient))
	})

	t.Run("intrinsic gas too low", func(t *testing.T) {
		key, err := crypto.GenerateKey()
		require.NoError(t, err)
		sender := crypto.PubkeyToAddress(key.PublicKey)

		state := NewMemoryState()
		state.SetBalance(sender, big.NewInt(1_000_000_000_000_000))
		rawTx := signLegacyTxWithGas(t, key, chainID, 0, &recipient, big.NewInt(1), nil, 20_000)
		executor := NewExecutor(Config{}, WithState(state))

		result, err := executor.ExecuteBlock(context.Background(), BlockRequest{
			Context: blockContext(chainID),
			Txs:     [][]byte{rawTx},
		})

		require.Error(t, err)
		require.True(t, errors.Is(err, core.ErrIntrinsicGas))
		require.Nil(t, result)
		require.Equal(t, uint64(0), state.GetNonce(sender))
		require.Equal(t, big.NewInt(0), state.GetBalance(recipient))
	})

	t.Run("block gas exhausted", func(t *testing.T) {
		key, err := crypto.GenerateKey()
		require.NoError(t, err)
		sender := crypto.PubkeyToAddress(key.PublicKey)

		state := NewMemoryState()
		state.SetBalance(sender, big.NewInt(1_000_000_000_000_000))
		firstTransfer := signLegacyTxWithGas(t, key, chainID, 0, &recipient, big.NewInt(1), nil, 21_000)
		secondTransfer := signLegacyTxWithGas(t, key, chainID, 1, &recipient, big.NewInt(1), nil, 21_000)
		executor := NewExecutor(Config{}, WithState(state))
		ctx := blockContext(chainID)
		ctx.GasLimit = 30_000

		result, err := executor.ExecuteBlock(context.Background(), BlockRequest{
			Context: ctx,
			Txs:     [][]byte{firstTransfer, secondTransfer},
		})

		require.Error(t, err)
		require.True(t, errors.Is(err, core.ErrGasLimitReached))
		require.Nil(t, result)
		require.Equal(t, uint64(0), state.GetNonce(sender))
		require.Equal(t, big.NewInt(0), state.GetBalance(recipient))
	})
}

func TestExecutorRejectsBadSignatureBeforeExecution(t *testing.T) {
	chainID := big.NewInt(713715)
	recipient := testAddress(0xa8)

	t.Run("wrong chain id", func(t *testing.T) {
		wrongChainID := big.NewInt(1)
		key, err := crypto.GenerateKey()
		require.NoError(t, err)
		sender := crypto.PubkeyToAddress(key.PublicKey)

		state := NewMemoryState()
		state.SetBalance(sender, big.NewInt(1_000_000_000_000_000))
		rawTx := signLegacyTx(t, key, wrongChainID, 0, &recipient, big.NewInt(1), nil)
		executor := NewExecutor(Config{}, WithState(state))

		result, err := executor.ExecuteBlock(context.Background(), BlockRequest{
			Context: blockContext(chainID),
			Txs:     [][]byte{rawTx},
		})

		require.Error(t, err)
		require.True(t, errors.Is(err, ethtypes.ErrInvalidChainId))
		require.Nil(t, result)
		require.Equal(t, uint64(0), state.GetNonce(sender))
		require.Equal(t, big.NewInt(0), state.GetBalance(recipient))
	})

	t.Run("invalid signature values", func(t *testing.T) {
		state := NewMemoryState()
		rawTx := legacyTxWithSignatureValues(
			t,
			0,
			&recipient,
			big.NewInt(1),
			nil,
			100_000,
			big.NewInt(testGasPriceWei),
			new(big.Int).Add(big.NewInt(35), new(big.Int).Mul(big.NewInt(2), chainID)),
			new(big.Int),
			new(big.Int),
		)
		executor := NewExecutor(Config{}, WithState(state))

		result, err := executor.ExecuteBlock(context.Background(), BlockRequest{
			Context: blockContext(chainID),
			Txs:     [][]byte{rawTx},
		})

		require.Error(t, err)
		require.True(t, errors.Is(err, ethtypes.ErrInvalidSig))
		require.Nil(t, result)
		require.Equal(t, big.NewInt(0), state.GetBalance(recipient))
	})
}

func TestExecutorCreatesContractThenUpdatesStorage(t *testing.T) {
	chainID := big.NewInt(713715)
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	sender := crypto.PubkeyToAddress(key.PublicKey)
	storageKey := testHash(0x11)
	storageValue := testHash(0x22)
	runtime := storeCode(storageKey, storageValue)
	contractAddr := crypto.CreateAddress(sender, 0)

	state := NewMemoryState()
	state.SetBalance(sender, big.NewInt(2_000_000_000_000_000))

	createContract := signLegacyTxWithGas(t, key, chainID, 0, nil, big.NewInt(0), initCode(runtime), 300_000)
	callContract := signLegacyTx(t, key, chainID, 1, &contractAddr, big.NewInt(0), nil)
	executor := NewExecutor(Config{}, WithState(state))

	result, err := executor.ExecuteBlock(context.Background(), BlockRequest{
		Context: blockContext(chainID),
		Txs:     [][]byte{createContract, callContract},
	})

	require.NoError(t, err)
	require.Len(t, result.Receipts, 2)
	require.Equal(t, ethtypes.ReceiptStatusSuccessful, result.Txs[0].Status)
	require.Equal(t, contractAddr, result.Txs[0].ContractAddress)
	require.Equal(t, contractAddr, result.Receipts[0].ContractAddress)
	require.Equal(t, ethtypes.ReceiptStatusSuccessful, result.Txs[1].Status)

	state.ApplyChangeSet(result.ChangeSet)
	require.Equal(t, runtime, state.GetCode(contractAddr))
	require.Equal(t, storageValue, state.GetState(contractAddr, storageKey))
	require.Equal(t, uint64(2), state.GetNonce(sender))
}

func TestExecutorCreateSelfDestructThenTransferSameAddress(t *testing.T) {
	chainID := big.NewInt(713715)
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	sender := crypto.PubkeyToAddress(key.PublicKey)
	beneficiary := testAddress(0xb2)
	runtime := selfDestructCode(beneficiary)
	contractAddr := crypto.CreateAddress(sender, 0)

	state := NewMemoryState()
	state.SetBalance(sender, big.NewInt(2_000_000_000_000_000))

	createContract := signLegacyTxWithGas(t, key, chainID, 0, nil, big.NewInt(0), initCode(runtime), 300_000)
	destroyContract := signLegacyTx(t, key, chainID, 1, &contractAddr, big.NewInt(0), nil)
	transferToDestroyed := signLegacyTx(t, key, chainID, 2, &contractAddr, big.NewInt(9), nil)
	executor := NewExecutor(Config{
		ChainConfig: legacySelfDestructChainConfig(chainID),
	}, WithState(state))

	result, err := executor.ExecuteBlock(context.Background(), BlockRequest{
		Context: blockContext(chainID),
		Txs:     [][]byte{createContract, destroyContract, transferToDestroyed},
	})

	require.NoError(t, err)
	require.Len(t, result.Receipts, 3)
	for _, txResult := range result.Txs {
		require.Equal(t, ethtypes.ReceiptStatusSuccessful, txResult.Status)
	}

	state.ApplyChangeSet(result.ChangeSet)
	require.Empty(t, state.GetCode(contractAddr))
	require.Equal(t, big.NewInt(9), state.GetBalance(contractAddr))
	require.Equal(t, uint64(0), state.GetNonce(contractAddr))
	require.Equal(t, big.NewInt(0), state.GetBalance(beneficiary))
	require.Equal(t, uint64(3), state.GetNonce(sender))
}

func TestExecutorEIP6780CreateFlagExpiresAfterTx(t *testing.T) {
	chainID := big.NewInt(713715)
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	sender := crypto.PubkeyToAddress(key.PublicKey)
	beneficiary := testAddress(0xb3)
	runtime := selfDestructCode(beneficiary)
	contractAddr := crypto.CreateAddress(sender, 0)

	state := NewMemoryState()
	state.SetBalance(sender, big.NewInt(2_000_000_000_000_000))

	createContract := signLegacyTxWithGas(t, key, chainID, 0, nil, big.NewInt(0), initCode(runtime), 300_000)
	selfDestructAfterCreateTx := signLegacyTx(t, key, chainID, 1, &contractAddr, big.NewInt(0), nil)
	executor := NewExecutor(Config{}, WithState(state))

	result, err := executor.ExecuteBlock(context.Background(), BlockRequest{
		Context: blockContext(chainID),
		Txs:     [][]byte{createContract, selfDestructAfterCreateTx},
	})

	require.NoError(t, err)
	require.Len(t, result.Receipts, 2)
	for _, txResult := range result.Txs {
		require.Equal(t, ethtypes.ReceiptStatusSuccessful, txResult.Status)
	}

	state.ApplyChangeSet(result.ChangeSet)
	require.Equal(t, runtime, state.GetCode(contractAddr))
	require.Equal(t, uint64(1), state.GetNonce(contractAddr))
	require.Equal(t, big.NewInt(0), state.GetBalance(beneficiary))
}

func TestExecutorFinalisesAfterEachTx(t *testing.T) {
	chainID := big.NewInt(713715)
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	sender := crypto.PubkeyToAddress(key.PublicKey)
	contract := common.HexToAddress("0x00000000000000000000000000000000000000c1")
	beneficiary := common.HexToAddress("0x00000000000000000000000000000000000000b1")

	state := NewMemoryState()
	state.SetBalance(sender, big.NewInt(500_000_000_000_000))
	state.SetCode(contract, selfDestructCode(beneficiary))

	firstCall := signLegacyTx(t, key, chainID, 0, &contract, big.NewInt(0), nil)
	secondCall := signLegacyTx(t, key, chainID, 1, &contract, big.NewInt(5), nil)
	executor := NewExecutor(Config{
		ChainConfig: legacySelfDestructChainConfig(chainID),
	}, WithState(state))

	result, err := executor.ExecuteBlock(context.Background(), BlockRequest{
		Context: blockContext(chainID),
		Txs:     [][]byte{firstCall, secondCall},
	})

	require.NoError(t, err)
	require.Len(t, result.Receipts, 2)

	state.ApplyChangeSet(result.ChangeSet)
	require.Empty(t, state.GetCode(contract))
	require.Equal(t, big.NewInt(5), state.GetBalance(contract))
	require.Equal(t, big.NewInt(0), state.GetBalance(beneficiary))
}

func TestStateDBSelfDestructEmitsStorageClear(t *testing.T) {
	contract := testAddress(0xc4)
	loadedKey := testHash(0x01)
	unreadKey := testHash(0x02)
	loadedValue := testHash(0x11)
	unreadValue := testHash(0x22)

	state := NewMemoryState()
	state.SetCode(contract, []byte{0x60, 0x00, 0x00})
	state.SetState(contract, loadedKey, loadedValue)
	state.SetState(contract, unreadKey, unreadValue)
	stateDB := newNativeStateDB(state)

	require.Equal(t, loadedValue, stateDB.GetState(contract, loadedKey))
	stateDB.SelfDestruct(contract)
	stateDB.Finalise(true)

	changes := stateDB.ChangeSet()
	require.Contains(t, changes.StorageClears, contract)
	state.ApplyChangeSet(changes)
	require.Empty(t, state.GetCode(contract))
	require.Equal(t, common.Hash{}, state.GetState(contract, loadedKey))
	require.Equal(t, common.Hash{}, state.GetState(contract, unreadKey))
}

func TestStateDBCreateAccountPreservesStorageClear(t *testing.T) {
	contract := testAddress(0xc6)
	unreadKey := testHash(0x02)
	unreadValue := testHash(0x22)

	state := NewMemoryState()
	state.SetCode(contract, []byte{0x60, 0x00, 0x00})
	state.SetState(contract, unreadKey, unreadValue)
	stateDB := newNativeStateDB(state)

	stateDB.SelfDestruct(contract)
	stateDB.Finalise(true)
	stateDB.CreateAccount(contract)
	stateDB.Finalise(true)

	changes := stateDB.ChangeSet()
	require.Contains(t, changes.StorageClears, contract)
	state.ApplyChangeSet(changes)
	require.Equal(t, common.Hash{}, state.GetState(contract, unreadKey))
}

func TestStateDBStorageClearThenSameValueWriteIsEmitted(t *testing.T) {
	contract := testAddress(0xc5)
	key := testHash(0x01)
	value := testHash(0x22)

	state := NewMemoryState()
	state.SetState(contract, key, value)
	stateDB := newNativeStateDB(state)

	require.Equal(t, value, stateDB.GetState(contract, key))
	stateDB.SelfDestruct(contract)
	stateDB.Finalise(true)
	stateDB.SetState(contract, key, value)

	changes := stateDB.ChangeSet()
	require.Contains(t, changes.StorageClears, contract)
	require.Len(t, changes.Storage, 1)
	require.Equal(t, key, changes.Storage[0].Key)
	require.Equal(t, value, changes.Storage[0].Value)
	require.False(t, changes.Storage[0].Delete)

	state.ApplyChangeSet(changes)
	require.Equal(t, value, state.GetState(contract, key))
}

func TestStateDBGetCommittedStateAdvancesAtFinalise(t *testing.T) {
	addr := testAddress(0xc7)
	key := testHash(0x01)
	first := testHash(0x11)
	second := testHash(0x22)
	third := testHash(0x33)

	state := NewMemoryState()
	state.SetState(addr, key, first)
	stateDB := newNativeStateDB(state)

	require.Equal(t, first, stateDB.GetCommittedState(addr, key))
	require.Equal(t, first, stateDB.SetState(addr, key, second))
	require.Equal(t, first, stateDB.GetCommittedState(addr, key))
	stateDB.Finalise(true)
	require.Equal(t, second, stateDB.GetCommittedState(addr, key))
	require.Equal(t, second, stateDB.SetState(addr, key, third))
	require.Equal(t, second, stateDB.GetCommittedState(addr, key))
}

func TestStateDBGetStateAfterStorageClearDoesNotReloadPersistedSlot(t *testing.T) {
	contract := testAddress(0xc8)
	key := testHash(0x01)
	value := testHash(0x22)

	state := NewMemoryState()
	state.SetState(contract, key, value)
	stateDB := newNativeStateDB(state)

	require.Equal(t, value, stateDB.GetState(contract, key))
	stateDB.SelfDestruct(contract)
	stateDB.Finalise(true)

	require.Equal(t, common.Hash{}, stateDB.GetCommittedState(contract, key))
	require.Equal(t, common.Hash{}, stateDB.GetState(contract, key))
}

func TestPrepareClearsTransientStorage(t *testing.T) {
	stateDB := newNativeStateDB(NewMemoryState())
	addr := common.HexToAddress("0x00000000000000000000000000000000000000a3")
	key := common.HexToHash("0x01")
	value := common.HexToHash("0x02")

	stateDB.SetTransientState(addr, key, value)
	require.Equal(t, value, stateDB.GetTransientState(addr, key))

	stateDB.Prepare(params.Rules{}, addr, common.Address{}, nil, nil, nil)

	require.Equal(t, common.Hash{}, stateDB.GetTransientState(addr, key))
}

func TestSnapshotRevertRestoresBaseState(t *testing.T) {
	addr := common.HexToAddress("0x00000000000000000000000000000000000000a4")
	key := common.HexToHash("0x01")
	value := common.HexToHash("0x02")

	state := NewMemoryState()
	state.SetState(addr, key, value)
	stateDB := newNativeStateDB(state)
	stateDB.GetBalance(addr)

	snapshot := stateDB.Snapshot()
	require.Equal(t, value, stateDB.GetState(addr, key))
	stateDB.RevertToSnapshot(snapshot)

	require.Empty(t, stateDB.ChangeSet().Storage)
}

func TestStateDBCopyPreservesSnapshotCommutativeBalanceDeltas(t *testing.T) {
	coinbase := testAddress(0xc9)
	stateDB := newNativeStateDB(NewMemoryState())
	stateDB.addCommutativeBalance(coinbase, uint256.NewInt(5))
	snapshot := stateDB.Snapshot()
	stateDB.addCommutativeBalance(coinbase, uint256.NewInt(7))

	copied := stateDB.Copy().(*nativeStateDB)
	copied.RevertToSnapshot(snapshot)

	require.Equal(t, map[common.Address]*big.Int{
		coinbase: big.NewInt(5),
	}, copied.commutativeBalanceDeltasBig())
}

func TestSnapshotRevertRestoresJournaledSideState(t *testing.T) {
	addr := testAddress(0xca)
	nextAddr := testAddress(0xcb)
	key := testHash(0x01)
	nextKey := testHash(0x02)
	value := testHash(0x03)
	nextValue := testHash(0x04)
	preimageHash := testHash(0x05)
	nextPreimageHash := testHash(0x06)

	stateDB := newNativeStateDB(NewMemoryState())
	stateDB.AddAddressToAccessList(addr)
	stateDB.SetTransientState(addr, key, value)
	stateDB.addCommutativeBalance(addr, uint256.NewInt(5))
	stateDB.AddPreimage(preimageHash, []byte{0x01})
	stateDB.markForFinalise(addr)
	stateDB.markTxStorageWrite(addr, key)
	stateDB.markTxStorageClear(addr)

	snapshot := stateDB.Snapshot()
	stateDB.AddAddressToAccessList(nextAddr)
	stateDB.AddSlotToAccessList(addr, nextKey)
	stateDB.SetTransientState(addr, key, nextValue)
	stateDB.SetTransientState(nextAddr, key, nextValue)
	stateDB.addCommutativeBalance(addr, uint256.NewInt(7))
	stateDB.AddPreimage(preimageHash, []byte{0x02})
	stateDB.AddPreimage(nextPreimageHash, []byte{0x03})
	stateDB.markForFinalise(nextAddr)
	stateDB.markTxStorageWrite(addr, nextKey)
	stateDB.markTxStorageClear(nextAddr)

	stateDB.RevertToSnapshot(snapshot)

	require.True(t, stateDB.AddressInAccessList(addr))
	require.False(t, stateDB.AddressInAccessList(nextAddr))
	_, slotPresent := stateDB.SlotInAccessList(addr, nextKey)
	require.False(t, slotPresent)
	require.Equal(t, value, stateDB.GetTransientState(addr, key))
	require.Equal(t, common.Hash{}, stateDB.GetTransientState(nextAddr, key))
	require.Equal(t, map[common.Address]*big.Int{addr: big.NewInt(5)}, stateDB.commutativeBalanceDeltasBig())
	require.Equal(t, []byte{0x01}, stateDB.Preimages()[preimageHash])
	_, hasNextPreimage := stateDB.Preimages()[nextPreimageHash]
	require.False(t, hasNextPreimage)
	require.Contains(t, stateDB.finaliseAddrs, addr)
	require.NotContains(t, stateDB.finaliseAddrs, nextAddr)
	require.Contains(t, stateDB.txStorageWrites[addr], key)
	require.NotContains(t, stateDB.txStorageWrites[addr], nextKey)
	require.Contains(t, stateDB.txStorageClears, addr)
	require.NotContains(t, stateDB.txStorageClears, nextAddr)
}

func TestStateDBFirstStorageReadPreservesBase(t *testing.T) {
	addr := testAddress(0xa9)
	key := testHash(0x01)
	value := testHash(0x02)
	nextValue := testHash(0x03)

	t.Run("get state", func(t *testing.T) {
		state := NewMemoryState()
		state.SetState(addr, key, value)
		stateDB := newNativeStateDB(state)

		require.Equal(t, value, stateDB.GetState(addr, key))
		require.Empty(t, stateDB.ChangeSet().Storage)
	})

	t.Run("get committed state", func(t *testing.T) {
		state := NewMemoryState()
		state.SetState(addr, key, value)
		stateDB := newNativeStateDB(state)

		require.Equal(t, value, stateDB.GetCommittedState(addr, key))
		require.Empty(t, stateDB.ChangeSet().Storage)
	})

	t.Run("set state returns persisted previous value", func(t *testing.T) {
		state := NewMemoryState()
		state.SetState(addr, key, value)
		stateDB := newNativeStateDB(state)

		require.Equal(t, value, stateDB.SetState(addr, key, nextValue))
		changes := stateDB.ChangeSet()
		require.Len(t, changes.Storage, 1)
		require.Equal(t, nextValue, changes.Storage[0].Value)
	})
}

func TestStateDBZeroStorageWriteStaysDirty(t *testing.T) {
	addr := testAddress(0xaa)
	key := testHash(0x01)
	value := testHash(0x02)

	state := NewMemoryState()
	state.SetState(addr, key, value)
	stateDB := newNativeStateDB(state)

	require.Equal(t, value, stateDB.GetState(addr, key))
	require.Equal(t, value, stateDB.SetState(addr, key, common.Hash{}))
	require.Equal(t, common.Hash{}, stateDB.GetState(addr, key))

	changes := stateDB.ChangeSet()
	require.Len(t, changes.Storage, 1)
	require.Equal(t, addr, changes.Storage[0].Address)
	require.Equal(t, key, changes.Storage[0].Key)
	require.True(t, changes.Storage[0].Delete)

	state.ApplyChangeSet(changes)
	require.Equal(t, common.Hash{}, state.GetState(addr, key))
}

func TestStateDBGetCodeHashDistinguishesExistingCodelessAccounts(t *testing.T) {
	missing := testAddress(0xab)
	eoa := testAddress(0xac)
	contract := testAddress(0xad)
	code := []byte{0x60, 0x00, 0x00}

	state := NewMemoryState()
	state.SetBalance(eoa, big.NewInt(1))
	state.SetCode(contract, code)
	stateDB := newNativeStateDB(state)

	require.Equal(t, common.Hash{}, stateDB.GetCodeHash(missing))
	require.Equal(t, ethtypes.EmptyCodeHash, stateDB.GetCodeHash(eoa))
	require.Equal(t, crypto.Keccak256Hash(code), stateDB.GetCodeHash(contract))
}

func TestStateDBGetCodeHashTracksCodelessAccountExistenceReads(t *testing.T) {
	eoa := testAddress(0xbd)
	state := NewMemoryState()
	state.SetBalance(eoa, big.NewInt(1))
	stateDB := newNativeStateDB(state)
	stateDB.enableAccessTracking()

	require.Equal(t, ethtypes.EmptyCodeHash, stateDB.GetCodeHash(eoa))
	readSet, _ := stateDB.accessSets()
	require.Contains(t, readSet, stateAccessKey{kind: stateAccessCode, address: eoa})
	require.Contains(t, readSet, stateAccessKey{kind: stateAccessBalance, address: eoa})
	require.Contains(t, readSet, stateAccessKey{kind: stateAccessNonce, address: eoa})

	writes := newStateAccessIndex()
	writes.addAll(map[stateAccessKey]struct{}{
		{kind: stateAccessBalance, address: eoa}: {},
	})
	validation := occValidationResult{valid: true}
	accepted := validateSTMResultAgainstPrefix(&validation, writes, occTxExecution{gasLimit: 1, readSet: readSet}, 0, 10)
	require.False(t, accepted)
	require.False(t, validation.valid)
	require.Equal(t, occFallbackReasonConflict, validation.fallbackReason)
}

func TestFinaliseClearsRefund(t *testing.T) {
	stateDB := newNativeStateDB(NewMemoryState())
	stateDB.AddRefund(12)

	stateDB.Finalise(true)

	require.Zero(t, stateDB.GetRefund())
}

func TestExecutorCustomPrecompilePlaceholder(t *testing.T) {
	chainID := big.NewInt(713715)
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	sender := crypto.PubkeyToAddress(key.PublicKey)
	customAddr := common.HexToAddress("0x0000000000000000000000000000000000001001")

	state := NewMemoryState()
	state.SetBalance(sender, big.NewInt(200_000_000_000_000))

	rawTx := signLegacyTx(t, key, chainID, 0, &customAddr, big.NewInt(0), []byte{0x01})
	executor := NewExecutor(Config{
		CustomPrecompiles: staticPrecompileRegistry{addr: customAddr},
	}, WithState(state))

	result, err := executor.ExecuteBlock(context.Background(), BlockRequest{
		Context: blockContext(chainID),
		Txs:     [][]byte{rawTx},
	})

	require.NoError(t, err)
	require.Len(t, result.Txs, 1)
	require.Len(t, result.Receipts, 1)
	require.Equal(t, ethtypes.ReceiptStatusFailed, result.Txs[0].Status)
	require.True(t, errors.Is(result.Txs[0].Err, precompiles.ErrCustomPrecompilesOpen))
}

func signLegacyTx(t *testing.T, key *ecdsa.PrivateKey, chainID *big.Int, nonce uint64, to *common.Address, value *big.Int, data []byte) []byte {
	t.Helper()
	return signLegacyTxWithGas(t, key, chainID, nonce, to, value, data, 100_000)
}

func signLegacyTxWithGas(t *testing.T, key *ecdsa.PrivateKey, chainID *big.Int, nonce uint64, to *common.Address, value *big.Int, data []byte, gas uint64) []byte {
	t.Helper()
	return signLegacyTxWithGasPrice(t, key, chainID, nonce, to, value, data, gas, big.NewInt(testGasPriceWei))
}

func signLegacyTxWithGasPrice(t *testing.T, key *ecdsa.PrivateKey, chainID *big.Int, nonce uint64, to *common.Address, value *big.Int, data []byte, gas uint64, gasPrice *big.Int) []byte {
	t.Helper()
	tx := ethtypes.NewTx(&ethtypes.LegacyTx{
		Nonce:    nonce,
		GasPrice: new(big.Int).Set(gasPrice),
		Gas:      gas,
		To:       to,
		Value:    value,
		Data:     data,
	})
	signed, err := ethtypes.SignTx(tx, ethtypes.LatestSignerForChainID(chainID), key)
	require.NoError(t, err)
	raw, err := signed.MarshalBinary()
	require.NoError(t, err)
	return raw
}

func legacyTxWithSignatureValues(t *testing.T, nonce uint64, to *common.Address, value *big.Int, data []byte, gas uint64, gasPrice *big.Int, v *big.Int, r *big.Int, s *big.Int) []byte {
	t.Helper()
	tx := ethtypes.NewTx(&ethtypes.LegacyTx{
		Nonce:    nonce,
		GasPrice: new(big.Int).Set(gasPrice),
		Gas:      gas,
		To:       to,
		Value:    value,
		Data:     data,
		V:        new(big.Int).Set(v),
		R:        new(big.Int).Set(r),
		S:        new(big.Int).Set(s),
	})
	raw, err := tx.MarshalBinary()
	require.NoError(t, err)
	return raw
}

func decodeTx(t *testing.T, raw []byte) *ethtypes.Transaction {
	t.Helper()
	var tx ethtypes.Transaction
	require.NoError(t, tx.UnmarshalBinary(raw))
	return &tx
}

func signDynamicFeeTx(t *testing.T, key *ecdsa.PrivateKey, chainID *big.Int, nonce uint64, to *common.Address, value *big.Int, data []byte) []byte {
	t.Helper()
	return signDynamicFeeTxWithFees(
		t,
		key,
		chainID,
		nonce,
		to,
		value,
		data,
		big.NewInt(testGasPriceWei),
		big.NewInt(testGasPriceWei),
		100_000,
	)
}

func signDynamicFeeTxWithFees(t *testing.T, key *ecdsa.PrivateKey, chainID *big.Int, nonce uint64, to *common.Address, value *big.Int, data []byte, gasTipCap *big.Int, gasFeeCap *big.Int, gas uint64) []byte {
	t.Helper()
	tx := ethtypes.NewTx(&ethtypes.DynamicFeeTx{
		ChainID:   chainID,
		Nonce:     nonce,
		GasTipCap: new(big.Int).Set(gasTipCap),
		GasFeeCap: new(big.Int).Set(gasFeeCap),
		Gas:       gas,
		To:        to,
		Value:     value,
		Data:      data,
	})
	signed, err := ethtypes.SignTx(tx, ethtypes.LatestSignerForChainID(chainID), key)
	require.NoError(t, err)
	raw, err := signed.MarshalBinary()
	require.NoError(t, err)
	return raw
}

func signBlobTxWithFees(
	t *testing.T,
	key *ecdsa.PrivateKey,
	chainID *big.Int,
	nonce uint64,
	to common.Address,
	value *big.Int,
	data []byte,
	gasTipCap *big.Int,
	gasFeeCap *big.Int,
	blobFeeCap *big.Int,
	gas uint64,
	blobHashes []common.Hash,
) []byte {
	t.Helper()
	tx := ethtypes.NewTx(&ethtypes.BlobTx{
		ChainID:    uint256.MustFromBig(chainID),
		Nonce:      nonce,
		GasTipCap:  uint256.MustFromBig(gasTipCap),
		GasFeeCap:  uint256.MustFromBig(gasFeeCap),
		Gas:        gas,
		To:         to,
		Value:      uint256.MustFromBig(value),
		Data:       data,
		BlobFeeCap: uint256.MustFromBig(blobFeeCap),
		BlobHashes: append([]common.Hash(nil), blobHashes...),
	})
	signed, err := ethtypes.SignTx(tx, ethtypes.LatestSignerForChainID(chainID), key)
	require.NoError(t, err)
	raw, err := signed.MarshalBinary()
	require.NoError(t, err)
	return raw
}

func blockContext(chainID *big.Int) BlockContext {
	return BlockContext{
		Number:      1,
		Time:        1,
		GasLimit:    30_000_000,
		ChainID:     chainID,
		BaseFee:     big.NewInt(0),
		BlobBaseFee: big.NewInt(0),
		Coinbase:    common.HexToAddress("0x00000000000000000000000000000000000000cb"),
	}
}

func legacySelfDestructChainConfig(chainID *big.Int) *params.ChainConfig {
	return &params.ChainConfig{
		ChainID:             chainID,
		HomesteadBlock:      big.NewInt(0),
		DAOForkBlock:        nil,
		DAOForkSupport:      false,
		EIP150Block:         big.NewInt(0),
		EIP155Block:         big.NewInt(0),
		EIP158Block:         big.NewInt(0),
		ByzantiumBlock:      big.NewInt(0),
		ConstantinopleBlock: big.NewInt(0),
		PetersburgBlock:     big.NewInt(0),
		IstanbulBlock:       big.NewInt(0),
		BerlinBlock:         big.NewInt(0),
		LondonBlock:         big.NewInt(0),
	}
}

func selfDestructCode(beneficiary common.Address) []byte {
	code := append([]byte{0x73}, beneficiary.Bytes()...)
	return append(code, 0xff)
}

func log0Code() []byte {
	return []byte{0x60, 0x00, 0x60, 0x00, 0xa0, 0x00}
}

func storeCode(key, value common.Hash) []byte {
	code := append([]byte{0x7f}, value.Bytes()...)
	code = append(code, 0x7f)
	code = append(code, key.Bytes()...)
	return append(code, 0x55, 0x00)
}

func balanceStoreCode(addr common.Address, key common.Hash) []byte {
	code := append([]byte{0x73}, addr.Bytes()...)
	code = append(code, 0x31, 0x7f)
	code = append(code, key.Bytes()...)
	return append(code, 0x55, 0x00)
}

func initCode(runtime []byte) []byte {
	if len(runtime) > 255 {
		panic("test runtime too large")
	}
	runtimeLen := byte(len(runtime)) //nolint:gosec // bounded by the check above.
	code := []byte{
		0x60, runtimeLen,
		0x60, 0x0c,
		0x60, 0x00,
		0x39,
		0x60, runtimeLen,
		0x60, 0x00,
		0xf3,
	}
	return append(code, runtime...)
}

func testAddress(suffix byte) common.Address {
	return common.BytesToAddress([]byte{suffix})
}

func testHash(suffix byte) common.Hash {
	return common.BytesToHash([]byte{suffix})
}

type staticPrecompileRegistry struct {
	addr common.Address
}

func (r staticPrecompileRegistry) Get(addr common.Address) (precompiles.Contract, bool) {
	return nil, addr == r.addr
}

func (r staticPrecompileRegistry) Addresses() []common.Address {
	return []common.Address{r.addr}
}
