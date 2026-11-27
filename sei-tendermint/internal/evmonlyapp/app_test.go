package evmonlyapp

import (
	"context"
	"crypto/ecdsa"
	"encoding/binary"
	"fmt"
	"math"
	"math/big"
	"slices"
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
	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-db/bootstrap"
	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	seidbmetrics "github.com/sei-protocol/sei-chain/sei-db/common/metrics"
	seidbconfig "github.com/sei-protocol/sei-chain/sei-db/config"
	receiptdb "github.com/sei-protocol/sei-chain/sei-db/receipt_db"
	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
	abevm "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/evm"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/epoch"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
)

const evmOnlyTestChainID uint64 = 713700

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

func evmOnlyTestBlock(app abevm.Application, height int64, txs ...[]byte) abevm.FinalizeBlockReq {
	return abevm.FinalizeBlockReq{
		Txs:  utils.OrPanic1(newTestSigner(app.EvmChainID(), 0).ParseTxs(txs)),
		Hash: common.BytesToHash(crypto.Keccak256(binary.BigEndian.AppendUint64([]byte("block-"), uint64(height)))), //nolint:gosec // G115: test heights are positive.

		Height: atypes.RoadHeight(height),
		Time:   evmOnlyTestBlockTime(height)}
}

func evmOnlyTestBlockTime(height int64) time.Time {
	return time.Unix(1_700_000_000+height, 0)
}

func finalizeEVMOnlyTestBlock(t *testing.T, app abevm.Application, req abevm.FinalizeBlockReq) []byte {
	t.Helper()
	_, err := app.FinalizeBlock(t.Context(), req)
	require.NoError(t, err)
	return app.Info().LastBlockAppHash
}

// evmOnlyTestInitCode deploys a contract whose runtime code is the single
// INVALID opcode 0xfe: PUSH1 0xfe, PUSH1 0, MSTORE8, PUSH1 1, PUSH1 0, RETURN.
var evmOnlyTestInitCode = []byte{0x60, 0xfe, 0x60, 0x00, 0x53, 0x60, 0x01, 0x60, 0x00, 0xf3}

func signedEVMOnlyTestCreateTx(t *testing.T, chainID uint64) ([]byte, common.Address) {
	t.Helper()
	return signedEVMOnlyTestCreateTxWithInitCode(t, chainID, evmOnlyTestInitCode)
}

// signedEVMOnlyTestCreateTxWithInitCode signs a contract creation running
// initCode from a fresh sender, returning the raw tx and that sender.
func signedEVMOnlyTestCreateTxWithInitCode(t *testing.T, chainID uint64, initCode []byte) ([]byte, common.Address) {
	t.Helper()
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	tx := ethtypes.NewTx(&ethtypes.LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(evmOnlyMinGasPrice),
		Gas:      100_000,
		Data:     initCode,
	})
	signed, err := ethtypes.SignTx(tx, ethtypes.LatestSignerForChainID(new(big.Int).SetUint64(chainID)), key)
	require.NoError(t, err)
	raw, err := signed.MarshalBinary()
	require.NoError(t, err)
	return raw, crypto.PubkeyToAddress(key.PublicKey)
}

func evmOnlyTestInitChain() abevm.InitConfig {
	return abevm.InitConfig{
		InitialHeight: 1,
		GasLimit:      30_000_000}
}

func newInitializedEVMOnlyTestApp(t *testing.T) abevm.Application {
	t.Helper()
	app := newEVMOnlyTestApp(t, nil)
	err := app.InitChain(evmOnlyTestInitChain())
	require.NoError(t, err)
	return app
}

// TestEpochRegistryRowSurvivesReopen pins that a restarted app reopens the registry row the
// first block wrote.
func TestEpochRegistryRowSurvivesReopen(t *testing.T) {
	home := t.TempDir()
	storageConfig, err := seidbconfig.AutobahnStorageConfig(home)
	require.NoError(t, err)
	storage := openEVMOnlyTestStorage(t, storageConfig)
	app, err := NewEVMOnlyApplication(
		evmOnlyTestChainID,
		storage,
		openEVMOnlyTestEpochs(t, storage, nil, 1),
		gigaconfig.DefaultConfig.Execution,
		utils.None[precompiles.Registry](),
		utils.None[GenesisAlloc](),
		utils.None[string](),
	)
	require.NoError(t, err)
	err = app.InitChain(evmOnlyTestInitChain())
	require.NoError(t, err)
	finalizeEVMOnlyTestBlock(t, app, evmOnlyTestBlock(app, 1))
	require.NoError(t, app.(*evmOnlyApplication).AwaitCommits())
	want := epochRegistryRow(t, storage)

	resumed, reopened := reopenEVMOnlyTestApp(t, app, storage, home)
	t.Cleanup(func() { closeEVMOnlyTestApp(t, resumed, reopened) })
	require.Equal(t, want, epochRegistryRow(t, reopened))
}

// TestEpochRegistryWritesOnGenesisAndClosingRoads pins which blocks write the registry row. The
// first road writes the genesis row, and each closing road writes the row that derives the epoch
// after the newest one.
func TestEpochRegistryWritesOnGenesisAndClosingRoads(t *testing.T) {
	storageConfig, err := seidbconfig.AutobahnStorageConfig(t.TempDir())
	require.NoError(t, err)
	storage := openEVMOnlyTestStorage(t, storageConfig)
	epochs := openEVMOnlyTestEpochsWithLength(t, storage, nil, 1, 3)
	app, err := NewEVMOnlyApplication(
		evmOnlyTestChainID,
		storage,
		epochs,
		gigaconfig.DefaultConfig.Execution,
		utils.None[precompiles.Registry](),
		utils.None[GenesisAlloc](),
		utils.None[string](),
	)
	require.NoError(t, err)
	t.Cleanup(func() { closeEVMOnlyTestApp(t, app, storage) })
	err = app.InitChain(evmOnlyTestInitChain())
	require.NoError(t, err)
	initialRegistry, err := app.EpochRegistry()
	require.NoError(t, err)
	encoded := installRecordingEVMExecutor(app.(*evmOnlyApplication))
	const last = 7
	for height := int64(1); height <= last; height++ {
		finalizeEVMOnlyTestBlock(t, app, evmOnlyTestBlock(app, height))
	}
	require.NoError(t, app.(*evmOnlyApplication).AwaitCommits())

	// Epochs of length 3 close at roads 3 and 6.
	written := map[int64]bool{1: true, 3: true, 6: true}
	for blocks := range encoded.Lock() {
		for height := int64(1); height <= last; height++ {
			misc := blocks[uint64(height)] //nolint:gosec // G115: test heights are positive.
			require.Equal(t, written[height], epochModuleKeyCount(misc, epochRegistryKey) == 1, "registry block %d", height)
		}
	}
	registry, err := app.EpochRegistry()
	require.NoError(t, err)
	window := registry.Epochs()
	require.Equal(t, atypes.EpochIndex(3), window[len(window)-1].EpochIndex())
	require.Len(t, initialRegistry.Epochs(), 2)
	require.Equal(t, atypes.EpochIndex(1), initialRegistry.Epochs()[1].EpochIndex())
}

// TestEpochRegistryRestartReadsTheRowOfTheClosingRoad pins the restart after an epoch closes. The
// row written by the closing road of epoch 0 is durable, and a restarted app reads epoch 2 from it.
func TestEpochRegistryRestartReadsTheRowOfTheClosingRoad(t *testing.T) {
	const length = 100
	home := t.TempDir()
	storageConfig, err := seidbconfig.AutobahnStorageConfig(home)
	require.NoError(t, err)
	storage := openEVMOnlyTestStorage(t, storageConfig)
	app, err := NewEVMOnlyApplication(
		evmOnlyTestChainID,
		storage,
		openEVMOnlyTestEpochsWithLength(t, storage, nil, 1, length),
		gigaconfig.DefaultConfig.Execution,
		utils.None[precompiles.Registry](),
		utils.None[GenesisAlloc](),
		utils.None[string](),
	)
	require.NoError(t, err)
	err = app.InitChain(evmOnlyTestInitChain())
	require.NoError(t, err)
	// Epoch 0 is roads [1, 101), so road 100 is its closing road.
	for height := int64(1); height <= length; height++ {
		finalizeEVMOnlyTestBlock(t, app, evmOnlyTestBlock(app, height))
	}
	require.NoError(t, app.(*evmOnlyApplication).AwaitCommits())

	resumed, reopened := reopenEVMOnlyTestAppWithEpochs(t, app, storage, home, length)
	t.Cleanup(func() { closeEVMOnlyTestApp(t, resumed, reopened) })
	registry, err := resumed.EpochRegistry()
	require.NoError(t, err)
	window := registry.Epochs()
	require.Equal(t, atypes.EpochIndex(2), window[len(window)-1].EpochIndex())
	require.Equal(t, atypes.RoadHeight(2*length+1), window[len(window)-1].RoadRange().First)

	// Execution resumes after the closing road, and the window stays on epoch 2.
	finalizeEVMOnlyTestBlock(t, resumed, evmOnlyTestBlock(resumed, length+1))
	require.NoError(t, resumed.(*evmOnlyApplication).AwaitCommits())
	registry, err = resumed.EpochRegistry()
	require.NoError(t, err)
	window = registry.Epochs()
	require.Equal(t, atypes.EpochIndex(2), window[len(window)-1].EpochIndex())
}

