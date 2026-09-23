package evmonlyapp

import (
	"errors"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/sei-protocol/sei-chain/giga/evmonly"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-db/bootstrap"
	seidbconfig "github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
)

const evmOnlyTestChainID uint64 = 713715

func decodeEVMOnlyTestTx(t *testing.T, raw []byte) *ethtypes.Transaction {
	t.Helper()
	tx := new(ethtypes.Transaction)
	require.NoError(t, tx.UnmarshalBinary(raw))
	return tx
}

func signedEVMOnlyTestTx(t *testing.T, chainID uint64, nonce uint64) ([]byte, common.Address) {
	t.Helper()
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	recipient := common.HexToAddress("0x1000000000000000000000000000000000000001")
	tx := ethtypes.NewTx(&ethtypes.LegacyTx{
		Nonce:    nonce,
		GasPrice: big.NewInt(evmOnlyMinGasPrice),
		Gas:      21_000,
		To:       &recipient,
		Value:    big.NewInt(1),
	})
	signed, err := ethtypes.SignTx(tx, ethtypes.LatestSignerForChainID(new(big.Int).SetUint64(chainID)), key)
	require.NoError(t, err)
	raw, err := signed.MarshalBinary()
	require.NoError(t, err)
	return raw, crypto.PubkeyToAddress(key.PublicKey)
}

// evmOnlyTestInitCode deploys a contract whose runtime code is the single
// INVALID opcode 0xfe: PUSH1 0xfe, PUSH1 0, MSTORE8, PUSH1 1, PUSH1 0, RETURN.
var evmOnlyTestInitCode = []byte{0x60, 0xfe, 0x60, 0x00, 0x53, 0x60, 0x01, 0x60, 0x00, 0xf3}

func signedEVMOnlyTestCreateTx(t *testing.T, chainID uint64) ([]byte, common.Address) {
	t.Helper()
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	tx := ethtypes.NewTx(&ethtypes.LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(evmOnlyMinGasPrice),
		Gas:      100_000,
		Data:     evmOnlyTestInitCode,
	})
	signed, err := ethtypes.SignTx(tx, ethtypes.LatestSignerForChainID(new(big.Int).SetUint64(chainID)), key)
	require.NoError(t, err)
	raw, err := signed.MarshalBinary()
	require.NoError(t, err)
	return raw, crypto.PubkeyToAddress(key.PublicKey)
}

func newInitializedEVMOnlyTestApp(t *testing.T) abci.Application {
	t.Helper()
	app := newEVMOnlyTestApp(t, nil)
	_, err := app.InitChain(&abci.RequestInitChain{
		InitialHeight: 1,
		ConsensusParams: &tmproto.ConsensusParams{
			Block: &tmproto.BlockParams{MaxGas: 30_000_000},
		},
	})
	require.NoError(t, err)
	return app
}

func newEVMOnlyTestApp(t *testing.T, validators []abci.ValidatorUpdate) abci.Application {
	t.Helper()
	storageConfig, err := seidbconfig.AutobahnStorageConfig(t.TempDir())
	require.NoError(t, err)
	storage, err := bootstrap.NewGigaStorageManager(t.Context(), storageConfig)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, storage.Close()) })
	return NewEVMOnlyApplication(evmOnlyTestChainID, validators, storage, evmonly.NewFlatKVChangeSetEncoder(storage.SC()))
}

// waitForReceiptVersion blocks until the receipt store has published height. The store applies
// writes in the background, so a receipt is readable once LatestVersion reaches its block rather
// than once SetReceipts returns.
func waitForReceiptVersion(t *testing.T, store receipt.ReceiptStore, height int64) {
	t.Helper()
	for store.LatestVersion() < height {
		select {
		case <-t.Context().Done():
			t.Fatalf("receipt store still at version %d before %d: %v", store.LatestVersion(), height, t.Context().Err())
		case <-time.After(time.Millisecond):
		}
	}
}

