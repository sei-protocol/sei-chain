package keeper_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"os"
	"testing"
	"time"

	wasmtypes "github.com/CosmWasm/wasmd/x/wasm/types"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
	abci "github.com/tendermint/tendermint/abci/types"

	"github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/example/contracts/echo"
	"github.com/sei-protocol/sei-chain/example/contracts/sendall"
	"github.com/sei-protocol/sei-chain/example/contracts/simplestorage"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/ante"
	"github.com/sei-protocol/sei-chain/x/evm/artifacts/erc20"
	"github.com/sei-protocol/sei-chain/x/evm/artifacts/erc721"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/sei-protocol/sei-chain/x/evm/types/ethtx"
)

type mockTx struct {
	msgs    []sdk.Msg
	signers []sdk.AccAddress
}

func (tx mockTx) GetMsgs() []sdk.Msg                              { return tx.msgs }
func (tx mockTx) ValidateBasic() error                            { return nil }
func (tx mockTx) GetSigners() []sdk.AccAddress                    { return tx.signers }
func (tx mockTx) GetPubKeys() ([]cryptotypes.PubKey, error)       { return nil, nil }
func (tx mockTx) GetSignaturesV2() ([]signing.SignatureV2, error) { return nil, nil }

func TestSimple(t *testing.T) {
	a := testkeeper.EVMTestApp
	ctx := a.GetContextForDeliverTx(nil).WithChainID("pacific-1").WithBlockHeight(117842992).WithBlockTime(time.Unix(1732924863, 0))
	codeHash, _ := hex.DecodeString("4EBD7DA3920F3EF5914F76B229DA29F8251DBCFB6F32C3FEEE1626A5BAD71F90")
	code, _ := os.ReadFile("/Users/xiaoyuchen/Downloads/4485.code")
	require.Nil(t, a.WasmKeeper.ImportCode(ctx, 4485, wasmtypes.CodeInfo{
		CodeHash:          codeHash,
		Creator:           "sei1efpl63nwegea0j2a69hhapx0a7khmvaacxd67c",
		InstantiateConfig: wasmtypes.AllowEverybody,
	}, code))
	mockStateFromJson(ctx, "/Users/xiaoyuchen/Downloads/dump.json", a)
	to := common.HexToAddress("0x0000000000000000000000000000000000001002")
	bz, err := hex.DecodeString("44d227ae000000000000000000000000000000000000000000000000000000000000006000000000000000000000000000000000000000000000000000000000000000c00000000000000000000000000000000000000000000000000000000000000100000000000000000000000000000000000000000000000000000000000000003e7365693165336774747a713565356b343966396635677a76726c30726c746c617636357875367039786330616a376538346c616e74646a717037636e63630000000000000000000000000000000000000000000000000000000000000000000b7b22626f6e64223a7b7d7d00000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000275b7b22616d6f756e74223a22313638303030303030222c2264656e6f6d223a2275736569227d5d00000000000000000000000000000000000000000000000000")
	require.Nil(t, err)
	R := new(hexutil.Big)
	require.Nil(t, R.UnmarshalText([]byte("0x6a23f05dda3d3b48a6f88ddd97f542ce610de01417376bb0a120c23e8f7375ab")))
	S := new(hexutil.Big)
	require.Nil(t, S.UnmarshalText([]byte("0x501feb6fccea582bde7371c23551ac1cc23e97909008ecf2966660b52a6fc3e")))
	txData := ethtypes.DynamicFeeTx{
		ChainID:    big.NewInt(1329),
		Gas:        610546,
		To:         &to,
		Value:      new(big.Int).Mul(big.NewInt(1000000000000000000), big.NewInt(168)),
		Data:       bz,
		Nonce:      12,
		GasTipCap:  big.NewInt(100855536788),
		GasFeeCap:  big.NewInt(100855536788),
		AccessList: ethtypes.AccessList{},
		V:          big.NewInt(1),
		R:          R.ToInt(),
		S:          S.ToInt(),
	}
	tx := ethtypes.NewTx(&txData)
	require.Equal(t, "0x01aaf9dd754d86f887c0c530cdd769da9202dfc1eb4a3e229050b417a4dc1adf", tx.Hash().Hex())
	txwrapper, err := ethtx.NewDynamicFeeTx(tx)
	require.Nil(t, err)
	req, err := types.NewMsgEVMTransaction(txwrapper)
	require.Nil(t, err)
	tb := testkeeper.EVMTestApp.GetTxConfig().NewTxBuilder()
	tb.SetMsgs(req)
	sdktx := tb.GetTx()
	txbz, err := testkeeper.EVMTestApp.GetTxConfig().TxEncoder()(sdktx)
	require.Nil(t, err)

	res := testkeeper.EVMTestApp.DeliverTx(ctx, abci.RequestDeliverTx{Tx: txbz}, sdktx, sha256.Sum256(txbz))
	fmt.Println(res.EvmTxInfo)
	panic(1)
	require.Equal(t, uint32(45), res.Code)
	// r, _ := testkeeper.EVMTestApp.EvmKeeper.GetReceipt(ctx, common.HexToHash("0x01aaf9dd754d86f887c0c530cdd769da9202dfc1eb4a3e229050b417a4dc1adf"))
	// fmt.Println(r.GasUsed)
	// panic(1)
}

