package app_test

import (
	"crypto/sha256"
	"embed"
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
	eabi "github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	pcommon "github.com/sei-protocol/sei-chain/precompiles/common"
	"github.com/sei-protocol/sei-chain/precompiles/wasmd"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/artifacts/cw1155"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/sei-protocol/sei-chain/x/evm/types/ethtx"
	"github.com/stretchr/testify/require"
	abci "github.com/tendermint/tendermint/abci/types"
)

//go:embed wasm_abi.json
var f embed.FS

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
	abi := pcommon.MustGetABI(f, "wasm_abi.json")
	emptyCoins, err := sdk.NewCoins().MarshalJSON()
	require.Nil(t, err)
	data, err := abi.Pack("execute", contractAddr.String(), payload, emptyCoins)
	require.Nil(t, err)
	wasmAddr := common.HexToAddress(wasmd.WasmdAddress)
	txData := ethtypes.LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(100000000000),
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
	sum = sha256.Sum256(txbz)
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
	privKeyRecipient := testkeeper.MockPrivateKey()
	recipient, _ := testkeeper.PrivateKeyToAddresses(privKeyRecipient)
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
	abi := pcommon.MustGetABI(f, "wasm_abi.json")
	emptyCoins, err := sdk.NewCoins().MarshalJSON()
	require.Nil(t, err)
	payload = []byte(fmt.Sprintf("{\"mint\":{\"token_id\":\"2\",\"owner\":\"%s\"}}", creator.String()))
	data, err := abi.Pack("execute", contractAddr.String(), payload, emptyCoins)
	require.Nil(t, err)
	wasmAddr := common.HexToAddress(wasmd.WasmdAddress)
	txData := ethtypes.LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(333000000000),
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
	sum = sha256.Sum256(txbz)
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
	require.Equal(t, uint32(0), receipt.Logs[0].Index)
	tokenIdHash := receipt.Logs[0].Topics[3]
	require.Equal(t, "0x0000000000000000000000000000000000000000000000000000000000000002", tokenIdHash)
	_, found = testkeeper.EVMTestApp.EvmKeeper.GetEVMTxDeferredInfo(ctx)
	require.True(t, found)
	require.Equal(t, common.HexToHash("0x0").Bytes(), receipt.Logs[0].Data)

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
	tokenIdHash = receipt.Logs[0].Topics[3]
	require.Equal(t, "0x0000000000000000000000000000000000000000000000000000000000000002", tokenIdHash)
	require.Equal(t, common.HexToHash("0x0").Bytes(), receipt.Logs[0].Data)

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

	// transfer on behalf
	k.BankKeeper().MintCoins(ctx, "evm", amt)
	k.BankKeeper().SendCoinsFromModuleToAccount(ctx, "evm", recipient, amt)
	payload = []byte(fmt.Sprintf("{\"transfer_nft\":{\"token_id\":\"2\",\"recipient\":\"%s\"}}", recipient.String()))
	msg = &wasmtypes.MsgExecuteContract{
		Sender:   recipient.String(),
		Contract: contractAddr.String(),
		Msg:      payload,
	}
	txBuilder = testkeeper.EVMTestApp.GetTxConfig().NewTxBuilder()
	txBuilder.SetMsgs(msg)
	txBuilder.SetFeeAmount(sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(1000000))))
	txBuilder.SetGasLimit(300000)
	tx = signTx(txBuilder, privKeyRecipient, k.AccountKeeper().GetAccount(ctx, recipient))
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
	require.Equal(t, uint32(0), receipt.Logs[0].Index)
	ownerHash := receipt.Logs[0].Topics[1]
	// make sure that the owner is set correctly to the creator, not the spender.
	creatorEvmAddr := testkeeper.EVMTestApp.EvmKeeper.GetEVMAddressOrDefault(ctx, creator)
	require.Equal(t, common.BytesToHash(creatorEvmAddr[:]).Hex(), ownerHash)
	tokenIdHash = receipt.Logs[0].Topics[3]
	require.Equal(t, "0x0000000000000000000000000000000000000000000000000000000000000002", tokenIdHash)
	require.Equal(t, common.HexToHash("0x0").Bytes(), receipt.Logs[0].Data)

	// transfer back
	payload = []byte(fmt.Sprintf("{\"transfer_nft\":{\"token_id\":\"2\",\"recipient\":\"%s\"}}", creator.String()))
	msg = &wasmtypes.MsgExecuteContract{
		Sender:   recipient.String(),
		Contract: contractAddr.String(),
		Msg:      payload,
	}
	txBuilder = testkeeper.EVMTestApp.GetTxConfig().NewTxBuilder()
	txBuilder.SetMsgs(msg)
	txBuilder.SetFeeAmount(sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(1000000))))
	txBuilder.SetGasLimit(300000)
	tx = signTx(txBuilder, privKeyRecipient, k.AccountKeeper().GetAccount(ctx, recipient))
	txbz, err = testkeeper.EVMTestApp.GetTxConfig().TxEncoder()(tx)
	require.Nil(t, err)
	sum = sha256.Sum256(txbz)
	res = testkeeper.EVMTestApp.DeliverTx(ctx.WithEventManager(sdk.NewEventManager()), abci.RequestDeliverTx{Tx: txbz}, tx, sum)
	require.Equal(t, uint32(0), res.Code)

	// acct2 transfer on behalf of acct1 to acct2, acct2 approve acct1, acct1 transfer on behalf of acct2 to acct1
	txBuilder = testkeeper.EVMTestApp.GetTxConfig().NewTxBuilder()
	msg1 := &wasmtypes.MsgExecuteContract{
		Sender:   recipient.String(),
		Contract: contractAddr.String(),
		Msg:      []byte(fmt.Sprintf("{\"transfer_nft\":{\"token_id\":\"2\",\"recipient\":\"%s\"}}", recipient.String())),
	}
	msg2 := &wasmtypes.MsgExecuteContract{
		Sender:   recipient.String(),
		Contract: contractAddr.String(),
		Msg:      []byte(fmt.Sprintf("{\"approve_all\":{\"operator\":\"%s\"}}", creator.String())),
	}
	msg3 := &wasmtypes.MsgExecuteContract{
		Sender:   creator.String(),
		Contract: contractAddr.String(),
		Msg:      []byte(fmt.Sprintf("{\"transfer_nft\":{\"token_id\":\"2\",\"recipient\":\"%s\"}}", creator.String())),
	}
	txBuilder.SetMsgs(msg1, msg2, msg3)
	txBuilder.SetFeeAmount(sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(1000000))))
	txBuilder.SetGasLimit(1000000)
	tx = signTxMultiple(txBuilder, []cryptotypes.PrivKey{privKeyRecipient, privKey}, []authtypes.AccountI{k.AccountKeeper().GetAccount(ctx, recipient), k.AccountKeeper().GetAccount(ctx, creator)})
	txbz, err = testkeeper.EVMTestApp.GetTxConfig().TxEncoder()(tx)
	require.Nil(t, err)
	sum = sha256.Sum256(txbz)
	res = testkeeper.EVMTestApp.DeliverTx(ctx.WithEventManager(sdk.NewEventManager()), abci.RequestDeliverTx{Tx: txbz}, tx, sum)
	require.Equal(t, uint32(0), res.Code)
	receipt, err = testkeeper.EVMTestApp.EvmKeeper.GetTransientReceipt(ctx, common.BytesToHash(sum[:]))
	require.Nil(t, err)
	require.Equal(t, 3, len(receipt.Logs))
	// make sure that the owner is set correctly to the creator, not the spender.
	require.Equal(t, common.BytesToHash(creatorEvmAddr[:]).Hex(), receipt.Logs[0].Topics[1])
	// the second log is the approval log, which doesn't affect ownership thus not checking here.
	recipientEvmAddr := testkeeper.EVMTestApp.EvmKeeper.GetEVMAddressOrDefault(ctx, recipient)
	require.Equal(t, common.BytesToHash(recipientEvmAddr[:]).Hex(), receipt.Logs[2].Topics[1])

	// burn on behalf
	payload = []byte("{\"burn\":{\"token_id\":\"2\"}}")
	msg = &wasmtypes.MsgExecuteContract{
		Sender:   recipient.String(),
		Contract: contractAddr.String(),
		Msg:      payload,
	}
	txBuilder = testkeeper.EVMTestApp.GetTxConfig().NewTxBuilder()
	txBuilder.SetMsgs(msg)
	txBuilder.SetFeeAmount(sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(1000000))))
	txBuilder.SetGasLimit(300000)
	tx = signTx(txBuilder, privKeyRecipient, k.AccountKeeper().GetAccount(ctx, recipient))
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
	require.Equal(t, uint32(0), receipt.Logs[0].Index)
	ownerHash = receipt.Logs[0].Topics[1]
	// make sure that the owner is set correctly to the creator, not the spender.
	creatorEvmAddr = testkeeper.EVMTestApp.EvmKeeper.GetEVMAddressOrDefault(ctx, creator)
	require.Equal(t, common.BytesToHash(creatorEvmAddr[:]).Hex(), ownerHash)
	tokenIdHash = receipt.Logs[0].Topics[3]
	require.Equal(t, "0x0000000000000000000000000000000000000000000000000000000000000002", tokenIdHash)
	require.Equal(t, common.HexToHash("0x0").Bytes(), receipt.Logs[0].Data)

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
}

