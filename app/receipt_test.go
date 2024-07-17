package app_test

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/big"
	"os"
	"testing"
	"time"

	wasmtypes "github.com/CosmWasm/wasmd/x/wasm/types"
	"github.com/cosmos/cosmos-sdk/client"
	clienttx "github.com/cosmos/cosmos-sdk/client/tx"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	xauthsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/sei-protocol/sei-chain/precompiles/wasmd"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/sei-protocol/sei-chain/x/evm/types/ethtx"
	"github.com/stretchr/testify/require"
	abci "github.com/tendermint/tendermint/abci/types"
)

func TestEvmEventsForCw20(t *testing.T) {
	k := testkeeper.EVMTestApp.EvmKeeper
	wasmKeeper := k.WasmKeeper()
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx([]byte{}).WithBlockTime(time.Now()).WithChainID("sei-test").WithBlockHeight(1)
	code, err := os.ReadFile("../contracts/wasm/cw20_base.wasm")
	require.Nil(t, err)
	privKey := testkeeper.MockPrivateKey()
	creator, _ := testkeeper.PrivateKeyToAddresses(privKey)
	codeID, err := wasmKeeper.Create(ctx, creator, code, nil)
	require.Nil(t, err)
	contractAddr, _, err := wasmKeeper.Instantiate(ctx, codeID, creator, creator, []byte(fmt.Sprintf("{\"name\":\"test\",\"symbol\":\"test\",\"decimals\":6,\"initial_balances\":[{\"address\":\"%s\",\"amount\":\"1000000000\"}]}", creator.String())), "test", sdk.NewCoins())
	require.Nil(t, err)

	_, mockPointerAddr := testkeeper.MockAddressPair()
	k.SetERC20CW20Pointer(ctx, contractAddr.String(), mockPointerAddr)

	// calling CW contract directly
	amt := sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(1000000000000)))
	k.BankKeeper().MintCoins(ctx, "evm", amt)
	k.BankKeeper().SendCoinsFromModuleToAccount(ctx, "evm", creator, amt)
	recipient, _ := testkeeper.MockAddressPair()
	payload := []byte(fmt.Sprintf("{\"transfer\":{\"recipient\":\"%s\",\"amount\":\"100\"}}", recipient.String()))
	msg := &wasmtypes.MsgExecuteContract{
		Sender:   creator.String(),
		Contract: contractAddr.String(),
		Msg:      payload,
	}
	txBuilder := testkeeper.EVMTestApp.GetTxConfig().NewTxBuilder()
	txBuilder.SetMsgs(msg)
	txBuilder.SetFeeAmount(sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(1000000))))
	txBuilder.SetGasLimit(300000)
	tx := signTx(txBuilder, privKey, k.AccountKeeper().GetAccount(ctx, creator))
	txbz, err := testkeeper.EVMTestApp.GetTxConfig().TxEncoder()(tx)
	require.Nil(t, err)
	sum := sha256.Sum256(txbz)
	res := testkeeper.EVMTestApp.DeliverTx(ctx.WithEventManager(sdk.NewEventManager()), abci.RequestDeliverTx{Tx: txbz}, tx, sum)
	require.Equal(t, uint32(0), res.Code)
	receipt, err := testkeeper.EVMTestApp.EvmKeeper.GetTransientReceipt(ctx, common.BytesToHash(sum[:]))
	require.Nil(t, err)
	require.Equal(t, 1, len(receipt.Logs))
	require.NotEmpty(t, receipt.LogsBloom)
	require.Equal(t, mockPointerAddr.Hex(), receipt.Logs[0].Address)
	_, found := testkeeper.EVMTestApp.EvmKeeper.GetEVMTxDeferredInfo(ctx)
	require.True(t, found)

	// calling from wasmd precompile
	abi, err := wasmd.GetABI()
	require.Nil(t, err)
	emptyCoins, err := sdk.NewCoins().MarshalJSON()
	require.Nil(t, err)
	data, err := abi.Pack("execute", contractAddr.String(), payload, emptyCoins)
	require.Nil(t, err)
	wasmAddr := common.HexToAddress(wasmd.WasmdAddress)
	txData := ethtypes.LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(1000000000),
		Gas:      1000000,
		To:       &wasmAddr,
		Data:     data,
	}
	chainID := k.ChainID(ctx)
	chainCfg := evmtypes.DefaultChainConfig()
	ethCfg := chainCfg.EthereumConfig(chainID)
	blockNum := big.NewInt(ctx.BlockHeight())
	signer := ethtypes.MakeSigner(ethCfg, blockNum, uint64(ctx.BlockTime().Unix()))
	testPrivHex := hex.EncodeToString(privKey.Bytes())
	key, _ := crypto.HexToECDSA(testPrivHex)
	signedTx, err := ethtypes.SignTx(ethtypes.NewTx(&txData), signer, key)
	require.Nil(t, err)
	typedTx, err := ethtx.NewLegacyTx(signedTx)
	require.Nil(t, err)
	emsg, err := evmtypes.NewMsgEVMTransaction(typedTx)
	require.Nil(t, err)
	txBuilder = testkeeper.EVMTestApp.GetTxConfig().NewTxBuilder()
	txBuilder.SetMsgs(emsg)
	tx = txBuilder.GetTx()
	txbz, err = testkeeper.EVMTestApp.GetTxConfig().TxEncoder()(tx)
	require.Nil(t, err)
	res = testkeeper.EVMTestApp.DeliverTx(ctx.WithEventManager(sdk.NewEventManager()).WithTxIndex(1), abci.RequestDeliverTx{Tx: txbz}, tx, sum)
	require.Equal(t, uint32(0), res.Code)
	receipt, err = testkeeper.EVMTestApp.EvmKeeper.GetTransientReceipt(ctx, signedTx.Hash())
	require.Nil(t, err)
	require.Equal(t, 1, len(receipt.Logs))
	require.NotEmpty(t, receipt.LogsBloom)
	require.Equal(t, mockPointerAddr.Hex(), receipt.Logs[0].Address)
	_, found = testkeeper.EVMTestApp.EvmKeeper.GetEVMTxDeferredInfo(ctx)
	require.True(t, found)

	// test approval message
	payload = []byte(fmt.Sprintf("{\"increase_allowance\":{\"spender\":\"%s\",\"amount\":\"100\"}}", recipient.String()))
	msg = &wasmtypes.MsgExecuteContract{
		Sender:   creator.String(),
		Contract: contractAddr.String(),
		Msg:      payload,
	}
	txBuilder = testkeeper.EVMTestApp.GetTxConfig().NewTxBuilder()
	txBuilder.SetMsgs(msg)
	txBuilder.SetFeeAmount(sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(1000000))))
	txBuilder.SetGasLimit(300000)
	tx = signTx(txBuilder, privKey, k.AccountKeeper().GetAccount(ctx, creator))
	txbz, err = testkeeper.EVMTestApp.GetTxConfig().TxEncoder()(tx)
	require.Nil(t, err)
	sum = sha256.Sum256(txbz)
	res = testkeeper.EVMTestApp.DeliverTx(ctx.WithEventManager(sdk.NewEventManager()), abci.RequestDeliverTx{Tx: txbz}, tx, sum)
	require.Equal(t, uint32(0), res.Code)
	receipt, err = testkeeper.EVMTestApp.EvmKeeper.GetTransientReceipt(ctx, common.BytesToHash(sum[:]))
	require.Nil(t, err)
	require.Equal(t, 1, len(receipt.Logs))
	require.NotEmpty(t, receipt.LogsBloom)
	require.Equal(t, mockPointerAddr.Hex(), receipt.Logs[0].Address)
	_, found = testkeeper.EVMTestApp.EvmKeeper.GetEVMTxDeferredInfo(ctx)
	require.True(t, found)
	require.Equal(t, common.HexToHash("0x64").Bytes(), receipt.Logs[0].Data)
}

