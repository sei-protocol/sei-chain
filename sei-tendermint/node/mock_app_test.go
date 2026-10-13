package node

import (
	"crypto/ecdsa"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	ethcrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/gogo/protobuf/proto"

	"github.com/sei-protocol/sei-chain/giga/apphash"
	"github.com/sei-protocol/sei-chain/giga/apphash/builder"
	codectypes "github.com/sei-protocol/sei-chain/sei-cosmos/codec/types"
	sdkerrors "github.com/sei-protocol/sei-chain/sei-cosmos/types/errors"
	txtypes "github.com/sei-protocol/sei-chain/sei-cosmos/types/tx"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/sei-protocol/sei-chain/x/evm/types/ethtx"
)

func TestMockAppFinalizeBlockBumpsNoncesAndUsesGasWanted(t *testing.T) {
	key, err := ethcrypto.GenerateKey()
	require.NoError(t, err)

	tx1, _, addr := buildFastCheckTxBytesForKey(t, key, 0, 21_000)
	tx2, ethTx2, _ := buildFastCheckTxBytesForKey(t, key, 1, 31_000)
	app := newTestMockApp(t, abci.BaseApplication{}, 3)
	_, err = app.InitChain(&abci.RequestInitChain{InitialHeight: 3})
	require.NoError(t, err)

	res, err := app.FinalizeBlock(t.Context(), &abci.RequestFinalizeBlock{
		Txs:    [][]byte{tx1, tx2},
		Header: &tmproto.Header{Height: 3},
	})
	require.NoError(t, err)
	require.Len(t, res.TxResults, 2)
	require.Equal(t, int64(21_000), res.TxResults[0].GasWanted)
	require.Equal(t, int64(21_000), res.TxResults[0].GasUsed)
	require.Equal(t, int64(31_000), res.TxResults[1].GasWanted)
	require.Equal(t, int64(31_000), res.TxResults[1].GasUsed)
	require.Equal(t, uint64(2), app.EvmNonce(addr))
	require.Empty(t, res.AppHash)

	checkRes := app.CheckTx(t.Context(), &abci.RequestCheckTxV2{Tx: tx2})
	require.True(t, checkRes.IsEVM)
	require.Equal(t, ethTx2.Hash(), checkRes.EVMHash)
}