func mockStateFromJson(ctx sdk.Context, filepath string, a *app.App) {
	file, _ := os.Open(filepath)
	data, _ := io.ReadAll(file)
	typed := map[string]interface{}{}
	json.Unmarshal(data, &typed)
	typed = typed["modules"].(map[string]interface{})
	for moduleName, data := range typed {
		var storeKey sdk.StoreKey
		kvStoreKey := a.GetKey(moduleName)
		if kvStoreKey != nil {
			storeKey = kvStoreKey
		} else {
			storeKey = a.GetMemKey(moduleName)
		}
		store := ctx.KVStore(storeKey)
		typedData := data.(map[string]interface{})
		for _, key := range typedData["has"].([]interface{}) {
			bz, _ := hex.DecodeString(key.(string))
			store.Set(bz, []byte{1})
		}
		for key, value := range typedData["reads"].(map[string]interface{}) {
			if value == "" {
				continue
			}
			kbz, _ := hex.DecodeString(key)
			vbz, _ := hex.DecodeString(value.(string))
			store.Set(kbz, vbz)
		}
	}
}

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
	chainCfg := types.DefaultChainConfig()
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
	ante.Preprocess(ctx, req)
	ctx, err = ante.NewEVMFeeCheckDecorator(k).AnteHandle(ctx, mockTx{msgs: []sdk.Msg{req}}, false, func(sdk.Context, sdk.Tx, bool) (sdk.Context, error) {
		return ctx, nil
	})
	require.Nil(t, err)
	res, err := msgServer.EVMTransaction(sdk.WrapSDKContext(ctx), req)
	require.Nil(t, err)
	require.LessOrEqual(t, res.GasUsed, uint64(200000))
	require.Empty(t, res.VmError)
	require.NotEmpty(t, res.ReturnData)
	require.NotEmpty(t, res.Hash)
	require.Equal(t, uint64(1000000)-res.GasUsed, k.BankKeeper().GetBalance(ctx, sdk.AccAddress(evmAddr[:]), "usei").Amount.Uint64())
	require.Equal(t, res.GasUsed, k.BankKeeper().GetBalance(ctx, state.GetCoinbaseAddress(ctx.TxIndex()), k.GetBaseDenom(ctx)).Amount.Uint64())
	require.NoError(t, k.FlushTransientReceipts(ctx))
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
	ante.Preprocess(ctx, req)
	ctx, err = ante.NewEVMFeeCheckDecorator(k).AnteHandle(ctx, mockTx{msgs: []sdk.Msg{req}}, false, func(sdk.Context, sdk.Tx, bool) (sdk.Context, error) {
		return ctx, nil
	})
	require.Nil(t, err)
	res, err = msgServer.EVMTransaction(sdk.WrapSDKContext(ctx), req)
	require.Nil(t, err)
	require.NotEmpty(t, res.Logs)
	require.LessOrEqual(t, res.GasUsed, uint64(200000))
	require.Empty(t, res.VmError)
	require.NoError(t, k.FlushTransientReceipts(ctx))
	receipt, err = k.GetReceipt(ctx, common.HexToHash(res.Hash))
	require.Nil(t, err)
	require.NotNil(t, receipt)
	require.Equal(t, uint32(ethtypes.ReceiptStatusSuccessful), receipt.Status)
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
	chainCfg := types.DefaultChainConfig()
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

	ante.Preprocess(ctx, req)
	ctx, err = ante.NewEVMFeeCheckDecorator(k).AnteHandle(ctx, mockTx{msgs: []sdk.Msg{req}}, false, func(sdk.Context, sdk.Tx, bool) (sdk.Context, error) {
		return ctx, nil
	})
	require.Nil(t, err)
	res, err := msgServer.EVMTransaction(sdk.WrapSDKContext(ctx), req)
	require.Nil(t, err) // there should only be VM error, no msg-level error
	require.NotEmpty(t, res.VmError)
	// gas should be charged and receipt should be created
	require.Equal(t, uint64(800000), k.BankKeeper().GetBalance(ctx, sdk.AccAddress(evmAddr[:]), "usei").Amount.Uint64())
	require.NoError(t, k.FlushTransientReceipts(ctx))
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
	chainCfg := types.DefaultChainConfig()
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
	ante.Preprocess(ctx, req)
	ctx, err = ante.NewEVMFeeCheckDecorator(k).AnteHandle(ctx, mockTx{msgs: []sdk.Msg{req}}, false, func(sdk.Context, sdk.Tx, bool) (sdk.Context, error) {
		return ctx, nil
	})
	require.Nil(t, err)
	_, err = msgServer.EVMTransaction(sdk.WrapSDKContext(ctx), req)
	require.NotNil(t, err)
	require.Contains(t, err.Error(), "intrinsic gas too low")                                               // this can only happen in test because we didn't call CheckTx in this test
	require.Equal(t, sdk.ZeroInt(), k.BankKeeper().GetBalance(ctx, evmAddr[:], k.GetBaseDenom(ctx)).Amount) // fee should be charged
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
	chainCfg := types.DefaultChainConfig()
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
	ante.Preprocess(ctx, req)
	ctx, err = ante.NewEVMFeeCheckDecorator(k).AnteHandle(ctx, mockTx{msgs: []sdk.Msg{req}}, false, func(sdk.Context, sdk.Tx, bool) (sdk.Context, error) {
		return ctx, nil
	})
	require.Nil(t, err)
	res, err := msgServer.EVMTransaction(sdk.WrapSDKContext(ctx), req)
	require.Nil(t, err)
	require.LessOrEqual(t, res.GasUsed, uint64(200000))
	require.Empty(t, res.VmError)
	require.NotEmpty(t, res.ReturnData)
	require.NotEmpty(t, res.Hash)
	require.LessOrEqual(t, k.BankKeeper().GetBalance(ctx, sdk.AccAddress(evmAddr[:]), "usei").Amount.Uint64(), uint64(1000000)-res.GasUsed)
	require.NoError(t, k.FlushTransientReceipts(ctx))
	receipt, err := k.GetReceipt(ctx, common.HexToHash(res.Hash))
	require.Nil(t, err)
	require.NotNil(t, receipt)
	require.Equal(t, uint32(ethtypes.ReceiptStatusSuccessful), receipt.Status) // value is 0x14 = 20
}