func TestEVMOnlyApplicationExecutesRawEthereumBlock(t *testing.T) {
	app := newInitializedEVMOnlyTestApp(t)
	raw, sender := signedEVMOnlyTestTx(t, evmOnlyTestChainID, 0)
	tx := new(ethtypes.Transaction)
	require.NoError(t, tx.UnmarshalBinary(raw))
	check := app.CheckTx(t.Context(), &abci.RequestCheckTxV2{Tx: raw})
	require.True(t, check.IsOK())
	require.True(t, check.IsEVM)
	require.Equal(t, sender, check.EVMSenderAddress)
	require.Equal(t, uint64(0), app.EvmNonce(sender))
	gotBalance := app.EvmBalance(sender, nil)
	require.Equal(t, evmOnlyBaseBalance, gotBalance.ToBig())

	response, err := app.FinalizeBlock(t.Context(), &abci.RequestFinalizeBlock{
		Txs:  [][]byte{raw},
		Hash: crypto.Keccak256([]byte("block-1")),
		Header: &tmproto.Header{
			Height: 1,
			Time:   time.Unix(1_700_000_001, 0),
		},
	})
	require.NoError(t, err)
	require.Len(t, response.AppHash, common.HashLength)
	require.Len(t, response.TxResults, 1)
	require.Positive(t, response.TxResults[0].GasUsed)
	_, err = app.Commit(t.Context())
	require.NoError(t, err)
	require.Equal(t, int64(1), app.LastBlockHeight())
	require.Equal(t, uint64(1), app.EvmNonce(sender))
	wantBalance := new(big.Int).Sub(
		new(big.Int).Sub(new(big.Int).Set(evmOnlyBaseBalance), big.NewInt(1)),
		new(big.Int).Mul(big.NewInt(evmOnlyMinGasPrice), big.NewInt(response.TxResults[0].GasUsed)),
	)
	gotBalance = app.EvmBalance(sender, nil)
	require.Equal(t, wantBalance, gotBalance.ToBig())
	require.Equal(t, response.AppHash, app.Info().LastBlockAppHash)
	receiptDB := app.(*evmOnlyApplication).storage.ReceiptDB()
	waitForReceiptVersion(t, receiptDB, 1)
	receiptCtx := sdk.NewContext(nil, tmproto.Header{Height: 1}, false).WithContext(t.Context())
	gotReceipt, err := receiptDB.GetReceipt(receiptCtx, tx.Hash())
	require.NoError(t, err)
	require.Equal(t, tx.Hash().Hex(), gotReceipt.TxHashHex)
	require.Equal(t, uint64(1), gotReceipt.BlockNumber)
}

func TestEVMOnlyApplicationRejectsWrongChain(t *testing.T) {
	app := newInitializedEVMOnlyTestApp(t)
	raw, _ := signedEVMOnlyTestTx(t, evmOnlyTestChainID+1, 0)

	response := app.CheckTx(t.Context(), &abci.RequestCheckTxV2{Tx: raw})

	require.True(t, response.IsErr())
}

// A zero priority fee is valid EIP-1559, and admitting on tx.GasPrice() — the
// fee cap on a dynamic-fee tx — let one through that the executor then refused.
// An executor refusal is a node panic rather than a failed receipt, so this has
// to be caught here.
func TestEVMOnlyApplicationRefusesATxTheExecutorWouldRefuse(t *testing.T) {
	app := newInitializedEVMOnlyTestApp(t)
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	recipient := common.HexToAddress("0x1000000000000000000000000000000000000001")
	chainID := new(big.Int).SetUint64(evmOnlyTestChainID)
	// Fee cap far above the minimum, tip cap zero. At a zero base fee the
	// effective gas price is the tip, so block validity refuses it.
	tx := ethtypes.NewTx(&ethtypes.DynamicFeeTx{
		ChainID:   chainID,
		Nonce:     0,
		GasTipCap: new(big.Int),
		GasFeeCap: big.NewInt(100 * evmOnlyMinGasPrice),
		Gas:       21_000,
		To:        &recipient,
		Value:     big.NewInt(1),
	})
	signed, err := ethtypes.SignTx(tx, ethtypes.LatestSignerForChainID(chainID), key)
	require.NoError(t, err)
	raw, err := signed.MarshalBinary()
	require.NoError(t, err)

	response := app.CheckTx(t.Context(), &abci.RequestCheckTxV2{Tx: raw})

	require.True(t, response.IsErr())
}