// epochRegistryRow returns the registry row storage holds.
func epochRegistryRow(t *testing.T, storage *bootstrap.GigaStorageManager) []byte {
	t.Helper()
	raw, found := storage.StateDB().LiveStateDB().Get(epochRegistryModule, []byte(epochRegistryKey))
	require.True(t, found)
	return raw
}

// epochModuleKeyCount counts writes of key in the epoch module within misc.
func epochModuleKeyCount(misc []gigatypes.MiscChange, key string) int {
	count := 0
	for _, change := range misc {
		if change.Module == epochRegistryModule && string(change.Key) == key {
			count++
		}
	}
	return count
}

// installRecordingEVMExecutor replaces app's executor with one that records the misc
// changes app encodes for each block, by block number.
func installRecordingEVMExecutor(app *evmOnlyApplication) *utils.Mutex[map[uint64][]gigatypes.MiscChange] {
	encoded := new(utils.NewMutex(map[uint64][]gigatypes.MiscChange{}))
	installEVMExecutorEncoder(app, func(block evmonly.BlockContext, state evmonly.StateReader, result *evmonly.BlockResult, after func() precompiles.State) ([]gigatypes.MiscChange, error) {
		misc, err := app.encodeCursorChangeSet(block, state, result, after)
		if err != nil {
			return nil, err
		}
		for blocks := range encoded.Lock() {
			blocks[block.Number] = misc
		}
		return misc, nil
	})
	return encoded
}

// newUndoLogEVMOnlyTestApp returns an initialized application whose state store
// keeps EVM history as an undo log.
func newUndoLogEVMOnlyTestApp(t *testing.T) *evmOnlyApplication {
	t.Helper()
	return newUndoLogEVMOnlyTestAppAt(t, 1)
}

// newUndoLogEVMOnlyTestAppAt returns an application whose state store keeps EVM
// history as an undo log, initialized for a chain starting at initialHeight.
func newUndoLogEVMOnlyTestAppAt(t *testing.T, initialHeight int64) *evmOnlyApplication {
	t.Helper()
	storageConfig, err := seidbconfig.AutobahnStorageConfig(t.TempDir())
	require.NoError(t, err)
	// The chain's first block is the first one with an app hash.
	storageConfig.AppHashBuilderConfig.GigaActivationHeight = uint64(initialHeight) //nolint:gosec // G115: test heights are positive.
	storageConfig.WithFullNodeMode()
	storage := openEVMOnlyTestStorage(t, storageConfig)
	app, err := NewEVMOnlyApplication(
		evmOnlyTestChainID,
		storage,
		openEVMOnlyTestEpochs(t, storage, nil, initialHeight),
		gigaconfig.DefaultConfig.Execution,
		utils.None[precompiles.Registry](),
		utils.None[GenesisAlloc](),
		utils.None[string](),
	)
	require.NoError(t, err)
	t.Cleanup(func() { closeEVMOnlyTestApp(t, app, storage) })
	require.NotNil(t, app.(*evmOnlyApplication).storage.StateDB().HistoricalStateDB())
	init := evmOnlyTestInitChain()
	init.InitialHeight = atypes.RoadHeight(initialHeight)
	err = app.InitChain(init)
	require.NoError(t, err)
	return app.(*evmOnlyApplication)
}

func newEVMOnlyTestApp(t *testing.T, validators abevm.CommitteeWeights) abevm.Application {
	t.Helper()
	return newEVMOnlyTestAppWithExecution(t, validators, gigaconfig.DefaultConfig.Execution)
}

func newEVMOnlyTestAppWithExecution(
	t *testing.T,
	validators abevm.CommitteeWeights,
	execution gigaconfig.ExecutionConfig,
) abevm.Application {
	t.Helper()
	storageConfig, err := seidbconfig.AutobahnStorageConfig(t.TempDir())
	require.NoError(t, err)
	return newEVMOnlyTestAppWithStorage(t, validators, execution, storageConfig)
}

