package keeper_test

import (
	"bytes"
	"encoding/hex"
	"math/big"
	"os"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/sei-protocol/sei-chain/example/contracts/sendall"
	"github.com/sei-protocol/sei-chain/example/contracts/simplestorage"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/ante"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/sei-protocol/sei-chain/x/evm/types/ethtx"
	"github.com/stretchr/testify/require"
)

func TestEVMTransaction(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper()
	code, err := os.ReadFile("../../../example/contracts/simplestorage/SimpleStorage.bin")
	require.Nil(t, err)
	bz, err := hex.DecodeString(string(code))
	require.Nil(t, err)
	privKey := testkeeper.MockPrivateKey()
	testPrivHex := hex.EncodeToString(privKey.Bytes())
	key, _ := crypto.HexToECDSA(testPrivHex)
	txData := ethtypes.LegacyTx{
		GasPrice: big.NewInt(1000000000000),
		Gas:      200000,
		To:       nil,
		Value:    big.NewInt(0),
		Data:     bz,
		Nonce:    0,
	}
	chainID := k.ChainID(ctx)
	evmParams := k.GetParams(ctx)
	chainCfg := evmParams.GetChainConfig()
	ethCfg := chainCfg.EthereumConfig(chainID)
	blockNum := big.NewInt(ctx.BlockHeight())
	signer := ethtypes.MakeSigner(ethCfg, blockNum, uint64(ctx.BlockTime().Unix()))
	tx, err := ethtypes.SignTx(ethtypes.NewTx(&txData), signer, key)
	require.Nil(t, err)
	txwrapper, err := ethtx.NewLegacyTx(tx)
	require.Nil(t, err)
	req, err := types.NewMsgEVMTransaction(txwrapper)
	require.Nil(t, err)

	_, evmAddr := testkeeper.PrivateKeyToAddresses(privKey)
	amt := sdk.NewCoins(sdk.NewCoin(k.GetBaseDenom(ctx), sdk.NewInt(1000000)))
	k.BankKeeper().MintCoins(ctx, types.ModuleName, sdk.NewCoins(sdk.NewCoin(k.GetBaseDenom(ctx), sdk.NewInt(1000000))))
	k.BankKeeper().SendCoinsFromModuleToAccount(ctx, types.ModuleName, evmAddr[:], amt)

	msgServer := keeper.NewMsgServerImpl(k)

	// Deploy Simple Storage contract
	ante.Preprocess(ctx, req, k.GetParams(ctx))
	res, err := msgServer.EVMTransaction(sdk.WrapSDKContext(ctx), req)
	require.Nil(t, err)
	require.LessOrEqual(t, res.GasUsed, uint64(200000))
	require.Empty(t, res.VmError)
	require.NotEmpty(t, res.ReturnData)
	require.NotEmpty(t, res.Hash)
	require.Equal(t, uint64(1000000)-res.GasUsed, k.BankKeeper().GetBalance(ctx, sdk.AccAddress(evmAddr[:]), "usei").Amount.Uint64())
	require.Equal(t, res.GasUsed, k.BankKeeper().GetBalance(ctx, state.GetCoinbaseAddress(ctx), k.GetBaseDenom(ctx)).Amount.Uint64())
	receipt, err := k.GetReceipt(ctx, common.HexToHash(res.Hash))
	require.Nil(t, err)
	require.NotNil(t, receipt)
	require.Equal(t, uint32(ethtypes.ReceiptStatusSuccessful), receipt.Status)

	// send transaction to the contract
	contractAddr := common.HexToAddress(receipt.ContractAddress)
	abi, err := simplestorage.SimplestorageMetaData.GetAbi()
	require.Nil(t, err)
	bz, err = abi.Pack("set", big.NewInt(20))
	require.Nil(t, err)
	txData = ethtypes.LegacyTx{
		GasPrice: big.NewInt(1000000000000),
		Gas:      200000,
		To:       &contractAddr,
		Value:    big.NewInt(0),
		Data:     bz,
		Nonce:    1,
	}
	tx, err = ethtypes.SignTx(ethtypes.NewTx(&txData), signer, key)
	require.Nil(t, err)
	txwrapper, err = ethtx.NewLegacyTx(tx)
	require.Nil(t, err)
	req, err = types.NewMsgEVMTransaction(txwrapper)
	require.Nil(t, err)
	ante.Preprocess(ctx, req, k.GetParams(ctx))
	res, err = msgServer.EVMTransaction(sdk.WrapSDKContext(ctx), req)
	require.Nil(t, err)
	require.LessOrEqual(t, res.GasUsed, uint64(200000))
	require.Empty(t, res.VmError)
	receipt, err = k.GetReceipt(ctx, common.HexToHash(res.Hash))
	require.Nil(t, err)
	require.NotNil(t, receipt)
	require.Equal(t, uint32(ethtypes.ReceiptStatusSuccessful), receipt.Status)
	found := false
	for _, e := range ctx.EventManager().Events() {
		if e.Type == types.EventTypeEVMLog {
			found = true
		}
	}
	require.True(t, found)
	stateDB := state.NewDBImpl(ctx, k, false)
	val := hex.EncodeToString(bytes.Trim(stateDB.GetState(contractAddr, common.Hash{}).Bytes(), "\x00")) // key is 0x0 since the contract only has one variable
	require.Equal(t, "14", val)                                                                          // value is 0x14 = 20
}

