package evmonly

import (
	"context"
	"crypto/ecdsa"
	"errors"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
	stakingprecompile "github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/staking"
	precompileutil "github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/util"
)

const testGasPriceWei = 1_000_000_000

type recordingResultSink struct {
	changeSetHeights []uint64
	receiptHeights   []uint64
	changeSets       []StateChangeSet
	receipts         []ethtypes.Receipts
}

func (s *recordingResultSink) StoreChangeSet(_ context.Context, height uint64, changeSet StateChangeSet) error {
	s.changeSetHeights = append(s.changeSetHeights, height)
	s.changeSets = append(s.changeSets, changeSet)
	return nil
}

func (s *recordingResultSink) StoreReceipts(_ context.Context, height uint64, receipts ethtypes.Receipts) error {
	s.receiptHeights = append(s.receiptHeights, height)
	s.receipts = append(s.receipts, receipts)
	return nil
}

type recordingBlockResultSink struct {
	result  *BlockResult
	release func()
}

func (s *recordingBlockResultSink) StoreChangeSet(context.Context, uint64, StateChangeSet) error {
	return nil
}

func (s *recordingBlockResultSink) StoreReceipts(context.Context, uint64, ethtypes.Receipts) error {
	return nil
}

func (s *recordingBlockResultSink) StoreBlockResult(_ context.Context, _ uint64, result *BlockResult, release func()) error {
	s.result = result
	s.release = release
	return nil
}

func TestExecutorEmptyBlock(t *testing.T) {
	executor := NewExecutor(Config{})

	result, err := executor.ExecuteBlock(context.Background(), BlockRequest{})

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
	require.Len(t, sink.changeSets, 1)
	require.Len(t, sink.receipts, 1)
	require.Equal(t, []uint64{ctx.Number}, sink.changeSetHeights)
	require.Equal(t, []uint64{ctx.Number}, sink.receiptHeights)
	require.Equal(t, result.ChangeSet, sink.changeSets[0])
	require.Equal(t, result.Receipts, sink.receipts[0])
}