func newEVMOnlyTestAppWithStorage(
	t *testing.T,
	validators abevm.CommitteeWeights,
	execution gigaconfig.ExecutionConfig,
	storageConfig *seidbconfig.GigaStorageConfig,
) abevm.Application {
	t.Helper()
	storage := openEVMOnlyTestStorage(t, storageConfig)
	app, err := NewEVMOnlyApplication(
		evmOnlyTestChainID,
		storage,
		openEVMOnlyTestEpochs(t, storage, validators, 1),
		execution,
		utils.None[precompiles.Registry](),
		utils.None[GenesisAlloc](),
		utils.None[string](),
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

// reportMissingBlockHashes reports a block hash for each committed height above the newest app hash the
// builder has published, so the builder can build the app hash of every committed block.
func reportMissingBlockHashes(manager *bootstrap.GigaStorageManager) error {
	version := manager.StateDB().LiveStateDB().Version()
	//nolint:gosec // state heights are non-negative
	height := uint64(version)
	_, published, ok, err := manager.AppHashBuilder().Bounds()
	if err != nil {
		return fmt.Errorf("read the published app hash bounds: %w", err)
	}
	if !ok {
		published = 0
	}
	for n := published + 1; n <= height; n++ {
		var blockHash [32]byte
		binary.BigEndian.PutUint64(blockHash[:8], n)
		if err := manager.AppHashBuilder().ReportBlockHash(context.Background(), n, blockHash); err != nil {
			return fmt.Errorf("report the block hash of block %d: %w", n, err)
		}
	}
	return nil
}

// closeEVMOnlyTestApp closes storage the way the node does: after the
// application has landed every block commit it started.
//
// It also reports the block hash of every committed block, so closing stores
// each one's app hash. A test's block store is empty, so a reopen skips
// recovery, and the app hash builder resumes at the state's height only when it
// holds the app hashes up to it. A node lowers its recovery target to the app
// hashes it holds instead.
func closeEVMOnlyTestApp(t *testing.T, app abevm.Application, storage *bootstrap.GigaStorageManager) {
	t.Helper()
	settler, ok := app.(*evmOnlyApplication)
	require.True(t, ok)
	require.NoError(t, settler.AwaitCommits())
	require.NoError(t, reportMissingBlockHashes(storage))
	require.NoError(t, storage.Close())
}

// reopenEVMOnlyTestApp closes app's storage and constructs a fresh application
// over the same home, the way a restarted process does.
func reopenEVMOnlyTestApp(t *testing.T, app abevm.Application, storage *bootstrap.GigaStorageManager, home string) (abevm.Application, *bootstrap.GigaStorageManager) {
	t.Helper()
	return reopenEVMOnlyTestAppWithEpochs(t, app, storage, home, epoch.DefaultEpochLength)
}

// reopenEVMOnlyTestAppWithEpochs is reopenEVMOnlyTestApp for a chain whose epochs have length roads.
func reopenEVMOnlyTestAppWithEpochs(t *testing.T, app abevm.Application, storage *bootstrap.GigaStorageManager, home string, length atypes.RoadHeight) (abevm.Application, *bootstrap.GigaStorageManager) {
	t.Helper()
	closeEVMOnlyTestApp(t, app, storage)
	reopened := openEVMOnlyTestStorageAt(t, home)
	resumed, err := NewEVMOnlyApplication(
		evmOnlyTestChainID, reopened,
		openEVMOnlyTestEpochsWithLength(t, reopened, nil, 1, length),
		gigaconfig.DefaultConfig.Execution,
		utils.None[precompiles.Registry](),
		utils.None[GenesisAlloc](),
		utils.None[string](),
	)
	require.NoError(t, err)
	return resumed, reopened
}

// evmOnlyTestGenesisValidators returns validators, or a one-validator set when validators is empty.
func evmOnlyTestGenesisValidators(validators abevm.CommitteeWeights) abevm.CommitteeWeights {
	if len(validators) > 0 {
		return validators
	}
	key := atypes.GenSecretKey(utils.TestRng())
	return abevm.CommitteeWeights{key.Public(): 1}
}

// openEVMOnlyTestEpochs opens the epoch registry storage holds for a chain whose first block is
// initialHeight, with epochs of the default length.
func openEVMOnlyTestEpochs(
	t *testing.T,
	storage *bootstrap.GigaStorageManager,
	validators abevm.CommitteeWeights,
	initialHeight int64,
) *EpochRegistry {
	t.Helper()
	return openEVMOnlyTestEpochsWithLength(t, storage, validators, initialHeight, epoch.DefaultEpochLength)
}

// openEVMOnlyTestEpochsWithLength is openEVMOnlyTestEpochs with epochs of length roads.
func openEVMOnlyTestEpochsWithLength(
	t *testing.T,
	storage *bootstrap.GigaStorageManager,
	validators abevm.CommitteeWeights,
	initialHeight int64,
	length atypes.RoadHeight,
) *EpochRegistry {
	t.Helper()
	epochs, err := OpenEpochRegistry(storage, atypes.RoadHeight(initialHeight), length, evmOnlyTestGenesisValidators(validators)) //nolint:gosec // G115: test heights are positive.
	require.NoError(t, err)
	return epochs
}

// waitForReceiptVersion blocks until the receipt store has published height. The store applies
// writes in the background, so a receipt is readable once LatestVersion reaches its block rather
// than once SetReceipts returns.
func waitForReceiptVersion(t *testing.T, store receiptdb.ReceiptStore, height int64) {
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
	_, checkErr := app.CheckTx(utils.OrPanic1(newTestSigner(app.EvmChainID(), 0).ParseTx(raw)))
	require.NoError(t, checkErr)
	require.Equal(t, uint64(0), app.EvmNonce(sender))
	gotBalance := app.EvmBalance(sender)
	require.Equal(t, evmOnlyBaseBalance, gotBalance.ToBig())

	response, err := app.FinalizeBlock(t.Context(), abevm.FinalizeBlockReq{
		Txs:    utils.OrPanic1(newTestSigner(app.EvmChainID(), 0).ParseTxs([][]byte{raw})),
		Hash:   common.BytesToHash(crypto.Keccak256([]byte("block-1"))),
		Height: 1,
		Time:   time.Unix(1_700_000_001, 0)})
	require.NoError(t, err)
	finalizedHash := applicationHash(t, app)
	require.Len(t, finalizedHash, common.HashLength)
	require.Positive(t, response.GasUsed)
	require.Equal(t, int64(1), app.LastBlockHeight())
	require.Equal(t, uint64(1), app.EvmNonce(sender))
	wantBalance := new(big.Int).Sub(
		new(big.Int).Sub(new(big.Int).Set(evmOnlyBaseBalance), big.NewInt(1)),
		new(big.Int).Mul(big.NewInt(evmOnlyMinGasPrice), new(big.Int).SetUint64(response.GasUsed)),
	)
	gotBalance = app.EvmBalance(sender)
	require.Equal(t, wantBalance, gotBalance.ToBig())
	require.Equal(t, finalizedHash, app.Info().LastBlockAppHash)
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
	err = app.InitChain(abevm.InitConfig{
		InitialHeight: 1,
		GasLimit:      30_000_000})
	require.NoError(t, err)
	raw, sender := signedEVMOnlyTestTx(t, evmOnlyTestChainID, 0)

	response, err := app.FinalizeBlock(t.Context(), abevm.FinalizeBlockReq{
		Txs:    utils.OrPanic1(newTestSigner(app.EvmChainID(), 0).ParseTxs([][]byte{raw})),
		Hash:   common.BytesToHash(crypto.Keccak256([]byte("block-1"))),
		Height: 1,
		Time:   time.Unix(1_700_000_001, 0)})
	require.NoError(t, err)
	require.Positive(t, response.GasUsed)
	require.Equal(t, int64(1), app.LastBlockHeight())
	require.Equal(t, uint64(1), app.EvmNonce(sender))
}

func TestEVMOnlyApplicationRejectsWrongChain(t *testing.T) {
	app := newInitializedEVMOnlyTestApp(t)
	raw, _ := signedEVMOnlyTestTx(t, evmOnlyTestChainID+1, 0)
	require.ErrorIs(t, admissionError(app, raw), ethtypes.ErrInvalidChainId)
}

// A dynamic-fee tx pays the base fee plus at most its tip, so it is admitted on
// that price, not on tx.GasPrice(), its fee cap. A zero tip pays the base fee,
// which a node whose floor is above the base fee refuses.
func TestEVMOnlyApplicationAdmitsADynamicFeeTxOnItsEffectivePrice(t *testing.T) {
	raised := gigaconfig.DefaultConfig.Execution
	raised.MinGasPrice = 2 * evmOnlyMinGasPrice
	app := newEVMOnlyTestAppWithExecution(t, nil, raised)
	require.NoError(t, app.InitChain(evmOnlyTestInitChain()))
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	recipient := common.HexToAddress("0x1000000000000000000000000000000000000001")
	chainID := new(big.Int).SetUint64(evmOnlyTestChainID)
	// Fee cap far above the floor, tip cap zero.
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

	_, checkErr := app.CheckTx(utils.OrPanic1(newTestSigner(app.EvmChainID(), 0).ParseTx(raw)))

	require.Error(t, checkErr)
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

	_, checkErr := app.CheckTx(utils.OrPanic1(newTestSigner(app.EvmChainID(), 0).ParseTx(raw)))

	require.NoError(t, checkErr)
}

func TestEVMOnlyApplicationProducesDeterministicRoot(t *testing.T) {
	raw, _ := signedEVMOnlyTestTx(t, evmOnlyTestChainID, 0)
	first := newInitializedEVMOnlyTestApp(t)
	second := newInitializedEVMOnlyTestApp(t)
	request := abevm.FinalizeBlockReq{
		Txs:    utils.OrPanic1(newTestSigner(first.EvmChainID(), 0).ParseTxs([][]byte{raw})),
		Hash:   common.BytesToHash(crypto.Keccak256([]byte("same-block"))),
		Height: 1,
		Time:   time.Unix(1_700_000_001, 0)}

	_, err := first.FinalizeBlock(t.Context(), request)
	require.NoError(t, err)
	_, err = second.FinalizeBlock(t.Context(), request)
	require.NoError(t, err)

	require.Equal(t, applicationHash(t, first), applicationHash(t, second))
}

func TestEVMOnlyApplicationExecutesCheckedTxLikeUncheckedTx(t *testing.T) {
	raw, sender := signedEVMOnlyTestTx(t, evmOnlyTestChainID, 0)
	checked, ok := newInitializedEVMOnlyTestApp(t).(*evmOnlyApplication)
	require.True(t, ok)
	unchecked := newInitializedEVMOnlyTestApp(t)

	checkedProxy := abevm.NewProxy(checked)
	request := abevm.FinalizeBlockReq{
		Txs:    utils.OrPanic1(newTestSigner(checkedProxy.EvmChainID(), 0).ParseTxs([][]byte{raw})),
		Hash:   common.BytesToHash(crypto.Keccak256([]byte("checked-block"))),
		Height: 1,
		Time:   time.Unix(1_700_000_001, 0)}
	_, checkErr := checkedProxy.CheckTx(utils.OrPanic1(newTestSigner(checkedProxy.EvmChainID(), 0).ParseTx(raw)))
	require.NoError(t, checkErr)
	checkedResponse, err := checkedProxy.FinalizeBlock(t.Context(), request)
	require.NoError(t, err)
	uncheckedResponse, err := unchecked.FinalizeBlock(t.Context(), request)
	require.NoError(t, err)

	require.Equal(t, applicationHash(t, unchecked), applicationHash(t, checked))
	require.Equal(t, uncheckedResponse.GasUsed, checkedResponse.GasUsed)
	require.Equal(t, uint64(1), checked.EvmNonce(sender))
}

// CheckTx uses the sender recorded for a transaction before it arrived, instead of recovering it.
func TestEVMOnlyApplicationChecksATxByItsRememberedSender(t *testing.T) {
	raw, _ := signedEVMOnlyTestTx(t, evmOnlyTestChainID, 0)
	app := abevm.NewProxy(newInitializedEVMOnlyTestApp(t))
	signer := newTestSigner(app.EvmChainID(), 0)
	remembered := common.HexToAddress("0x2000000000000000000000000000000000000002")
	signer.Remember(decodeEVMOnlyTestTx(t, raw).Hash(), remembered)

	decoded := utils.OrPanic1(signer.ParseTx(raw))
	require.Equal(t, remembered, decoded.Sender())
	_, err := app.CheckTx(decoded)
	require.NoError(t, err)
}

func TestEVMOnlyApplicationForgetsARememberedSender(t *testing.T) {
	raw, sender := signedEVMOnlyTestTx(t, evmOnlyTestChainID, 0)
	app := abevm.NewProxy(newInitializedEVMOnlyTestApp(t))
	signer := newTestSigner(app.EvmChainID(), 0)
	hash := decodeEVMOnlyTestTx(t, raw).Hash()
	signer.Remember(hash, common.HexToAddress("0x2000000000000000000000000000000000000002"))
	signer.Forget(hash)

	decoded := utils.OrPanic1(signer.ParseTx(raw))
	_, err := app.CheckTx(decoded)
	require.NoError(t, err)
	require.Equal(t, sender, decoded.Sender())
}

// The cursor does not carry the block time, so a resumed app would answer
// EvmCall with TIMESTAMP 0 until the next FinalizeBlock. The router seeds it through
// RestoreBlockTime on its restart path.
func TestEVMOnlyApplicationRestoreBlockTimeSeedsBlockTime(t *testing.T) {
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	const blocks = 3
	home := t.TempDir()
	storage := openEVMOnlyTestStorageAt(t, home)
	app, err := NewEVMOnlyApplication(
		evmOnlyTestChainID, storage,
		openEVMOnlyTestEpochs(t, storage, nil, 1),
		gigaconfig.DefaultConfig.Execution,
		utils.None[precompiles.Registry](),
		utils.None[GenesisAlloc](),
		utils.None[string](),
	)
	require.NoError(t, err)
	err = app.InitChain(evmOnlyTestInitChain())
	require.NoError(t, err)
	var last abevm.FinalizeBlockReq
	for height := range int64(blocks) {
		last = evmOnlyTestBlock(app, height+1, signedEVMOnlyTestTxFrom(t, key, evmOnlyTestChainID, uint64(height))) //nolint:gosec // G115: test heights are positive.
		finalizeEVMOnlyTestBlock(t, app, last)
	}

	app, storage = reopenEVMOnlyTestApp(t, app, storage, home)
	t.Cleanup(func() { closeEVMOnlyTestApp(t, app, storage) })

	resumed, ok := app.(*evmOnlyApplication)
	require.True(t, ok)
	for state := range resumed.cursor.Lock() {
		require.Zero(t, state.lastBlockTime, "a resumed app has no block time before RestoreBlockTime")
	}
	resumed.RestoreBlockTime(last.Time)
	for state := range resumed.cursor.Lock() {
		require.Equal(t, uint64(last.Time.Unix()), state.lastBlockTime) //nolint:gosec // G115: test times are positive.
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
	block := func(app abevm.Application, height int64) abevm.FinalizeBlockReq {
		return evmOnlyTestBlock(app, height, signedEVMOnlyTestTxFrom(t, key, evmOnlyTestChainID, uint64(height-1))) //nolint:gosec // G115: test heights are positive.
	}

	reference := newInitializedEVMOnlyTestApp(t)
	wantHashes := make([][]byte, 0, blocks+1)
	for height := range int64(blocks + 1) {
		wantHashes = append(wantHashes, finalizeEVMOnlyTestBlock(t, reference, block(reference, height+1)))
	}

	home := t.TempDir()
	storage := openEVMOnlyTestStorageAt(t, home)
	app, err := NewEVMOnlyApplication(
		evmOnlyTestChainID, storage,
		openEVMOnlyTestEpochs(t, storage, nil, 1),
		gigaconfig.DefaultConfig.Execution,
		utils.None[precompiles.Registry](),
		utils.None[GenesisAlloc](),
		utils.None[string](),
	)
	require.NoError(t, err)
	err = app.InitChain(evmOnlyTestInitChain())
	require.NoError(t, err)
	for height := range int64(blocks) {
		require.Equal(t, wantHashes[height], finalizeEVMOnlyTestBlock(t, app, block(app, height+1)))
	}

	app, storage = reopenEVMOnlyTestApp(t, app, storage, home)
	t.Cleanup(func() { closeEVMOnlyTestApp(t, app, storage) })

	info := app.Info()
	require.Equal(t, int64(blocks), info.LastBlockHeight)
	require.Equal(t, wantHashes[blocks-1], info.LastBlockAppHash)
	require.Equal(t, uint64(blocks), app.EvmNonce(sender))
	err = app.InitChain(evmOnlyTestInitChain())
	require.Error(t, err)
	require.Equal(t, wantHashes[blocks], finalizeEVMOnlyTestBlock(t, app, block(app, blocks+1)))
	require.Equal(t, int64(blocks+1), app.LastBlockHeight())
}

// A finalized block is durable across restart and must not execute a second time.
// An app resumed from its cursor builds its executor without InitChain, and
// must still publish it to the settler, or committed-state readers and storage
// shutdown would not wait for the block commits it starts.
func TestEVMOnlyApplicationResumedAppSettlesItsCommits(t *testing.T) {
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	sender := crypto.PubkeyToAddress(key.PublicKey)
	home := t.TempDir()
	storage := openEVMOnlyTestStorageAt(t, home)
	app, err := NewEVMOnlyApplication(
		evmOnlyTestChainID, storage,
		openEVMOnlyTestEpochs(t, storage, nil, 1),
		gigaconfig.DefaultConfig.Execution,
		utils.None[precompiles.Registry](),
		utils.None[GenesisAlloc](),
		utils.None[string](),
	)
	require.NoError(t, err)
	err = app.InitChain(evmOnlyTestInitChain())
	require.NoError(t, err)
	finalizeEVMOnlyTestBlock(t, app, evmOnlyTestBlock(app, 1, signedEVMOnlyTestTxFrom(t, key, evmOnlyTestChainID, 0)))

	app, storage = reopenEVMOnlyTestApp(t, app, storage, home)
	t.Cleanup(func() { closeEVMOnlyTestApp(t, app, storage) })

	resumed, ok := app.(*evmOnlyApplication)
	require.True(t, ok)
	require.True(t, resumed.settler.Load().IsPresent(), "a resumed app must publish its executor to the settler")
	finalizeEVMOnlyTestBlock(t, app, evmOnlyTestBlock(app, 2, signedEVMOnlyTestTxFrom(t, key, evmOnlyTestChainID, 1)))
	require.Equal(t, uint64(2), app.EvmNonce(sender))
}

func TestEVMOnlyApplicationResumesFromFinalizedBlock(t *testing.T) {
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	block := func(app abevm.Application, height int64) abevm.FinalizeBlockReq {
		return evmOnlyTestBlock(app, height, signedEVMOnlyTestTxFrom(t, key, evmOnlyTestChainID, uint64(height-1))) //nolint:gosec // G115: test heights are positive.
	}

	home := t.TempDir()
	storage := openEVMOnlyTestStorageAt(t, home)
	app, err := NewEVMOnlyApplication(
		evmOnlyTestChainID, storage,
		openEVMOnlyTestEpochs(t, storage, nil, 1),
		gigaconfig.DefaultConfig.Execution,
		utils.None[precompiles.Registry](),
		utils.None[GenesisAlloc](),
		utils.None[string](),
	)
	require.NoError(t, err)
	err = app.InitChain(evmOnlyTestInitChain())
	require.NoError(t, err)
	finalizeEVMOnlyTestBlock(t, app, block(app, 1))
	_, err = app.FinalizeBlock(t.Context(), block(app, 2))
	require.NoError(t, err)

	require.Equal(t, int64(2), app.LastBlockHeight())
	finalizedHash := app.Info().LastBlockAppHash
	app, storage = reopenEVMOnlyTestApp(t, app, storage, home)
	t.Cleanup(func() { closeEVMOnlyTestApp(t, app, storage) })

	info := app.Info()
	require.Equal(t, int64(2), info.LastBlockHeight)
	require.Equal(t, finalizedHash, info.LastBlockAppHash)
	finalizeEVMOnlyTestBlock(t, app, block(app, 3))
	require.Equal(t, int64(3), app.LastBlockHeight())
}

// InitChain with an initial height above one seeds the store before any block
// exists. Crashing there must still allow InitChain to run again.
func TestEVMOnlyApplicationRepeatsInitChainAfterSeedingOnly(t *testing.T) {
	storageConfig, err := seidbconfig.AutobahnStorageConfig(t.TempDir())
	require.NoError(t, err)
	// The chain starts at block 5, so that is the first block with an app hash.
	storageConfig.AppHashBuilderConfig.GigaActivationHeight = 5
	storage := openEVMOnlyTestStorage(t, storageConfig)
	app, err := NewEVMOnlyApplication(
		evmOnlyTestChainID, storage,
		openEVMOnlyTestEpochs(t, storage, nil, 1),
		gigaconfig.DefaultConfig.Execution,
		utils.None[precompiles.Registry](),
		utils.None[GenesisAlloc](),
		utils.None[string](),
	)
	require.NoError(t, err)
	init := evmOnlyTestInitChain()
	init.InitialHeight = 5
	err = app.InitChain(init)
	require.NoError(t, err)

	// Seeding writes only the live state DB, so a shutdown before the first block is not clean.
	require.Error(t, storage.Close())
	storage = openEVMOnlyTestStorage(t, storageConfig)
	app, err = NewEVMOnlyApplication(
		evmOnlyTestChainID, storage,
		openEVMOnlyTestEpochs(t, storage, nil, 1),
		gigaconfig.DefaultConfig.Execution, utils.None[precompiles.Registry](), utils.None[GenesisAlloc](), utils.None[string]())
	require.NoError(t, err)
	t.Cleanup(func() { closeEVMOnlyTestApp(t, app, storage) })

	require.Equal(t, int64(0), app.Info().LastBlockHeight)
	err = app.InitChain(init)
	require.NoError(t, err)
	require.Equal(t, int64(4), app.Info().LastBlockHeight)
	finalizeEVMOnlyTestBlock(t, app, evmOnlyTestBlock(app, 5))
	require.Equal(t, int64(5), app.LastBlockHeight())
}

func TestEVMOnlyApplicationRequiresInitChain(t *testing.T) {
	app := newEVMOnlyTestApp(t, nil)

	_, err := app.FinalizeBlock(t.Context(), abevm.FinalizeBlockReq{
		Hash:   common.BytesToHash(crypto.Keccak256([]byte("block-1"))),
		Height: 1,
		Time:   time.Unix(1_700_000_001, 0)})

	require.Error(t, err)
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

	_, err := app.FinalizeBlock(t.Context(), abevm.FinalizeBlockReq{
		Hash:   common.BytesToHash(crypto.Keccak256([]byte("block-1"))),
		Height: 1,
		Time:   time.Unix(1_700_000_001, 0)})
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
	finalizeEVMOnlyTestBlock(t, app, evmOnlyTestBlock(app, 1, first))

	response, err := app.FinalizeBlock(t.Context(), evmOnlyTestBlock(app, 2, first, next, next))
	require.NoError(t, err)
	require.Equal(t, uint64(21_000), response.GasUsed)
	require.Equal(t, int64(2), app.LastBlockHeight())
	require.Equal(t, uint64(2), app.EvmNonce(sender))
	finalizeEVMOnlyTestBlock(t, app, evmOnlyTestBlock(app, 3))
}

// A transaction rejected for a nonce gap leaves no receipt behind, so its hash is
// still free for the receipt of the block in which it finally executes.
func TestEVMOnlyApplicationReceiptFollowsLateExecution(t *testing.T) {
	app := newInitializedEVMOnlyTestApp(t)
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	first := signedEVMOnlyTestTxFrom(t, key, evmOnlyTestChainID, 0)
	second := signedEVMOnlyTestTxFrom(t, key, evmOnlyTestChainID, 1)

	response, err := app.FinalizeBlock(t.Context(), evmOnlyTestBlock(app, 1, second))
	require.NoError(t, err)
	require.Zero(t, response.GasUsed)
	finalizeEVMOnlyTestBlock(t, app, evmOnlyTestBlock(app, 2, first))
	finalizeEVMOnlyTestBlock(t, app, evmOnlyTestBlock(app, 3, second))

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
	proposer := newInitializedEVMOnlyTestApp(t)
	block := abevm.FinalizeBlockReq{
		Txs:    utils.OrPanic1(newTestSigner(proposer.EvmChainID(), 0).ParseTxs([][]byte{raw})),
		Hash:   common.BytesToHash(crypto.Keccak256([]byte("block-1"))),
		Height: 1,
		Time:   time.Unix(1_700_000_001, 0)}
	initChain := func(app abevm.Application) {
		err := app.InitChain(abevm.InitConfig{
			InitialHeight: 1,
			GasLimit:      30_000_000})
		require.NoError(t, err)
	}

	require.NoError(t, admissionError(proposer, raw))
	_, err := proposer.FinalizeBlock(t.Context(), block)
	require.NoError(t, err)

	raised := gigaconfig.DefaultConfig.Execution
	raised.MinGasPrice = 2 * evmOnlyMinGasPrice
	strict := newEVMOnlyTestAppWithExecution(t, nil, raised)
	initChain(strict)
	require.Equal(t, big.NewInt(2*evmOnlyMinGasPrice), strict.(evmMinGasPricer).EvmMinGasPrice())
	require.Error(t, admissionError(strict, raw))
	_, err = strict.FinalizeBlock(t.Context(), block)
	require.NoError(t, err)
	require.Equal(t, applicationHash(t, proposer), applicationHash(t, strict))

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

	response, err := app.FinalizeBlock(t.Context(), abevm.FinalizeBlockReq{
		Txs:    utils.OrPanic1(newTestSigner(app.EvmChainID(), 0).ParseTxs([][]byte{raw})),
		Hash:   common.BytesToHash(crypto.Keccak256([]byte("block-1"))),
		Height: 1,
		Time:   time.Unix(1_700_000_001, 0)})
	require.NoError(t, err)
	require.Positive(t, response.GasUsed)

	got := codeReader.EvmCode(contract)
	require.Equal(t, []byte{0xfe}, got)
	// The returned slice is a copy: mutating it must not alter the next read.
	got[0] = 0x00
	require.Equal(t, []byte{0xfe}, codeReader.EvmCode(contract))
	require.Empty(t, codeReader.EvmCode(sender))
}

func TestEVMOnlyApplicationServesStoredStorage(t *testing.T) {
	app := newInitializedEVMOnlyTestApp(t)
	// Stores 0x2a at slot 1, then deploys evmOnlyTestInitCode's runtime code.
	initCode := append([]byte{0x60, 0x2a, 0x60, 0x01, 0x55}, evmOnlyTestInitCode...)
	raw, sender := signedEVMOnlyTestCreateTxWithInitCode(t, evmOnlyTestChainID, initCode)
	contract := crypto.CreateAddress(sender, 0)
	storageReader := app.(*evmOnlyApplication)
	slot := common.BigToHash(big.NewInt(1))
	require.Equal(t, common.Hash{}, storageReader.EvmStorage(contract, slot))

	response, err := app.FinalizeBlock(t.Context(), abevm.FinalizeBlockReq{
		Txs:    utils.OrPanic1(newTestSigner(app.EvmChainID(), 0).ParseTxs([][]byte{raw})),
		Hash:   common.BytesToHash(crypto.Keccak256([]byte("block-1"))),
		Height: 1,
		Time:   time.Unix(1_700_000_001, 0)})
	require.NoError(t, err)
	require.Positive(t, response.GasUsed)

	require.Equal(t, common.BigToHash(big.NewInt(0x2a)), storageReader.EvmStorage(contract, slot))
	require.Equal(t, common.Hash{}, storageReader.EvmStorage(contract, common.Hash{}))
	require.Equal(t, common.Hash{}, storageReader.EvmStorage(sender, slot))
}

// A node keeping EVM history as an undo log serves every past height it holds with the state the
// chain had then.
func TestEVMOnlyApplicationServesHistoryFromTheUndoLog(t *testing.T) {
	app := newUndoLogEVMOnlyTestApp(t)

	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	sender := crypto.PubkeyToAddress(key.PublicKey)
	recipient := common.HexToAddress("0x1000000000000000000000000000000000000001")
	type accounts struct {
		senderBalance, recipientBalance *big.Int
		senderNonce                     uint64
	}
	observe := func() accounts {
		senderBalance, recipientBalance := app.EvmBalance(sender), app.EvmBalance(recipient)
		return accounts{
			senderBalance:    senderBalance.ToBig(),
			recipientBalance: recipientBalance.ToBig(),
			senderNonce:      app.EvmNonce(sender),
		}
	}
	const blocks = 11
	history := []accounts{observe()}
	for height := int64(1); height <= blocks; height++ {
		nonce := uint64(height - 1) //nolint:gosec // positive
		finalizeEVMOnlyTestBlock(t, app, evmOnlyTestBlock(app, height, signedEVMOnlyTestTxFrom(t, key, evmOnlyTestChainID, nonce)))
		history = append(history, observe())
	}
	// The undo log applies blocks behind the commit; until it has, their history is not held.
	app.storage.StateDB().HistoricalStateDB().WaitForPendingWrites()

	for height, want := range history {
		nonce, err := app.EvmNonceAt(sender, int64(height))
		require.NoError(t, err)
		require.Equal(t, want.senderNonce, nonce, "sender nonce at %d", height)
		senderBalance, err := app.EvmBalanceAt(sender, int64(height))
		require.NoError(t, err)
		require.Equal(t, want.senderBalance, senderBalance.ToBig(), "sender balance at %d", height)
		recipientBalance, err := app.EvmBalanceAt(recipient, int64(height))
		require.NoError(t, err)
		require.Equal(t, want.recipientBalance, recipientBalance.ToBig(), "recipient balance at %d", height)
	}
	require.Equal(t, uint64(blocks), history[blocks].senderNonce)
}

// A node keeping EVM history as an undo log commits blocks that change no EVM state, and serves the
// heights they leave behind.
func TestEVMOnlyApplicationCommitsEmptyBlocksOnTheUndoLog(t *testing.T) {
	app := newUndoLogEVMOnlyTestApp(t)
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	sender := crypto.PubkeyToAddress(key.PublicKey)

	for height := int64(1); height <= 3; height++ {
		finalizeEVMOnlyTestBlock(t, app, evmOnlyTestBlock(app, height))
	}
	finalizeEVMOnlyTestBlock(t, app, evmOnlyTestBlock(app, 4, signedEVMOnlyTestTxFrom(t, key, evmOnlyTestChainID, 0)))
	require.NoError(t, app.AwaitCommits())
	app.storage.StateDB().HistoricalStateDB().WaitForPendingWrites()

	for height, want := range []uint64{0, 0, 0, 0, 1} {
		nonce, err := app.EvmNonceAt(sender, int64(height))
		require.NoError(t, err, "height %d", height)
		require.Equal(t, want, nonce, "sender nonce at %d", height)
	}
}

// A chain starting above block 1 seeds the state store at InitChain, and the undo log serves history
// from the seeded height on.
func TestEVMOnlyApplicationServesHistoryFromASeededHeightOnTheUndoLog(t *testing.T) {
	app := newUndoLogEVMOnlyTestAppAt(t, 5)
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	sender := crypto.PubkeyToAddress(key.PublicKey)

	for height := int64(5); height <= 7; height++ {
		nonce := uint64(height - 5) //nolint:gosec // positive
		finalizeEVMOnlyTestBlock(t, app, evmOnlyTestBlock(app, height, signedEVMOnlyTestTxFrom(t, key, evmOnlyTestChainID, nonce)))
	}
	require.NoError(t, app.AwaitCommits())
	app.storage.StateDB().HistoricalStateDB().WaitForPendingWrites()

	for height, want := range map[int64]uint64{4: 0, 5: 1, 6: 2, 7: 3} {
		nonce, err := app.EvmNonceAt(sender, height)
		require.NoError(t, err, "height %d", height)
		require.Equal(t, want, nonce, "sender nonce at %d", height)
	}
}

// Contract storage written in a block is visible from that block on, and a slot is unset at every
// height before the write.
func TestEVMOnlyApplicationServesStorageHistoryFromTheUndoLog(t *testing.T) {
	app := newUndoLogEVMOnlyTestApp(t)

	// Each contract's constructor stores a value at slot 1 as it is deployed.
	deploy := func(height int64, value byte) common.Address {
		initCode := append([]byte{0x60, value, 0x60, 0x01, 0x55}, evmOnlyTestInitCode...)
		raw, sender := signedEVMOnlyTestCreateTxWithInitCode(t, evmOnlyTestChainID, initCode)
		finalizeEVMOnlyTestBlock(t, app, evmOnlyTestBlock(app, height, raw))
		return crypto.CreateAddress(sender, 0)
	}
	first := deploy(1, 0x2a)
	second := deploy(2, 0x2b)
	slot := common.BigToHash(big.NewInt(1))

	// The newest block reads straight after its commit, before the undo log has applied it.
	got, err := app.EvmStorageAt(second, slot, 2)
	require.NoError(t, err)
	require.Equal(t, common.BigToHash(big.NewInt(0x2b)), got)

	// The undo log applies blocks behind the commit; until it has, their history is not held.
	app.storage.StateDB().HistoricalStateDB().WaitForPendingWrites()
	for _, tc := range []struct {
		contract common.Address
		height   int64
		want     common.Hash
	}{
		{first, 0, common.Hash{}},
		{first, 1, common.BigToHash(big.NewInt(0x2a))},
		{first, 2, common.BigToHash(big.NewInt(0x2a))},
		{second, 0, common.Hash{}},
		{second, 1, common.Hash{}},
		{second, 2, common.BigToHash(big.NewInt(0x2b))},
	} {
		got, err := app.EvmStorageAt(tc.contract, slot, tc.height)
		require.NoError(t, err, "contract %s at %d", tc.contract, tc.height)
		require.Equal(t, tc.want, got, "contract %s at %d", tc.contract, tc.height)
	}
	_, err = app.EvmStorageAt(first, slot, 3)
	require.Error(t, err)

	// A read at block 2 stays at block 2 once block 3, which overwrites the slot, is finalized.
	overwrite := append([]byte{0x60, 0x2c, 0x60, 0x01, 0x55}, evmOnlyTestInitCode...)
	raw, sender := signedEVMOnlyTestCreateTxWithInitCode(t, evmOnlyTestChainID, overwrite)
	_, err = app.FinalizeBlock(t.Context(), evmOnlyTestBlock(app, 3, raw))
	require.NoError(t, err)
	third := crypto.CreateAddress(sender, 0)
	got, err = app.EvmStorageAt(third, slot, 2)
	require.NoError(t, err)
	require.Equal(t, common.Hash{}, got)
	got, err = app.EvmStorageAt(third, slot, 3)
	require.NoError(t, err)
	require.Equal(t, common.BigToHash(big.NewInt(0x2c)), got)
}

// Every stage FinalizeBlock runs on its turn is charged to a phase.
func TestEVMOnlyApplicationTimesEveryFinalizeBlockPhase(t *testing.T) {
	app := newInitializedEVMOnlyTestApp(t)
	evmOnlyApp, ok := app.(*evmOnlyApplication)
	require.True(t, ok)
	reader := sdkmetric.NewManualReader()
	meter := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter(finalizeMeterName)
	evmOnlyApp.finalizePhases = seidbmetrics.NewPhaseTimer(meter, finalizeTimerName)

	raw, _ := signedEVMOnlyTestTx(t, evmOnlyTestChainID, 0)
	_, err := app.FinalizeBlock(t.Context(), abevm.FinalizeBlockReq{
		Txs:    utils.OrPanic1(newTestSigner(app.EvmChainID(), 0).ParseTxs([][]byte{raw})),
		Hash:   common.BytesToHash(crypto.Keccak256([]byte("block-1"))),
		Height: 1,
		Time:   time.Unix(1_700_000_001, 0)})
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
	for _, want := range []string{finalizePhaseExecute} {
		_, ok := phases[want]
		require.True(t, ok, "phase %q not recorded", want)
	}
}

// A block's state lands in the store behind FinalizeBlock. Readers settle the
// writes, and the next block builds on the finalized cursor.
func TestEVMOnlyApplicationReadsSettleBehindFinalizeBlock(t *testing.T) {
	app := newInitializedEVMOnlyTestApp(t)
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	sender := crypto.PubkeyToAddress(key.PublicKey)
	block := func(height int64) abevm.FinalizeBlockReq {
		return evmOnlyTestBlock(app, height, signedEVMOnlyTestTxFrom(t, key, evmOnlyTestChainID, uint64(height-1))) //nolint:gosec // G115: test heights are positive.
	}

	for height := range int64(4) {
		_, err := app.FinalizeBlock(t.Context(), block(height+1))
		require.NoError(t, err)
		require.Equal(t, uint64(height+1), app.EvmNonce(sender)) //nolint:gosec // G115: test heights are positive.
		require.Equal(t, height+1, app.LastBlockHeight())
	}

	settler, ok := app.(*evmOnlyApplication)
	require.True(t, ok)
	require.NoError(t, settler.AwaitCommits())
	require.Equal(t, int64(4), settler.storage.StateDB().LiveStateDB().Version())
}

// installUnwritableEVMExecutor replaces app's executor with one that also writes, with every block, a misc change
// into the EVM module, which the state DB refuses to commit. Each block executes and encodes cleanly, and its commit
// is the first thing that fails.
func installUnwritableEVMExecutor(app *evmOnlyApplication) {
	installEVMExecutorEncoder(app, func(block evmonly.BlockContext, state evmonly.StateReader, result *evmonly.BlockResult, after func() precompiles.State) ([]gigatypes.MiscChange, error) {
		misc, err := app.encodeCursorChangeSet(block, state, result, after)
		if err != nil {
			return nil, fmt.Errorf("encode the cursor: %w", err)
		}
		return append(misc, gigatypes.MiscChange{
			Module: keys.EVMStoreKey,
			Key:    []byte{1},
			Value:  []byte{1},
		}), nil
	})
}

// installEVMExecutorEncoder replaces app's executor with a new one that encodes each
// block's changesets with encode.
func installEVMExecutorEncoder(app *evmOnlyApplication, encode evmonly.BlockChangeSetEncoder) {
	for slot := range app.executor.Lock() {
		if previous, ok := slot.Get(); ok {
			previous.Close()
		}
		executor := app.newExecutor()
		evmonly.WithStoreIndependentBlockChangeSetEncoder(encode)(executor)
		*slot = utils.Some(executor)
		app.settler.Store(utils.Some(executor))
	}
}

// A block's commit lands behind FinalizeBlock: the block whose commit fails is
// still finalized and committed, and the failure surfaces from the next
// FinalizeBlock, from read-only calls, and from settling the store, while the
// store itself stays at the last version that landed.
func TestEVMOnlyApplicationSurfacesAFailedCommitFromTheNextBlock(t *testing.T) {
	storageConfig, err := seidbconfig.AutobahnStorageConfig(t.TempDir())
	require.NoError(t, err)
	storage := openEVMOnlyTestStorage(t, storageConfig)
	// The failed commits leave the receipt store ahead of the state, so the shutdown is not clean.
	t.Cleanup(func() { require.Error(t, storage.Close()) })
	app, err := NewEVMOnlyApplication(evmOnlyTestChainID, storage, openEVMOnlyTestEpochs(t, storage, nil, 1), gigaconfig.DefaultConfig.Execution, utils.None[precompiles.Registry](), utils.None[GenesisAlloc](), utils.None[string]())
	require.NoError(t, err)
	err = app.InitChain(abevm.InitConfig{
		InitialHeight: 1,
		GasLimit:      30_000_000})
	require.NoError(t, err)
	settler, ok := app.(*evmOnlyApplication)
	require.True(t, ok)
	installUnwritableEVMExecutor(settler)

	// Block 1 is unwritable, yet it finalizes: the write has not
	// been waited for.
	raw, _ := signedEVMOnlyTestTx(t, evmOnlyTestChainID, 0)
	finalizeEVMOnlyTestBlock(t, app, evmOnlyTestBlock(app, 1, raw))
	require.Equal(t, int64(1), app.LastBlockHeight())

	// The failed write is reported by the next block, and stays reported.
	_, err = app.FinalizeBlock(t.Context(), evmOnlyTestBlock(app, 2))
	require.Error(t, err)
	err = settler.AwaitCommits()
	require.Error(t, err)
	require.Contains(t, err.Error(), "in the EVM module")
	_, err = settler.EvmCall(t.Context(), &ethcore.Message{GasLimit: 21_000, GasPrice: new(uint256.Int), Value: new(uint256.Int)}, abevm.CallOverrides{})
	require.Error(t, err)

	// The block that failed to finalize left nothing staged, and the store never
	// moved past genesis.
	for state := range settler.cursor.Lock() {
		require.False(t, state.pending.IsPresent())
	}
	require.Equal(t, int64(1), app.LastBlockHeight())
	require.Equal(t, int64(0), storage.StateDB().LiveStateDB().Version())
}

// Committed-state readers running alongside FinalizeBlock observe every block
// in order and never see a block's state go backwards while its commit lands,
// and the committed nonce never runs ahead of the settled one.
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
			var last, lastCommitted uint64
			for !done.Load() {
				committed := settler.EvmCommittedNonce(sender)
				if committed < lastCommitted {
					return fmt.Errorf("committed nonce went back from %d to %d", lastCommitted, committed)
				}
				lastCommitted = committed
				nonce := app.EvmNonce(sender)
				if nonce < last {
					return fmt.Errorf("nonce went back from %d to %d", last, nonce)
				}
				if nonce < committed {
					return fmt.Errorf("nonce %d is behind the committed nonce %d", nonce, committed)
				}
				last = nonce
				// Calls must not race the storage commit they settle.
				_, _ = settler.EvmCall(ctx, callMessage(sender, nil), abevm.CallOverrides{})
			}
			return nil
		})
		defer done.Store(true)
		for height := range int64(blocks) {
			if _, err := app.FinalizeBlock(ctx, evmOnlyTestBlock(app, height+1, txs[height])); err != nil {
				return err
			}
		}
		return nil
	})
	require.NoError(t, err)
	require.NoError(t, settler.AwaitCommits())
	require.Equal(t, uint64(blocks), app.EvmNonce(sender))
	require.Equal(t, uint64(blocks), settler.EvmCommittedNonce(sender))
}