func TestEvmEventsForCw1155(t *testing.T) {
	k := testkeeper.EVMTestApp.EvmKeeper
	wasmKeeper := k.WasmKeeper()
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx([]byte{}).WithBlockTime(time.Now()).WithChainID("sei-test").WithBlockHeight(1)
	// Create account 1 and associate its addresses
	amt := sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(1000000000000)))
	privKey := testkeeper.MockPrivateKey()
	creator, creatorEvmAddr := testkeeper.PrivateKeyToAddresses(privKey)
	creatorHash := common.BytesToHash(creatorEvmAddr[:]).Hex()
	k.BankKeeper().MintCoins(ctx, "evm", amt)
	k.BankKeeper().SendCoinsFromModuleToAccount(ctx, "evm", creator, amt)
	k.SetAddressMapping(ctx, creator, creatorEvmAddr)
	// Create account 2 and associate its addresses
	privKeyRecipient := testkeeper.MockPrivateKey()
	recipient, recipientEvmAddr := testkeeper.PrivateKeyToAddresses(privKeyRecipient)
	recipientHash := common.BytesToHash(recipientEvmAddr[:]).Hex()
	k.BankKeeper().MintCoins(ctx, "evm", amt)
	k.BankKeeper().SendCoinsFromModuleToAccount(ctx, "evm", recipient, amt)
	k.SetAddressMapping(ctx, recipient, recipientEvmAddr)
	// Deploy contract
	code, err := os.ReadFile("../contracts/wasm/cw1155_base.wasm")
	require.Nil(t, err)
	codeID, err := wasmKeeper.Create(ctx, creator, code, nil)
	require.Nil(t, err)
	contractAddr, _, err := wasmKeeper.Instantiate(ctx, codeID, creator, creator, []byte(fmt.Sprintf("{\"name\":\"test\",\"symbol\":\"test\",\"minter\":\"%s\"}", creator.String())), "test", sdk.NewCoins())
	require.Nil(t, err)
	_, mockPointerAddr := testkeeper.MockAddressPair()
	k.SetERC1155CW1155Pointer(ctx, contractAddr.String(), mockPointerAddr)

	// calling CW contract directly
	payload := []byte(fmt.Sprintf("{\"mint\":{\"recipient\":\"%s\",\"msg\":{\"token_id\":\"1\",\"amount\":\"10\"}}}", recipient.String()))
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
	require.Equal(t, creatorHash, receipt.Logs[0].Topics[1])
	require.Equal(t, common.HexToHash("0x0").Hex(), receipt.Logs[0].Topics[2])
	require.Equal(t, recipientHash, receipt.Logs[0].Topics[3])
	expectedData := append(common.BigToHash(big.NewInt(1)).Bytes(), common.BigToHash(big.NewInt(10)).Bytes()...)
	require.Equal(t, expectedData, receipt.Logs[0].Data)

	// calling from wasmd precompile
	abi := pcommon.MustGetABI(f, "wasm_abi.json")
	emptyCoins, err := sdk.NewCoins().MarshalJSON()
	require.Nil(t, err)
	payload = []byte(fmt.Sprintf("{\"mint_batch\":{\"recipient\":\"%s\",\"msgs\":[{\"token_id\":\"1\",\"amount\":\"30\"},{\"token_id\":\"2\",\"amount\":\"40\"}]}}", creator.String()))
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
	sum = sha256.Sum256(txbz)
	res = testkeeper.EVMTestApp.DeliverTx(ctx.WithEventManager(sdk.NewEventManager()).WithTxIndex(1), abci.RequestDeliverTx{Tx: txbz}, tx, sum)
	require.Equal(t, uint32(0), res.Code)
	receipt, err = testkeeper.EVMTestApp.EvmKeeper.GetTransientReceipt(ctx, signedTx.Hash())
	require.Nil(t, err)
	require.Equal(t, 1, len(receipt.Logs))
	require.NotEmpty(t, receipt.LogsBloom)
	require.Equal(t, mockPointerAddr.Hex(), receipt.Logs[0].Address)
	_, found = testkeeper.EVMTestApp.EvmKeeper.GetEVMTxDeferredInfo(ctx)
	require.True(t, found)
	require.Equal(t, creatorHash, receipt.Logs[0].Topics[1])
	require.Equal(t, common.HexToHash("0x0").Hex(), receipt.Logs[0].Topics[2])
	require.Equal(t, creatorHash, receipt.Logs[0].Topics[3])
	tokenIDsArg := cw1155.GetParsedABI().Events["TransferBatch"].Inputs[3]
	tokenAmountsArg := cw1155.GetParsedABI().Events["TransferBatch"].Inputs[4]
	require.Equal(t, "ids", tokenIDsArg.Name)
	require.Equal(t, "values", tokenAmountsArg.Name)
	dataArgs := eabi.Arguments{tokenIDsArg, tokenAmountsArg}
	expectedData, err = dataArgs.Pack([]*big.Int{big.NewInt(1), big.NewInt(2)}, []*big.Int{big.NewInt(30), big.NewInt(40)})
	require.Nil(t, err)
	require.Equal(t, expectedData, receipt.Logs[0].Data)

	// send
	payload = []byte(fmt.Sprintf("{\"send\":{\"to\":\"%s\",\"token_id\":\"1\",\"amount\":\"2\"}}", recipient.String()))
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
	require.Equal(t, creatorHash, receipt.Logs[0].Topics[1])
	require.Equal(t, creatorHash, receipt.Logs[0].Topics[2])
	require.Equal(t, recipientHash, receipt.Logs[0].Topics[3])
	expectedData = append(common.HexToHash("0x1").Bytes(), common.HexToHash("0x2").Bytes()...)
	require.Equal(t, expectedData, receipt.Logs[0].Data)

	// send_batch
	payload = []byte(fmt.Sprintf("{\"send_batch\":{\"to\":\"%s\",\"batch\":[{\"token_id\":\"1\",\"amount\":\"1\"},{\"token_id\":\"2\",\"amount\":\"2\"}]}}", recipient.String()))
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
	require.Equal(t, creatorHash, receipt.Logs[0].Topics[1])
	require.Equal(t, creatorHash, receipt.Logs[0].Topics[2])
	require.Equal(t, recipientHash, receipt.Logs[0].Topics[3])
	expectedData, err = dataArgs.Pack([]*big.Int{big.NewInt(1), big.NewInt(2)}, []*big.Int{big.NewInt(1), big.NewInt(2)})
	require.Nil(t, err)
	require.Equal(t, expectedData, receipt.Logs[0].Data)

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
	require.Equal(t, creatorHash, receipt.Logs[0].Topics[1])
	require.Equal(t, recipientHash, receipt.Logs[0].Topics[2])
	require.Equal(t, common.HexToHash("0x1").Bytes(), receipt.Logs[0].Data)

	// send on behalf
	payload = []byte(fmt.Sprintf("{\"send\":{\"to\":\"%s\",\"from\":\"%s\",\"token_id\":\"1\",\"amount\":\"1\"}}", recipient.String(), creator.String()))
	msg = &wasmtypes.MsgExecuteContract{
		Sender:   recipient.String(),
		Contract: contractAddr.String(),
		Msg:      payload,
	}
	txBuilder = testkeeper.EVMTestApp.GetTxConfig().NewTxBuilder()
	txBuilder.SetMsgs(msg)
	txBuilder.SetFeeAmount(sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(1000000))))
	txBuilder.SetGasLimit(300000)
	tx = signTx(txBuilder, privKeyRecipient, k.AccountKeeper().GetAccount(ctx, recipient))
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
	require.Equal(t, recipientHash, receipt.Logs[0].Topics[1])
	require.Equal(t, creatorHash, receipt.Logs[0].Topics[2])
	require.Equal(t, recipientHash, receipt.Logs[0].Topics[3])
	expectedData = append(common.HexToHash("0x1").Bytes(), common.HexToHash("0x1").Bytes()...)
	require.Equal(t, expectedData, receipt.Logs[0].Data)

	// send_batch on behalf
	payload = []byte(fmt.Sprintf("{\"send_batch\":{\"to\":\"%s\",\"from\":\"%s\",\"batch\":[{\"token_id\":\"1\",\"amount\":\"1\"},{\"token_id\":\"2\",\"amount\":\"1\"}]}}", recipient.String(), creator.String()))
	msg = &wasmtypes.MsgExecuteContract{
		Sender:   recipient.String(),
		Contract: contractAddr.String(),
		Msg:      payload,
	}
	txBuilder = testkeeper.EVMTestApp.GetTxConfig().NewTxBuilder()
	txBuilder.SetMsgs(msg)
	txBuilder.SetFeeAmount(sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(1000000))))
	txBuilder.SetGasLimit(300000)
	tx = signTx(txBuilder, privKeyRecipient, k.AccountKeeper().GetAccount(ctx, recipient))
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
	require.Equal(t, recipientHash, receipt.Logs[0].Topics[1])
	require.Equal(t, creatorHash, receipt.Logs[0].Topics[2])
	require.Equal(t, recipientHash, receipt.Logs[0].Topics[3])
	expectedData, err = dataArgs.Pack([]*big.Int{big.NewInt(1), big.NewInt(2)}, []*big.Int{big.NewInt(1), big.NewInt(1)})
	require.Nil(t, err)
	require.Equal(t, expectedData, receipt.Logs[0].Data)

	// burn on behalf
	payload = []byte(fmt.Sprintf("{\"burn\":{\"from\":\"%s\",\"token_id\":\"1\",\"amount\":\"1\"}}", creator.String()))
	msg = &wasmtypes.MsgExecuteContract{
		Sender:   recipient.String(),
		Contract: contractAddr.String(),
		Msg:      payload,
	}
	txBuilder = testkeeper.EVMTestApp.GetTxConfig().NewTxBuilder()
	txBuilder.SetMsgs(msg)
	txBuilder.SetFeeAmount(sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(1000000))))
	txBuilder.SetGasLimit(300000)
	tx = signTx(txBuilder, privKeyRecipient, k.AccountKeeper().GetAccount(ctx, recipient))
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
	require.Equal(t, recipientHash, receipt.Logs[0].Topics[1])
	require.Equal(t, creatorHash, receipt.Logs[0].Topics[2])
	require.Equal(t, common.HexToHash("0x0").Hex(), receipt.Logs[0].Topics[3])
	expectedData = append(common.HexToHash("0x1").Bytes(), common.HexToHash("0x1").Bytes()...)
	require.Equal(t, expectedData, receipt.Logs[0].Data)

	// burn_batch on behalf
	payload = []byte(fmt.Sprintf("{\"burn_batch\":{\"from\":\"%s\",\"batch\":[{\"token_id\":\"1\",\"amount\":\"1\"},{\"token_id\":\"2\",\"amount\":\"1\"}]}}", creator.String()))
	msg = &wasmtypes.MsgExecuteContract{
		Sender:   recipient.String(),
		Contract: contractAddr.String(),
		Msg:      payload,
	}
	txBuilder = testkeeper.EVMTestApp.GetTxConfig().NewTxBuilder()
	txBuilder.SetMsgs(msg)
	txBuilder.SetFeeAmount(sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(1000000))))
	txBuilder.SetGasLimit(300000)
	tx = signTx(txBuilder, privKeyRecipient, k.AccountKeeper().GetAccount(ctx, recipient))
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
	require.Equal(t, recipientHash, receipt.Logs[0].Topics[1])
	require.Equal(t, creatorHash, receipt.Logs[0].Topics[2])
	require.Equal(t, common.HexToHash("0x0").Hex(), receipt.Logs[0].Topics[3])
	expectedData, err = dataArgs.Pack([]*big.Int{big.NewInt(1), big.NewInt(2)}, []*big.Int{big.NewInt(1), big.NewInt(1)})
	require.Nil(t, err)
	require.Equal(t, expectedData, receipt.Logs[0].Data)

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
	require.Equal(t, creatorHash, receipt.Logs[0].Topics[1])
	require.Equal(t, recipientHash, receipt.Logs[0].Topics[2])
	require.Equal(t, common.HexToHash("0x0").Bytes(), receipt.Logs[0].Data)

	// burn
	payload = []byte("{\"burn\":{\"token_id\":\"2\",\"amount\":\"1\"}}")
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
	require.Equal(t, creatorHash, receipt.Logs[0].Topics[1])
	require.Equal(t, creatorHash, receipt.Logs[0].Topics[2])
	require.Equal(t, common.HexToHash("0x0").Hex(), receipt.Logs[0].Topics[3])
	expectedData = append(common.HexToHash("0x2").Bytes(), common.HexToHash("0x1").Bytes()...)
	require.Equal(t, expectedData, receipt.Logs[0].Data)

	// burn_batch
	payload = []byte("{\"burn_batch\":{\"batch\":[{\"token_id\":\"1\",\"amount\":\"1\"},{\"token_id\":\"2\",\"amount\":\"1\"}]}}")
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
	require.Equal(t, creatorHash, receipt.Logs[0].Topics[1])
	require.Equal(t, creatorHash, receipt.Logs[0].Topics[2])
	require.Equal(t, common.HexToHash("0x0").Hex(), receipt.Logs[0].Topics[3])
	expectedData, err = dataArgs.Pack([]*big.Int{big.NewInt(1), big.NewInt(2)}, []*big.Int{big.NewInt(1), big.NewInt(1)})
	require.Nil(t, err)
	require.Equal(t, expectedData, receipt.Logs[0].Data)
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