func TestEVMTransactionError(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper()
	privKey := testkeeper.MockPrivateKey()
	testPrivHex := hex.EncodeToString(privKey.Bytes())
	key, _ := crypto.HexToECDSA(testPrivHex)
	txData := ethtypes.LegacyTx{
		GasPrice: big.NewInt(1000000000000),
		Gas:      200000,
		To:       nil,
		Value:    big.NewInt(0),
		Data:     []byte("123090321920390920123"), // gibberish data
		Nonce:    0,
	}
	chainID := k.ChainID(ctx)
	evmParams := k.GetParams(ctx)
	chainCfg := evmParams.GetChainConfig()
	ethCfg := chainCfg.EthereumConfig(chainID)
	blockNum := big.NewInt(ctx.BlockHeight())
	signer := ethtypes.MakeSigner(ethCfg, blockNum, uint64(ctx.BlockTime().Unix()))
	tx, err := ethtypes.SignTx(ethtypes.NewTx(&txData), signer, key)
	require.Nil(t, err)
	txwrapper, err := ethtx.NewLegacyTx(tx)
	require.Nil(t, err)
	req, err := types.NewMsgEVMTransaction(txwrapper)
	require.Nil(t, err)

	_, evmAddr := testkeeper.PrivateKeyToAddresses(privKey)
	amt := sdk.NewCoins(sdk.NewCoin(k.GetBaseDenom(ctx), sdk.NewInt(1000000)))
	k.BankKeeper().MintCoins(ctx, types.ModuleName, sdk.NewCoins(sdk.NewCoin(k.GetBaseDenom(ctx), sdk.NewInt(1000000))))
	k.BankKeeper().SendCoinsFromModuleToAccount(ctx, types.ModuleName, evmAddr[:], amt)

	msgServer := keeper.NewMsgServerImpl(k)

	ante.Preprocess(ctx, req, k.GetParams(ctx))
	res, err := msgServer.EVMTransaction(sdk.WrapSDKContext(ctx), req)
	require.Nil(t, err) // there should only be VM error, no msg-level error
	require.NotEmpty(t, res.VmError)
	// gas should be charged and receipt should be created
	require.Equal(t, uint64(800000), k.BankKeeper().GetBalance(ctx, sdk.AccAddress(evmAddr[:]), "usei").Amount.Uint64())
	receipt, err := k.GetReceipt(ctx, common.HexToHash(res.Hash))
	require.Nil(t, err)
	require.Equal(t, uint32(ethtypes.ReceiptStatusFailed), receipt.Status)
	// yet there should be no contract state
	stateDB := state.NewDBImpl(ctx, k, false)
	require.Empty(t, stateDB.GetState(common.HexToAddress(receipt.ContractAddress), common.Hash{}))
}

