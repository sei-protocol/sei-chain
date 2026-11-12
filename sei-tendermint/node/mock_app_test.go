package node

import (
	"context"
	"crypto/ecdsa"
	"crypto/sha256"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	ethcore "github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	ethcrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/gogo/protobuf/proto"

	gigaconfig "github.com/sei-protocol/sei-chain/giga/config"
	"github.com/sei-protocol/sei-chain/giga/evmonly"
	codectypes "github.com/sei-protocol/sei-chain/sei-cosmos/codec/types"
	sdkerrors "github.com/sei-protocol/sei-chain/sei-cosmos/types/errors"
	txtypes "github.com/sei-protocol/sei-chain/sei-cosmos/types/tx"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/sei-protocol/sei-chain/x/evm/types/ethtx"
)

func TestMockAppFinalizeBlockBumpsNoncesAndUsesGasWanted(t *testing.T) {
	key, err := ethcrypto.GenerateKey()
	require.NoError(t, err)

	tx1, _, addr := buildFastCheckTxBytesForKey(t, key, 0, 21_000)
	tx2, ethTx2, _ := buildFastCheckTxBytesForKey(t, key, 1, 31_000)
	app := NewMockApp(mockAppFixture{})
	_, err = app.InitChain(&proxy.RequestInitChain{InitialHeight: 3})
	require.NoError(t, err)

	res, err := app.FinalizeBlock(t.Context(), &proxy.RequestFinalizeBlock{
		Txs:    [][]byte{tx1, tx2},
		Header: &proxy.Header{Height: 3},
	})
	require.NoError(t, err)
	require.Len(t, res.TxResults, 2)
	require.Equal(t, int64(21_000), res.TxResults[0].GasWanted)
	require.Equal(t, int64(21_000), res.TxResults[0].GasUsed)
	require.Equal(t, int64(31_000), res.TxResults[1].GasWanted)
	require.Equal(t, int64(31_000), res.TxResults[1].GasUsed)
	require.Equal(t, uint64(2), app.EvmNonce(addr))
	require.NotEmpty(t, res.AppHash)

	checkRes := app.CheckTx(t.Context(), &proxy.RequestCheckTxV2{Tx: tx2})
	require.True(t, checkRes.IsEVM)
	require.Equal(t, ethTx2.Hash(), checkRes.EVMHash)
}

func TestMockAppServesTheEVMOnlyChainWithoutExecution(t *testing.T) {
	app := NewMockApp(mockAppFixture{})

	require.Equal(t, config.AutobahnEVMOnlyChainID, app.EvmChainID())
	require.Equal(t, new(big.Int).SetUint64(config.AutobahnEVMOnlyChainID), app.EvmChainConfig().ChainID)
	require.Equal(t, 0, app.EvmBaseFee().Sign())
	require.Equal(t, 0, app.EvmMinGasPrice().Sign())
	require.Equal(t, uint64(0), app.EvmGasLimit())
	_, err := app.EvmCall(t.Context(), &ethcore.Message{}, evmonly.CallOverrides{})
	require.ErrorIs(t, err, errMockAppNoExecution)
	_, _, err = app.EvmEstimateGas(t.Context(), &ethcore.Message{}, 0, evmonly.CallOverrides{})
	require.ErrorIs(t, err, errMockAppNoExecution)
	_, _, err = app.EvmEstimateGasAfterCalls(t.Context(), nil, &ethcore.Message{}, 0)
	require.ErrorIs(t, err, errMockAppNoExecution)
	_, _, err = app.EvmCreateAccessList(t.Context(), &ethcore.Message{})
	require.ErrorIs(t, err, errMockAppNoExecution)
}

