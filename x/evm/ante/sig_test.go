package ante_test

import (
	"encoding/hex"
	"math/big"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/ante"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/sei-protocol/sei-chain/x/evm/types/ethtx"
	"github.com/stretchr/testify/require"
	abci "github.com/tendermint/tendermint/abci/types"
)

func TestEVMSigVerifyDecorator(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper()
	handler := ante.NewEVMSigVerifyDecorator(k, func() sdk.Context { return ctx })
	privKey := testkeeper.MockPrivateKey()
	testPrivHex := hex.EncodeToString(privKey.Bytes())
	key, _ := crypto.HexToECDSA(testPrivHex)
	to := new(common.Address)
	copy(to[:], []byte("0x1234567890abcdef1234567890abcdef12345678"))
	txData := ethtypes.LegacyTx{
		Nonce:    1,
		GasPrice: big.NewInt(10),
		Gas:      1000,
		To:       to,
		Value:    big.NewInt(1000),
		Data:     []byte("abc"),
	}
	chainID := k.ChainID()
	chainCfg := types.DefaultChainConfig()
	ethCfg := chainCfg.EthereumConfig(chainID)
	blockNum := big.NewInt(ctx.BlockHeight())
	signer := ethtypes.MakeSigner(ethCfg, blockNum, uint64(ctx.BlockTime().Unix()))
	tx, err := ethtypes.SignTx(ethtypes.NewTx(&txData), signer, key)
	require.Nil(t, err)
	sender, err := signer.Sender(tx)
	require.Nil(t, err)
	typedTx, err := ethtx.NewLegacyTx(tx)
	require.Nil(t, err)
	msg, err := types.NewMsgEVMTransaction(typedTx)
	require.Nil(t, err)

	preprocessor := ante.NewEVMPreprocessDecorator(k, k.AccountKeeper())
	ctx, err = preprocessor.AnteHandle(ctx, mockTx{msgs: []sdk.Msg{msg}}, false, func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		return ctx, nil
	})
	require.Nil(t, err)

	// should return error because nonce is incorrect
	resCtx, err := handler.AnteHandle(ctx, mockTx{msgs: []sdk.Msg{msg}}, false, func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		return ctx, nil
	})
	require.NotNil(t, err)

	require.Equal(t, uint64(1), resCtx.EVMNonce())
	require.Equal(t, sender.Hex(), resCtx.EVMSenderAddress())
	require.Equal(t, tx.Hash().Hex(), resCtx.EVMTxHash())
	require.Equal(t, true, resCtx.IsEVM())

	// should return error if acc is not found (i.e. preprocess not called)
	txData.Nonce = 0
	tx, err = ethtypes.SignTx(ethtypes.NewTx(&txData), signer, key)
	require.Nil(t, err)
	typedTx, err = ethtx.NewLegacyTx(tx)
	require.Nil(t, err)
	msg, err = types.NewMsgEVMTransaction(typedTx)
	require.Nil(t, err)

	require.Panics(t, func() {
		handler.AnteHandle(ctx, mockTx{msgs: []sdk.Msg{msg}}, false, func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
			return ctx, nil
		})
	})

	// should succeed
	txData.Nonce = 0
	tx, err = ethtypes.SignTx(ethtypes.NewTx(&txData), signer, key)
	require.Nil(t, err)
	typedTx, err = ethtx.NewLegacyTx(tx)
	require.Nil(t, err)
	msg, err = types.NewMsgEVMTransaction(typedTx)
	require.Nil(t, err)

	ctx, err = preprocessor.AnteHandle(ctx, mockTx{msgs: []sdk.Msg{msg}}, false, func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		return ctx, nil
	})
	require.Nil(t, err)
	_, err = handler.AnteHandle(ctx, mockTx{msgs: []sdk.Msg{msg}}, false, func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		return ctx, nil
	})
	require.Nil(t, err)
}

func TestSigVerifyPendingTransaction(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper()
	ctx = ctx.WithIsCheckTx(true)
	handler := ante.NewEVMSigVerifyDecorator(k, func() sdk.Context { return ctx })
	privKey := testkeeper.MockPrivateKey()
	testPrivHex := hex.EncodeToString(privKey.Bytes())
	key, _ := crypto.HexToECDSA(testPrivHex)
	_, evmAddr := testkeeper.PrivateKeyToAddresses(privKey)
	to := new(common.Address)
	copy(to[:], []byte("0x1234567890abcdef1234567890abcdef12345678"))
	txData := ethtypes.LegacyTx{
		Nonce:    1,
		GasPrice: big.NewInt(10),
		Gas:      1000,
		To:       to,
		Value:    big.NewInt(1000),
		Data:     []byte("abc"),
	}
	chainID := k.ChainID()
	chainCfg := types.DefaultChainConfig()
	ethCfg := chainCfg.EthereumConfig(chainID)
	blockNum := big.NewInt(ctx.BlockHeight())
	signer := ethtypes.MakeSigner(ethCfg, blockNum, uint64(ctx.BlockTime().Unix()))
	tx, err := ethtypes.SignTx(ethtypes.NewTx(&txData), signer, key)
	require.Nil(t, err)
	typedTx, err := ethtx.NewLegacyTx(tx)
	require.Nil(t, err)
	msg, err := types.NewMsgEVMTransaction(typedTx)
	require.Nil(t, err)

	preprocessor := ante.NewEVMPreprocessDecorator(k, k.AccountKeeper())
	ctx, err = preprocessor.AnteHandle(ctx, mockTx{msgs: []sdk.Msg{msg}}, false, func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		return ctx, nil
	})
	require.Nil(t, err)

	// should not return error but include pending tx checker
	newCtx, err := handler.AnteHandle(ctx, mockTx{msgs: []sdk.Msg{msg}}, false, func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		return ctx, nil
	})
	require.Nil(t, err)
	require.NotNil(t, newCtx.PendingTxChecker())
	newCtx.CheckTxCallback()(newCtx, err)

	// test checker
	require.Equal(t, abci.Pending, newCtx.PendingTxChecker()())
	k.SetNonce(ctx, evmAddr, 1)
	require.Equal(t, abci.Accepted, newCtx.PendingTxChecker()())
	k.SetNonce(ctx, evmAddr, 2)
	require.Equal(t, abci.Rejected, newCtx.PendingTxChecker()())

	// should return error because current nonce is larger than tx nonce
	_, err = handler.AnteHandle(ctx, mockTx{msgs: []sdk.Msg{msg}}, false, func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		return ctx, nil
	})
	require.NotNil(t, err)
}
