package node

import (
	"context"
	"crypto/ecdsa"
	"crypto/sha256"
	"encoding/binary"
	"math"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	ethcore "github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	ethcrypto "github.com/ethereum/go-ethereum/crypto"

	gigaconfig "github.com/sei-protocol/sei-chain/giga/config"
	"github.com/sei-protocol/sei-chain/sei-db/bootstrap"
	seidbconfig "github.com/sei-protocol/sei-chain/sei-db/config"
	abevm "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/evm"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/evmonlyapp"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

func TestMockAppFinalizeBlockBumpsNoncesAndUsesGasWanted(t *testing.T) {
	key, err := ethcrypto.GenerateKey()
	require.NoError(t, err)

	tx1, _, addr := buildFastCheckTxBytesForKey(t, key, 0, 21_000)
	tx2, _, _ := buildFastCheckTxBytesForKey(t, key, 1, 31_000)
	app := NewMockApp(mockAppFixture{})
	err = app.InitChain(abevm.InitConfig{InitialHeight: 3})
	require.NoError(t, err)

	res, err := app.FinalizeBlock(t.Context(), abevm.FinalizeBlockReq{
		Txs:    utils.OrPanic1(newTestSigner(app.EvmChainID(), 0).ParseTxs([][]byte{tx1, tx2})),
		Height: 3})
	require.NoError(t, err)
	require.Equal(t, uint64(2), app.EvmNonce(addr))
	require.Equal(t, uint64(52_000), res.GasUsed)

	decoded := utils.OrPanic1(newTestSigner(app.EvmChainID(), 0).ParseTx(tx2))
	_, checkErr := app.CheckTx(decoded)
	require.NoError(t, checkErr)
	require.Equal(t, addr, decoded.Sender())
}

func TestMockAppServesTheEVMOnlyChainWithoutExecution(t *testing.T) {
	app := NewMockApp(mockAppFixture{})

	require.Equal(t, mockAppFixtureEVMChainID, app.EvmChainID())
	require.Equal(t, new(big.Int).SetUint64(mockAppFixtureEVMChainID), app.EvmChainConfig().ChainID)
	require.Equal(t, 0, app.EvmBaseFee().Sign())
	require.Equal(t, 0, app.EvmMinGasPrice().Sign())
	require.Equal(t, uint64(0), app.EvmGasLimit())
	_, err := app.EvmCall(t.Context(), &ethcore.Message{}, abevm.CallOverrides{})
	require.ErrorIs(t, err, errMockAppNoExecution)
	_, _, err = app.EvmEstimateGas(t.Context(), &ethcore.Message{}, 0, abevm.CallOverrides{})
	require.ErrorIs(t, err, errMockAppNoExecution)
	_, _, err = app.EvmEstimateGasAfterCalls(t.Context(), nil, &ethcore.Message{}, 0, abevm.CallOverrides{})
	require.ErrorIs(t, err, errMockAppNoExecution)
	_, _, err = app.EvmEstimateGasAfterCallsAt(t.Context(), nil, &ethcore.Message{}, 0, abevm.BlockRef{Height: 1}, abevm.CallOverrides{})
	require.ErrorIs(t, err, errMockAppNoExecution)
	_, _, err = app.EvmCreateAccessList(t.Context(), &ethcore.Message{})
	require.ErrorIs(t, err, errMockAppNoExecution)
	_, _, err = app.EvmCreateAccessListAt(t.Context(), &ethcore.Message{}, abevm.BlockRef{Height: 1})
	require.ErrorIs(t, err, errMockAppNoExecution)
	// The block RPC renders every block's context, so the mock app answers it without execution.
	blockCtx, err := app.EvmBlockContext(abevm.BlockRef{Height: 1, Time: 5})
	require.NoError(t, err)
	require.Equal(t, uint64(1), blockCtx.BlockNumber.Uint64())
	require.Equal(t, uint64(5), blockCtx.Time)
	require.Equal(t, common.Hash{}, *blockCtx.Random)
}

func TestMockAppFinalizeBlockFailsNonceMismatch(t *testing.T) {
	key, err := ethcrypto.GenerateKey()
	require.NoError(t, err)

	tx0, _, addr := buildFastCheckTxBytesForKey(t, key, 0, 21_000)
	tx2, _, _ := buildFastCheckTxBytesForKey(t, key, 2, 31_000)
	tx1, _, _ := buildFastCheckTxBytesForKey(t, key, 1, 41_000)
	app := NewMockApp(mockAppFixture{})
	err = app.InitChain(abevm.InitConfig{InitialHeight: 1})
	require.NoError(t, err)

	res, err := app.FinalizeBlock(t.Context(), abevm.FinalizeBlockReq{
		Txs:    utils.OrPanic1(newTestSigner(app.EvmChainID(), 0).ParseTxs([][]byte{tx0, tx2, tx1})),
		Height: 1})
	require.NoError(t, err)
	require.Equal(t, uint64(62_000), res.GasUsed)
	require.Equal(t, uint64(2), app.EvmNonce(addr))

	duplicate, err := app.FinalizeBlock(t.Context(), abevm.FinalizeBlockReq{
		Txs:    utils.OrPanic1(newTestSigner(app.EvmChainID(), 0).ParseTxs([][]byte{tx1})),
		Height: 2})
	require.NoError(t, err)
	require.Zero(t, duplicate.GasUsed)
	require.Equal(t, uint64(2), app.EvmNonce(addr))
}

func TestMockAppFinalizeBlockSkipsInvalidTransactions(t *testing.T) {
	key, err := ethcrypto.GenerateKey()
	require.NoError(t, err)

	tx0, _, addr := buildFastCheckTxBytesForKey(t, key, 0, 21_000)
	tx1, _, _ := buildFastCheckTxBytesForKey(t, key, 1, 31_000)
	app := NewMockApp(mockAppFixture{})
	err = app.InitChain(abevm.InitConfig{InitialHeight: 1})
	require.NoError(t, err)

	invalid, _, _ := buildFastCheckTxBytesForKey(t, key, 0, math.MaxUint64)

	res, err := app.FinalizeBlock(t.Context(), abevm.FinalizeBlockReq{
		Txs:    utils.OrPanic1(newTestSigner(app.EvmChainID(), 0).ParseTxs([][]byte{tx0, invalid, tx1})),
		Height: 1})
	require.NoError(t, err)
	require.Equal(t, uint64(52_000), res.GasUsed)
	require.Equal(t, uint64(2), app.EvmNonce(addr))
	require.Equal(t, int64(1), app.LastBlockHeight())
}

func TestMockAppFinalizeBlockRequiresSequentialHeight(t *testing.T) {
	app := NewMockApp(mockAppFixture{})
	err := app.InitChain(abevm.InitConfig{InitialHeight: 4})
	require.NoError(t, err)

	_, err = app.FinalizeBlock(t.Context(), abevm.FinalizeBlockReq{})
	require.Error(t, err)

	// A later height waits for the blocks before it.
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = app.FinalizeBlock(ctx, abevm.FinalizeBlockReq{
		Height: 5})
	require.ErrorIs(t, err, context.Canceled)

	res, err := app.FinalizeBlock(t.Context(), abevm.FinalizeBlockReq{
		Height: 4})
	require.NoError(t, err)
	require.Equal(t, uint64(0), res.GasUsed)
	require.Equal(t, int64(4), app.LastBlockHeight())
}

func TestMockAppChecksTransitions(t *testing.T) {
	app := NewMockApp(mockAppFixture{})
	_, err := app.FinalizeBlock(t.Context(), abevm.FinalizeBlockReq{Height: 1})
	require.Error(t, err)
	err = app.InitChain(abevm.InitConfig{InitialHeight: 1})
	require.NoError(t, err)
	err = app.InitChain(abevm.InitConfig{InitialHeight: 1})
	require.Error(t, err)
	_, err = app.FinalizeBlock(t.Context(), abevm.FinalizeBlockReq{Height: 1})
	require.NoError(t, err)
	_, err = app.FinalizeBlock(t.Context(), abevm.FinalizeBlockReq{Height: 1})
	require.Error(t, err)
	_, err = app.FinalizeBlock(t.Context(), abevm.FinalizeBlockReq{Height: 2})
	require.NoError(t, err)
}

func TestMockAppFinalizesHighBlocks(t *testing.T) {
	app := NewMockApp(mockAppFixture{})
	err := app.InitChain(abevm.InitConfig{InitialHeight: 10_005})
	require.NoError(t, err)

	res, err := app.FinalizeBlock(t.Context(), abevm.FinalizeBlockReq{
		Height: 10_005})
	require.NoError(t, err)
	require.Equal(t, uint64(0), res.GasUsed)
	require.Equal(t, int64(10005), app.LastBlockHeight())

	_, err = app.FinalizeBlock(t.Context(), abevm.FinalizeBlockReq{
		Height: 10_006})
	require.NoError(t, err)
	require.Equal(t, int64(10006), app.LastBlockHeight())
}

func TestMockAppForwardsInitChainAndIgnoresRestoreBlockTime(t *testing.T) {
	inner := &mockAppForwardTarget{}
	app := NewMockApp(inner)
	header := time.Unix(11, 0)

	err := app.InitChain(abevm.InitConfig{})
	require.Error(t, err)

	err = app.InitChain(abevm.InitConfig{InitialHeight: 7})
	require.NoError(t, err)
	require.Equal(t, int64(6), app.LastBlockHeight())
	app.RestoreBlockTime(header)

	require.True(t, inner.initChainCalled)
	require.False(t, inner.lastHeader.IsPresent())
	require.Equal(t, int64(6), app.LastBlockHeight())
}

func TestMockAppAppHashRollsBlockHashes(t *testing.T) {
	app := NewMockApp(mockAppFixture{})
	err := app.InitChain(abevm.InitConfig{InitialHeight: 1})
	require.NoError(t, err)
	require.Empty(t, app.Info().LastBlockAppHash)

	blockHash1 := common.BytesToHash([]byte("block-1"))
	_, err = app.FinalizeBlock(t.Context(), abevm.FinalizeBlockReq{
		Height: 1,
		Hash:   blockHash1})
	require.NoError(t, err)
	wantHash1 := sha256.Sum256(blockHash1[:])
	require.Equal(t, wantHash1[:], app.Info().LastBlockAppHash)

	blockHash2 := common.BytesToHash([]byte("block-2"))
	_, err = app.FinalizeBlock(t.Context(), abevm.FinalizeBlockReq{
		Height: 2,
		Hash:   blockHash2})
	require.NoError(t, err)
	h := sha256.New()
	_, _ = h.Write(wantHash1[:])
	_, _ = h.Write(blockHash2[:])
	require.Equal(t, h.Sum(nil), app.Info().LastBlockAppHash)

	app.RestoreBlockTime(time.Unix(5, 0))
	require.Equal(t, int64(2), app.LastBlockHeight())
}

func TestMockAppInfoPreservesWrappedAppMetadata(t *testing.T) {
	app := NewMockApp(mockAppInfoTarget{})
	err := app.InitChain(abevm.InitConfig{InitialHeight: 9})
	require.NoError(t, err)

	info := app.Info()
	require.Equal(t, "wrapped-data", info.Data)
	require.Equal(t, "wrapped-version", info.Version)
	require.Equal(t, uint64(17), info.AppVersion)
	require.Equal(t, "1usei", info.MinimumGasPrices)
	require.Equal(t, int64(8), info.LastBlockHeight)
	require.Empty(t, info.LastBlockAppHash)
}

func TestMockAppGetValidatorsCachesWrappedAppValidators(t *testing.T) {
	rng := utils.TestRng()
	key := atypes.GenSecretKey(rng).Public()
	inner := &mockAppValidatorsTarget{validators: abevm.CommitteeWeights{key: 7}}
	app := NewMockApp(inner)

	require.Empty(t, app.GetValidators())
	require.Zero(t, inner.calls)

	err := app.InitChain(abevm.InitConfig{InitialHeight: 1})
	require.NoError(t, err)
	require.Equal(t, abevm.CommitteeWeights{key: 7}, app.GetValidators())
	require.Equal(t, abevm.CommitteeWeights{key: 7}, app.GetValidators())
	require.Equal(t, 1, inner.calls)

	inner.validators[key] = 11
	returned := app.GetValidators()
	returned[key] = 13
	app.RestoreBlockTime(time.Unix(3, 0))
	require.Equal(t, abevm.CommitteeWeights{key: 7}, app.GetValidators())
	require.Equal(t, 1, inner.calls)
}

func TestMockAppEvmBalanceReturns100ETH(t *testing.T) {
	app := NewMockApp(mockAppFixture{})
	want := new(big.Int).Mul(big.NewInt(100), big.NewInt(1_000_000_000_000_000_000))

	got := app.EvmBalance(common.Address{})
	require.Equal(t, want, got.ToBig())
}

// TestMockAppCommitAdvancesTheEpochRegistry pins that a mock-app commit derives the next epoch
// on a closing road. Deleting the Changes call leaves the window on epoch 1.
func TestMockAppCommitAdvancesTheEpochRegistry(t *testing.T) {
	const length = 3
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	storageConfig, err := seidbconfig.AutobahnStorageConfig(t.TempDir())
	require.NoError(t, err)
	storage, err := bootstrap.NewGigaStorageManager(ctx, storageConfig)
	require.NoError(t, err)
	t.Cleanup(func() {
		for n := uint64(1); n <= length; n++ {
			var hash [32]byte
			binary.BigEndian.PutUint64(hash[:8], n)
			require.NoError(t, storage.AppHashBuilder().ReportBlockHash(context.Background(), n, hash))
		}
		require.NoError(t, storage.Close())
	})

	key := atypes.GenSecretKey(utils.TestRng())
	epochs, err := evmonlyapp.OpenEpochRegistry(storage, 1, length, abevm.CommitteeWeights{key.Public(): 1})
	require.NoError(t, err)

	app := NewMockApp(mockAppFixture{})
	app.storage = utils.Some(storage)
	app.epochRegistry = epochs
	err = app.InitChain(abevm.InitConfig{InitialHeight: 1, GasLimit: 30_000_000})
	require.NoError(t, err)
	for height := int64(1); height <= length; height++ {
		_, err = app.FinalizeBlock(t.Context(), abevm.FinalizeBlockReq{Height: atypes.RoadHeight(height)})
		require.NoError(t, err)
	}
	registry, err := app.EpochRegistry()
	require.NoError(t, err)
	window := registry.Epochs()
	require.Equal(t, atypes.EpochIndex(2), window[len(window)-1].EpochIndex())
}

const mockAppFixtureEVMChainID uint64 = 4242

type mockAppFixture struct {
	abevm.Application
}

func (mockAppFixture) InitChain(abevm.InitConfig) error {
	return nil
}
func (mockAppFixture) EvmChainID() uint64                    { return mockAppFixtureEVMChainID }
func (mockAppFixture) GetValidators() abevm.CommitteeWeights { return nil }
func (mockAppFixture) Info() *abevm.ResponseInfo             { return &abevm.ResponseInfo{} }

type mockAppForwardTarget struct {
	mockAppFixture
	initChainCalled bool
	lastHeader      utils.Option[time.Time]
}

func (app *mockAppForwardTarget) InitChain(abevm.InitConfig) error {
	app.initChainCalled = true
	return nil
}

func (app *mockAppForwardTarget) RestoreBlockTime(lastHeader time.Time) {
	app.lastHeader = utils.Some(lastHeader)
}

type mockAppInfoTarget struct {
	mockAppFixture
}

func (mockAppInfoTarget) Info() *abevm.ResponseInfo {
	return &abevm.ResponseInfo{
		Data:             "wrapped-data",
		Version:          "wrapped-version",
		AppVersion:       17,
		LastBlockHeight:  123,
		LastBlockAppHash: []byte("wrapped-hash"),
		MinimumGasPrices: "1usei",
	}
}

type mockAppValidatorsTarget struct {
	mockAppFixture
	validators abevm.CommitteeWeights
	calls      int
}

func (app *mockAppValidatorsTarget) GetValidators() abevm.CommitteeWeights {
	app.calls++
	return app.validators
}

func buildFastCheckTxBytesForKey(t *testing.T, key *ecdsa.PrivateKey, nonce uint64, gas uint64) ([]byte, *ethtypes.Transaction, common.Address) {
	t.Helper()

	chainID := new(big.Int).SetUint64(mockAppFixtureEVMChainID)
	to := common.HexToAddress("0x1000000000000000000000000000000000000001")
	ethTx := ethtypes.NewTx(&ethtypes.DynamicFeeTx{
		ChainID:   chainID,
		Nonce:     nonce,
		GasTipCap: big.NewInt(1),
		GasFeeCap: big.NewInt(10),
		Gas:       gas,
		To:        &to,
		Value:     big.NewInt(0),
	})
	signedTx, err := ethtypes.SignTx(ethTx, ethtypes.LatestSignerForChainID(chainID), key)
	require.NoError(t, err)
	txBytes, err := signedTx.MarshalBinary()
	require.NoError(t, err)
	addr := ethcrypto.PubkeyToAddress(key.PublicKey)
	return txBytes, signedTx, addr
}

func TestCheckMockTxEVMTransaction(t *testing.T) {
	raw, ethTx := buildFastCheckTxBytes(t)
	app := NewMockApp(mockAppFixture{})
	decoded := utils.OrPanic1(newTestSigner(app.EvmChainID(), 0).ParseTx(raw))
	res, err := checkMockTx(decoded)
	require.NoError(t, err)
	require.Equal(t, uint64(123456), res.GasEstimated)
	sender, err := ethtypes.Sender(ethtypes.LatestSignerForChainID(ethTx.ChainId()), ethTx)
	require.NoError(t, err)
	require.Equal(t, sender, decoded.Sender())
	require.NotEqual(t, common.Address{}, decoded.Sender())
}

func TestMockAppRejectsUnprotectedTransaction(t *testing.T) {
	wrapped := NewMockApp(mockAppFixture{})
	key, err := ethcrypto.GenerateKey()
	require.NoError(t, err)
	signed, err := ethtypes.SignTx(ethtypes.NewTx(&ethtypes.LegacyTx{}), ethtypes.HomesteadSigner{}, key)
	require.NoError(t, err)
	_, _, err = abevm.CheckTx(abevm.NewProxy(wrapped), newTestSigner(wrapped.EvmChainID(), 0), utils.OrPanic1(signed.MarshalBinary()))
	require.ErrorIs(t, err, abevm.ErrTxRejected)
}

func TestMockAppChecksTransactionsWithoutCallingWrappedApp(t *testing.T) {
	app := &checkTxCountingApp{}
	txBytes, _ := buildFastCheckTxBytes(t)

	wrapped := NewMockApp(app)
	res, checkErr := wrapped.CheckTx(utils.OrPanic1(newTestSigner(wrapped.EvmChainID(), 0).ParseTx(txBytes)))
	require.False(t, app.called)
	require.Equal(t, uint64(123456), res.GasEstimated)
	require.NoError(t, checkErr)
}

func TestMakeEVMApplicationAutobahnMockAppKeepsMockApp(t *testing.T) {
	validator := makeValidator([]byte("evm-only-validator"), []byte("evm-only-node"), "localhost:26660")
	autobahnConfigFile := writeAutobahnConfig(t, defaultFileConfig(t, []config.AutobahnValidator{validator}))

	app, manager, _, err := makeEVMApplication(storageContext(t), &config.Config{
		BaseConfig: config.BaseConfig{
			MockApp: true,
		},
		AutobahnConfigFile: autobahnConfigFile,
	}, gigaconfig.DefaultConfig, genesisAt(1))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, manager.Close()) })

	mock, ok := app.(*MockApp)
	require.True(t, ok)
	require.Equal(t, "evmonly", mock.Info().Data)

	err = mock.InitChain(abevm.InitConfig{InitialHeight: 1, GasLimit: 30_000_000})
	require.NoError(t, err)
	validators := mock.GetValidators()
	require.Len(t, validators, 1)
	require.Equal(t, uint64(1), validators[validator.ValidatorKey])
	var blockHash [32]byte
	blockHash[0] = 1
	require.NoError(t, manager.AppHashBuilder().ReportBlockHash(t.Context(), 1, blockHash))
	_, err = mock.FinalizeBlock(t.Context(), abevm.FinalizeBlockReq{
		Height: 1})
	require.NoError(t, err)
	got, err := manager.AppHashBuilder().GetAppHash(t.Context(), 1)
	require.NoError(t, err)
	require.Equal(t, uint64(1), got.BlockHeight())
}

