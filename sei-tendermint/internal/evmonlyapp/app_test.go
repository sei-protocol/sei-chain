package evmonlyapp

import (
	"context"
	"crypto/ecdsa"
	"encoding/binary"
	"errors"
	"fmt"
	"math/big"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	ethcore "github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	gigaconfig "github.com/sei-protocol/sei-chain/giga/config"
	"github.com/sei-protocol/sei-chain/giga/evmonly"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-db/bootstrap"
	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	seidbmetrics "github.com/sei-protocol/sei-chain/sei-db/common/metrics"
	seidbconfig "github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
)

const evmOnlyTestChainID uint64 = 713715

// evmOnlyMinGasPrice is the admission price the default execution config runs at.
const evmOnlyMinGasPrice = 1_000_000_000

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
	return signedEVMOnlyTestTxFrom(t, key, chainID, nonce), crypto.PubkeyToAddress(key.PublicKey)
}

func signedEVMOnlyTestTxFrom(t *testing.T, key *ecdsa.PrivateKey, chainID uint64, nonce uint64) []byte {
	t.Helper()
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
	return raw
}

func evmOnlyTestBlock(height int64, txs ...[]byte) *abci.RequestFinalizeBlock {
	return &abci.RequestFinalizeBlock{
		Txs:  txs,
		Hash: crypto.Keccak256(binary.BigEndian.AppendUint64([]byte("block-"), uint64(height))), //nolint:gosec // G115: test heights are positive.
		Header: &tmproto.Header{
			Height: height,
			Time:   time.Unix(1_700_000_000+height, 0),
		},
	}
}