func TestEVMPrecompiles(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeperWithPrecompiles()
	params := k.GetParams(ctx)
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
	chainCfg := types.DefaultChainConfig()
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
	ante.Preprocess(ctx, req)
	ctx, err = ante.NewEVMFeeCheckDecorator(k).AnteHandle(ctx, mockTx{msgs: []sdk.Msg{req}}, false, func(sdk.Context, sdk.Tx, bool) (sdk.Context, error) {
		return ctx, nil
	})
	require.Nil(t, err)
	coinbaseBalanceBefore := k.BankKeeper().GetBalance(ctx, state.GetCoinbaseAddress(ctx.TxIndex()), "usei").Amount.Uint64()
	res, err := msgServer.EVMTransaction(sdk.WrapSDKContext(ctx), req)
	require.Nil(t, err)
	require.LessOrEqual(t, res.GasUsed, uint64(500000))
	require.Empty(t, res.VmError)
	require.NotEmpty(t, res.ReturnData)
	require.NotEmpty(t, res.Hash)
	require.Equal(t, uint64(1000000)-res.GasUsed, k.BankKeeper().GetBalance(ctx, sdk.AccAddress(evmAddr[:]), k.GetBaseDenom(ctx)).Amount.Uint64())
	coinbaseBalanceAfter := k.BankKeeper().GetBalance(ctx, state.GetCoinbaseAddress(ctx.TxIndex()), k.GetBaseDenom(ctx)).Amount.Uint64()
	diff := coinbaseBalanceAfter - coinbaseBalanceBefore
	require.Equal(t, res.GasUsed, diff)
	require.NoError(t, k.FlushTransientReceipts(ctx))
	receipt, err := k.GetReceipt(ctx, common.HexToHash(res.Hash))
	require.Nil(t, err)
	require.NotNil(t, receipt)
	require.Equal(t, uint32(ethtypes.ReceiptStatusSuccessful), receipt.Status)
	k.SetERC20NativePointer(ctx, k.GetBaseDenom(ctx), common.HexToAddress(receipt.ContractAddress))

	// call sendall
	addr1, evmAddr1 := testkeeper.MockAddressPair()
	addr2, evmAddr2 := testkeeper.MockAddressPair()
	k.SetAddressMapping(ctx, addr1, evmAddr1)
	k.SetAddressMapping(ctx, addr2, evmAddr2)
	err = k.BankKeeper().MintCoins(ctx, types.ModuleName, sdk.NewCoins(sdk.NewCoin(k.GetBaseDenom(ctx), sdk.NewInt(100000))))
	require.Nil(t, err)
	err = k.BankKeeper().SendCoinsFromModuleToAccount(ctx, types.ModuleName, addr1, sdk.NewCoins(sdk.NewCoin(k.GetBaseDenom(ctx), sdk.NewInt(100000))))
	require.Nil(t, err)
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
	ante.Preprocess(ctx, req)
	ctx, err = ante.NewEVMFeeCheckDecorator(k).AnteHandle(ctx, mockTx{msgs: []sdk.Msg{req}}, false, func(sdk.Context, sdk.Tx, bool) (sdk.Context, error) {
		return ctx, nil
	})
	require.Nil(t, err)
	res, err = msgServer.EVMTransaction(sdk.WrapSDKContext(ctx), req)
	require.Nil(t, err)
	require.LessOrEqual(t, res.GasUsed, uint64(200000))
	require.Empty(t, res.VmError)
	require.NoError(t, k.FlushTransientReceipts(ctx))
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

	ante.Preprocess(ctx, req)
	res, err := msgServer.EVMTransaction(sdk.WrapSDKContext(ctx), req)
	require.Nil(t, err)
	require.Equal(t, types.MsgEVMTransactionResponse{}, *res)
}