func TestEVMTransactionInsufficientGas(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper()
	code, err := os.ReadFile("../../../example/contracts/simplestorage/SimpleStorage.bin")
	require.Nil(t, err)
	bz, err := hex.DecodeString(string(code))
	require.Nil(t, err)
	privKey := testkeeper.MockPrivateKey()
	testPrivHex := hex.EncodeToString(privKey.Bytes())
	key, _ := crypto.HexToECDSA(testPrivHex)
	txData := ethtypes.LegacyTx{
		GasPrice: big.NewInt(1000000000000),
		Gas:      1000,
		To:       nil,
		Value:    big.NewInt(0),
		Data:     bz,
		Nonce:    0,
	}
	chainID := k.ChainID(ctx)
	evmParams := k.GetParams(ctx)
	chainCfg := evmParams.GetChainConfig()
	ethCfg := chainCfg.EthereumConfig(chainID)
	blockNum := big.NewInt(ctx.BlockHeight())
	signer := ethtypes.MakeSigner(ethCfg, blockNum, uint64(ctx.BlockTime().Unix()))
	tx, err := ethtypes.SignTx(ethtypes.NewTx(&txData), signer, key)
	require.Nil(t, err)
	txwrapper, err := ethtx.NewLegacyTx(tx)
	require.Nil(t, err)
	req, err := types.NewMsgEVMTransaction(txwrapper)
	require.Nil(t, err)

	_, evmAddr := testkeeper.PrivateKeyToAddresses(privKey)
	amt := sdk.NewCoins(sdk.NewCoin(k.GetBaseDenom(ctx), sdk.NewInt(1000)))
	k.BankKeeper().MintCoins(ctx, types.ModuleName, sdk.NewCoins(sdk.NewCoin(k.GetBaseDenom(ctx), sdk.NewInt(1000))))
	k.BankKeeper().SendCoinsFromModuleToAccount(ctx, types.ModuleName, evmAddr[:], amt)

	msgServer := keeper.NewMsgServerImpl(k)

	// Deploy Simple Storage contract with insufficient gas
	ante.Preprocess(ctx, req, k.GetParams(ctx))
	_, err = msgServer.EVMTransaction(sdk.WrapSDKContext(ctx), req)
	require.NotNil(t, err)
	require.Contains(t, err.Error(), "intrinsic gas too low") // this can only happen in test because we didn't call CheckTx in this test
}

