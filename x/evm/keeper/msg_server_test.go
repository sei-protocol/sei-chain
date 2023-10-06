package keeper

import (
	"bytes"
	"encoding/hex"
	"math/big"
	"os"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/sei-protocol/sei-chain/x/evm/keeper/testdata"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/sei-protocol/sei-chain/x/evm/types/ethtx"
	"github.com/stretchr/testify/require"
)

func TestEVMTransaction(t *testing.T) {
	k, _, ctx := MockEVMKeeper()
	code, err := os.ReadFile("./testdata/SimpleStorage/SimpleStorage.bin")
	require.Nil(t, err)
	bz, err := hex.DecodeString(string(code))
	require.Nil(t, err)
	privKey := MockPrivateKey()
	testPrivHex := hex.EncodeToString(privKey.Bytes())
	key, _ := crypto.HexToECDSA(testPrivHex)
	txData := ethtypes.LegacyTx{
		GasPrice: big.NewInt(1),
		Gas:      200000,
		To:       nil,
		Value:    big.NewInt(0),
		Data:     bz,
		Nonce:    0,
	}
	chainID := k.ChainID()
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

	_, evmAddr := PrivateKeyToAddresses(privKey)
	k.SetOrDeleteBalance(ctx, evmAddr, 1000000)
	k.bankKeeper.MintCoins(ctx, types.ModuleName, sdk.NewCoins(sdk.NewCoin(k.GetBaseDenom(ctx), sdk.NewInt(1000000))))

	msgServer := NewMsgServerImpl(*k)

	// Deploy Simple Storage contract
	res, err := msgServer.EVMTransaction(sdk.WrapSDKContext(ctx), req)
	require.Nil(t, err)
	require.LessOrEqual(t, res.GasUsed, uint64(200000))
	require.Empty(t, res.VmError)
	require.NotEmpty(t, res.ReturnData)
	require.NotEmpty(t, res.Hash)
	require.Equal(t, uint64(1000000)-res.GasUsed, k.GetBalance(ctx, evmAddr))
	require.Equal(t, res.GasUsed, k.BankKeeper().GetBalance(ctx, k.accountKeeper.GetModuleAddress(authtypes.FeeCollectorName), k.GetBaseDenom(ctx)).Amount.Uint64())
	receipt, err := k.GetReceipt(ctx, common.HexToHash(res.Hash))
	require.Nil(t, err)
	require.NotNil(t, receipt)
	require.Equal(t, uint32(ethtypes.ReceiptStatusSuccessful), receipt.Status)

	// send transaction to the contract
	contractAddr := common.HexToAddress(receipt.ContractAddress)
	abi, err := testdata.TestdataMetaData.GetAbi()
	require.Nil(t, err)
	bz, err = abi.Pack("set", big.NewInt(20))
	require.Nil(t, err)
	txData = ethtypes.LegacyTx{
		GasPrice: big.NewInt(1),
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
	stateDB := state.NewStateDBImpl(ctx, k)
	val := hex.EncodeToString(bytes.Trim(stateDB.GetState(contractAddr, common.Hash{}).Bytes(), "\x00")) // key is 0x0 since the contract only has one variable
	require.Equal(t, "14", val)                                                                          // value is 0x14 = 20
}

func TestEVMTransactionError(t *testing.T) {
	k, _, ctx := MockEVMKeeper()
	privKey := MockPrivateKey()
	testPrivHex := hex.EncodeToString(privKey.Bytes())
	key, _ := crypto.HexToECDSA(testPrivHex)
	txData := ethtypes.LegacyTx{
		GasPrice: big.NewInt(1),
		Gas:      200000,
		To:       nil,
		Value:    big.NewInt(0),
		Data:     []byte("123090321920390920123"), // gibberish data
		Nonce:    0,
	}
	chainID := k.ChainID()
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

	_, evmAddr := PrivateKeyToAddresses(privKey)
	k.SetOrDeleteBalance(ctx, evmAddr, 1000000)
	k.bankKeeper.MintCoins(ctx, types.ModuleName, sdk.NewCoins(sdk.NewCoin(k.GetBaseDenom(ctx), sdk.NewInt(1000000))))

	msgServer := NewMsgServerImpl(*k)

	res, err := msgServer.EVMTransaction(sdk.WrapSDKContext(ctx), req)
	require.Nil(t, err) // there should only be VM error, no msg-level error
	require.NotEmpty(t, res.VmError)
	// gas should be charged and receipt should be created
	require.Equal(t, uint64(800000), k.GetBalance(ctx, evmAddr))
	receipt, err := k.GetReceipt(ctx, common.HexToHash(res.Hash))
	require.Nil(t, err)
	require.Equal(t, uint32(ethtypes.ReceiptStatusFailed), receipt.Status)
	// yet there should be no contract state
	stateDB := state.NewStateDBImpl(ctx, k)
	require.Empty(t, stateDB.GetState(common.HexToAddress(receipt.ContractAddress), common.Hash{}))
}

func TestEVMTransactionInsufficientGas(t *testing.T) {
	k, _, ctx := MockEVMKeeper()
	code, err := os.ReadFile("./testdata/SimpleStorage/SimpleStorage.bin")
	require.Nil(t, err)
	bz, err := hex.DecodeString(string(code))
	require.Nil(t, err)
	privKey := MockPrivateKey()
	testPrivHex := hex.EncodeToString(privKey.Bytes())
	key, _ := crypto.HexToECDSA(testPrivHex)
	txData := ethtypes.LegacyTx{
		GasPrice: big.NewInt(1),
		Gas:      1000,
		To:       nil,
		Value:    big.NewInt(0),
		Data:     bz,
		Nonce:    0,
	}
	chainID := k.ChainID()
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

	_, evmAddr := PrivateKeyToAddresses(privKey)
	k.SetOrDeleteBalance(ctx, evmAddr, 1000)
	k.bankKeeper.MintCoins(ctx, types.ModuleName, sdk.NewCoins(sdk.NewCoin(k.GetBaseDenom(ctx), sdk.NewInt(1000))))

	msgServer := NewMsgServerImpl(*k)

	// Deploy Simple Storage contract with insufficient gas
	res, err := msgServer.EVMTransaction(sdk.WrapSDKContext(ctx), req)
	require.Nil(t, err) // there should be no error on Sei level, since we don't want all state changes (like gas charge and receipt) to revert
	require.Contains(t, res.VmError, "intrinsic gas too low")
	require.Equal(t, uint64(1000), res.GasUsed) // all gas should be consumed
	require.Equal(t, uint64(0), k.bankKeeper.GetBalance(ctx, k.accountKeeper.GetModuleAddress(authtypes.FeeCollectorName), k.GetBaseDenom(ctx)).Amount.Uint64())
	require.Equal(t, uint64(0), k.bankKeeper.GetBalance(ctx, k.accountKeeper.GetModuleAddress(types.ModuleName), k.GetBaseDenom(ctx)).Amount.Uint64())
	require.Equal(t, uint64(0), k.GetBalance(ctx, evmAddr))
}

func TestEVMDynamicFeeTransaction(t *testing.T) {
	k, _, ctx := MockEVMKeeper()
	code, err := os.ReadFile("./testdata/SimpleStorage/SimpleStorage.bin")
	require.Nil(t, err)
	bz, err := hex.DecodeString(string(code))
	require.Nil(t, err)
	privKey := MockPrivateKey()
	testPrivHex := hex.EncodeToString(privKey.Bytes())
	key, _ := crypto.HexToECDSA(testPrivHex)
	txData := ethtypes.DynamicFeeTx{
		GasFeeCap: big.NewInt(1),
		Gas:       200000,
		To:        nil,
		Value:     big.NewInt(0),
		Data:      bz,
		Nonce:     0,
	}
	chainID := k.ChainID()
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

	_, evmAddr := PrivateKeyToAddresses(privKey)
	k.SetOrDeleteBalance(ctx, evmAddr, 1000000)
	k.bankKeeper.MintCoins(ctx, types.ModuleName, sdk.NewCoins(sdk.NewCoin(k.GetBaseDenom(ctx), sdk.NewInt(1000000))))

	msgServer := NewMsgServerImpl(*k)

	// Deploy Simple Storage contract
	res, err := msgServer.EVMTransaction(sdk.WrapSDKContext(ctx), req)
	require.Nil(t, err)
	require.LessOrEqual(t, res.GasUsed, uint64(200000))
	require.Empty(t, res.VmError)
	require.NotEmpty(t, res.ReturnData)
	require.NotEmpty(t, res.Hash)
	require.LessOrEqual(t, k.GetBalance(ctx, evmAddr), uint64(1000000)-res.GasUsed)
	receipt, err := k.GetReceipt(ctx, common.HexToHash(res.Hash))
	require.Nil(t, err)
	require.NotNil(t, receipt)
	require.Equal(t, uint32(ethtypes.ReceiptStatusSuccessful), receipt.Status) // value is 0x14 = 20
}