func finalizeAndCommitEVMOnlyTestBlock(t *testing.T, app abci.Application, req *abci.RequestFinalizeBlock) []byte {
	t.Helper()
	response, err := app.FinalizeBlock(t.Context(), req)
	require.NoError(t, err)
	_, err = app.Commit(t.Context())
	require.NoError(t, err)
	return response.AppHash
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

func evmOnlyTestInitChain() *abci.RequestInitChain {
	return &abci.RequestInitChain{
		InitialHeight: 1,
		ConsensusParams: &tmproto.ConsensusParams{
			Block: &tmproto.BlockParams{MaxGas: 30_000_000},
		},
	}
}

func newInitializedEVMOnlyTestApp(t *testing.T) abci.Application {
	t.Helper()
	app := newEVMOnlyTestApp(t, nil)
	_, err := app.InitChain(evmOnlyTestInitChain())
	require.NoError(t, err)
	return app
}

func newEVMOnlyTestApp(t *testing.T, validators []abci.ValidatorUpdate) abci.Application {
	t.Helper()
	return newEVMOnlyTestAppWithExecution(t, validators, gigaconfig.DefaultConfig.Execution)
}

func newEVMOnlyTestAppWithExecution(
	t *testing.T,
	validators []abci.ValidatorUpdate,
	execution gigaconfig.ExecutionConfig,
) abci.Application {
	t.Helper()
	storageConfig, err := seidbconfig.AutobahnStorageConfig(t.TempDir())
	require.NoError(t, err)
	return newEVMOnlyTestAppWithStorage(t, validators, execution, storageConfig)
}

func newEVMOnlyTestAppWithStorage(
	t *testing.T,
	validators []abci.ValidatorUpdate,
	execution gigaconfig.ExecutionConfig,
	storageConfig *seidbconfig.GigaStorageConfig,
) abci.Application {
	t.Helper()
	storage := openEVMOnlyTestStorage(t, storageConfig)
	app, err := NewEVMOnlyApplication(
		evmOnlyTestChainID,
		validators,
		storage,
		evmonly.NewFlatKVChangeSetEncoder(storage.SC()),
		execution,
	)
	require.NoError(t, err)
	t.Cleanup(func() { closeEVMOnlyTestApp(t, app, storage) })
	return app
}

// openEVMOnlyTestStorage opens Giga storage that outlives the test body: the
// last block's commit may still be landing when it ends, and it is settled from
// a cleanup, which runs after t.Context() is cancelled.
func openEVMOnlyTestStorage(t *testing.T, storageConfig *seidbconfig.GigaStorageConfig) *bootstrap.GigaStorageManager {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	storage, err := bootstrap.NewGigaStorageManager(ctx, storageConfig)
	require.NoError(t, err)
	return storage
}

// openEVMOnlyTestStorageAt opens the Autobahn storage layout under home, so a
// test can reopen the same store the way a restarted process does.
func openEVMOnlyTestStorageAt(t *testing.T, home string) *bootstrap.GigaStorageManager {
	t.Helper()
	storageConfig, err := seidbconfig.AutobahnStorageConfig(home)
	require.NoError(t, err)
	return openEVMOnlyTestStorage(t, storageConfig)
}

// closeEVMOnlyTestApp closes storage the way the node does: after the
// application has landed every block commit it started.
func closeEVMOnlyTestApp(t *testing.T, app abci.Application, storage *bootstrap.GigaStorageManager) {
	t.Helper()
	settler, ok := app.(*evmOnlyApplication)
	require.True(t, ok)
	require.NoError(t, settler.AwaitCommits())
	require.NoError(t, storage.Close())
}

// reopenEVMOnlyTestApp closes app's storage and constructs a fresh application
// over the same home, the way a restarted process does.
func reopenEVMOnlyTestApp(t *testing.T, app abci.Application, storage *bootstrap.GigaStorageManager, home string) (abci.Application, *bootstrap.GigaStorageManager) {
	t.Helper()
	closeEVMOnlyTestApp(t, app, storage)
	reopened := openEVMOnlyTestStorageAt(t, home)
	resumed, err := NewEVMOnlyApplication(evmOnlyTestChainID, nil, reopened, evmonly.NewFlatKVChangeSetEncoder(reopened.SC()), gigaconfig.DefaultConfig.Execution)
	require.NoError(t, err)
	return resumed, reopened
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

func TestEVMOnlyApplicationFinalizesBlocksWithoutReceiptStore(t *testing.T) {
	storageConfig, err := seidbconfig.AutobahnStorageConfig(t.TempDir())
	require.NoError(t, err)
	storageConfig.ReceiptDBConfig.Enable = false
	app := newEVMOnlyTestAppWithStorage(t, nil, gigaconfig.DefaultConfig.Execution, storageConfig)
	require.Nil(t, app.(*evmOnlyApplication).storage.ReceiptDB())
	_, err = app.InitChain(&abci.RequestInitChain{
		InitialHeight: 1,
		ConsensusParams: &tmproto.ConsensusParams{
			Block: &tmproto.BlockParams{MaxGas: 30_000_000},
		},
	})
	require.NoError(t, err)
	raw, sender := signedEVMOnlyTestTx(t, evmOnlyTestChainID, 0)

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
	require.True(t, response.TxResults[0].IsOK())
	_, err = app.Commit(t.Context())
	require.NoError(t, err)
	require.Equal(t, int64(1), app.LastBlockHeight())
	require.Equal(t, uint64(1), app.EvmNonce(sender))
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

// The cursor does not carry the block time, so a resumed app would answer
// EvmCall with TIMESTAMP 0 until the next Commit. The router seeds it through
// InitLastHeader on its restart path.
func TestEVMOnlyApplicationInitLastHeaderSeedsBlockTime(t *testing.T) {
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	const blocks = 3
	home := t.TempDir()
	storage := openEVMOnlyTestStorageAt(t, home)
	app, err := NewEVMOnlyApplication(evmOnlyTestChainID, nil, storage, evmonly.NewFlatKVChangeSetEncoder(storage.SC()), gigaconfig.DefaultConfig.Execution)
	require.NoError(t, err)
	_, err = app.InitChain(evmOnlyTestInitChain())
	require.NoError(t, err)
	var last *abci.RequestFinalizeBlock
	for height := range int64(blocks) {
		last = evmOnlyTestBlock(height+1, signedEVMOnlyTestTxFrom(t, key, evmOnlyTestChainID, uint64(height))) //nolint:gosec // G115: test heights are positive.
		finalizeAndCommitEVMOnlyTestBlock(t, app, last)
	}

	app, storage = reopenEVMOnlyTestApp(t, app, storage, home)
	t.Cleanup(func() { closeEVMOnlyTestApp(t, app, storage) })

	resumed, ok := app.(*evmOnlyApplication)
	require.True(t, ok)
	for state := range resumed.cursor.Lock() {
		require.Zero(t, state.lastBlockTime, "a resumed app has no block time before InitLastHeader")
	}
	resumed.InitLastHeader(last.Header)
	for state := range resumed.cursor.Lock() {
		require.Equal(t, uint64(last.Header.Time.Unix()), state.lastBlockTime) //nolint:gosec // G115: test times are positive.
	}
}

// A restarted node must resume from the height and app hash its storage holds,
// and continue executing without an InitChain. The reference app runs the same
// blocks without restarting, so the resumed chain has to match it hash for hash.
func TestEVMOnlyApplicationResumesFromStorageAfterRestart(t *testing.T) {
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	sender := crypto.PubkeyToAddress(key.PublicKey)
	const blocks = 3
	block := func(height int64) *abci.RequestFinalizeBlock {
		return evmOnlyTestBlock(height, signedEVMOnlyTestTxFrom(t, key, evmOnlyTestChainID, uint64(height-1))) //nolint:gosec // G115: test heights are positive.
	}

	reference := newInitializedEVMOnlyTestApp(t)
	wantHashes := make([][]byte, 0, blocks+1)
	for height := range int64(blocks + 1) {
		wantHashes = append(wantHashes, finalizeAndCommitEVMOnlyTestBlock(t, reference, block(height+1)))
	}

	home := t.TempDir()
	storage := openEVMOnlyTestStorageAt(t, home)
	app, err := NewEVMOnlyApplication(evmOnlyTestChainID, nil, storage, evmonly.NewFlatKVChangeSetEncoder(storage.SC()), gigaconfig.DefaultConfig.Execution)
	require.NoError(t, err)
	_, err = app.InitChain(evmOnlyTestInitChain())
	require.NoError(t, err)
	for height := range int64(blocks) {
		require.Equal(t, wantHashes[height], finalizeAndCommitEVMOnlyTestBlock(t, app, block(height+1)))
	}

	app, storage = reopenEVMOnlyTestApp(t, app, storage, home)
	t.Cleanup(func() { closeEVMOnlyTestApp(t, app, storage) })

	info := app.Info()
	require.Equal(t, int64(blocks), info.LastBlockHeight)
	require.Equal(t, wantHashes[blocks-1], info.LastBlockAppHash)
	require.Equal(t, uint64(blocks), app.EvmNonce(sender))
	_, err = app.InitChain(evmOnlyTestInitChain())
	require.Error(t, err)
	require.Equal(t, wantHashes[blocks], finalizeAndCommitEVMOnlyTestBlock(t, app, block(blocks+1)))
	require.Equal(t, int64(blocks+1), app.LastBlockHeight())
}

// State is committed by FinalizeBlock, so a crash before Commit leaves the
// finalized block durable. The restarted node must report it rather than
// execute it a second time.
// An app resumed from its cursor builds its executor without InitChain, and
// must still publish it to the settler, or committed-state readers and storage
// shutdown would not wait for the block commits it starts.
func TestEVMOnlyApplicationResumedAppSettlesItsCommits(t *testing.T) {
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	sender := crypto.PubkeyToAddress(key.PublicKey)
	home := t.TempDir()
	storage := openEVMOnlyTestStorageAt(t, home)
	app, err := NewEVMOnlyApplication(evmOnlyTestChainID, nil, storage, evmonly.NewFlatKVChangeSetEncoder(storage.SC()), gigaconfig.DefaultConfig.Execution)
	require.NoError(t, err)
	_, err = app.InitChain(evmOnlyTestInitChain())
	require.NoError(t, err)
	finalizeAndCommitEVMOnlyTestBlock(t, app, evmOnlyTestBlock(1, signedEVMOnlyTestTxFrom(t, key, evmOnlyTestChainID, 0)))

	app, storage = reopenEVMOnlyTestApp(t, app, storage, home)
	t.Cleanup(func() { closeEVMOnlyTestApp(t, app, storage) })

	resumed, ok := app.(*evmOnlyApplication)
	require.True(t, ok)
	require.True(t, resumed.settler.Load().IsPresent(), "a resumed app must publish its executor to the settler")
	finalizeAndCommitEVMOnlyTestBlock(t, app, evmOnlyTestBlock(2, signedEVMOnlyTestTxFrom(t, key, evmOnlyTestChainID, 1)))
	require.Equal(t, uint64(2), app.EvmNonce(sender))
}

func TestEVMOnlyApplicationResumesFromBlockFinalizedButNotCommitted(t *testing.T) {
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	block := func(height int64) *abci.RequestFinalizeBlock {
		return evmOnlyTestBlock(height, signedEVMOnlyTestTxFrom(t, key, evmOnlyTestChainID, uint64(height-1))) //nolint:gosec // G115: test heights are positive.
	}

	home := t.TempDir()
	storage := openEVMOnlyTestStorageAt(t, home)
	app, err := NewEVMOnlyApplication(evmOnlyTestChainID, nil, storage, evmonly.NewFlatKVChangeSetEncoder(storage.SC()), gigaconfig.DefaultConfig.Execution)
	require.NoError(t, err)
	_, err = app.InitChain(evmOnlyTestInitChain())
	require.NoError(t, err)
	finalizeAndCommitEVMOnlyTestBlock(t, app, block(1))
	finalized, err := app.FinalizeBlock(t.Context(), block(2))
	require.NoError(t, err)

	app, storage = reopenEVMOnlyTestApp(t, app, storage, home)
	t.Cleanup(func() { closeEVMOnlyTestApp(t, app, storage) })

	info := app.Info()
	require.Equal(t, int64(2), info.LastBlockHeight)
	require.Equal(t, finalized.AppHash, info.LastBlockAppHash)
	finalizeAndCommitEVMOnlyTestBlock(t, app, block(3))
	require.Equal(t, int64(3), app.LastBlockHeight())
}

// InitChain with an initial height above one seeds the store before any block
// exists. Crashing there must still allow InitChain to run again.
func TestEVMOnlyApplicationRepeatsInitChainAfterSeedingOnly(t *testing.T) {
	home := t.TempDir()
	storage := openEVMOnlyTestStorageAt(t, home)
	app, err := NewEVMOnlyApplication(evmOnlyTestChainID, nil, storage, evmonly.NewFlatKVChangeSetEncoder(storage.SC()), gigaconfig.DefaultConfig.Execution)
	require.NoError(t, err)
	init := evmOnlyTestInitChain()
	init.InitialHeight = 5
	_, err = app.InitChain(init)
	require.NoError(t, err)

	app, storage = reopenEVMOnlyTestApp(t, app, storage, home)
	t.Cleanup(func() { closeEVMOnlyTestApp(t, app, storage) })

	require.Equal(t, int64(0), app.Info().LastBlockHeight)
	_, err = app.InitChain(init)
	require.NoError(t, err)
	require.Equal(t, int64(4), app.Info().LastBlockHeight)
	finalizeAndCommitEVMOnlyTestBlock(t, app, evmOnlyTestBlock(5))
	require.Equal(t, int64(5), app.LastBlockHeight())
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

func TestEVMOnlyApplicationCommitsBlockWithStaleNonce(t *testing.T) {
	app := newInitializedEVMOnlyTestApp(t)
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	sender := crypto.PubkeyToAddress(key.PublicKey)
	first := signedEVMOnlyTestTxFrom(t, key, evmOnlyTestChainID, 0)
	next := signedEVMOnlyTestTxFrom(t, key, evmOnlyTestChainID, 1)
	finalizeAndCommitEVMOnlyTestBlock(t, app, evmOnlyTestBlock(1, first))

	response, err := app.FinalizeBlock(t.Context(), evmOnlyTestBlock(2, first, next, next))
	require.NoError(t, err)
	require.Len(t, response.TxResults, 3)
	for _, i := range []int{0, 2} {
		require.Equal(t, uint32(abci.CodeTypeOK), response.TxResults[i].Code)
		require.Equal(t, int64(0), response.TxResults[i].GasUsed)
		require.True(t, response.TxResults[i].Log != "")
	}
	require.Equal(t, int64(21_000), response.TxResults[1].GasUsed)
	_, err = app.Commit(t.Context())
	require.NoError(t, err)
	require.Equal(t, int64(2), app.LastBlockHeight())
	require.Equal(t, uint64(2), app.EvmNonce(sender))
	finalizeAndCommitEVMOnlyTestBlock(t, app, evmOnlyTestBlock(3))
}

// A transaction rejected for a nonce gap leaves no receipt behind, so its hash is
// still free for the receipt of the block in which it finally executes.
func TestEVMOnlyApplicationReceiptFollowsLateExecution(t *testing.T) {
	app := newInitializedEVMOnlyTestApp(t)
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	first := signedEVMOnlyTestTxFrom(t, key, evmOnlyTestChainID, 0)
	second := signedEVMOnlyTestTxFrom(t, key, evmOnlyTestChainID, 1)

	response, err := app.FinalizeBlock(t.Context(), evmOnlyTestBlock(1, second))
	require.NoError(t, err)
	require.Equal(t, int64(0), response.TxResults[0].GasUsed)
	_, err = app.Commit(t.Context())
	require.NoError(t, err)
	finalizeAndCommitEVMOnlyTestBlock(t, app, evmOnlyTestBlock(2, first))
	finalizeAndCommitEVMOnlyTestBlock(t, app, evmOnlyTestBlock(3, second))

	receiptDB := app.(*evmOnlyApplication).storage.ReceiptDB()
	waitForReceiptVersion(t, receiptDB, 3)
	secondTx := new(ethtypes.Transaction)
	require.NoError(t, secondTx.UnmarshalBinary(second))
	receiptCtx := sdk.NewContext(nil, tmproto.Header{Height: 3}, false).WithContext(t.Context())
	got, err := receiptDB.GetReceipt(receiptCtx, secondTx.Hash())
	require.NoError(t, err)
	require.Equal(t, uint64(3), got.BlockNumber)
	require.Equal(t, uint32(ethtypes.ReceiptStatusSuccessful), got.Status)
	require.Equal(t, uint64(21_000), got.GasUsed)
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

// A node whose operator raised the admission floor must still execute a block
// that a node on the default floor proposed, and one that lowered it must not
// admit what the block would refuse.
func TestEVMOnlyApplicationMinGasPriceIsAdmissionOnly(t *testing.T) {
	raw, _ := signedEVMOnlyTestTx(t, evmOnlyTestChainID, 0)
	block := &abci.RequestFinalizeBlock{
		Txs:  [][]byte{raw},
		Hash: crypto.Keccak256([]byte("block-1")),
		Header: &tmproto.Header{
			Height: 1,
			Time:   time.Unix(1_700_000_001, 0),
		},
	}
	initChain := func(app abci.Application) {
		_, err := app.InitChain(&abci.RequestInitChain{
			InitialHeight: 1,
			ConsensusParams: &tmproto.ConsensusParams{
				Block: &tmproto.BlockParams{MaxGas: 30_000_000},
			},
		})
		require.NoError(t, err)
	}

	proposer := newInitializedEVMOnlyTestApp(t)
	require.True(t, proposer.CheckTx(t.Context(), &abci.RequestCheckTxV2{Tx: raw}).IsOK())
	proposed, err := proposer.FinalizeBlock(t.Context(), block)
	require.NoError(t, err)

	raised := gigaconfig.DefaultConfig.Execution
	raised.MinGasPrice = 2 * evmOnlyMinGasPrice
	strict := newEVMOnlyTestAppWithExecution(t, nil, raised)
	initChain(strict)
	require.Equal(t, big.NewInt(2*evmOnlyMinGasPrice), strict.(evmMinGasPricer).EvmMinGasPrice())
	require.False(t, strict.CheckTx(t.Context(), &abci.RequestCheckTxV2{Tx: raw}).IsOK())
	followed, err := strict.FinalizeBlock(t.Context(), block)
	require.NoError(t, err)
	require.Equal(t, proposed.AppHash, followed.AppHash)

	lowered := gigaconfig.DefaultConfig.Execution
	lowered.MinGasPrice = 1
	lax := newEVMOnlyTestAppWithExecution(t, nil, lowered)
	initChain(lax)
	require.Equal(t, big.NewInt(evmOnlyMinGasPrice), lax.(evmMinGasPricer).EvmMinGasPrice())
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

// Every FinalizeBlock stage is charged to a phase, so the timer's total is the
// time the block loop spent inside the application.
func TestEVMOnlyApplicationTimesEveryFinalizeBlockPhase(t *testing.T) {
	app := newInitializedEVMOnlyTestApp(t)
	evmOnlyApp, ok := app.(*evmOnlyApplication)
	require.True(t, ok)
	reader := sdkmetric.NewManualReader()
	meter := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter(finalizeMeterName)
	evmOnlyApp.finalizePhases = seidbmetrics.NewPhaseTimer(meter, finalizeTimerName)

	raw, _ := signedEVMOnlyTestTx(t, evmOnlyTestChainID, 0)
	_, err := app.FinalizeBlock(t.Context(), &abci.RequestFinalizeBlock{
		Txs:  [][]byte{raw},
		Hash: crypto.Keccak256([]byte("block-1")),
		Header: &tmproto.Header{
			Height: 1,
			Time:   time.Unix(1_700_000_001, 0),
		},
	})
	require.NoError(t, err)
	_, err = app.Commit(t.Context())
	require.NoError(t, err)

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &rm))
	phases := map[string]struct{}{}
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "evmonly_finalize_phase_duration_seconds_total" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[float64])
			require.True(t, ok)
			for _, point := range sum.DataPoints {
				phase, ok := point.Attributes.Value("phase")
				require.True(t, ok)
				phases[phase.AsString()] = struct{}{}
			}
		}
	}
	for _, want := range []string{finalizePhaseTakeSenders, finalizePhasePrepare, finalizePhaseExecute, finalizePhaseTxResults} {
		_, ok := phases[want]
		require.True(t, ok, "phase %q not recorded", want)
	}
}

// TestHashRawTxsMatchesKeccak256Hash pins hashRawTxs to crypto.Keccak256Hash, which keys the sender cache.
func TestHashRawTxsMatchesKeccak256Hash(t *testing.T) {
	for _, count := range []int{0, 1, 2, 17, 64, 65, 200, 1848} {
		t.Run(fmt.Sprintf("count=%d", count), func(t *testing.T) {
			txs := make([][]byte, count)
			for i := range txs {
				txs[i] = []byte(fmt.Sprintf("raw transaction %d with a body of some length", i))
			}
			hashes := hashRawTxs(txs)
			require.Equal(t, count, len(hashes))
			for i, raw := range txs {
				require.Equal(t, crypto.Keccak256Hash(raw), hashes[i], "tx %d", i)
			}
		})
	}
}

// A block's state lands in the store behind FinalizeBlock. Readers of committed
// state see it before Commit, and the next block builds on it whether or not
// the store has caught up.
func TestEVMOnlyApplicationReadsSettleBehindFinalizeBlock(t *testing.T) {
	app := newInitializedEVMOnlyTestApp(t)
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	sender := crypto.PubkeyToAddress(key.PublicKey)
	block := func(height int64) *abci.RequestFinalizeBlock {
		return evmOnlyTestBlock(height, signedEVMOnlyTestTxFrom(t, key, evmOnlyTestChainID, uint64(height-1))) //nolint:gosec // G115: test heights are positive.
	}

	for height := range int64(4) {
		_, err := app.FinalizeBlock(t.Context(), block(height+1))
		require.NoError(t, err)
		require.Equal(t, uint64(height+1), app.EvmNonce(sender)) //nolint:gosec // G115: test heights are positive.
		require.Equal(t, height, app.LastBlockHeight())
		_, err = app.Commit(t.Context())
		require.NoError(t, err)
		require.Equal(t, height+1, app.LastBlockHeight())
	}

	settler, ok := app.(*evmOnlyApplication)
	require.True(t, ok)
	require.NoError(t, settler.AwaitCommits())
	latest, err := settler.storage.SC().GetLatestVersion()
	require.NoError(t, err)
	require.Equal(t, int64(4), latest)
}

// unwritableEVMChangeSetEncoder encodes every block with an EVM pair the store
// refuses to apply, so the block executes and encodes cleanly and its commit is
// the first thing that fails.
func unwritableEVMChangeSetEncoder(evmonly.StateChangeSet) ([]*proto.NamedChangeSet, error) {
	return []*proto.NamedChangeSet{{
		Name:      keys.EVMStoreKey,
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{{Key: nil, Value: []byte{1}}}},
	}}, nil
}