// A tip that clears the minimum still has to be admitted, or the fix has
// rejected every dynamic-fee transaction rather than the inexecutable ones.
func TestEVMOnlyApplicationAdmitsADynamicFeeTxThatClearsTheMinimum(t *testing.T) {
	app := newInitializedEVMOnlyTestApp(t)
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	recipient := common.HexToAddress("0x1000000000000000000000000000000000000001")
	chainID := new(big.Int).SetUint64(evmOnlyTestChainID)
	tx := ethtypes.NewTx(&ethtypes.DynamicFeeTx{
		ChainID:   chainID,
		Nonce:     0,
		GasTipCap: big.NewInt(evmOnlyMinGasPrice),
		GasFeeCap: big.NewInt(100 * evmOnlyMinGasPrice),
		Gas:       21_000,
		To:        &recipient,
		Value:     big.NewInt(1),
	})
	signed, err := ethtypes.SignTx(tx, ethtypes.LatestSignerForChainID(chainID), key)
	require.NoError(t, err)
	raw, err := signed.MarshalBinary()
	require.NoError(t, err)

	response := app.CheckTx(t.Context(), &abci.RequestCheckTxV2{Tx: raw})

	require.True(t, response.IsOK())
}

func TestEVMOnlyApplicationProducesDeterministicRoot(t *testing.T) {
	raw, _ := signedEVMOnlyTestTx(t, evmOnlyTestChainID, 0)
	request := &abci.RequestFinalizeBlock{
		Txs:  [][]byte{raw},
		Hash: crypto.Keccak256([]byte("same-block")),
		Header: &tmproto.Header{
			Height: 1,
			Time:   time.Unix(1_700_000_001, 0),
		},
	}
	first := newInitializedEVMOnlyTestApp(t)
	second := newInitializedEVMOnlyTestApp(t)

	firstResponse, err := first.FinalizeBlock(t.Context(), request)
	require.NoError(t, err)
	secondResponse, err := second.FinalizeBlock(t.Context(), request)
	require.NoError(t, err)

	require.Equal(t, firstResponse.AppHash, secondResponse.AppHash)
}

func TestEVMOnlyApplicationExecutesCheckedTxLikeUncheckedTx(t *testing.T) {
	raw, sender := signedEVMOnlyTestTx(t, evmOnlyTestChainID, 0)
	request := &abci.RequestFinalizeBlock{
		Txs:  [][]byte{raw},
		Hash: crypto.Keccak256([]byte("checked-block")),
		Header: &tmproto.Header{
			Height: 1,
			Time:   time.Unix(1_700_000_001, 0),
		},
	}
	checked, ok := newInitializedEVMOnlyTestApp(t).(*evmOnlyApplication)
	require.True(t, ok)
	unchecked := newInitializedEVMOnlyTestApp(t)

	check := checked.CheckTx(t.Context(), &abci.RequestCheckTxV2{Tx: raw})
	require.True(t, check.IsOK())
	require.Equal(t, sender, check.EVMSenderAddress)
	for senders := range checked.checkedSenders.Lock() {
		require.Equal(t, map[common.Hash]common.Address{decodeEVMOnlyTestTx(t, raw).Hash(): sender}, senders.fresh)
	}
	checkedResponse, err := checked.FinalizeBlock(t.Context(), request)
	require.NoError(t, err)
	for senders := range checked.checkedSenders.Lock() {
		require.Empty(t, senders.fresh)
	}
	uncheckedResponse, err := unchecked.FinalizeBlock(t.Context(), request)
	require.NoError(t, err)

	require.Equal(t, uncheckedResponse.AppHash, checkedResponse.AppHash)
	require.Equal(t, uncheckedResponse.TxResults[0].GasUsed, checkedResponse.TxResults[0].GasUsed)
	_, err = checked.Commit(t.Context())
	require.NoError(t, err)
	require.Equal(t, uint64(1), checked.EvmNonce(sender))
}

func TestSenderCacheKeepsRecentEntriesAcrossRollover(t *testing.T) {
	hashOf := func(i int) common.Hash { return common.BigToHash(big.NewInt(int64(i))) }
	addrOf := func(i int) common.Address { return common.BigToAddress(big.NewInt(int64(i))) }
	cache := newSenderCache()
	for i := range 2*checkedSendersCap + 1 {
		cache.put(hashOf(i), addrOf(i))
	}

	require.Equal(t, utils.None[common.Address](), cache.take(hashOf(0)))
	require.Equal(t, utils.Some(addrOf(checkedSendersCap)), cache.take(hashOf(checkedSendersCap)))
	require.Equal(t, utils.Some(addrOf(2*checkedSendersCap)), cache.take(hashOf(2*checkedSendersCap)))
	require.Equal(t, utils.None[common.Address](), cache.take(hashOf(checkedSendersCap)))
}

func TestEVMOnlyApplicationRequiresInitChain(t *testing.T) {
	app := newEVMOnlyTestApp(t, nil)

	_, err := app.FinalizeBlock(t.Context(), &abci.RequestFinalizeBlock{
		Hash: crypto.Keccak256([]byte("block-1")),
		Header: &tmproto.Header{
			Height: 1,
			Time:   time.Unix(1_700_000_001, 0),
		},
	})

	require.Error(t, err)
}

