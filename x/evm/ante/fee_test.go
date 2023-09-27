package ante

import (
	"encoding/hex"
	"math/big"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/sei-protocol/sei-chain/x/evm/types/ethtx"
	"github.com/stretchr/testify/require"
)

func TestEVMFeeCheckDecoratorCancun(t *testing.T) {
	k, p, ctx := keeper.MockEVMKeeper()
	ctx = ctx.WithIsCheckTx(true)
	handler := NewEVMFeeCheckDecorator(k, p)
	privKey := keeper.MockPrivateKey()
	testPrivHex := hex.EncodeToString(privKey.Bytes())
	key, _ := crypto.HexToECDSA(testPrivHex)
	to := new(common.Address)
	copy(to[:], []byte("0x1234567890abcdef1234567890abcdef12345678"))
	chainID := k.ChainID()
	txData := ethtypes.DynamicFeeTx{
		Nonce:     1,
		GasFeeCap: big.NewInt(10),
		Gas:       1000,
		To:        to,
		Value:     big.NewInt(1000),
		Data:      []byte("abc"),
		ChainID:   chainID,
	}
	evmParams := k.GetParams(ctx)
	evmParams.ChainConfig.CancunTime = 0 // overwrite to enable cancun
	k.SetParams(ctx, evmParams)
	chainCfg := evmParams.GetChainConfig()
	ethCfg := chainCfg.EthereumConfig(chainID)
	blockNum := big.NewInt(ctx.BlockHeight())
	signer := ethtypes.MakeSigner(ethCfg, blockNum, uint64(ctx.BlockTime().Unix()))
	tx, err := ethtypes.SignTx(ethtypes.NewTx(&txData), signer, key)
	require.Nil(t, err)
	typedTx, err := ethtx.NewDynamicFeeTx(tx)
	require.Nil(t, err)
	msg, err := types.NewMsgEVMTransaction(typedTx)
	require.Nil(t, err)

	preprocessor := NewEVMPreprocessDecorator(k, k.AccountKeeper())
	ctx, err = preprocessor.AnteHandle(ctx, mockTx{msgs: []sdk.Msg{msg}}, false, func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		return ctx, nil
	})
	require.Nil(t, err)

	// should return error because gas fee cap is too low
	_, err = handler.AnteHandle(ctx, mockTx{msgs: []sdk.Msg{msg}}, false, func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		return ctx, nil
	})
	require.NotNil(t, err)

	txData.GasFeeCap = new(big.Int).Mul(p.GetFeesParams(ctx).GlobalMinimumGasPrices[0].Amount.BigInt(), k.GetGasMultiplier(ctx).BigInt())
	tx, err = ethtypes.SignTx(ethtypes.NewTx(&txData), signer, key)
	require.Nil(t, err)
	typedTx, err = ethtx.NewDynamicFeeTx(tx)
	require.Nil(t, err)
	msg, err = types.NewMsgEVMTransaction(typedTx)
	require.Nil(t, err)

	// should return error because the sender does not have enough funds
	ctx, err = preprocessor.AnteHandle(ctx, mockTx{msgs: []sdk.Msg{msg}}, false, func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		return ctx, nil
	})
	require.Nil(t, err)
	_, err = handler.AnteHandle(ctx, mockTx{msgs: []sdk.Msg{msg}}, false, func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		return ctx, nil
	})
	require.NotNil(t, err)

	amt := new(big.Int).Mul(typedTx.Fee(), k.GetGasMultiplier(ctx).BigInt())
	coinsAmt := sdk.NewCoins(sdk.NewCoin(k.GetBaseDenom(ctx), sdk.NewIntFromBigInt(amt)))
	k.BankKeeper().MintCoins(ctx, types.ModuleName, coinsAmt)
	seiAddr, ok := types.GetContextSeiAddress(ctx)
	require.True(t, ok)
	k.BankKeeper().SendCoinsFromModuleToAccount(ctx, types.ModuleName, seiAddr, coinsAmt)

	// should succeed now that the sender has enough funds
	ctx, err = preprocessor.AnteHandle(ctx, mockTx{msgs: []sdk.Msg{msg}}, false, func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		return ctx, nil
	})
	require.Nil(t, err)
	_, err = handler.AnteHandle(ctx, mockTx{msgs: []sdk.Msg{msg}}, false, func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		return ctx, nil
	})
	require.Nil(t, err)

	// should fail because blob gas fee cap is too low
	blobTxData := ethtypes.BlobTx{
		Nonce:      1,
		GasFeeCap:  uint256.MustFromBig(txData.GasFeeCap),
		Gas:        1000,
		To:         *to,
		Value:      uint256.NewInt(1000),
		Data:       []byte("abc"),
		BlobHashes: []common.Hash{{}},
		ChainID:    uint256.MustFromBig(chainID),
	}
	tx, err = ethtypes.SignTx(ethtypes.NewTx(&blobTxData), signer, key)
	require.Nil(t, err)
	typedBlobTx, err := ethtx.NewBlobTx(tx)
	require.Nil(t, err)
	msg, err = types.NewMsgEVMTransaction(typedBlobTx)
	require.Nil(t, err)
	ctx, err = preprocessor.AnteHandle(ctx, mockTx{msgs: []sdk.Msg{msg}}, false, func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		return ctx, nil
	})
	require.Nil(t, err)
	_, err = handler.AnteHandle(ctx, mockTx{msgs: []sdk.Msg{msg}}, false, func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		return ctx, nil
	})
	require.NotNil(t, err)

	// should fail because insufficient balance due to additional blob cost
	blobTxData.BlobFeeCap = uint256.NewInt(1)
	tx, err = ethtypes.SignTx(ethtypes.NewTx(&blobTxData), signer, key)
	require.Nil(t, err)
	typedBlobTx, err = ethtx.NewBlobTx(tx)
	require.Nil(t, err)
	msg, err = types.NewMsgEVMTransaction(typedBlobTx)
	require.Nil(t, err)
	ctx, err = preprocessor.AnteHandle(ctx, mockTx{msgs: []sdk.Msg{msg}}, false, func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		return ctx, nil
	})
	require.Nil(t, err)
	_, err = handler.AnteHandle(ctx, mockTx{msgs: []sdk.Msg{msg}}, false, func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		return ctx, nil
	})
	require.NotNil(t, err)

	// should succeed
	amt = new(big.Int).Mul(new(big.Int).Mul(typedBlobTx.GetBlobFeeCap(), new(big.Int).SetUint64(typedBlobTx.BlobGas())), k.GetGasMultiplier(ctx).BigInt())
	coinsAmt = sdk.NewCoins(sdk.NewCoin(k.GetBaseDenom(ctx), sdk.NewIntFromBigInt(amt)))
	k.BankKeeper().MintCoins(ctx, types.ModuleName, coinsAmt)
	require.True(t, ok)
	k.BankKeeper().SendCoinsFromModuleToAccount(ctx, types.ModuleName, seiAddr, coinsAmt)

	ctx, err = preprocessor.AnteHandle(ctx, mockTx{msgs: []sdk.Msg{msg}}, false, func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		return ctx, nil
	})
	require.Nil(t, err)
	_, err = handler.AnteHandle(ctx, mockTx{msgs: []sdk.Msg{msg}}, false, func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		return ctx, nil
	})
	require.Nil(t, err)
}