func TestMockAppFinalizeBlockFailsNonceMismatch(t *testing.T) {
	key, err := ethcrypto.GenerateKey()
	require.NoError(t, err)

	tx0, _, addr := buildFastCheckTxBytesForKey(t, key, 0, 21_000)
	tx2, _, _ := buildFastCheckTxBytesForKey(t, key, 2, 31_000)
	tx1, _, _ := buildFastCheckTxBytesForKey(t, key, 1, 41_000)
	app := NewMockApp(mockAppFixture{})
	_, err = app.InitChain(&proxy.RequestInitChain{InitialHeight: 1})
	require.NoError(t, err)

	res, err := app.FinalizeBlock(t.Context(), &proxy.RequestFinalizeBlock{
		Txs:    [][]byte{tx0, tx2, tx1},
		Header: &proxy.Header{Height: 1},
	})
	require.NoError(t, err)
	require.Len(t, res.TxResults, 3)
	require.Equal(t, proxy.CodeTypeOK, res.TxResults[0].Code)
	require.Equal(t, sdkerrors.ErrWrongSequence.ABCICode(), res.TxResults[1].Code)
	require.Equal(t, sdkerrors.ErrWrongSequence.Codespace(), res.TxResults[1].Codespace)
	require.Equal(t, sdkerrors.ErrWrongSequence.Error(), res.TxResults[1].Log)
	require.Equal(t, int64(0), res.TxResults[1].GasWanted)
	require.Equal(t, int64(0), res.TxResults[1].GasUsed)
	require.Equal(t, proxy.CodeTypeOK, res.TxResults[2].Code)
	require.Equal(t, uint64(2), app.EvmNonce(addr))
	_, err = app.Commit(t.Context())
	require.NoError(t, err)

	duplicate, err := app.FinalizeBlock(t.Context(), &proxy.RequestFinalizeBlock{
		Txs:    [][]byte{tx1},
		Header: &proxy.Header{Height: 2},
	})
	require.NoError(t, err)
	require.Equal(t, sdkerrors.ErrWrongSequence.ABCICode(), duplicate.TxResults[0].Code)
	require.Equal(t, uint64(2), app.EvmNonce(addr))
}

func TestMockAppFinalizeBlockRecordsParseFailurePerTx(t *testing.T) {
	key, err := ethcrypto.GenerateKey()
	require.NoError(t, err)

	tx0, _, addr := buildFastCheckTxBytesForKey(t, key, 0, 21_000)
	tx1, _, _ := buildFastCheckTxBytesForKey(t, key, 1, 31_000)
	app := NewMockApp(mockAppFixture{})
	_, err = app.InitChain(&proxy.RequestInitChain{InitialHeight: 1})
	require.NoError(t, err)

	res, err := app.FinalizeBlock(t.Context(), &proxy.RequestFinalizeBlock{
		Txs:    [][]byte{tx0, []byte("not a tx"), tx1},
		Header: &proxy.Header{Height: 1},
	})
	require.NoError(t, err)
	require.Len(t, res.TxResults, 3)
	require.Equal(t, proxy.CodeTypeOK, res.TxResults[0].Code)
	require.NotEqual(t, proxy.CodeTypeOK, res.TxResults[1].Code)
	require.Equal(t, int64(0), res.TxResults[1].GasWanted)
	require.Equal(t, int64(0), res.TxResults[1].GasUsed)
	require.Equal(t, proxy.CodeTypeOK, res.TxResults[2].Code)
	require.Equal(t, uint64(2), app.EvmNonce(addr))
	require.Equal(t, int64(1), app.LastBlockHeight())
}

func TestMockAppFinalizeBlockRequiresSequentialHeader(t *testing.T) {
	app := NewMockApp(mockAppFixture{})
	_, err := app.InitChain(&proxy.RequestInitChain{InitialHeight: 4})
	require.NoError(t, err)

	_, err = app.FinalizeBlock(t.Context(), &proxy.RequestFinalizeBlock{})
	require.Error(t, err)

	_, err = app.FinalizeBlock(t.Context(), &proxy.RequestFinalizeBlock{
		Header: &proxy.Header{Height: 5},
	})
	require.Error(t, err)

	res, err := app.FinalizeBlock(t.Context(), &proxy.RequestFinalizeBlock{
		Header: &proxy.Header{Height: 4},
	})
	require.NoError(t, err)
	require.Empty(t, res.TxResults)
	require.Equal(t, int64(4), app.LastBlockHeight())
}