func TestMakeEVMApplicationAutobahnRequiresReadableConfig(t *testing.T) {
	_, _, _, err := makeEVMApplication(t.Context(), &config.Config{
		AutobahnConfigFile: "/missing/autobahn.json",
	}, gigaconfig.DefaultConfig, genesisAt(1))

	require.Error(t, err)
}

func TestValidateNodeSetupConfigAllowsMockAppWithoutAutobahn(t *testing.T) {
	err := validateNodeSetupConfig(&config.Config{
		BaseConfig: config.BaseConfig{
			MockApp: true,
		},
	})

	require.NoError(t, err)
}

func TestValidateNodeSetupConfigAllowsMockAppWithAutobahn(t *testing.T) {
	err := validateNodeSetupConfig(&config.Config{
		BaseConfig: config.BaseConfig{
			MockApp: true,
		},
		AutobahnConfigFile: "/tmp/autobahn.json",
	})

	require.NoError(t, err)
}

type checkTxCountingApp struct {
	abevm.Application
	called bool
}

func (*checkTxCountingApp) EvmChainID() uint64 { return mockAppFixtureEVMChainID }

func (app *checkTxCountingApp) CheckTx(abevm.EvmTx) (abevm.AdmittedTx, error) {
	app.called = true
	return abevm.AdmittedTx{}, nil
}