func TestMockAppFinalizeBlockFailsNonceMismatch(t *testing.T) {
	key, err := ethcrypto.GenerateKey()
	require.NoError(t, err)

	tx0, _, addr := buildFastCheckTxBytesForKey(t, key, 0, 21_000)
	tx2, _, _ := buildFastCheckTxBytesForKey(t, key, 2, 31_000)
	tx1, _, _ := buildFastCheckTxBytesForKey(t, key, 1, 41_000)
	app := newTestMockApp(t, abci.BaseApplication{}, 1)
	_, err = app.InitChain(&abci.RequestInitChain{InitialHeight: 1})
	require.NoError(t, err)

	res, err := app.FinalizeBlock(t.Context(), &abci.RequestFinalizeBlock{
		Txs:    [][]byte{tx0, tx2, tx1},
		Header: &tmproto.Header{Height: 1},
	})
	require.NoError(t, err)
	require.Len(t, res.TxResults, 3)
	require.Equal(t, abci.CodeTypeOK, res.TxResults[0].Code)
	require.Equal(t, sdkerrors.ErrWrongSequence.ABCICode(), res.TxResults[1].Code)
	require.Equal(t, sdkerrors.ErrWrongSequence.Codespace(), res.TxResults[1].Codespace)
	require.Equal(t, sdkerrors.ErrWrongSequence.Error(), res.TxResults[1].Log)
	require.Equal(t, int64(0), res.TxResults[1].GasWanted)
	require.Equal(t, int64(0), res.TxResults[1].GasUsed)
	require.Equal(t, abci.CodeTypeOK, res.TxResults[2].Code)
	require.Equal(t, uint64(2), app.EvmNonce(addr))
	_, err = app.Commit(t.Context())
	require.NoError(t, err)

	duplicate, err := app.FinalizeBlock(t.Context(), &abci.RequestFinalizeBlock{
		Txs:    [][]byte{tx1},
		Header: &tmproto.Header{Height: 2},
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
	app := newTestMockApp(t, abci.BaseApplication{}, 1)
	_, err = app.InitChain(&abci.RequestInitChain{InitialHeight: 1})
	require.NoError(t, err)

	res, err := app.FinalizeBlock(t.Context(), &abci.RequestFinalizeBlock{
		Txs:    [][]byte{tx0, []byte("not a tx"), tx1},
		Header: &tmproto.Header{Height: 1},
	})
	require.NoError(t, err)
	require.Len(t, res.TxResults, 3)
	require.Equal(t, abci.CodeTypeOK, res.TxResults[0].Code)
	require.NotEqual(t, abci.CodeTypeOK, res.TxResults[1].Code)
	require.Equal(t, int64(0), res.TxResults[1].GasWanted)
	require.Equal(t, int64(0), res.TxResults[1].GasUsed)
	require.Equal(t, abci.CodeTypeOK, res.TxResults[2].Code)
	require.Equal(t, uint64(2), app.EvmNonce(addr))
	require.Equal(t, int64(1), app.LastBlockHeight())
}

func TestMockAppFinalizeBlockRequiresSequentialHeader(t *testing.T) {
	app := newTestMockApp(t, abci.BaseApplication{}, 4)
	_, err := app.InitChain(&abci.RequestInitChain{InitialHeight: 4})
	require.NoError(t, err)

	_, err = app.FinalizeBlock(t.Context(), &abci.RequestFinalizeBlock{})
	require.Error(t, err)

	_, err = app.FinalizeBlock(t.Context(), &abci.RequestFinalizeBlock{
		Header: &tmproto.Header{Height: 5},
	})
	require.Error(t, err)

	res, err := app.FinalizeBlock(t.Context(), &abci.RequestFinalizeBlock{
		Header: &tmproto.Header{Height: 4},
	})
	require.NoError(t, err)
	require.Empty(t, res.TxResults)
	require.Equal(t, int64(4), app.LastBlockHeight())
}

func TestMockAppChecksTransitions(t *testing.T) {
	app := newTestMockApp(t, abci.BaseApplication{}, 1)

	_, err := app.Commit(t.Context())
	require.Error(t, err)
	_, err = app.FinalizeBlock(t.Context(), &abci.RequestFinalizeBlock{
		Header: &tmproto.Header{Height: 1},
	})
	require.Error(t, err)

	_, err = app.InitChain(&abci.RequestInitChain{InitialHeight: 1})
	require.NoError(t, err)
	_, err = app.InitChain(&abci.RequestInitChain{InitialHeight: 1})
	require.Error(t, err)
	_, err = app.Commit(t.Context())
	require.Error(t, err)

	_, err = app.FinalizeBlock(t.Context(), &abci.RequestFinalizeBlock{
		Header: &tmproto.Header{Height: 1},
	})
	require.NoError(t, err)
	_, err = app.FinalizeBlock(t.Context(), &abci.RequestFinalizeBlock{
		Header: &tmproto.Header{Height: 2},
	})
	require.Error(t, err)

	_, err = app.Commit(t.Context())
	require.NoError(t, err)
	_, err = app.FinalizeBlock(t.Context(), &abci.RequestFinalizeBlock{
		Header: &tmproto.Header{Height: 2},
	})
	require.NoError(t, err)
}

func TestMockAppCommitSetsRetainHeight(t *testing.T) {
	app := newTestMockApp(t, abci.BaseApplication{}, 10_005)
	_, err := app.InitChain(&abci.RequestInitChain{InitialHeight: 10_005})
	require.NoError(t, err)

	res, err := app.FinalizeBlock(t.Context(), &abci.RequestFinalizeBlock{
		Header: &tmproto.Header{Height: 10_005},
	})
	require.NoError(t, err)
	require.Empty(t, res.TxResults)
	commit, err := app.Commit(t.Context())
	require.NoError(t, err)
	require.Equal(t, int64(5), commit.RetainHeight)

	_, err = app.FinalizeBlock(t.Context(), &abci.RequestFinalizeBlock{
		Header: &tmproto.Header{Height: 10_006},
	})
	require.NoError(t, err)
	commit, err = app.Commit(t.Context())
	require.NoError(t, err)
	require.Equal(t, int64(6), commit.RetainHeight)
}

func TestMockAppForwardsInitChainAndIgnoresInitLastHeader(t *testing.T) {
	inner := &mockAppForwardTarget{}
	app := newTestMockApp(t, inner, 7)
	header := &tmproto.Header{Height: 11}

	_, err := app.InitChain(&abci.RequestInitChain{})
	require.Error(t, err)

	res, err := app.InitChain(&abci.RequestInitChain{InitialHeight: 7})
	require.NoError(t, err)
	require.Equal(t, int64(6), app.LastBlockHeight())
	require.Empty(t, res.AppHash)
	app.InitLastHeader(header)

	require.True(t, inner.initChainCalled)
	require.Nil(t, inner.lastHeader)
	require.Equal(t, int64(6), app.LastBlockHeight())
}

func TestMockAppReportsBlocksToTheAppHashBuilder(t *testing.T) {
	app, appHashBuilder := newTestMockAppWithBuilder(t, abci.BaseApplication{}, 1)
	_, err := app.InitChain(&abci.RequestInitChain{InitialHeight: 1})
	require.NoError(t, err)
	published := make(chan *apphash.AppHashData, 2)
	listener := func(appHash *apphash.AppHashData) { published <- appHash }
	_, err = appHashBuilder.RegisterListener(t.Context(), listener)
	require.NoError(t, err)

	blockHashes := [][32]byte{{1}, {2}}
	for i, blockHash := range blockHashes {
		res, err := app.FinalizeBlock(t.Context(), &abci.RequestFinalizeBlock{
			Header: &tmproto.Header{Height: int64(i) + 1},
			Hash:   blockHash[:],
		})
		require.NoError(t, err)
		require.Empty(t, res.AppHash)
		_, err = app.Commit(t.Context())
		require.NoError(t, err)
	}
	for i, blockHash := range blockHashes {
		appHash := <-published
		require.Equal(t, uint64(i)+1, appHash.BlockHeight()) //nolint:gosec // small test heights
		require.Equal(t, blockHash, appHash.BlockHash())
	}
	require.Empty(t, app.Info().LastBlockAppHash)

	app.InitLastHeader(&tmproto.Header{Height: 5, AppHash: []byte("restored")})
	require.Equal(t, int64(2), app.LastBlockHeight())
}

func TestMockAppInfoPreservesWrappedAppMetadata(t *testing.T) {
	app := newTestMockApp(t, mockAppInfoTarget{}, 9)
	_, err := app.InitChain(&abci.RequestInitChain{InitialHeight: 9})
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
	inner := &mockAppValidatorsTarget{validators: []abci.ValidatorUpdate{{Power: 7}}}
	app := newTestMockApp(t, inner, 1)

	require.Empty(t, app.GetValidators())
	require.Zero(t, inner.calls)

	_, err := app.InitChain(&abci.RequestInitChain{InitialHeight: 1})
	require.NoError(t, err)
	require.Equal(t, []abci.ValidatorUpdate{{Power: 7}}, app.GetValidators())
	require.Equal(t, []abci.ValidatorUpdate{{Power: 7}}, app.GetValidators())
	require.Equal(t, 1, inner.calls)

	inner.validators = []abci.ValidatorUpdate{{Power: 11}}
	app.InitLastHeader(&tmproto.Header{Height: 3})
	require.Equal(t, []abci.ValidatorUpdate{{Power: 7}}, app.GetValidators())
	require.Equal(t, 1, inner.calls)
}

func TestMockAppEvmBalanceReturns100ETH(t *testing.T) {
	app := newTestMockApp(t, abci.BaseApplication{}, 1)
	want := new(big.Int).Mul(big.NewInt(100), big.NewInt(1_000_000_000_000_000_000))

	got := app.EvmBalance(common.Address{}, nil)
	require.Equal(t, want, got.ToBig())
}

type mockAppForwardTarget struct {
	abci.BaseApplication
	initChainCalled bool
	lastHeader      *tmproto.Header
}

func (app *mockAppForwardTarget) InitChain(*abci.RequestInitChain) (*abci.ResponseInitChain, error) {
	app.initChainCalled = true
	return &abci.ResponseInitChain{AppHash: []byte("inner")}, nil
}

func (app *mockAppForwardTarget) InitLastHeader(lastHeader *tmproto.Header) {
	app.lastHeader = lastHeader
}

type mockAppInfoTarget struct {
	abci.BaseApplication
}

func (mockAppInfoTarget) Info() *abci.ResponseInfo {
	return &abci.ResponseInfo{
		Data:             "wrapped-data",
		Version:          "wrapped-version",
		AppVersion:       17,
		LastBlockHeight:  123,
		LastBlockAppHash: []byte("wrapped-hash"),
		MinimumGasPrices: "1usei",
	}
}

type mockAppValidatorsTarget struct {
	abci.BaseApplication
	validators []abci.ValidatorUpdate
	calls      int
}

func (app *mockAppValidatorsTarget) GetValidators() []abci.ValidatorUpdate {
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

// newTestMockApp returns a mock app over inner whose app hash builder starts at gigaGenesisHeight.
func newTestMockApp(t *testing.T, inner abci.Application, gigaGenesisHeight uint64) *MockApp {
	t.Helper()
	app, _ := newTestMockAppWithBuilder(t, inner, gigaGenesisHeight)
	return app
}

// newTestMockAppWithBuilder returns a mock app over inner and the app hash builder it reports to, which starts at
// gigaGenesisHeight and is closed when the test ends.
func newTestMockAppWithBuilder(
	t *testing.T,
	inner abci.Application,
	gigaGenesisHeight uint64,
) (*MockApp, builder.AppHashBuilder) {
	t.Helper()
	config := builder.DefaultAppHashBuilderConfig()
	config.GigaGenesisHeight = gigaGenesisHeight
	config.HashVaultConfig.Path = t.TempDir()
	appHashBuilder, err := builder.NewStandardAppHashBuilder(config)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, appHashBuilder.Close()) })
	require.NoError(t, appHashBuilder.SetupComplete(t.Context(), 0))
	return NewMockApp(inner, appHashBuilder), appHashBuilder
}