func newEVMOnlyTestAppWithAlloc(t *testing.T, alloc GenesisAlloc) abevm.Application {
	t.Helper()
	storageConfig, err := seidbconfig.AutobahnStorageConfig(t.TempDir())
	require.NoError(t, err)
	storage := openEVMOnlyTestStorage(t, storageConfig)
	app, err := NewEVMOnlyApplication(
		evmOnlyTestChainID, storage,
		openEVMOnlyTestEpochs(t, storage, nil, 1),
		gigaconfig.DefaultConfig.Execution,
		utils.None[precompiles.Registry](),
		utils.Some(alloc),
		utils.None[string](),
	)
	require.NoError(t, err)
	t.Cleanup(func() { closeEVMOnlyTestApp(t, app, storage) })
	return app
}

func TestEVMOnlyApplicationAllocFundsOnlyListedAccounts(t *testing.T) {
	funder, err := crypto.GenerateKey()
	require.NoError(t, err)
	funderAddr := crypto.PubkeyToAddress(funder.PublicKey)
	allocBalance := new(uint256.Int).Lsh(uint256.NewInt(1), 70)
	app := newEVMOnlyTestAppWithAlloc(t, GenesisAlloc{InitialHeight: 1, Accounts: balanceAlloc(map[common.Address]*uint256.Int{funderAddr: allocBalance})})
	err = app.InitChain(evmOnlyTestInitChain())
	require.NoError(t, err)

	raw := signedEVMOnlyTestTxFrom(t, funder, evmOnlyTestChainID, 0)
	unfunded, unfundedAddr := signedEVMOnlyTestTx(t, evmOnlyTestChainID, 0)
	response, err := app.FinalizeBlock(t.Context(), evmOnlyTestBlock(app, 1, raw, unfunded))
	require.NoError(t, err)
	require.Equal(t, uint64(21_000), response.GasUsed)
	_, err = app.FinalizeBlock(t.Context(), evmOnlyTestBlock(app, 2))
	require.NoError(t, err)

	recipient := common.HexToAddress("0x1000000000000000000000000000000000000001")
	gotBalance := app.EvmBalance(recipient)
	require.Equal(t, big.NewInt(1), gotBalance.ToBig())
	wantBalance := new(big.Int).Sub(
		new(big.Int).Sub(allocBalance.ToBig(), big.NewInt(1)),
		new(big.Int).Mul(big.NewInt(evmOnlyMinGasPrice), new(big.Int).SetUint64(response.GasUsed)),
	)
	gotBalance = app.EvmBalance(funderAddr)
	require.Equal(t, wantBalance, gotBalance.ToBig(), "only the initial height credits the alloc")
	gotBalance = app.EvmBalance(unfundedAddr)
	require.Zero(t, gotBalance.Sign())
	require.Zero(t, app.EvmNonce(unfundedAddr))
}