func TestEvmEventsForCw721(t *testing.T) {
	k := testkeeper.EVMTestApp.EvmKeeper
	wasmKeeper := k.WasmKeeper()
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx([]byte{}).WithBlockTime(time.Now()).WithChainID("sei-test").WithBlockHeight(1)
	code, err := os.ReadFile("../contracts/wasm/cw721_base.wasm")
	require.Nil(t, err)
	privKey := testkeeper.MockPrivateKey()
	creator, _ := testkeeper.PrivateKeyToAddresses(privKey)
	codeID, err := wasmKeeper.Create(ctx, creator, code, nil)
	require.Nil(t, err)
	contractAddr, _, err := wasmKeeper.Instantiate(ctx, codeID, creator, creator, []byte(fmt.Sprintf("{\"name\":\"test\",\"symbol\":\"test\",\"minter\":\"%s\"}", creator.String())), "test", sdk.NewCoins())
	require.Nil(t, err)

	_, mockPointerAddr := testkeeper.MockAddressPair()
	k.SetERC721CW721Pointer(ctx, contractAddr.String(), mockPointerAddr)

	// calling CW contract directly
	amt := sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(1000000000000)))
	k.BankKeeper().MintCoins(ctx, "evm", amt)
	k.BankKeeper().SendCoinsFromModuleToAccount(ctx, "evm", creator, amt)
	recipient, _ := testkeeper.MockAddressPair()
	payload := []byte(fmt.Sprintf("{\"mint\":{\"token_id\":\"1\",\"owner\":\"%s\"}}", recipient.String()))
	msg := &wasmtypes.MsgExecuteContract{
		Sender:   creator.String(),
		Contract: contractAddr.String(),
		Msg:      payload,
	}
	txBuilder := testkeeper.EVMTestApp.GetTxConfig().NewTxBuilder()
	txBuilder.SetMsgs(msg)
	txBuilder.SetFeeAmount(sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(1000000))))
	txBuilder.SetGasLimit(300000)
	tx := signTx(txBuilder, privKey, k.AccountKeeper().GetAccount(ctx, creator))
	txbz, err := testkeeper.EVMTestApp.GetTxConfig().TxEncoder()(tx)
	require.Nil(t, err)
	sum := sha256.Sum256(txbz)
	res := testkeeper.EVMTestApp.DeliverTx(ctx.WithEventManager(sdk.NewEventManager()), abci.RequestDeliverTx{Tx: txbz}, tx, sum)
	require.Equal(t, uint32(0), res.Code)
	receipt, err := testkeeper.EVMTestApp.EvmKeeper.GetTransientReceipt(ctx, common.BytesToHash(sum[:]))
	require.Nil(t, err)
	require.Equal(t, 1, len(receipt.Logs))
	require.NotEmpty(t, receipt.LogsBloom)
	require.Equal(t, mockPointerAddr.Hex(), receipt.Logs[0].Address)
	_, found := testkeeper.EVMTestApp.EvmKeeper.GetEVMTxDeferredInfo(ctx)
	require.True(t, found)

	// calling from wasmd precompile
	abi, err := wasmd.GetABI()
	require.Nil(t, err)
	emptyCoins, err := sdk.NewCoins().MarshalJSON()
	require.Nil(t, err)
	payload = []byte(fmt.Sprintf("{\"mint\":{\"token_id\":\"2\",\"owner\":\"%s\"}}", creator.String()))
	data, err := abi.Pack("execute", contractAddr.String(), payload, emptyCoins)
	require.Nil(t, err)
	wasmAddr := common.HexToAddress(wasmd.WasmdAddress)
	txData := ethtypes.LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(1000000000),
		Gas:      1000000,
		To:       &wasmAddr,
		Data:     data,
	}
	chainID := k.ChainID(ctx)
	chainCfg := evmtypes.DefaultChainConfig()
	ethCfg := chainCfg.EthereumConfig(chainID)
	blockNum := big.NewInt(ctx.BlockHeight())
	signer := ethtypes.MakeSigner(ethCfg, blockNum, uint64(ctx.BlockTime().Unix()))
	testPrivHex := hex.EncodeToString(privKey.Bytes())
	key, _ := crypto.HexToECDSA(testPrivHex)
	signedTx, err := ethtypes.SignTx(ethtypes.NewTx(&txData), signer, key)
	require.Nil(t, err)
	typedTx, err := ethtx.NewLegacyTx(signedTx)
	require.Nil(t, err)
	emsg, err := evmtypes.NewMsgEVMTransaction(typedTx)
	require.Nil(t, err)
	txBuilder = testkeeper.EVMTestApp.GetTxConfig().NewTxBuilder()
	txBuilder.SetMsgs(emsg)
	tx = txBuilder.GetTx()
	txbz, err = testkeeper.EVMTestApp.GetTxConfig().TxEncoder()(tx)
	require.Nil(t, err)
	res = testkeeper.EVMTestApp.DeliverTx(ctx.WithEventManager(sdk.NewEventManager()).WithTxIndex(1), abci.RequestDeliverTx{Tx: txbz}, tx, sum)
	require.Equal(t, uint32(0), res.Code)
	receipt, err = testkeeper.EVMTestApp.EvmKeeper.GetTransientReceipt(ctx, signedTx.Hash())
	require.Nil(t, err)
	require.Equal(t, 1, len(receipt.Logs))
	require.NotEmpty(t, receipt.LogsBloom)
	require.Equal(t, mockPointerAddr.Hex(), receipt.Logs[0].Address)
	_, found = testkeeper.EVMTestApp.EvmKeeper.GetEVMTxDeferredInfo(ctx)
	require.True(t, found)

	// test approval message
	payload = []byte(fmt.Sprintf("{\"approve\":{\"spender\":\"%s\",\"token_id\":\"2\"}}", recipient.String()))
	msg = &wasmtypes.MsgExecuteContract{
		Sender:   creator.String(),
		Contract: contractAddr.String(),
		Msg:      payload,
	}
	txBuilder = testkeeper.EVMTestApp.GetTxConfig().NewTxBuilder()
	txBuilder.SetMsgs(msg)
	txBuilder.SetFeeAmount(sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(1000000))))
	txBuilder.SetGasLimit(300000)
	tx = signTx(txBuilder, privKey, k.AccountKeeper().GetAccount(ctx, creator))
	txbz, err = testkeeper.EVMTestApp.GetTxConfig().TxEncoder()(tx)
	require.Nil(t, err)
	sum = sha256.Sum256(txbz)
	res = testkeeper.EVMTestApp.DeliverTx(ctx.WithEventManager(sdk.NewEventManager()), abci.RequestDeliverTx{Tx: txbz}, tx, sum)
	require.Equal(t, uint32(0), res.Code)
	receipt, err = testkeeper.EVMTestApp.EvmKeeper.GetTransientReceipt(ctx, common.BytesToHash(sum[:]))
	require.Nil(t, err)
	require.Equal(t, 1, len(receipt.Logs))
	require.NotEmpty(t, receipt.LogsBloom)
	require.Equal(t, mockPointerAddr.Hex(), receipt.Logs[0].Address)
	_, found = testkeeper.EVMTestApp.EvmKeeper.GetEVMTxDeferredInfo(ctx)
	require.True(t, found)
	require.Equal(t, common.HexToHash("0x2").Bytes(), receipt.Logs[0].Data)

	// revoke
	payload = []byte(fmt.Sprintf("{\"revoke\":{\"spender\":\"%s\",\"token_id\":\"2\"}}", recipient.String()))
	msg = &wasmtypes.MsgExecuteContract{
		Sender:   creator.String(),
		Contract: contractAddr.String(),
		Msg:      payload,
	}
	txBuilder = testkeeper.EVMTestApp.GetTxConfig().NewTxBuilder()
	txBuilder.SetMsgs(msg)
	txBuilder.SetFeeAmount(sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(1000000))))
	txBuilder.SetGasLimit(300000)
	tx = signTx(txBuilder, privKey, k.AccountKeeper().GetAccount(ctx, creator))
	txbz, err = testkeeper.EVMTestApp.GetTxConfig().TxEncoder()(tx)
	require.Nil(t, err)
	sum = sha256.Sum256(txbz)
	res = testkeeper.EVMTestApp.DeliverTx(ctx.WithEventManager(sdk.NewEventManager()), abci.RequestDeliverTx{Tx: txbz}, tx, sum)
	require.Equal(t, uint32(0), res.Code)
	receipt, err = testkeeper.EVMTestApp.EvmKeeper.GetTransientReceipt(ctx, common.BytesToHash(sum[:]))
	require.Nil(t, err)
	require.Equal(t, 1, len(receipt.Logs))
	require.NotEmpty(t, receipt.LogsBloom)
	require.Equal(t, mockPointerAddr.Hex(), receipt.Logs[0].Address)
	_, found = testkeeper.EVMTestApp.EvmKeeper.GetEVMTxDeferredInfo(ctx)
	require.True(t, found)
	require.Equal(t, common.HexToHash("0x2").Bytes(), receipt.Logs[0].Data)

	// approve all
	payload = []byte(fmt.Sprintf("{\"approve_all\":{\"operator\":\"%s\"}}", recipient.String()))
	msg = &wasmtypes.MsgExecuteContract{
		Sender:   creator.String(),
		Contract: contractAddr.String(),
		Msg:      payload,
	}
	txBuilder = testkeeper.EVMTestApp.GetTxConfig().NewTxBuilder()
	txBuilder.SetMsgs(msg)
	txBuilder.SetFeeAmount(sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(1000000))))
	txBuilder.SetGasLimit(300000)
	tx = signTx(txBuilder, privKey, k.AccountKeeper().GetAccount(ctx, creator))
	txbz, err = testkeeper.EVMTestApp.GetTxConfig().TxEncoder()(tx)
	require.Nil(t, err)
	sum = sha256.Sum256(txbz)
	res = testkeeper.EVMTestApp.DeliverTx(ctx.WithEventManager(sdk.NewEventManager()), abci.RequestDeliverTx{Tx: txbz}, tx, sum)
	require.Equal(t, uint32(0), res.Code)
	receipt, err = testkeeper.EVMTestApp.EvmKeeper.GetTransientReceipt(ctx, common.BytesToHash(sum[:]))
	require.Nil(t, err)
	require.Equal(t, 1, len(receipt.Logs))
	require.NotEmpty(t, receipt.LogsBloom)
	require.Equal(t, mockPointerAddr.Hex(), receipt.Logs[0].Address)
	_, found = testkeeper.EVMTestApp.EvmKeeper.GetEVMTxDeferredInfo(ctx)
	require.True(t, found)
	require.Equal(t, common.HexToHash("0x1").Bytes(), receipt.Logs[0].Data)

	// revoke all
	payload = []byte(fmt.Sprintf("{\"revoke_all\":{\"operator\":\"%s\"}}", recipient.String()))
	msg = &wasmtypes.MsgExecuteContract{
		Sender:   creator.String(),
		Contract: contractAddr.String(),
		Msg:      payload,
	}
	txBuilder = testkeeper.EVMTestApp.GetTxConfig().NewTxBuilder()
	txBuilder.SetMsgs(msg)
	txBuilder.SetFeeAmount(sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(1000000))))
	txBuilder.SetGasLimit(300000)
	tx = signTx(txBuilder, privKey, k.AccountKeeper().GetAccount(ctx, creator))
	txbz, err = testkeeper.EVMTestApp.GetTxConfig().TxEncoder()(tx)
	require.Nil(t, err)
	sum = sha256.Sum256(txbz)
	res = testkeeper.EVMTestApp.DeliverTx(ctx.WithEventManager(sdk.NewEventManager()), abci.RequestDeliverTx{Tx: txbz}, tx, sum)
	require.Equal(t, uint32(0), res.Code)
	receipt, err = testkeeper.EVMTestApp.EvmKeeper.GetTransientReceipt(ctx, common.BytesToHash(sum[:]))
	require.Nil(t, err)
	require.Equal(t, 1, len(receipt.Logs))
	require.NotEmpty(t, receipt.LogsBloom)
	require.Equal(t, mockPointerAddr.Hex(), receipt.Logs[0].Address)
	_, found = testkeeper.EVMTestApp.EvmKeeper.GetEVMTxDeferredInfo(ctx)
	require.True(t, found)
	require.Equal(t, common.HexToHash("0x0").Bytes(), receipt.Logs[0].Data)

	// burn
	payload = []byte("{\"burn\":{\"token_id\":\"2\"}}")
	msg = &wasmtypes.MsgExecuteContract{
		Sender:   creator.String(),
		Contract: contractAddr.String(),
		Msg:      payload,
	}
	txBuilder = testkeeper.EVMTestApp.GetTxConfig().NewTxBuilder()
	txBuilder.SetMsgs(msg)
	txBuilder.SetFeeAmount(sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(1000000))))
	txBuilder.SetGasLimit(300000)
	tx = signTx(txBuilder, privKey, k.AccountKeeper().GetAccount(ctx, creator))
	txbz, err = testkeeper.EVMTestApp.GetTxConfig().TxEncoder()(tx)
	require.Nil(t, err)
	sum = sha256.Sum256(txbz)
	res = testkeeper.EVMTestApp.DeliverTx(ctx.WithEventManager(sdk.NewEventManager()), abci.RequestDeliverTx{Tx: txbz}, tx, sum)
	require.Equal(t, uint32(0), res.Code)
	receipt, err = testkeeper.EVMTestApp.EvmKeeper.GetTransientReceipt(ctx, common.BytesToHash(sum[:]))
	require.Nil(t, err)
	require.Equal(t, 1, len(receipt.Logs))
	require.NotEmpty(t, receipt.LogsBloom)
	require.Equal(t, mockPointerAddr.Hex(), receipt.Logs[0].Address)
	_, found = testkeeper.EVMTestApp.EvmKeeper.GetEVMTxDeferredInfo(ctx)
	require.True(t, found)
	require.Equal(t, common.HexToHash("0x2").Bytes(), receipt.Logs[0].Data)
}