func buildFastCheckTxBytes(t *testing.T) ([]byte, *ethtypes.Transaction) {
	t.Helper()

	key, err := ethcrypto.GenerateKey()
	require.NoError(t, err)
	chainID := new(big.Int).SetUint64(mockAppFixtureEVMChainID)
	to := common.HexToAddress("0x1000000000000000000000000000000000000001")
	ethTx := ethtypes.NewTx(&ethtypes.DynamicFeeTx{
		ChainID:   chainID,
		Nonce:     7,
		GasTipCap: big.NewInt(1),
		GasFeeCap: big.NewInt(10),
		Gas:       123456,
		To:        &to,
		Value:     big.NewInt(0),
		Data:      []byte{1, 2, 3},
	})
	signedTx, err := ethtypes.SignTx(ethTx, ethtypes.LatestSignerForChainID(chainID), key)
	require.NoError(t, err)
	raw, err := signedTx.MarshalBinary()
	require.NoError(t, err)
	return raw, signedTx
}

func TestABCINodeIgnoresMockAppSetting(t *testing.T) {
	cfg, err := config.ResetTestRoot(t.TempDir(), "abci_ignores_mock_app")
	require.NoError(t, err)
	cfg.Mode = config.ModeFull
	cfg.MockApp = true
	cfg.FastCheckTx = true

	node := getTestNode(t.Context(), t, cfg)
	require.NoError(t, node.Start(t.Context()))
	t.Cleanup(func() {
		node.Stop()
		node.Wait()
	})

	env := node.RPCEnvironment()
	require.True(t, env.App.IsPresent())
	require.False(t, env.EVM.IsPresent())
}

func newTestSigner(chainID uint64, workers int) *abevm.Signer {
	return abevm.NewSigner(ethtypes.LatestSignerForChainID(new(big.Int).SetUint64(chainID)), 1024, workers)
}