func TestEVMFeeCheckDecoratorLondon(t *testing.T) {
	k, p, ctx := keeper.MockEVMKeeper()
	ctx = ctx.WithIsCheckTx(true)
	handler := NewEVMFeeCheckDecorator(k, p)
	privKey := keeper.MockPrivateKey()
	testPrivHex := hex.EncodeToString(privKey.Bytes())
	key, _ := crypto.HexToECDSA(testPrivHex)
	to := new(common.Address)
	copy(to[:], []byte("0x1234567890abcdef1234567890abcdef12345678"))
	chainID := k.ChainID()
	txData := ethtypes.DynamicFeeTx{
		Nonce:     1,
		GasFeeCap: big.NewInt(10),
		Gas:       1000,
		To:        to,
		Value:     big.NewInt(1000),
		Data:      []byte("abc"),
		ChainID:   chainID,
	}
	evmParams := k.GetParams(ctx)
	chainCfg := evmParams.GetChainConfig()
	ethCfg := chainCfg.EthereumConfig(chainID)
	blockNum := big.NewInt(ctx.BlockHeight())
	signer := ethtypes.MakeSigner(ethCfg, blockNum, uint64(ctx.BlockTime().Unix()))
	tx, err := ethtypes.SignTx(ethtypes.NewTx(&txData), signer, key)
	require.Nil(t, err)
	typedTx, err := ethtx.NewDynamicFeeTx(tx)
	require.Nil(t, err)
	msg, err := types.NewMsgEVMTransaction(typedTx)
	require.Nil(t, err)

	preprocessor := NewEVMPreprocessDecorator(k, k.AccountKeeper())
	ctx, err = preprocessor.AnteHandle(ctx, mockTx{msgs: []sdk.Msg{msg}}, false, func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		return ctx, nil
	})
	require.Nil(t, err)

	// should return error because gas fee cap is too low
	_, err = handler.AnteHandle(ctx, mockTx{msgs: []sdk.Msg{msg}}, false, func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		return ctx, nil
	})
	require.NotNil(t, err)

	txData.GasFeeCap = new(big.Int).Mul(p.GetFeesParams(ctx).GlobalMinimumGasPrices[0].Amount.BigInt(), k.GetGasMultiplier(ctx).BigInt())
	tx, err = ethtypes.SignTx(ethtypes.NewTx(&txData), signer, key)
	require.Nil(t, err)
	typedTx, err = ethtx.NewDynamicFeeTx(tx)
	require.Nil(t, err)
	msg, err = types.NewMsgEVMTransaction(typedTx)
	require.Nil(t, err)

	// should return error because the sender does not have enough funds
	ctx, err = preprocessor.AnteHandle(ctx, mockTx{msgs: []sdk.Msg{msg}}, false, func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		return ctx, nil
	})
	require.Nil(t, err)
	_, err = handler.AnteHandle(ctx, mockTx{msgs: []sdk.Msg{msg}}, false, func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		return ctx, nil
	})
	require.NotNil(t, err)

	amt := new(big.Int).Mul(typedTx.Fee(), k.GetGasMultiplier(ctx).BigInt())
	coinsAmt := sdk.NewCoins(sdk.NewCoin(k.GetBaseDenom(ctx), sdk.NewIntFromBigInt(amt)))
	k.BankKeeper().MintCoins(ctx, types.ModuleName, coinsAmt)
	seiAddr, ok := types.GetContextSeiAddress(ctx)
	require.True(t, ok)
	k.BankKeeper().SendCoinsFromModuleToAccount(ctx, types.ModuleName, seiAddr, coinsAmt)

	// should succeed now that the sender has enough funds
	ctx, err = preprocessor.AnteHandle(ctx, mockTx{msgs: []sdk.Msg{msg}}, false, func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		return ctx, nil
	})
	require.Nil(t, err)
	_, err = handler.AnteHandle(ctx, mockTx{msgs: []sdk.Msg{msg}}, false, func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		return ctx, nil
	})
	require.Nil(t, err)

	// should fail because blob tx is not support in london
	blobTxData := ethtypes.BlobTx{
		Nonce:      1,
		GasFeeCap:  uint256.MustFromBig(txData.GasFeeCap),
		Gas:        1000,
		To:         *to,
		Value:      uint256.NewInt(1000),
		Data:       []byte("abc"),
		BlobHashes: []common.Hash{{}},
		ChainID:    uint256.MustFromBig(chainID),
	}
	tx, err = ethtypes.SignTx(ethtypes.NewTx(&blobTxData), signer, key)
	require.NotNil(t, err)
}