func TestMockAppChecksTransitions(t *testing.T) {
	app := NewMockApp(mockAppFixture{})

	_, err := app.Commit(t.Context())
	require.Error(t, err)
	_, err = app.FinalizeBlock(t.Context(), &proxy.RequestFinalizeBlock{
		Header: &proxy.Header{Height: 1},
	})
	require.Error(t, err)

	_, err = app.InitChain(&proxy.RequestInitChain{InitialHeight: 1})
	require.NoError(t, err)
	_, err = app.InitChain(&proxy.RequestInitChain{InitialHeight: 1})
	require.Error(t, err)
	_, err = app.Commit(t.Context())
	require.Error(t, err)

	_, err = app.FinalizeBlock(t.Context(), &proxy.RequestFinalizeBlock{
		Header: &proxy.Header{Height: 1},
	})
	require.NoError(t, err)
	_, err = app.FinalizeBlock(t.Context(), &proxy.RequestFinalizeBlock{
		Header: &proxy.Header{Height: 2},
	})
	require.Error(t, err)

	_, err = app.Commit(t.Context())
	require.NoError(t, err)
	_, err = app.FinalizeBlock(t.Context(), &proxy.RequestFinalizeBlock{
		Header: &proxy.Header{Height: 2},
	})
	require.NoError(t, err)
}

func TestMockAppCommitSetsRetainHeight(t *testing.T) {
	app := NewMockApp(mockAppFixture{})
	_, err := app.InitChain(&proxy.RequestInitChain{InitialHeight: 10_005})
	require.NoError(t, err)

	res, err := app.FinalizeBlock(t.Context(), &proxy.RequestFinalizeBlock{
		Header: &proxy.Header{Height: 10_005},
	})
	require.NoError(t, err)
	require.Empty(t, res.TxResults)
	commit, err := app.Commit(t.Context())
	require.NoError(t, err)
	require.Equal(t, int64(5), commit.RetainHeight)

	_, err = app.FinalizeBlock(t.Context(), &proxy.RequestFinalizeBlock{
		Header: &proxy.Header{Height: 10_006},
	})
	require.NoError(t, err)
	commit, err = app.Commit(t.Context())
	require.NoError(t, err)
	require.Equal(t, int64(6), commit.RetainHeight)
}

func TestMockAppForwardsInitChainAndIgnoresInitLastHeader(t *testing.T) {
	inner := &mockAppForwardTarget{}
	app := NewMockApp(inner)
	header := &proxy.Header{Height: 11}

	_, err := app.InitChain(&proxy.RequestInitChain{})
	require.Error(t, err)

	res, err := app.InitChain(&proxy.RequestInitChain{InitialHeight: 7})
	require.NoError(t, err)
	require.Equal(t, int64(6), app.LastBlockHeight())
	require.Empty(t, res.AppHash)
	app.InitLastHeader(header)

	require.True(t, inner.initChainCalled)
	require.False(t, inner.lastHeader.IsPresent())
	require.Equal(t, int64(6), app.LastBlockHeight())
}

func TestMockAppAppHashRollsBlockHashes(t *testing.T) {
	app := NewMockApp(mockAppFixture{})
	_, err := app.InitChain(&proxy.RequestInitChain{InitialHeight: 1})
	require.NoError(t, err)
	require.Empty(t, app.Info().LastBlockAppHash)

	blockHash1 := []byte("block-1")
	res, err := app.FinalizeBlock(t.Context(), &proxy.RequestFinalizeBlock{
		Header: &proxy.Header{Height: 1},
		Hash:   blockHash1,
	})
	require.NoError(t, err)
	wantHash1 := sha256.Sum256(blockHash1)
	require.Equal(t, wantHash1[:], res.AppHash)
	_, err = app.Commit(t.Context())
	require.NoError(t, err)

	blockHash2 := []byte("block-2")
	res, err = app.FinalizeBlock(t.Context(), &proxy.RequestFinalizeBlock{
		Header: &proxy.Header{Height: 2},
		Hash:   blockHash2,
	})
	require.NoError(t, err)
	h := sha256.New()
	_, _ = h.Write(wantHash1[:])
	_, _ = h.Write(blockHash2)
	require.Equal(t, h.Sum(nil), res.AppHash)

	app.InitLastHeader(&proxy.Header{Height: 5})
	require.Equal(t, int64(2), app.LastBlockHeight())
}