func signTxMultiple(txBuilder client.TxBuilder, privKeys []cryptotypes.PrivKey, accs []authtypes.AccountI) sdk.Tx {
	var sigsV2 []signing.SignatureV2
	for i, privKey := range privKeys {
		sigsV2 = append(sigsV2, signing.SignatureV2{
			PubKey: privKey.PubKey(),
			Data: &signing.SingleSignatureData{
				SignMode:  testkeeper.EVMTestApp.GetTxConfig().SignModeHandler().DefaultMode(),
				Signature: nil,
			},
			Sequence: accs[i].GetSequence(),
		})
	}
	_ = txBuilder.SetSignatures(sigsV2...)
	sigsV2 = []signing.SignatureV2{}
	for i, privKey := range privKeys {
		signerData := xauthsigning.SignerData{
			ChainID:       "sei-test",
			AccountNumber: accs[i].GetAccountNumber(),
			Sequence:      accs[i].GetSequence(),
		}
		sigV2, _ := clienttx.SignWithPrivKey(
			testkeeper.EVMTestApp.GetTxConfig().SignModeHandler().DefaultMode(),
			signerData,
			txBuilder,
			privKey,
			testkeeper.EVMTestApp.GetTxConfig(),
			accs[i].GetSequence(),
		)
		sigsV2 = append(sigsV2, sigV2)
	}
	_ = txBuilder.SetSignatures(sigsV2...)
	return txBuilder.GetTx()
}