func TestEVMBlockEnv(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper()
	code, err := os.ReadFile("../../../example/contracts/echo/Echo.bin")
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
	chainCfg := types.DefaultChainConfig()
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
	ante.Preprocess(ctx, req)
	ctx, err = ante.NewEVMFeeCheckDecorator(k).AnteHandle(ctx, mockTx{msgs: []sdk.Msg{req}}, false, func(sdk.Context, sdk.Tx, bool) (sdk.Context, error) {
		return ctx, nil
	})
	require.Nil(t, err)
	res, err := msgServer.EVMTransaction(sdk.WrapSDKContext(ctx), req)
	require.Nil(t, err)
	require.LessOrEqual(t, res.GasUsed, uint64(200000))
	require.Empty(t, res.VmError)
	require.NotEmpty(t, res.ReturnData)
	require.NotEmpty(t, res.Hash)
	require.Equal(t, uint64(1000000)-res.GasUsed, k.BankKeeper().GetBalance(ctx, sdk.AccAddress(evmAddr[:]), "usei").Amount.Uint64())
	fmt.Println("all balances sender = ", k.BankKeeper().GetAllBalances(ctx, sdk.AccAddress(evmAddr[:])))
	fmt.Println("all balances coinbase = ", k.BankKeeper().GetAllBalances(ctx, state.GetCoinbaseAddress(ctx.TxIndex())))
	fmt.Println("wei = ", k.BankKeeper().GetBalance(ctx, state.GetCoinbaseAddress(ctx.TxIndex()), "wei").Amount.Uint64())
	require.Equal(t, res.GasUsed, k.BankKeeper().GetBalance(ctx, state.GetCoinbaseAddress(ctx.TxIndex()), "usei").Amount.Uint64())

	require.NoError(t, k.FlushTransientReceipts(ctx))
	receipt, err := k.GetReceipt(ctx, common.HexToHash(res.Hash))
	require.Nil(t, err)
	require.NotNil(t, receipt)
	require.Equal(t, uint32(ethtypes.ReceiptStatusSuccessful), receipt.Status)

	// send transaction to the contract
	contractAddr := common.HexToAddress(receipt.ContractAddress)
	abi, err := echo.EchoMetaData.GetAbi()
	require.Nil(t, err)
	bz, err = abi.Pack("setTime", big.NewInt(1))
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
	ante.Preprocess(ctx, req)
	ctx, err = ante.NewEVMFeeCheckDecorator(k).AnteHandle(ctx, mockTx{msgs: []sdk.Msg{req}}, false, func(sdk.Context, sdk.Tx, bool) (sdk.Context, error) {
		return ctx, nil
	})
	require.Nil(t, err)
	res, err = msgServer.EVMTransaction(sdk.WrapSDKContext(ctx), req)
	require.Nil(t, err)
	require.LessOrEqual(t, res.GasUsed, uint64(200000))
	require.Empty(t, res.VmError)
	require.NoError(t, k.FlushTransientReceipts(ctx))
	receipt, err = k.GetReceipt(ctx, common.HexToHash(res.Hash))
	require.Nil(t, err)
	require.NotNil(t, receipt)
	require.Equal(t, uint32(ethtypes.ReceiptStatusSuccessful), receipt.Status)
}