func TestMockAppInfoPreservesWrappedAppMetadata(t *testing.T) {
	app := NewMockApp(mockAppInfoTarget{})
	_, err := app.InitChain(&proxy.RequestInitChain{InitialHeight: 9})
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
	inner := &mockAppValidatorsTarget{validators: []proxy.ValidatorUpdate{{PubKey: key, Power: 7}}}
	app := NewMockApp(inner)

	require.Empty(t, app.GetValidators())
	require.Zero(t, inner.calls)

	_, err := app.InitChain(&proxy.RequestInitChain{InitialHeight: 1})
	require.NoError(t, err)
	require.Equal(t, []proxy.ValidatorUpdate{{PubKey: key, Power: 7}}, app.GetValidators())
	require.Equal(t, []proxy.ValidatorUpdate{{PubKey: key, Power: 7}}, app.GetValidators())
	require.Equal(t, 1, inner.calls)

	inner.validators = []proxy.ValidatorUpdate{{PubKey: key, Power: 11}}
	app.InitLastHeader(&proxy.Header{Height: 3})
	require.Equal(t, []proxy.ValidatorUpdate{{PubKey: key, Power: 7}}, app.GetValidators())
	require.Equal(t, 1, inner.calls)
}

func TestMockAppEvmBalanceReturns100ETH(t *testing.T) {
	app := NewMockApp(mockAppFixture{})
	want := new(big.Int).Mul(big.NewInt(100), big.NewInt(1_000_000_000_000_000_000))

	got := app.EvmBalance(common.Address{}, nil)
	require.Equal(t, want, got.ToBig())
}

type mockAppFixture struct {
	proxy.EVMApplication
}

func (mockAppFixture) InitChain(*proxy.RequestInitChain) (*proxy.ResponseInitChain, error) {
	return &proxy.ResponseInitChain{}, nil
}
func (mockAppFixture) GetValidators() []proxy.ValidatorUpdate { return nil }
func (mockAppFixture) Info() *proxy.ResponseInfo              { return &proxy.ResponseInfo{} }

type mockAppForwardTarget struct {
	mockAppFixture
	initChainCalled bool
	lastHeader      utils.Option[*proxy.Header]
}

func (app *mockAppForwardTarget) InitChain(*proxy.RequestInitChain) (*proxy.ResponseInitChain, error) {
	app.initChainCalled = true
	return &proxy.ResponseInitChain{AppHash: []byte("inner")}, nil
}

func (app *mockAppForwardTarget) InitLastHeader(lastHeader *proxy.Header) {
	app.lastHeader = utils.Some(lastHeader)
}

type mockAppInfoTarget struct {
	mockAppFixture
}

