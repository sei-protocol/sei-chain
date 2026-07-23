package node

import (
	"context"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	ethcrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/gogo/protobuf/proto"
	"github.com/stretchr/testify/require"

	codectypes "github.com/sei-protocol/sei-chain/sei-cosmos/codec/types"
	txtypes "github.com/sei-protocol/sei-chain/sei-cosmos/types/tx"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/sei-protocol/sei-chain/x/evm/types/ethtx"
)

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

func TestFastCheckTxApplicationRejectsNonEVMTransaction(t *testing.T) {
	wrapped := fastCheckTxApplication{Application: abci.BaseApplication{}}
	nonEVMTx := buildTxRawBytes(t, []*codectypes.Any{{
		TypeUrl: "/example.MsgNotEVM",
		Value:   []byte{1},
	}})

	res := wrapped.CheckTx(context.Background(), &abci.RequestCheckTxV2{Tx: nonEVMTx})
	require.NotNil(t, res.ResponseCheckTx)
	require.NotEqual(t, abci.CodeTypeOK, res.Code)
}

func TestFastCheckTxApplicationOverridesCheckTx(t *testing.T) {
	app := &checkTxCountingApp{}
	txBytes, _ := buildFastCheckTxBytes(t)

	wrapped := fastCheckTxApplication{Application: app}
	res := wrapped.CheckTx(context.Background(), &abci.RequestCheckTxV2{Tx: txBytes})
	require.False(t, app.called)
	require.Equal(t, int64(123456), res.GasWanted)
	require.True(t, res.IsEVM)
}

type checkTxCountingApp struct {
	abci.BaseApplication
	called bool
}

func (app *checkTxCountingApp) CheckTx(context.Context, *abci.RequestCheckTxV2) *abci.ResponseCheckTxV2 {
	app.called = true
	return &abci.ResponseCheckTxV2{
		ResponseCheckTx: &abci.ResponseCheckTx{
			Code:      abci.CodeTypeOK,
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