func TestEVMOnlyApplicationAllocRefusesAnotherInitialHeight(t *testing.T) {
	app := newEVMOnlyTestAppWithAlloc(t, GenesisAlloc{InitialHeight: 5, Accounts: balanceAlloc(map[common.Address]*uint256.Int{{1}: uint256.NewInt(1)})})
	err := app.InitChain(evmOnlyTestInitChain())
	require.Error(t, err)
}

func TestEVMOnlyApplicationRefusesAnEmptyAlloc(t *testing.T) {
	storageConfig, err := seidbconfig.AutobahnStorageConfig(t.TempDir())
	require.NoError(t, err)
	storage := openEVMOnlyTestStorage(t, storageConfig)
	t.Cleanup(func() { require.NoError(t, storage.Close()) })
	_, err = NewEVMOnlyApplication(
		evmOnlyTestChainID, storage,
		openEVMOnlyTestEpochs(t, storage, nil, 1),
		gigaconfig.DefaultConfig.Execution,
		utils.None[precompiles.Registry](),
		utils.Some(GenesisAlloc{InitialHeight: 1, Accounts: balanceAlloc(map[common.Address]*uint256.Int{})}),
		utils.None[string](),
	)
	require.Error(t, err)
}

func TestEVMOnlyApplicationResumesOnlyWithTheAllocItStartedWith(t *testing.T) {
	alloc := func(balance uint64) utils.Option[GenesisAlloc] {
		return utils.Some(GenesisAlloc{InitialHeight: 1, Accounts: balanceAlloc(map[common.Address]*uint256.Int{
			{1}: uint256.NewInt(balance),
			{2}: uint256.NewInt(7),
		})})
	}
	none := utils.None[GenesisAlloc]()
	for _, tc := range []struct {
		name            string
		started, resume utils.Option[GenesisAlloc]
		ok              bool
	}{
		{"without alloc", none, none, true},
		{"same alloc", alloc(5), alloc(5), true},
		{"alloc added", none, alloc(5), false},
		{"alloc dropped", alloc(5), none, false},
		{"alloc changed", alloc(5), alloc(6), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			open := func(storage *bootstrap.GigaStorageManager, alloc utils.Option[GenesisAlloc]) (abevm.Application, error) {
				initialHeight := int64(1)
				if a, ok := alloc.Get(); ok {
					initialHeight = a.InitialHeight
				}
				return NewEVMOnlyApplication(
					evmOnlyTestChainID, storage,
					openEVMOnlyTestEpochs(t, storage, nil, initialHeight),
					gigaconfig.DefaultConfig.Execution,
					utils.None[precompiles.Registry](),
					alloc,
					utils.None[string](),
				)
			}
			home := t.TempDir()
			storage := openEVMOnlyTestStorageAt(t, home)
			app, err := open(storage, tc.started)
			require.NoError(t, err)
			err = app.InitChain(evmOnlyTestInitChain())
			require.NoError(t, err)
			finalizeEVMOnlyTestBlock(t, app, evmOnlyTestBlock(app, 1))
			closeEVMOnlyTestApp(t, app, storage)

			storage = openEVMOnlyTestStorageAt(t, home)
			app, err = open(storage, tc.resume)
			if err == nil {
				closeEVMOnlyTestApp(t, app, storage)
			} else {
				require.NoError(t, storage.Close())
			}
			require.Equal(t, tc.ok, err == nil, "resume error: %v", err)
		})
	}
}