func TestEVMDynamicFeeTransaction(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper()
	code, err := os.ReadFile("../../../example/contracts/simplestorage/SimpleStorage.bin")
	require.Nil(t, err)
	bz, err := hex.DecodeString(string(code))
	require.Nil(t, err)
	privKey := testkeeper.MockPrivateKey()
	testPrivHex := hex.EncodeToString(privKey.Bytes())
	key, _ := crypto.HexToECDSA(testPrivHex)
	txData := ethtypes.DynamicFeeTx{
		GasFeeCap: big.NewInt(1000000000000),
		Gas:       200000,
		To:        nil,
		Value:     big.NewInt(0),
		Data:      bz,
		Nonce:     0,
	}
	chainID := k.ChainID(ctx)
	evmParams := k.GetParams(ctx)
	chainCfg := evmParams.GetChainConfig()
	ethCfg := chainCfg.EthereumConfig(chainID)
	blockNum := big.NewInt(ctx.BlockHeight())
	signer := ethtypes.MakeSigner(ethCfg, blockNum, uint64(ctx.BlockTime().Unix()))
	tx, err := ethtypes.SignTx(ethtypes.NewTx(&txData), signer, key)
	require.Nil(t, err)
	txwrapper, err := ethtx.NewDynamicFeeTx(tx)
	require.Nil(t, err)
	req, err := types.NewMsgEVMTransaction(txwrapper)
	require.Nil(t, err)

	_, evmAddr := testkeeper.PrivateKeyToAddresses(privKey)
	amt := sdk.NewCoins(sdk.NewCoin(k.GetBaseDenom(ctx), sdk.NewInt(1000000)))
	k.BankKeeper().MintCoins(ctx, types.ModuleName, sdk.NewCoins(sdk.NewCoin(k.GetBaseDenom(ctx), sdk.NewInt(1000000))))
	k.BankKeeper().SendCoinsFromModuleToAccount(ctx, types.ModuleName, evmAddr[:], amt)

	msgServer := keeper.NewMsgServerImpl(k)

	// Deploy Simple Storage contract
	ante.Preprocess(ctx, req, k.GetParams(ctx))
	res, err := msgServer.EVMTransaction(sdk.WrapSDKContext(ctx), req)
	require.Nil(t, err)
	require.LessOrEqual(t, res.GasUsed, uint64(200000))
	require.Empty(t, res.VmError)
	require.NotEmpty(t, res.ReturnData)
	require.NotEmpty(t, res.Hash)
	require.LessOrEqual(t, k.BankKeeper().GetBalance(ctx, sdk.AccAddress(evmAddr[:]), "usei").Amount.Uint64(), uint64(1000000)-res.GasUsed)
	receipt, err := k.GetReceipt(ctx, common.HexToHash(res.Hash))
	require.Nil(t, err)
	require.NotNil(t, receipt)
	require.Equal(t, uint32(ethtypes.ReceiptStatusSuccessful), receipt.Status) // value is 0x14 = 20
}