// A block's commit lands behind FinalizeBlock: the block whose commit fails is
// still finalized and committed, and the failure surfaces from the next
// FinalizeBlock, from read-only calls, and from settling the store, while the
// store itself stays at the last version that landed.
func TestEVMOnlyApplicationSurfacesAFailedCommitFromTheNextBlock(t *testing.T) {
	storageConfig, err := seidbconfig.AutobahnStorageConfig(t.TempDir())
	require.NoError(t, err)
	storage := openEVMOnlyTestStorage(t, storageConfig)
	t.Cleanup(func() { require.NoError(t, storage.Close()) })
	app, err := NewEVMOnlyApplication(evmOnlyTestChainID, nil, storage, unwritableEVMChangeSetEncoder, gigaconfig.DefaultConfig.Execution)
	require.NoError(t, err)
	_, err = app.InitChain(&abci.RequestInitChain{
		InitialHeight: 1,
		ConsensusParams: &tmproto.ConsensusParams{
			Block: &tmproto.BlockParams{MaxGas: 30_000_000},
		},
	})
	require.NoError(t, err)
	settler, ok := app.(*evmOnlyApplication)
	require.True(t, ok)

	// Block 1 is unwritable, yet it finalizes and commits: the write has not
	// been waited for.
	raw, _ := signedEVMOnlyTestTx(t, evmOnlyTestChainID, 0)
	finalizeAndCommitEVMOnlyTestBlock(t, app, evmOnlyTestBlock(1, raw))
	require.Equal(t, int64(1), app.LastBlockHeight())

	// The failed write is reported by the next block, and stays reported.
	_, err = app.FinalizeBlock(t.Context(), evmOnlyTestBlock(2))
	require.Error(t, err)
	require.Error(t, settler.AwaitCommits())
	_, err = settler.EvmCall(t.Context(), &ethcore.Message{GasLimit: 21_000, GasPrice: new(uint256.Int), Value: new(uint256.Int)})
	require.Error(t, err)

	// The block that failed to finalize left nothing staged, and the store never
	// moved past genesis.
	_, err = app.Commit(t.Context())
	require.Error(t, err)
	require.Equal(t, int64(1), app.LastBlockHeight())
	latest, err := storage.SC().GetLatestVersion()
	require.NoError(t, err)
	require.Equal(t, int64(0), latest)
}