func signTx(txBuilder client.TxBuilder, privKey cryptotypes.PrivKey, acc authtypes.AccountI) sdk.Tx {
	var sigsV2 []signing.SignatureV2
	sigV2 := signing.SignatureV2{
		PubKey: privKey.PubKey(),
		Data: &signing.SingleSignatureData{
			SignMode:  testkeeper.EVMTestApp.GetTxConfig().SignModeHandler().DefaultMode(),
			Signature: nil,
		},
		Sequence: acc.GetSequence(),
	}
	sigsV2 = append(sigsV2, sigV2)
	_ = txBuilder.SetSignatures(sigsV2...)
	sigsV2 = []signing.SignatureV2{}
	signerData := xauthsigning.SignerData{
		ChainID:       "sei-test",
		AccountNumber: acc.GetAccountNumber(),
		Sequence:      acc.GetSequence(),
	}
	sigV2, _ = clienttx.SignWithPrivKey(
		testkeeper.EVMTestApp.GetTxConfig().SignModeHandler().DefaultMode(),
		signerData,
		txBuilder,
		privKey,
		testkeeper.EVMTestApp.GetTxConfig(),
		acc.GetSequence(),
	)
	sigsV2 = append(sigsV2, sigV2)
	_ = txBuilder.SetSignatures(sigsV2...)
	return txBuilder.GetTx()
}