func TestExecutorPooledResultRelease(t *testing.T) {
	chainID := big.NewInt(713715)
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	sender := crypto.PubkeyToAddress(key.PublicKey)
	recipient := common.HexToAddress("0x00000000000000000000000000000000000000a8")

	state := NewMemoryState()
	state.SetBalance(sender, big.NewInt(200_000_000_000_000))
	sink := &recordingBlockResultSink{}
	executor := NewExecutor(Config{BlockResultPoolSize: 1}, WithState(state), WithResultSink(sink))
	rawTx := signLegacyTx(t, key, chainID, 0, &recipient, big.NewInt(7), nil)
	req := BlockRequest{Context: blockContext(chainID), Txs: [][]byte{rawTx}}

	first, err := executor.ExecuteBlock(context.Background(), req)
	require.NoError(t, err)
	require.Same(t, first, sink.result)
	require.NotNil(t, sink.release)
	sink.release()
	first.Release()

	second, err := executor.ExecuteBlock(context.Background(), req)
	require.NoError(t, err)
	require.Same(t, first, second)
	sink.release()
	second.Release()
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
	require.True(t, occResult.OCCStats.Fallback)
	require.Equal(t, "conflict", occResult.OCCStats.FallbackReason)
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

func TestExecutorRegisteredCustomPrecompile(t *testing.T) {
	chainID := big.NewInt(713715)
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	sender := crypto.PubkeyToAddress(key.PublicKey)
	customAddr := common.HexToAddress("0x0000000000000000000000000000000000001005")

	state := NewMemoryState()
	state.SetBalance(sender, big.NewInt(200_000_000_000_000))

	rawTx := signLegacyTx(t, key, chainID, 0, &customAddr, big.NewInt(0), []byte{0x01})
	executor := NewExecutor(Config{
		CustomPrecompiles: contractPrecompileRegistry{
			customAddr: storeWritePrecompile{},
		},
	}, WithState(state))

	result, err := executor.ExecuteBlock(context.Background(), BlockRequest{
		Context: blockContext(chainID),
		Txs:     [][]byte{rawTx},
	})

	require.NoError(t, err)
	require.Len(t, result.Txs, 1)
	require.Equal(t, ethtypes.ReceiptStatusSuccessful, result.Txs[0].Status)
	require.Greater(t, result.Txs[0].GasUsed, uint64(21_000+100))
	require.NotEmpty(t, result.ChangeSet.Storage)

	state.ApplyChangeSet(result.ChangeSet)
	require.Equal(t, encodedStoredLength(2), state.GetState(customAddr, storeBaseSlot([]byte("seen"))))
}

func TestExecutorRegisteredCustomPrecompileMetersDynamicStoreGas(t *testing.T) {
	chainID := big.NewInt(713715)
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	sender := crypto.PubkeyToAddress(key.PublicKey)
	customAddr := common.HexToAddress("0x0000000000000000000000000000000000001005")

	state := NewMemoryState()
	state.SetBalance(sender, big.NewInt(200_000_000_000_000))

	rawTx := signLegacyTxWithGas(t, key, chainID, 0, &customAddr, big.NewInt(0), []byte{0x01}, 30_000)
	executor := NewExecutor(Config{
		CustomPrecompiles: contractPrecompileRegistry{
			customAddr: storeWritePrecompile{},
		},
	}, WithState(state))

	result, err := executor.ExecuteBlock(context.Background(), BlockRequest{
		Context: blockContext(chainID),
		Txs:     [][]byte{rawTx},
	})

	require.NoError(t, err)
	require.Len(t, result.Txs, 1)
	require.Equal(t, ethtypes.ReceiptStatusFailed, result.Txs[0].Status)
	require.True(t, errors.Is(result.Txs[0].Err, vm.ErrOutOfGas))
	require.Empty(t, result.ChangeSet.Storage)
}

func TestExecutorStakingPrecompileForwardsPayableValue(t *testing.T) {
	chainID := big.NewInt(713715)
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	sender := crypto.PubkeyToAddress(key.PublicKey)
	stakingAddr := common.HexToAddress(stakingprecompile.StakingAddress)

	state := NewMemoryState()
	initialBalance := sei(100)
	state.SetBalance(sender, initialBalance)

	contract, err := stakingprecompile.NewPrecompile()
	require.NoError(t, err)
	registry, err := stakingprecompile.NewRegistry()
	require.NoError(t, err)
	input, err := contract.ABI().Pack(
		stakingprecompile.CreateValidatorMethod,
		"01020304",
		"validator-one",
		"0.100000000000000000",
		"0.200000000000000000",
		"0.010000000000000000",
		big.NewInt(1),
	)
	require.NoError(t, err)
	value := sei(5)
	rawTx := signLegacyTxWithGas(t, key, chainID, 0, &stakingAddr, value, input, 8_000_000)
	executor := NewExecutor(Config{
		CustomPrecompiles: registry,
	}, WithState(state))

	result, err := executor.ExecuteBlock(context.Background(), BlockRequest{
		Context: blockContext(chainID),
		Txs:     [][]byte{rawTx},
	})

	require.NoError(t, err)
	require.Len(t, result.Txs, 1)
	require.Equal(t, ethtypes.ReceiptStatusSuccessful, result.Txs[0].Status)
	require.Equal(t, []ValidatorUpdate{{PubKey: []byte{0x01, 0x02, 0x03, 0x04}, Power: 5}}, result.ValidatorUpdates)

	state.ApplyChangeSet(result.ChangeSet)
	gasCost := new(big.Int).Mul(new(big.Int).SetUint64(result.Txs[0].GasUsed), result.Txs[0].EffectiveGasPrice)
	require.Equal(t, new(big.Int).Sub(new(big.Int).Sub(initialBalance, value), gasCost), state.GetBalance(sender))
	require.Zero(t, state.GetBalance(stakingAddr).Sign())
	require.Equal(t, value, state.GetBalance(stakingprecompile.EscrowAddress()))
}

func TestExecutorStakingDelegationLifecycleE2E(t *testing.T) {
	chainID := big.NewInt(713715)
	sourceKey, err := crypto.GenerateKey()
	require.NoError(t, err)
	dstKey, err := crypto.GenerateKey()
	require.NoError(t, err)
	delegatorKey, err := crypto.GenerateKey()
	require.NoError(t, err)

	source := crypto.PubkeyToAddress(sourceKey.PublicKey)
	destination := crypto.PubkeyToAddress(dstKey.PublicKey)
	delegator := crypto.PubkeyToAddress(delegatorKey.PublicKey)
	stakingAddr := common.HexToAddress(stakingprecompile.StakingAddress)
	escrowAddr := stakingprecompile.EscrowAddress()

	state := NewMemoryState()
	initialBalance := sei(1000)
	state.SetBalance(source, initialBalance)
	state.SetBalance(destination, initialBalance)
	state.SetBalance(delegator, initialBalance)

	registry, err := stakingprecompile.NewRegistry()
	require.NoError(t, err)
	contract, err := stakingprecompile.NewPrecompile()
	require.NoError(t, err)
	executor := NewExecutor(Config{CustomPrecompiles: registry}, WithState(state))

	nonces := map[common.Address]uint64{}
	signStakingTx := func(key *ecdsa.PrivateKey, value *big.Int, input []byte) []byte {
		sender := crypto.PubkeyToAddress(key.PublicKey)
		raw := signLegacyTxWithGas(t, key, chainID, nonces[sender], &stakingAddr, value, input, 8_000_000)
		nonces[sender]++
		return raw
	}
	expectedBalances := map[common.Address]*big.Int{
		source:      new(big.Int).Set(initialBalance),
		destination: new(big.Int).Set(initialBalance),
		delegator:   new(big.Int).Set(initialBalance),
		stakingAddr: new(big.Int),
		escrowAddr:  new(big.Int),
	}

	sourceSelfStake := sei(10)
	destinationSelfStake := sei(5)
	sourceSetupResult := executeBlockAndApply(t, executor, state, blockContextAt(chainID, 1, 100), [][]byte{
		signStakingTx(sourceKey, sourceSelfStake, mustPackStaking(t, contract, stakingprecompile.CreateValidatorMethod,
			"01020304",
			"source-validator",
			"0.100000000000000000",
			"0.200000000000000000",
			"0.010000000000000000",
			big.NewInt(1),
		)),
	})
	requireTxsSuccessful(t, sourceSetupResult, 1)
	debitExpectedBalance(expectedBalances, source, sourceSelfStake, sourceSetupResult.Txs[0])
	addExpectedBalance(expectedBalances, escrowAddr, sourceSelfStake)
	requireNativeBalances(t, state, expectedBalances)
	require.Equal(t, []ValidatorUpdate{{PubKey: []byte{0x01, 0x02, 0x03, 0x04}, Power: 10}}, sourceSetupResult.ValidatorUpdates)
	requireStakingPool(t, state, "10000000", "0")
	requireStakingValidator(t, state, source, "10000000", "10000000", 3)

	destinationSetupResult := executeBlockAndApply(t, executor, state, blockContextAt(chainID, 2, 125), [][]byte{
		signStakingTx(dstKey, destinationSelfStake, mustPackStaking(t, contract, stakingprecompile.CreateValidatorMethod,
			"05060708",
			"destination-validator",
			"0.100000000000000000",
			"0.200000000000000000",
			"0.010000000000000000",
			big.NewInt(1),
		)),
	})
	requireTxsSuccessful(t, destinationSetupResult, 1)
	debitExpectedBalance(expectedBalances, destination, destinationSelfStake, destinationSetupResult.Txs[0])
	addExpectedBalance(expectedBalances, escrowAddr, destinationSelfStake)
	requireNativeBalances(t, state, expectedBalances)
	require.Equal(t, []ValidatorUpdate{{PubKey: []byte{0x05, 0x06, 0x07, 0x08}, Power: 5}}, destinationSetupResult.ValidatorUpdates)
	requireStakingPool(t, state, "15000000", "0")
	requireStakingValidator(t, state, source, "10000000", "10000000", 3)
	requireStakingValidator(t, state, destination, "5000000", "5000000", 3)

	delegationValue := sei(7)
	delegateResult := executeBlockAndApply(t, executor, state, blockContextAt(chainID, 3, 150), [][]byte{
		signStakingTx(delegatorKey, delegationValue, mustPackStaking(t, contract, stakingprecompile.DelegateMethod, source.Hex())),
	})
	requireTxsSuccessful(t, delegateResult, 1)
	debitExpectedBalance(expectedBalances, delegator, delegationValue, delegateResult.Txs[0])
	addExpectedBalance(expectedBalances, escrowAddr, delegationValue)
	requireNativeBalances(t, state, expectedBalances)
	require.Equal(t, []ValidatorUpdate{{PubKey: []byte{0x01, 0x02, 0x03, 0x04}, Power: 17}}, delegateResult.ValidatorUpdates)
	requireStakingPool(t, state, "22000000", "0")
	requireStakingValidator(t, state, source, "17000000", "17000000", 3)
	requireStakingValidator(t, state, destination, "5000000", "5000000", 3)
	requireStakingDelegation(t, state, delegator, source, "7000000")

	redelegationAmount := big.NewInt(3_000_000)
	redelegationTime := uint64(200)
	redelegationCompletion := int64(redelegationTime + 1_814_400)
	redelegateResult := executeBlockAndApply(t, executor, state, blockContextAt(chainID, 4, redelegationTime), [][]byte{
		signStakingTx(delegatorKey, nil, mustPackStaking(t, contract, stakingprecompile.RedelegateMethod, source.Hex(), destination.Hex(), redelegationAmount)),
	})
	requireTxsSuccessful(t, redelegateResult, 1)
	debitExpectedBalance(expectedBalances, delegator, nil, redelegateResult.Txs[0])
	requireNativeBalances(t, state, expectedBalances)
	require.Equal(t, []ValidatorUpdate{
		{PubKey: []byte{0x01, 0x02, 0x03, 0x04}, Power: 14},
		{PubKey: []byte{0x05, 0x06, 0x07, 0x08}, Power: 8},
	}, redelegateResult.ValidatorUpdates)
	requireStakingPool(t, state, "22000000", "0")
	requireStakingValidator(t, state, source, "14000000", "14000000", 3)
	requireStakingValidator(t, state, destination, "8000000", "8000000", 3)
	requireStakingDelegation(t, state, delegator, source, "4000000")
	requireStakingDelegation(t, state, delegator, destination, "3000000")
	requireStakingRedelegation(t, state, delegator, source, destination, "3000000", redelegationCompletion)

	undelegationAmount := big.NewInt(2_000_000)
	undelegationTime := uint64(300)
	undelegationCompletion := int64(undelegationTime + 1_814_400)
	undelegateResult := executeBlockAndApply(t, executor, state, blockContextAt(chainID, 5, undelegationTime), [][]byte{
		signStakingTx(delegatorKey, nil, mustPackStaking(t, contract, stakingprecompile.UndelegateMethod, destination.Hex(), undelegationAmount)),
	})
	requireTxsSuccessful(t, undelegateResult, 1)
	debitExpectedBalance(expectedBalances, delegator, nil, undelegateResult.Txs[0])
	requireNativeBalances(t, state, expectedBalances)
	require.Equal(t, []ValidatorUpdate{{PubKey: []byte{0x05, 0x06, 0x07, 0x08}, Power: 6}}, undelegateResult.ValidatorUpdates)
	requireStakingPool(t, state, "20000000", "2000000")
	requireStakingValidator(t, state, source, "14000000", "14000000", 3)
	requireStakingValidator(t, state, destination, "6000000", "6000000", 3)
	requireStakingDelegation(t, state, delegator, source, "4000000")
	requireStakingDelegation(t, state, delegator, destination, "1000000")
	requireStakingRedelegation(t, state, delegator, source, destination, "3000000", redelegationCompletion)
	requireStakingUnbonding(t, state, delegator, destination, "2000000", undelegationCompletion)

	redelegationMaturityResult := executeBlockAndApply(t, executor, state, blockContextAt(chainID, 6, uint64(redelegationCompletion)), nil)
	require.Empty(t, redelegationMaturityResult.ValidatorUpdates)
	requireNativeBalances(t, state, expectedBalances)
	requireStakingPool(t, state, "20000000", "2000000")
	requireStakingValidator(t, state, source, "14000000", "14000000", 3)
	requireStakingValidator(t, state, destination, "6000000", "6000000", 3)
	requireStakingDelegation(t, state, delegator, source, "4000000")
	requireStakingDelegation(t, state, delegator, destination, "1000000")
	requireNoStakingRedelegation(t, state, delegator, source, destination)
	requireStakingUnbonding(t, state, delegator, destination, "2000000", undelegationCompletion)

	undelegationMaturityResult := executeBlockAndApply(t, executor, state, blockContextAt(chainID, 7, uint64(undelegationCompletion)), nil)
	require.Empty(t, undelegationMaturityResult.ValidatorUpdates)
	addExpectedBalance(expectedBalances, delegator, sei(2))
	addExpectedBalance(expectedBalances, escrowAddr, new(big.Int).Neg(sei(2)))
	requireNativeBalances(t, state, expectedBalances)
	requireStakingPool(t, state, "20000000", "0")
	requireStakingValidator(t, state, source, "14000000", "14000000", 3)
	requireStakingValidator(t, state, destination, "6000000", "6000000", 3)
	requireStakingDelegation(t, state, delegator, source, "4000000")
	requireStakingDelegation(t, state, delegator, destination, "1000000")
	requireNoStakingRedelegation(t, state, delegator, source, destination)
	requireNoStakingUnbonding(t, state, delegator, destination)
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

func blockContext(chainID *big.Int) BlockContext {
	return BlockContext{
		Number:   1,
		Time:     1,
		GasLimit: 30_000_000,
		ChainID:  chainID,
		BaseFee:  big.NewInt(0),
		Coinbase: common.HexToAddress("0x00000000000000000000000000000000000000cb"),
	}
}

func blockContextAt(chainID *big.Int, number uint64, blockTime uint64) BlockContext {
	ctx := blockContext(chainID)
	ctx.Number = number
	ctx.Time = blockTime
	return ctx
}

func executeBlockAndApply(t *testing.T, executor *Executor, state StateWriter, block BlockContext, txs [][]byte) *BlockResult {
	t.Helper()
	result, err := executor.ExecuteBlock(context.Background(), BlockRequest{
		Context: block,
		Txs:     txs,
	})
	require.NoError(t, err)
	state.ApplyChangeSet(result.ChangeSet)
	return result
}

func requireTxsSuccessful(t *testing.T, result *BlockResult, count int) {
	t.Helper()
	require.Len(t, result.Txs, count)
	require.Len(t, result.Receipts, count)
	for _, tx := range result.Txs {
		require.Equal(t, ethtypes.ReceiptStatusSuccessful, tx.Status)
		require.NoError(t, tx.Err)
	}
}

func mustPackStaking(t *testing.T, contract *stakingprecompile.Precompile, method string, args ...interface{}) []byte {
	t.Helper()
	input, err := contract.ABI().Pack(method, args...)
	require.NoError(t, err)
	return input
}

// sei returns amount whole SEI in wei. One SEI is 1e6 usei, so a sei(n) stake
// yields n consensus power under the 1e6 powerReduction.
func sei(amount int64) *big.Int {
	return new(big.Int).Mul(big.NewInt(amount), big.NewInt(1_000_000_000_000_000_000))
}

func debitExpectedBalance(expected map[common.Address]*big.Int, sender common.Address, value *big.Int, tx TxResult) {
	gasCost := new(big.Int).Mul(new(big.Int).SetUint64(tx.GasUsed), tx.EffectiveGasPrice)
	total := new(big.Int).Add(cloneBig(value), gasCost)
	addExpectedBalance(expected, sender, new(big.Int).Neg(total))
}

func addExpectedBalance(expected map[common.Address]*big.Int, addr common.Address, amount *big.Int) {
	if amount == nil || amount.Sign() == 0 {
		return
	}
	current := expected[addr]
	if current == nil {
		current = new(big.Int)
	}
	expected[addr] = new(big.Int).Add(current, amount)
}

func requireNativeBalances(t *testing.T, state StateReader, expected map[common.Address]*big.Int) {
	t.Helper()
	for addr, balance := range expected {
		require.Equal(t, balance, state.GetBalance(addr), "balance %s", addr.Hex())
	}
}

type stakingDelegationRecordForTest struct {
	DelegatorAddress string `json:"delegator_address"`
	ValidatorAddress string `json:"validator_address"`
	Amount           string `json:"amount"`
}

func requireStakingPool(t *testing.T, state StateReader, bonded string, notBonded string) {
	t.Helper()
	pool, ok := loadStakingJSON[stakingprecompile.Pool](t, state, []byte("pool"))
	require.True(t, ok)
	require.Equal(t, bonded, pool.BondedTokens)
	require.Equal(t, notBonded, pool.NotBondedTokens)
}

func requireStakingValidator(t *testing.T, state StateReader, validator common.Address, tokens string, shares string, status int32) {
	t.Helper()
	record, ok := loadStakingJSON[stakingprecompile.Validator](t, state, []byte("validator/"+validator.Hex()))
	require.True(t, ok)
	require.Equal(t, validator.Hex(), record.OperatorAddress)
	require.Equal(t, tokens, record.Tokens)
	require.Equal(t, shares, record.DelegatorShares)
	require.Equal(t, status, record.Status)
}

func requireStakingDelegation(t *testing.T, state StateReader, delegator common.Address, validator common.Address, amount string) {
	t.Helper()
	record, ok := loadStakingJSON[stakingDelegationRecordForTest](t, state, []byte("delegation/"+delegator.Hex()+"/"+validator.Hex()))
	require.True(t, ok)
	require.Equal(t, delegator.Hex(), record.DelegatorAddress)
	require.Equal(t, validator.Hex(), record.ValidatorAddress)
	require.Equal(t, amount, record.Amount)
}

func requireStakingRedelegation(t *testing.T, state StateReader, delegator common.Address, src common.Address, dst common.Address, amount string, completionTime int64) {
	t.Helper()
	record, ok := loadStakingJSON[stakingprecompile.Redelegation](t, state, stakingRedelegationKey(delegator, src, dst))
	require.True(t, ok)
	require.Equal(t, delegator.Hex(), record.DelegatorAddress)
	require.Equal(t, src.Hex(), record.ValidatorSrcAddress)
	require.Equal(t, dst.Hex(), record.ValidatorDstAddress)
	require.Len(t, record.Entries, 1)
	require.Equal(t, amount, record.Entries[0].InitialBalance)
	require.Equal(t, amount, record.Entries[0].SharesDst)
	require.Equal(t, completionTime, record.Entries[0].CompletionTime)
}

func requireNoStakingRedelegation(t *testing.T, state StateReader, delegator common.Address, src common.Address, dst common.Address) {
	t.Helper()
	_, ok := loadStakingJSON[stakingprecompile.Redelegation](t, state, stakingRedelegationKey(delegator, src, dst))
	require.False(t, ok)
}

func requireStakingUnbonding(t *testing.T, state StateReader, delegator common.Address, validator common.Address, amount string, completionTime int64) {
	t.Helper()
	record, ok := loadStakingJSON[stakingprecompile.UnbondingDelegation](t, state, stakingUnbondingKey(delegator, validator))
	require.True(t, ok)
	require.Equal(t, delegator.Hex(), record.DelegatorAddress)
	require.Equal(t, validator.Hex(), record.ValidatorAddress)
	require.Len(t, record.Entries, 1)
	require.Equal(t, amount, record.Entries[0].InitialBalance)
	require.Equal(t, amount, record.Entries[0].Balance)
	require.Equal(t, completionTime, record.Entries[0].CompletionTime)
}

func requireNoStakingUnbonding(t *testing.T, state StateReader, delegator common.Address, validator common.Address) {
	t.Helper()
	_, ok := loadStakingJSON[stakingprecompile.UnbondingDelegation](t, state, stakingUnbondingKey(delegator, validator))
	require.False(t, ok)
}

func loadStakingJSON[T any](t *testing.T, state StateReader, key []byte) (T, bool) {
	t.Helper()
	store := storageBackedStore{
		db:      newNativeStateDB(state),
		address: common.HexToAddress(stakingprecompile.StakingAddress),
	}
	value, ok, err := precompileutil.GetJSON[T](store, key)
	require.NoError(t, err)
	return value, ok
}

func stakingRedelegationKey(delegator common.Address, src common.Address, dst common.Address) []byte {
	return []byte("redelegation/" + delegator.Hex() + "\x00" + src.Hex() + "\x00" + dst.Hex())
}

func stakingUnbondingKey(delegator common.Address, validator common.Address) []byte {
	return []byte("unbonding/" + delegator.Hex() + "/" + validator.Hex())
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

type contractPrecompileRegistry map[common.Address]precompiles.Contract

func (r contractPrecompileRegistry) Get(addr common.Address) (precompiles.Contract, bool) {
	contract, ok := r[addr]
	return contract, ok
}

func (r contractPrecompileRegistry) Addresses() []common.Address {
	addresses := make([]common.Address, 0, len(r))
	for addr := range r {
		addresses = append(addresses, addr)
	}
	return addresses
}

type storeWritePrecompile struct{}

func (storeWritePrecompile) RequiredGas([]byte) uint64 {
	return 100
}

func (storeWritePrecompile) Run(ctx *precompiles.Context, _ []byte) ([]byte, error) {
	ctx.Store.Set([]byte("seen"), []byte{0xaa, 0xbb})
	return []byte{0x01}, nil
}