// Committed-state readers running alongside FinalizeBlock observe every block
// in order and never see a block's state go backwards while its commit lands.
func TestEVMOnlyApplicationReadsRaceFinalizeBlock(t *testing.T) {
	const blocks = 16
	app := newInitializedEVMOnlyTestApp(t)
	settler, ok := app.(*evmOnlyApplication)
	require.True(t, ok)
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	sender := crypto.PubkeyToAddress(key.PublicKey)
	txs := make([][]byte, blocks)
	for i := range txs {
		txs[i] = signedEVMOnlyTestTxFrom(t, key, evmOnlyTestChainID, uint64(i)) //nolint:gosec // G115: i is non-negative.
	}

	err = scope.Run(t.Context(), func(ctx context.Context, s scope.Scope) error {
		var done atomic.Bool
		s.Spawn(func() error {
			var last uint64
			for !done.Load() {
				nonce := app.EvmNonce(sender)
				if nonce < last {
					return fmt.Errorf("nonce went back from %d to %d", last, nonce)
				}
				last = nonce
				// A call may be refused while a finalized block awaits Commit;
				// what matters is that it never races the commit it settles.
				_, _ = settler.EvmCall(ctx, callMessage(sender, nil))
			}
			return nil
		})
		defer done.Store(true)
		for height := range int64(blocks) {
			if _, err := app.FinalizeBlock(ctx, evmOnlyTestBlock(height+1, txs[height])); err != nil {
				return err
			}
			if _, err := app.Commit(ctx); err != nil {
				return err
			}
		}
		return nil
	})
	require.NoError(t, err)
	require.NoError(t, settler.AwaitCommits())
	require.Equal(t, uint64(blocks), app.EvmNonce(sender))
}