func TestSend(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper()
	seiFrom, evmFrom := testkeeper.MockAddressPair()
	seiTo, evmTo := testkeeper.MockAddressPair()
	k.SetAddressMapping(ctx, seiFrom, evmFrom)
	k.SetAddressMapping(ctx, seiTo, evmTo)
	k.BankKeeper().AddCoins(ctx, seiFrom, sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(1000000))), true)
	_, err := keeper.NewMsgServerImpl(k).Send(sdk.WrapSDKContext(ctx), &types.MsgSend{
		FromAddress: seiFrom.String(),
		ToAddress:   evmTo.Hex(),
		Amount:      sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(500000))),
	})
	require.Nil(t, err)
	require.Equal(t, sdk.NewInt(500000), k.BankKeeper().GetBalance(ctx, seiFrom, "usei").Amount)
	require.Equal(t, sdk.NewInt(500000), k.BankKeeper().GetBalance(ctx, seiTo, "usei").Amount)
}

func TestRegisterPointer(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper()
	sender, _ := testkeeper.MockAddressPair()
	_, pointee := testkeeper.MockAddressPair()
	res, err := keeper.NewMsgServerImpl(k).RegisterPointer(sdk.WrapSDKContext(ctx), &types.MsgRegisterPointer{
		Sender:      sender.String(),
		PointerType: types.PointerType_ERC20,
		ErcAddress:  pointee.Hex(),
	})
	require.Nil(t, err)
	pointer, version, exists := k.GetCW20ERC20Pointer(ctx, pointee)
	require.True(t, exists)
	require.Equal(t, erc20.CurrentVersion, version)
	require.Equal(t, pointer.String(), res.PointerAddress)
	hasRegisteredEvent := false
	for _, e := range ctx.EventManager().Events() {
		if e.Type != types.EventTypePointerRegistered {
			continue
		}
		hasRegisteredEvent = true
		require.Equal(t, types.EventTypePointerRegistered, e.Type)
		require.Equal(t, "erc20", string(e.Attributes[0].Value))
	}
	require.True(t, hasRegisteredEvent)
	ctx = ctx.WithEventManager(sdk.NewEventManager())

	// already exists
	_, err = keeper.NewMsgServerImpl(k).RegisterPointer(sdk.WrapSDKContext(ctx), &types.MsgRegisterPointer{
		Sender:      sender.String(),
		PointerType: types.PointerType_ERC20,
		ErcAddress:  pointee.Hex(),
	})
	require.NotNil(t, err)
	hasRegisteredEvent = false
	for _, e := range ctx.EventManager().Events() {
		if e.Type != types.EventTypePointerRegistered {
			continue
		}
		hasRegisteredEvent = true
	}
	require.False(t, hasRegisteredEvent)
	ctx = ctx.WithEventManager(sdk.NewEventManager())

	res, err = keeper.NewMsgServerImpl(k).RegisterPointer(sdk.WrapSDKContext(ctx), &types.MsgRegisterPointer{
		Sender:      sender.String(),
		PointerType: types.PointerType_ERC721,
		ErcAddress:  pointee.Hex(),
	})
	require.Nil(t, err)
	pointer, version, exists = k.GetCW721ERC721Pointer(ctx, pointee)
	require.True(t, exists)
	require.Equal(t, erc721.CurrentVersion, version)
	require.Equal(t, pointer.String(), res.PointerAddress)
	hasRegisteredEvent = false
	for _, e := range ctx.EventManager().Events() {
		if e.Type != types.EventTypePointerRegistered {
			continue
		}
		hasRegisteredEvent = true
		require.Equal(t, types.EventTypePointerRegistered, e.Type)
		require.Equal(t, "erc721", string(e.Attributes[0].Value))
	}
	require.True(t, hasRegisteredEvent)
	ctx = ctx.WithEventManager(sdk.NewEventManager())

	// already exists
	_, err = keeper.NewMsgServerImpl(k).RegisterPointer(sdk.WrapSDKContext(ctx), &types.MsgRegisterPointer{
		Sender:      sender.String(),
		PointerType: types.PointerType_ERC721,
		ErcAddress:  pointee.Hex(),
	})
	require.NotNil(t, err)
	hasRegisteredEvent = false
	for _, e := range ctx.EventManager().Events() {
		if e.Type != types.EventTypePointerRegistered {
			continue
		}
		hasRegisteredEvent = true
	}
	require.False(t, hasRegisteredEvent)

	// upgrade
	k.DeleteCW721ERC721Pointer(ctx, pointee, version)
	k.SetCW721ERC721PointerWithVersion(ctx, pointee, pointer.String(), version-1)
	res, err = keeper.NewMsgServerImpl(k).RegisterPointer(sdk.WrapSDKContext(ctx), &types.MsgRegisterPointer{
		Sender:      sender.String(),
		PointerType: types.PointerType_ERC721,
		ErcAddress:  pointee.Hex(),
	})
	require.Nil(t, err)
	newPointer, version, exists := k.GetCW721ERC721Pointer(ctx, pointee)
	require.True(t, exists)
	require.Equal(t, erc721.CurrentVersion, version)
	require.Equal(t, newPointer.String(), res.PointerAddress)
	require.Equal(t, newPointer.String(), pointer.String()) // should retain the existing contract address
}

