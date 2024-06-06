package evm_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	ethabi "github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/sei-protocol/sei-chain/precompiles/pointer"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/artifacts/cw721"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/sei-protocol/sei-chain/x/evm/types/ethtx"
	"github.com/stretchr/testify/require"
	abci "github.com/tendermint/tendermint/abci/types"
)

func TestERC2981PointerToCW2981(t *testing.T) {
	k := testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx([]byte{}).WithBlockTime(time.Now())
	adminSeiAddr, adminEvmAddr := testkeeper.MockAddressPair()
	k.SetAddressMapping(ctx, adminSeiAddr, adminEvmAddr)
	// deploy cw2981
	bz, err := os.ReadFile("../../contracts/wasm/cw2981_royalties.wasm")
	if err != nil {
		panic(err)
	}
	codeID, err := k.WasmKeeper().Create(ctx, adminSeiAddr, bz, nil)
	require.Nil(t, err)
	instantiateMsg, err := json.Marshal(map[string]interface{}{"name": "test", "symbol": "TEST", "minter": adminSeiAddr.String()})
	require.Nil(t, err)
	cw2981Addr, _, err := k.WasmKeeper().Instantiate(ctx, codeID, adminSeiAddr, adminSeiAddr, instantiateMsg, "cw2981", sdk.NewCoins())
	require.Nil(t, err)
	require.NotEmpty(t, cw2981Addr)
	// mint a NFT and set royalty info to 1%
	executeMsg, err := json.Marshal(map[string]interface{}{
		"mint": map[string]interface{}{
			"token_id": "1",
			"owner":    adminSeiAddr.String(),
			"extension": map[string]interface{}{
				"royalty_percentage":      1,
				"royalty_payment_address": adminSeiAddr.String(),
			},
		},
	})
	require.Nil(t, err)
	_, err = k.WasmKeeper().Execute(ctx, cw2981Addr, adminSeiAddr, executeMsg, sdk.NewCoins())
	require.Nil(t, err)
	// deploy pointer to cw2981
	privKey := testkeeper.MockPrivateKey()
	seiAddr, evmAddr := testkeeper.PrivateKeyToAddresses(privKey)
	k.SetAddressMapping(ctx, seiAddr, evmAddr)
	require.Nil(t, k.BankKeeper().AddCoins(ctx, seiAddr, sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(10000000))), true))
	testPrivHex := hex.EncodeToString(privKey.Bytes())
	key, _ := crypto.HexToECDSA(testPrivHex)
	to := common.HexToAddress(pointer.PointerAddress)
	abi, err := pointer.ABI()
	require.Nil(t, err)
	data, err := abi.Pack("addCW721Pointer", cw2981Addr.String())
	require.Nil(t, err)
	txData := ethtypes.LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(1000000000),
		Gas:      5000000,
		To:       &to,
		Data:     data,
	}
	chainID := k.ChainID(ctx)
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
	txBuilder := testkeeper.EVMTestApp.GetTxConfig().NewTxBuilder()
	txBuilder.SetMsgs(msg)
	cosmosTx := txBuilder.GetTx()
	txbz, err := testkeeper.EVMTestApp.GetTxConfig().TxEncoder()(cosmosTx)
	require.Nil(t, err)
	res := testkeeper.EVMTestApp.DeliverTx(ctx, abci.RequestDeliverTx{Tx: txbz}, cosmosTx, sha256.Sum256(txbz))
	require.Equal(t, uint32(0), res.Code)
	pointerAddr, _, exists := k.GetERC721CW721Pointer(ctx, cw2981Addr.String())
	require.True(t, exists)
	require.NotEmpty(t, pointerAddr)
	// call pointer to get royalty info
	abi, err = cw721.Cw721MetaData.GetAbi()
	require.Nil(t, err)
	data, err = abi.Pack("royaltyInfo", big.NewInt(1), big.NewInt(1000))
	require.Nil(t, err)
	txData = ethtypes.LegacyTx{
		Nonce:    1,
		GasPrice: big.NewInt(1000000000),
		Gas:      1000000,
		To:       &pointerAddr,
		Data:     data,
	}
	tx, err = ethtypes.SignTx(ethtypes.NewTx(&txData), signer, key)
	require.Nil(t, err)
	typedTx, err = ethtx.NewLegacyTx(tx)
	require.Nil(t, err)
	msg, err = types.NewMsgEVMTransaction(typedTx)
	require.Nil(t, err)
	txBuilder = testkeeper.EVMTestApp.GetTxConfig().NewTxBuilder()
	txBuilder.SetMsgs(msg)
	cosmosTx = txBuilder.GetTx()
	txbz, err = testkeeper.EVMTestApp.GetTxConfig().TxEncoder()(cosmosTx)
	require.Nil(t, err)
	res = testkeeper.EVMTestApp.DeliverTx(ctx, abci.RequestDeliverTx{Tx: txbz}, cosmosTx, sha256.Sum256(txbz))
	require.Equal(t, uint32(0), res.Code)
	typedTxData := sdk.TxMsgData{}
	require.Nil(t, typedTxData.Unmarshal(res.Data))
	typedMsgData := types.MsgEVMTransactionResponse{}
	require.Nil(t, typedMsgData.Unmarshal(typedTxData.Data[0].Data))
	ret, err := abi.Unpack("royaltyInfo", typedMsgData.ReturnData)
	require.Nil(t, err)
	require.Equal(t, big.NewInt(10), ret[1].(*big.Int))
	require.Equal(t, adminEvmAddr.Hex(), ret[0].(common.Address).Hex())
}