func TestEVMOnlyABCIResultsCarryRevertReasonWithoutFailingTheTx(t *testing.T) {
	result := &evmonly.BlockResult{
		Txs: []evmonly.TxResult{
			{GasUsed: 21_000, Status: ethtypes.ReceiptStatusSuccessful},
			{GasUsed: 21_000, Status: ethtypes.ReceiptStatusFailed, Err: errors.New("execution reverted")},
		},
	}

	txResults := evmOnlyABCIResults(result)

	require.Equal(t, abci.CodeTypeOK, txResults[0].Code)
	require.Empty(t, txResults[0].Log)
	require.Equal(t, abci.CodeTypeOK, txResults[1].Code, "a revert must not mark the tx for a mempool retry")
	require.Equal(t, "execution reverted", txResults[1].Log)
}

func TestEVMOnlyApplicationReturnsConfiguredValidators(t *testing.T) {
	configured := []abci.ValidatorUpdate{{Power: 7}}
	app := newEVMOnlyTestApp(t, configured)
	configured[0].Power = 11

	first := app.GetValidators()
	require.Equal(t, []abci.ValidatorUpdate{{Power: 7}}, first)
	first[0].Power = 13
	require.Equal(t, []abci.ValidatorUpdate{{Power: 7}}, app.GetValidators())
}

// evmGasLimiter is implemented by an application that exposes its committed
// block gas limit, matching proxy.evmGasLimitProvider.
type evmGasLimiter interface {
	EvmGasLimit() uint64
}

func TestEVMOnlyApplicationEvmGasLimitReflectsConsensusParams(t *testing.T) {
	app := newInitializedEVMOnlyTestApp(t)
	gasLimiter, ok := app.(evmGasLimiter)
	require.True(t, ok)
	require.Equal(t, uint64(30_000_000), gasLimiter.EvmGasLimit())

	_, err := app.FinalizeBlock(t.Context(), &abci.RequestFinalizeBlock{
		Hash: crypto.Keccak256([]byte("block-1")),
		Header: &tmproto.Header{
			Height: 1,
			Time:   time.Unix(1_700_000_001, 0),
		},
	})
	require.NoError(t, err)
	_, err = app.Commit(t.Context())
	require.NoError(t, err)

	require.Equal(t, uint64(30_000_000), gasLimiter.EvmGasLimit())
}

// evmMinGasPricer is implemented by an application that exposes its
// admission gas-price floor, matching proxy.evmMinGasPriceProvider.
type evmMinGasPricer interface {
	EvmMinGasPrice() *big.Int
}

func TestEVMOnlyApplicationEvmMinGasPrice(t *testing.T) {
	app := newInitializedEVMOnlyTestApp(t)
	minGasPricer, ok := app.(evmMinGasPricer)
	require.True(t, ok)
	require.Equal(t, big.NewInt(evmOnlyMinGasPrice), minGasPricer.EvmMinGasPrice())
}

func TestEVMOnlyApplicationServesDeployedCode(t *testing.T) {
	app := newInitializedEVMOnlyTestApp(t)
	raw, sender := signedEVMOnlyTestCreateTx(t, evmOnlyTestChainID)
	contract := crypto.CreateAddress(sender, 0)
	codeReader := app.(*evmOnlyApplication)
	require.Empty(t, codeReader.EvmCode(contract))
	require.Empty(t, codeReader.EvmCode(sender))

	response, err := app.FinalizeBlock(t.Context(), &abci.RequestFinalizeBlock{
		Txs:  [][]byte{raw},
		Hash: crypto.Keccak256([]byte("block-1")),
		Header: &tmproto.Header{
			Height: 1,
			Time:   time.Unix(1_700_000_001, 0),
		},
	})
	require.NoError(t, err)
	require.Len(t, response.TxResults, 1)
	require.Equal(t, uint32(0), response.TxResults[0].Code)
	_, err = app.Commit(t.Context())
	require.NoError(t, err)

	got := codeReader.EvmCode(contract)
	require.Equal(t, []byte{0xfe}, got)
	// The returned slice is a copy: mutating it must not alter the next read.
	got[0] = 0x00
	require.Equal(t, []byte{0xfe}, codeReader.EvmCode(contract))
	require.Empty(t, codeReader.EvmCode(sender))
}