func TestEvmError(t *testing.T) {
	k := testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx([]byte{})
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
	chainCfg := types.DefaultChainConfig()
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

	tb := testkeeper.EVMTestApp.GetTxConfig().NewTxBuilder()
	tb.SetMsgs(req)
	sdktx := tb.GetTx()
	txbz, err := testkeeper.EVMTestApp.GetTxConfig().TxEncoder()(sdktx)
	require.Nil(t, err)

	res := testkeeper.EVMTestApp.DeliverTx(ctx, abci.RequestDeliverTx{Tx: txbz}, sdktx, sha256.Sum256(txbz))
	require.Equal(t, uint32(0), res.Code)
	require.NoError(t, k.FlushTransientReceipts(ctx))
	receipt, err := k.GetReceipt(ctx, common.HexToHash(res.EvmTxInfo.TxHash))
	require.Nil(t, err)

	// send transaction that's gonna be reverted to the contract
	contractAddr := common.HexToAddress(receipt.ContractAddress)
	abi, err := simplestorage.SimplestorageMetaData.GetAbi()
	require.Nil(t, err)
	bz, err = abi.Pack("bad")
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

	tb = testkeeper.EVMTestApp.GetTxConfig().NewTxBuilder()
	tb.SetMsgs(req)
	sdktx = tb.GetTx()
	txbz, err = testkeeper.EVMTestApp.GetTxConfig().TxEncoder()(sdktx)
	require.Nil(t, err)

	res = testkeeper.EVMTestApp.DeliverTx(ctx, abci.RequestDeliverTx{Tx: txbz}, sdktx, sha256.Sum256(txbz))
	require.NoError(t, k.FlushTransientReceipts(ctx))
	require.Equal(t, sdkerrors.ErrEVMVMError.ABCICode(), res.Code)
	receipt, err = k.GetReceipt(ctx, common.HexToHash(res.EvmTxInfo.TxHash))
	require.Nil(t, err)
	require.Equal(t, receipt.VmError, res.EvmTxInfo.VmError)
}