func TestCW2981PointerToERC2981(t *testing.T) {
	k := testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx([]byte{}).WithBlockTime(time.Now())
	// deploy erc2981
	privKey := testkeeper.MockPrivateKey()
	seiAddr, evmAddr := testkeeper.PrivateKeyToAddresses(privKey)
	k.SetAddressMapping(ctx, seiAddr, evmAddr)
	require.Nil(t, k.BankKeeper().AddCoins(ctx, seiAddr, sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(10000000))), true))
	testPrivHex := hex.EncodeToString(privKey.Bytes())
	key, _ := crypto.HexToECDSA(testPrivHex)
	abiBz, err := os.ReadFile("../../example/contracts/erc2981/ERC2981Example.abi")
	require.Nil(t, err)
	abi, err := ethabi.JSON(bytes.NewReader(abiBz))
	require.Nil(t, err)
	code, err := os.ReadFile("../../example/contracts/erc2981/ERC2981Example.bin")
	require.Nil(t, err)
	bz, err := hex.DecodeString(string(code))
	require.Nil(t, err)
	data, err := abi.Pack("", "test", "TEST")
	require.Nil(t, err)
	txData := ethtypes.LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(1000000000),
		Gas:      5000000,
		To:       nil,
		Data:     append(bz, data...),
	}
	chainID := k.ChainID(ctx)
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
	txBuilder := testkeeper.EVMTestApp.GetTxConfig().NewTxBuilder()
	txBuilder.SetMsgs(msg)
	cosmosTx := txBuilder.GetTx()
	txbz, err := testkeeper.EVMTestApp.GetTxConfig().TxEncoder()(cosmosTx)
	require.Nil(t, err)
	res := testkeeper.EVMTestApp.DeliverTx(ctx, abci.RequestDeliverTx{Tx: txbz}, cosmosTx, sha256.Sum256(txbz))
	require.Equal(t, uint32(0), res.Code)
	err = k.FlushTransientReceipts(ctx)
	require.NoError(t, err)
	receipt, err := k.GetReceipt(ctx, tx.Hash())
	require.Nil(t, err)
	require.NotEmpty(t, receipt.ContractAddress)
	require.Empty(t, receipt.VmError)
	// set royalty
	data, err = abi.Pack("setDefaultRoyalty", evmAddr)
	require.Nil(t, err)
	to := common.HexToAddress(receipt.ContractAddress)
	txData = ethtypes.LegacyTx{
		Nonce:    1,
		GasPrice: big.NewInt(1000000000),
		Gas:      1000000,
		To:       &to,
		Data:     data,
	}
	tx, err = ethtypes.SignTx(ethtypes.NewTx(&txData), signer, key)
	require.Nil(t, err)
	typedTx, err = ethtx.NewLegacyTx(tx)
	require.Nil(t, err)
	msg, err = types.NewMsgEVMTransaction(typedTx)
	require.Nil(t, err)
	txBuilder = testkeeper.EVMTestApp.GetTxConfig().NewTxBuilder()
	txBuilder.SetMsgs(msg)
	cosmosTx = txBuilder.GetTx()
	txbz, err = testkeeper.EVMTestApp.GetTxConfig().TxEncoder()(cosmosTx)
	require.Nil(t, err)
	res = testkeeper.EVMTestApp.DeliverTx(ctx, abci.RequestDeliverTx{Tx: txbz}, cosmosTx, sha256.Sum256(txbz))
	require.Equal(t, uint32(0), res.Code)
	// deploy CW->ERC pointer
	res2, err := keeper.NewMsgServerImpl(&k).RegisterPointer(sdk.WrapSDKContext(ctx), &types.MsgRegisterPointer{
		Sender:      seiAddr.String(),
		PointerType: types.PointerType_ERC721,
		ErcAddress:  receipt.ContractAddress,
	})
	require.Nil(t, err)
	require.NotEmpty(t, res2.PointerAddress)
	// call pointer to get royalty info
	query, err := json.Marshal(map[string]interface{}{
		"extension": map[string]interface{}{
			"msg": map[string]interface{}{
				"check_royalties": map[string]interface{}{},
			},
		},
	})
	require.Nil(t, err)
	ret, err := testkeeper.EVMTestApp.WasmKeeper.QuerySmart(ctx, sdk.MustAccAddressFromBech32(res2.PointerAddress), query)
	require.Nil(t, err)
	require.Equal(t, "{\"royalty_payments\":true}", string(ret))
	query, err = json.Marshal(map[string]interface{}{
		"extension": map[string]interface{}{
			"msg": map[string]interface{}{
				"royalty_info": map[string]interface{}{
					"token_id":   "1",
					"sale_price": "1000",
				},
			},
		},
	})
	require.Nil(t, err)
	ret, err = testkeeper.EVMTestApp.WasmKeeper.QuerySmart(ctx, sdk.MustAccAddressFromBech32(res2.PointerAddress), query)
	require.Nil(t, err)
	require.Equal(t, fmt.Sprintf("{\"address\":\"%s\",\"royalty_amount\":\"1000\"}", seiAddr.String()), string(ret))
}