func TestEVMPrecompiles(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeperWithPrecompiles()
	params := k.GetParams(ctx)
	params.WhitelistedCodehashesBankSend = append(params.WhitelistedCodehashesBankSend, "0x3071724680db53f64fe250bcb067e7f21280a0b6359486ba3a84a319eb3ef249")
	k.SetParams(ctx, params)
	code, err := os.ReadFile("../../../example/contracts/sendall/SendAll.bin")
	require.Nil(t, err)
	bz, err := hex.DecodeString(string(code))
	require.Nil(t, err)
	privKey := testkeeper.MockPrivateKey()
	testPrivHex := hex.EncodeToString(privKey.Bytes())
	key, _ := crypto.HexToECDSA(testPrivHex)
	txData := ethtypes.LegacyTx{
		GasPrice: big.NewInt(1000000000000),
		Gas:      500000,
		To:       nil,
		Value:    big.NewInt(0),
		Data:     bz,
		Nonce:    0,
	}
	chainID := k.ChainID(ctx)
	evmParams := k.GetParams(ctx)
	chainCfg := evmParams.GetChainConfig()
	ethCfg := chainCfg.EthereumConfig(chainID)
	blockNum := big.NewInt(ctx.BlockHeight())
	signer := ethtypes.MakeSigner(ethCfg, blockNum, uint64(ctx.BlockTime().Unix()))
	tx, err := ethtypes.SignTx(ethtypes.NewTx(&txData), signer, key)
	require.Nil(t, err)
	txwrapper, err := ethtx.NewLegacyTx(tx)
	require.Nil(t, err)
	req, err := types.NewMsgEVMTransaction(txwrapper)
	require.Nil(t, err)

	_, evmAddr := testkeeper.PrivateKeyToAddresses(privKey)
	amt := sdk.NewCoins(sdk.NewCoin(k.GetBaseDenom(ctx), sdk.NewInt(1000000)))
	k.BankKeeper().MintCoins(ctx, types.ModuleName, sdk.NewCoins(sdk.NewCoin(k.GetBaseDenom(ctx), sdk.NewInt(1000000))))
	k.BankKeeper().SendCoinsFromModuleToAccount(ctx, types.ModuleName, evmAddr[:], amt)

	msgServer := keeper.NewMsgServerImpl(k)

	// Deploy SendAll contract
	ante.Preprocess(ctx, req, k.GetParams(ctx))
	res, err := msgServer.EVMTransaction(sdk.WrapSDKContext(ctx), req)
	require.Nil(t, err)
	require.LessOrEqual(t, res.GasUsed, uint64(500000))
	require.Empty(t, res.VmError)
	require.NotEmpty(t, res.ReturnData)
	require.NotEmpty(t, res.Hash)
	require.Equal(t, uint64(1000000)-res.GasUsed, k.BankKeeper().GetBalance(ctx, sdk.AccAddress(evmAddr[:]), "usei").Amount.Uint64())
	require.Equal(t, res.GasUsed, k.BankKeeper().GetBalance(ctx, state.GetCoinbaseAddress(ctx), k.GetBaseDenom(ctx)).Amount.Uint64())
	receipt, err := k.GetReceipt(ctx, common.HexToHash(res.Hash))
	require.Nil(t, err)
	require.NotNil(t, receipt)
	require.Equal(t, uint32(ethtypes.ReceiptStatusSuccessful), receipt.Status)

	// call sendall
	addr1, evmAddr1 := testkeeper.MockAddressPair()
	addr2, evmAddr2 := testkeeper.MockAddressPair()
	k.SetAddressMapping(ctx, addr1, evmAddr1)
	k.SetAddressMapping(ctx, addr2, evmAddr2)
	k.BankKeeper().MintCoins(ctx, types.ModuleName, sdk.NewCoins(sdk.NewCoin(k.GetBaseDenom(ctx), sdk.NewInt(100000))))
	k.BankKeeper().SendCoinsFromModuleToAccount(ctx, types.ModuleName, addr1, sdk.NewCoins(sdk.NewCoin(k.GetBaseDenom(ctx), sdk.NewInt(100000))))
	contractAddr := common.HexToAddress(receipt.ContractAddress)
	abi, err := sendall.SendallMetaData.GetAbi()
	require.Nil(t, err)
	bz, err = abi.Pack("sendAll", evmAddr1, evmAddr2, k.GetBaseDenom(ctx))
	require.Nil(t, err)
	txData = ethtypes.LegacyTx{
		GasPrice: big.NewInt(1000000000000),
		Gas:      200000,
		To:       &contractAddr,
		Value:    big.NewInt(0),
		Data:     bz,
		Nonce:    1,
	}
	tx, err = ethtypes.SignTx(ethtypes.NewTx(&txData), signer, key)
	require.Nil(t, err)
	txwrapper, err = ethtx.NewLegacyTx(tx)
	require.Nil(t, err)
	req, err = types.NewMsgEVMTransaction(txwrapper)
	require.Nil(t, err)
	ante.Preprocess(ctx, req, k.GetParams(ctx))
	res, err = msgServer.EVMTransaction(sdk.WrapSDKContext(ctx), req)
	require.Nil(t, err)
	require.LessOrEqual(t, res.GasUsed, uint64(200000))
	require.Empty(t, res.VmError)
	receipt, err = k.GetReceipt(ctx, common.HexToHash(res.Hash))
	require.Nil(t, err)
	require.NotNil(t, receipt)
	require.Equal(t, uint32(ethtypes.ReceiptStatusSuccessful), receipt.Status)
	addr1Balance := k.BankKeeper().GetBalance(ctx, addr1, k.GetBaseDenom(ctx)).Amount.Uint64()
	require.Equal(t, uint64(0), addr1Balance)
	addr2Balance := k.BankKeeper().GetBalance(ctx, addr2, k.GetBaseDenom(ctx)).Amount.Uint64()
	require.Equal(t, uint64(100000), addr2Balance)
}

func TestEVMAssociateTx(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper()
	req, err := types.NewMsgEVMTransaction(&ethtx.AssociateTx{})
	require.Nil(t, err)
	msgServer := keeper.NewMsgServerImpl(k)

	ante.Preprocess(ctx, req, k.GetParams(ctx))
	res, err := msgServer.EVMTransaction(sdk.WrapSDKContext(ctx), req)
	require.Nil(t, err)
	require.Equal(t, types.MsgEVMTransactionResponse{}, *res)
}