func TestAssociateContractAddress(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper()
	msgServer := keeper.NewMsgServerImpl(k)
	dummySeiAddr, dummyEvmAddr := testkeeper.MockAddressPair()
	res, err := msgServer.RegisterPointer(sdk.WrapSDKContext(ctx), &types.MsgRegisterPointer{
		Sender:      dummySeiAddr.String(),
		PointerType: types.PointerType_ERC20,
		ErcAddress:  dummyEvmAddr.Hex(),
	})
	require.Nil(t, err)
	_, err = msgServer.AssociateContractAddress(sdk.WrapSDKContext(ctx), &types.MsgAssociateContractAddress{
		Sender:  dummySeiAddr.String(),
		Address: res.PointerAddress,
	})
	require.Nil(t, err)
	associatedEvmAddr, found := k.GetEVMAddress(ctx, sdk.MustAccAddressFromBech32(res.PointerAddress))
	require.True(t, found)
	require.Equal(t, common.BytesToAddress(sdk.MustAccAddressFromBech32(res.PointerAddress)), associatedEvmAddr)
	associatedSeiAddr, found := k.GetSeiAddress(ctx, associatedEvmAddr)
	require.True(t, found)
	require.Equal(t, res.PointerAddress, associatedSeiAddr.String())
	// setting for an associated address would fail
	_, err = msgServer.AssociateContractAddress(sdk.WrapSDKContext(ctx), &types.MsgAssociateContractAddress{
		Sender:  dummySeiAddr.String(),
		Address: res.PointerAddress,
	})
	require.NotNil(t, err)
	require.Contains(t, err.Error(), "contract already has an associated address")
	// setting for a non-contract would fail
	_, err = msgServer.AssociateContractAddress(sdk.WrapSDKContext(ctx), &types.MsgAssociateContractAddress{
		Sender:  dummySeiAddr.String(),
		Address: dummySeiAddr.String(),
	})
	require.NotNil(t, err)
	require.Contains(t, err.Error(), "no wasm contract found at the given address")
}

func TestAssociate(t *testing.T) {
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx([]byte{}).WithChainID("sei-test").WithBlockHeight(1)
	privKey := testkeeper.MockPrivateKey()
	seiAddr, evmAddr := testkeeper.PrivateKeyToAddresses(privKey)
	acc := testkeeper.EVMTestApp.AccountKeeper.NewAccountWithAddress(ctx, seiAddr)
	testkeeper.EVMTestApp.AccountKeeper.SetAccount(ctx, acc)
	msg := types.NewMsgAssociate(seiAddr, "test")
	tb := testkeeper.EVMTestApp.GetTxConfig().NewTxBuilder()
	tb.SetMsgs(msg)
	tb.SetSignatures(signing.SignatureV2{
		PubKey: privKey.PubKey(),
		Data: &signing.SingleSignatureData{
			SignMode:  testkeeper.EVMTestApp.GetTxConfig().SignModeHandler().DefaultMode(),
			Signature: nil,
		},
		Sequence: acc.GetSequence(),
	})
	signerData := authsigning.SignerData{
		ChainID:       "sei-test",
		AccountNumber: acc.GetAccountNumber(),
		Sequence:      acc.GetSequence(),
	}
	signBytes, err := testkeeper.EVMTestApp.GetTxConfig().SignModeHandler().GetSignBytes(testkeeper.EVMTestApp.GetTxConfig().SignModeHandler().DefaultMode(), signerData, tb.GetTx())
	require.Nil(t, err)
	sig, err := privKey.Sign(signBytes)
	require.Nil(t, err)
	sigs := make([]signing.SignatureV2, 1)
	sigs[0] = signing.SignatureV2{
		PubKey: privKey.PubKey(),
		Data: &signing.SingleSignatureData{
			SignMode:  testkeeper.EVMTestApp.GetTxConfig().SignModeHandler().DefaultMode(),
			Signature: sig,
		},
		Sequence: acc.GetSequence(),
	}
	require.Nil(t, tb.SetSignatures(sigs...))
	sdktx := tb.GetTx()
	txbz, err := testkeeper.EVMTestApp.GetTxConfig().TxEncoder()(sdktx)
	require.Nil(t, err)

	res := testkeeper.EVMTestApp.DeliverTx(ctx, abci.RequestDeliverTx{Tx: txbz}, sdktx, sha256.Sum256(txbz))
	require.NotEqual(t, uint32(0), res.Code) // not enough balance

	require.Nil(t, testkeeper.EVMTestApp.BankKeeper.AddWei(ctx, sdk.AccAddress(evmAddr[:]), sdk.OneInt()))

	res = testkeeper.EVMTestApp.DeliverTx(ctx, abci.RequestDeliverTx{Tx: txbz}, sdktx, sha256.Sum256(txbz))
	require.Equal(t, uint32(0), res.Code)
}