func TestEVMOnlyApplicationResumesOnlyOnTheChainIDItRanOn(t *testing.T) {
	for _, tc := range []struct {
		name            string
		started, resume uint64
		ok              bool
	}{
		{"same chain ID", 1330, 1330, true},
		{"chain ID changed", evmOnlyTestChainID, 1330, false},
		{"chain ID changed back", 1330, evmOnlyTestChainID, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			storage := openEVMOnlyTestStorageAt(t, home)
			app, err := newEVMOnlyTestAppOnChain(t, storage, tc.started)
			require.NoError(t, err)
			err = app.InitChain(evmOnlyTestInitChain())
			require.NoError(t, err)
			finalizeEVMOnlyTestBlock(t, app, evmOnlyTestBlock(app, 1))
			closeEVMOnlyTestApp(t, app, storage)

			storage = openEVMOnlyTestStorageAt(t, home)
			app, err = newEVMOnlyTestAppOnChain(t, storage, tc.resume)
			if err == nil {
				closeEVMOnlyTestApp(t, app, storage)
			} else {
				require.NoError(t, storage.Close())
			}
			require.Equal(t, tc.ok, err == nil, "resume error: %v", err)
		})
	}
}

// newEVMOnlyTestAppOnChain constructs an application on chainID over storage.
func newEVMOnlyTestAppOnChain(t *testing.T, storage *bootstrap.GigaStorageManager, chainID uint64) (abevm.Application, error) {
	t.Helper()
	return NewEVMOnlyApplication(
		chainID, storage,
		openEVMOnlyTestEpochs(t, storage, nil, 1),
		gigaconfig.DefaultConfig.Execution,
		utils.None[precompiles.Registry](),
		utils.None[GenesisAlloc](),
		utils.None[string](),
	)
}

