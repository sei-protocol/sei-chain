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