func (mockAppInfoTarget) Info() *proxy.ResponseInfo {
	return &proxy.ResponseInfo{
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
	validators []proxy.ValidatorUpdate
	calls      int
}

func (app *mockAppValidatorsTarget) GetValidators() []proxy.ValidatorUpdate {
	app.calls++
	return app.validators
}

func buildFastCheckTxBytesForKey(t *testing.T, key *ecdsa.PrivateKey, nonce uint64, gas uint64) ([]byte, *ethtypes.Transaction, common.Address) {
	t.Helper()

	chainID := big.NewInt(713715)
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
	txData, err := ethtx.NewTxDataFromTx(signedTx)
	require.NoError(t, err)
	msg, err := evmtypes.NewMsgEVMTransaction(txData)
	require.NoError(t, err)
	anyMsg, err := codectypes.NewAnyWithValue(msg)
	require.NoError(t, err)
	bodyBytes, err := proto.Marshal(&txtypes.TxBody{Messages: []*codectypes.Any{anyMsg}})
	require.NoError(t, err)
	txBytes, err := proto.Marshal(&txtypes.TxRaw{BodyBytes: bodyBytes})
	require.NoError(t, err)
	addr := ethcrypto.PubkeyToAddress(key.PublicKey)
	return txBytes, signedTx, addr
}

func TestParseFastCheckTxEVMTransaction(t *testing.T) {
	txBytes, ethTx := buildFastCheckTxBytes(t)

	res, err := parseFastCheckTx(txBytes)
	require.NoError(t, err)
	require.NotNil(t, res.ResponseCheckTx)
	require.True(t, res.ResponseCheckTx.IsOK())
	require.Equal(t, int64(123456), res.GasWanted)
	require.True(t, res.IsEVM)
	require.Equal(t, uint64(7), res.EVMNonce)
	require.Equal(t, ethTx.Hash(), res.EVMHash)
	require.NotEqual(t, common.Address{}, res.EVMSenderAddress)
	require.NotEmpty(t, res.SeiSenderAddress)
}

func TestMockAppRejectsNonEVMTransaction(t *testing.T) {
	wrapped := NewMockApp(mockAppFixture{})
	nonEVMTx := buildTxRawBytes(t, []*codectypes.Any{{
		TypeUrl: "/example.MsgNotEVM",
		Value:   []byte{1},
	}})

	res := wrapped.CheckTx(t.Context(), &proxy.RequestCheckTxV2{Tx: nonEVMTx})
	require.NotNil(t, res.ResponseCheckTx)
	require.NotEqual(t, proxy.CodeTypeOK, res.Code)
}

func TestMockAppChecksTransactionsWithoutCallingWrappedApp(t *testing.T) {
	app := &checkTxCountingApp{}
	txBytes, _ := buildFastCheckTxBytes(t)

	wrapped := NewMockApp(app)
	res := wrapped.CheckTx(t.Context(), &proxy.RequestCheckTxV2{Tx: txBytes})
	require.False(t, app.called)
	require.Equal(t, int64(123456), res.GasWanted)
	require.True(t, res.IsEVM)
}

func TestMakeEVMApplicationAutobahnMockAppKeepsMockApp(t *testing.T) {
	validator := makeValidator([]byte("evm-only-validator"), []byte("evm-only-node"), "localhost:26660")
	autobahnConfigFile := writeAutobahnConfig(t, defaultFileConfig(t, []config.AutobahnValidator{validator}))

	app, manager, err := makeEVMApplication(t.Context(), &config.Config{
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

	_, err = mock.InitChain(&proxy.RequestInitChain{InitialHeight: 1, MaxGas: 30_000_000})
	require.NoError(t, err)
	validators := mock.GetValidators()
	require.Len(t, validators, 1)
	require.Equal(t, validator.ValidatorKey, validators[0].PubKey)
	require.Equal(t, int64(1), validators[0].Power)
	var blockHash [32]byte
	blockHash[0] = 1
	require.NoError(t, manager.AppHashBuilder().ReportBlockHash(t.Context(), 1, blockHash))
	_, err = mock.FinalizeBlock(t.Context(), &proxy.RequestFinalizeBlock{
		Header: &proxy.Header{Height: 1},
	})
	require.NoError(t, err)
	_, err = mock.Commit(t.Context())
	require.NoError(t, err)
	got, err := manager.AppHashBuilder().GetAppHash(t.Context(), 1)
	require.NoError(t, err)
	require.Equal(t, uint64(1), got.BlockHeight())
}

func TestMakeEVMApplicationAutobahnRequiresReadableConfig(t *testing.T) {
	_, _, err := makeEVMApplication(t.Context(), &config.Config{
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
	proxy.EVMApplication
	called bool
}

func (app *checkTxCountingApp) CheckTx(context.Context, *proxy.RequestCheckTxV2) *proxy.ResponseCheckTxV2 {
	app.called = true
	return &proxy.ResponseCheckTxV2{
		ResponseCheckTx: &proxy.ResponseCheckTx{
			Code:      proxy.CodeTypeOK,
			GasWanted: 77,
		},
	}
}

func buildFastCheckTxBytes(t *testing.T) ([]byte, *ethtypes.Transaction) {
	t.Helper()

	key, err := ethcrypto.GenerateKey()
	require.NoError(t, err)
	chainID := big.NewInt(713715)
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
	txData, err := ethtx.NewTxDataFromTx(signedTx)
	require.NoError(t, err)
	msg, err := evmtypes.NewMsgEVMTransaction(txData)
	require.NoError(t, err)
	anyMsg, err := codectypes.NewAnyWithValue(msg)
	require.NoError(t, err)

	return buildTxRawBytes(t, []*codectypes.Any{anyMsg}), signedTx
}

func buildTxRawBytes(t *testing.T, messages []*codectypes.Any) []byte {
	t.Helper()

	bodyBytes, err := proto.Marshal(&txtypes.TxBody{Messages: messages})
	require.NoError(t, err)
	txBytes, err := proto.Marshal(&txtypes.TxRaw{BodyBytes: bodyBytes})
	require.NoError(t, err)
	return txBytes
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