func TestEVMOnlyApplicationRefusesToResumeWithoutARecordedChainID(t *testing.T) {
	home := t.TempDir()
	storage := openEVMOnlyTestStorageAt(t, home)
	started, err := newEVMOnlyTestAppOnChain(t, storage, evmOnlyTestChainID)
	require.NoError(t, err)
	err = started.InitChain(evmOnlyTestInitChain())
	require.NoError(t, err)
	app := started.(*evmOnlyApplication)
	installEVMExecutorEncoder(app, func(block evmonly.BlockContext, state evmonly.StateReader, result *evmonly.BlockResult, after func() precompiles.State) ([]gigatypes.MiscChange, error) {
		misc, err := app.encodeCursorChangeSet(block, state, result, after)
		return slices.DeleteFunc(misc, func(change gigatypes.MiscChange) bool {
			return change.Module == evmOnlyCursorModule && string(change.Key) == evmOnlyChainIDKey
		}), err
	})
	finalizeEVMOnlyTestBlock(t, app, evmOnlyTestBlock(app, 1))
	closeEVMOnlyTestApp(t, app, storage)

	storage = openEVMOnlyTestStorageAt(t, home)
	t.Cleanup(func() { require.NoError(t, storage.Close()) })
	_, err = newEVMOnlyTestAppOnChain(t, storage, evmOnlyTestChainID)
	require.Error(t, err)
}

func TestEVMOnlyApplicationAdmitsOnlyTxsTheSenderCanPayFor(t *testing.T) {
	funder, err := crypto.GenerateKey()
	require.NoError(t, err)
	funderAddr := crypto.PubkeyToAddress(funder.PublicKey)
	app := newEVMOnlyTestAppWithAlloc(t, GenesisAlloc{InitialHeight: 1, Accounts: balanceAlloc(map[common.Address]*uint256.Int{
		funderAddr: new(uint256.Int).Lsh(uint256.NewInt(1), 70),
	})})
	err = app.InitChain(evmOnlyTestInitChain())
	require.NoError(t, err)
	chainID := new(big.Int).SetUint64(evmOnlyTestChainID)
	recipient := common.HexToAddress("0x1000000000000000000000000000000000000001")
	transfer := func(nonce uint64, value *big.Int) []byte {
		signed, err := ethtypes.SignTx(ethtypes.NewTx(&ethtypes.LegacyTx{
			Nonce: nonce, GasPrice: big.NewInt(evmOnlyMinGasPrice), Gas: 21_000, To: &recipient, Value: value,
		}), ethtypes.LatestSignerForChainID(chainID), funder)
		require.NoError(t, err)
		raw, err := signed.MarshalBinary()
		require.NoError(t, err)
		return raw
	}
	unfunded, _ := signedEVMOnlyTestTx(t, evmOnlyTestChainID, 0)
	tooMuch := new(big.Int).Lsh(big.NewInt(1), 70)

	check := func(when string) {
		require.NoError(t, admissionError(app, transfer(0, big.NewInt(1))), "funded %s", when)
		require.Error(t, admissionError(app, transfer(0, tooMuch)), "over balance %s", when)
		require.Error(t, admissionError(app, unfunded), "unfunded %s", when)
	}
	check("before block 1")
	for height := int64(1); height <= 2; height++ {
		finalizeEVMOnlyTestBlock(t, app, evmOnlyTestBlock(app, height))
		check(fmt.Sprintf("after finalization of block %d", height))
		require.NoError(t, app.(*evmOnlyApplication).AwaitCommits())
		check(fmt.Sprintf("after block %d's state commit", height))
	}
}

func TestEVMOnlyApplicationAdmitsOnlyTxsWhoseGasCoversIntrinsicGas(t *testing.T) {
	funder, err := crypto.GenerateKey()
	require.NoError(t, err)
	unfunded, err := crypto.GenerateKey()
	require.NoError(t, err)
	app := newEVMOnlyTestAppWithAlloc(t, GenesisAlloc{InitialHeight: 1, Accounts: balanceAlloc(map[common.Address]*uint256.Int{
		crypto.PubkeyToAddress(funder.PublicKey): new(uint256.Int).Lsh(uint256.NewInt(1), 70),
	})})
	err = app.InitChain(evmOnlyTestInitChain())
	require.NoError(t, err)
	recipient := common.HexToAddress("0x1000000000000000000000000000000000000001")
	calldata := slices.Repeat([]byte{0xff}, 100)
	// 100 non-zero bytes cost 21_000+16*100 intrinsic gas and a 21_000+10*4*100 calldata floor.
	const calldataFloor = 25_000

	for _, tc := range []struct {
		name  string
		key   *ecdsa.PrivateKey
		to    *common.Address
		gas   uint64
		data  []byte
		value int64
		ok    bool
	}{
		{name: "zero gas from an unfunded sender", key: unfunded, to: &recipient},
		{name: "zero gas", key: funder, to: &recipient},
		{name: "below transfer intrinsic gas", key: funder, to: &recipient, gas: 20_999, value: 1},
		{name: "transfer intrinsic gas", key: funder, to: &recipient, gas: 21_000, value: 1, ok: true},
		{name: "below create intrinsic gas", key: funder, gas: 52_999},
		{name: "create intrinsic gas", key: funder, gas: 53_000, ok: true},
		{name: "below calldata floor", key: funder, to: &recipient, gas: calldataFloor - 1, data: calldata},
		{name: "calldata floor", key: funder, to: &recipient, gas: calldataFloor, data: calldata, ok: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			signed, err := ethtypes.SignTx(ethtypes.NewTx(&ethtypes.LegacyTx{
				GasPrice: big.NewInt(evmOnlyMinGasPrice), Gas: tc.gas, To: tc.to, Data: tc.data, Value: big.NewInt(tc.value),
			}), ethtypes.LatestSignerForChainID(new(big.Int).SetUint64(evmOnlyTestChainID)), tc.key)
			require.NoError(t, err)
			raw, err := signed.MarshalBinary()
			require.NoError(t, err)
			require.Equal(t, tc.ok, admissionError(app, raw) == nil)
		})
	}
}

func applicationHash(t *testing.T, app abevm.Application) []byte {
	t.Helper()
	concrete, ok := app.(*evmOnlyApplication)
	require.True(t, ok)
	for state := range concrete.cursor.Lock() {
		cursor := state.committed
		if pending, ok := state.pending.Get(); ok {
			cursor = pending
		}
		return append([]byte(nil), cursor.appHash[:]...)
	}
	panic("unreachable")
}

func admissionError(app abevm.Application, raw []byte) error {
	tx, err := newTestSigner(app.EvmChainID(), 0).ParseTx(raw)
	if err != nil {
		return err
	}
	_, err = app.CheckTx(tx)
	return err
}

func TestEVMOnlyApplicationChecksLifecycleRanges(t *testing.T) {
	for _, config := range []abevm.InitConfig{
		{InitialHeight: 0, GasLimit: 30_000_000},
		{InitialHeight: atypes.RoadHeight(math.MaxUint64), GasLimit: 30_000_000},
		{InitialHeight: 1, GasLimit: 0},
		{InitialHeight: 1, GasLimit: math.MaxUint64},
	} {
		app := newEVMOnlyTestApp(t, nil)
		require.Error(t, app.InitChain(config))
	}
	app := newInitializedEVMOnlyTestApp(t)
	block := evmOnlyTestBlock(app, 1)
	block.Height = atypes.RoadHeight(math.MaxUint64)
	_, err := app.FinalizeBlock(t.Context(), block)
	require.Error(t, err)
	finalizeEVMOnlyTestBlock(t, app, evmOnlyTestBlock(app, 1))
	require.Equal(t, int64(1), app.LastBlockHeight())
}

func newTestSigner(chainID uint64, workers int) *abevm.Signer {
	return abevm.NewSigner(ethtypes.LatestSignerForChainID(new(big.Int).SetUint64(chainID)), 1024, workers)
}

// balanceAlloc returns genesis accounts that set a balance and nothing else.
func balanceAlloc(balances map[common.Address]*uint256.Int) map[common.Address]evmonly.GenesisAccount {
	accounts := make(map[common.Address]evmonly.GenesisAccount, len(balances))
	for addr, balance := range balances {
		accounts[addr] = evmonly.GenesisAccount{Balance: balance}
	}
	return accounts
}

// A FinalizeBlock called before the previous block's returns waits for it, and
// leaves the same state as finalizing the blocks one after the other.
func TestEVMOnlyApplicationFinalizesTheNextBlockWhileTheCurrentOneRuns(t *testing.T) {
	pipelined := newInitializedEVMOnlyTestApp(t)
	sequential := newInitializedEVMOnlyTestApp(t)
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	tx0 := signedEVMOnlyTestTxFrom(t, key, evmOnlyTestChainID, 0)
	tx1 := signedEVMOnlyTestTxFrom(t, key, evmOnlyTestChainID, 1)

	require.NoError(t, scope.Run(t.Context(), func(ctx context.Context, s scope.Scope) error {
		s.Spawn(func() error {
			_, err := pipelined.FinalizeBlock(ctx, evmOnlyTestBlock(pipelined, 2, tx1))
			return err
		})
		_, err := pipelined.FinalizeBlock(ctx, evmOnlyTestBlock(pipelined, 1, tx0))
		return err
	}))
	finalizeEVMOnlyTestBlock(t, sequential, evmOnlyTestBlock(sequential, 1, tx0))
	require.Equal(t, finalizeEVMOnlyTestBlock(t, sequential, evmOnlyTestBlock(sequential, 2, tx1)), pipelined.Info().LastBlockAppHash)
}

// A cancel does not abort a block whose turn has come.
func TestEVMOnlyApplicationFinalizesABlockWhoseTurnHasComeDespiteACancel(t *testing.T) {
	app := newInitializedEVMOnlyTestApp(t)
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = app.FinalizeBlock(ctx, evmOnlyTestBlock(app, 1,
		signedEVMOnlyTestTxFrom(t, key, evmOnlyTestChainID, 0),
		signedEVMOnlyTestTxFrom(t, key, evmOnlyTestChainID, 1)))
	require.NoError(t, err)
	require.Equal(t, int64(1), app.LastBlockHeight())
}
