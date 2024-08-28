package evm_test

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/cosmos/cosmos-sdk/client"
	clienttx "github.com/cosmos/cosmos-sdk/client/tx"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	xauthsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	ethabi "github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	pcommon "github.com/sei-protocol/sei-chain/precompiles/common"
	"github.com/sei-protocol/sei-chain/precompiles/pointer"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/artifacts/cw721"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/sei-protocol/sei-chain/x/evm/types/ethtx"
	"github.com/stretchr/testify/require"
	abci "github.com/tendermint/tendermint/abci/types"
)

//go:embed pointer_abi.json
var f embed.FS

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
	abi := pcommon.MustGetABI(f, "pointer_abi.json")
	data, err := abi.Pack("addCW721Pointer", cw2981Addr.String())
	require.Nil(t, err)
	txData := ethtypes.LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(100000000000),
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
	cw721abi, err := cw721.Cw721MetaData.GetAbi()
	require.Nil(t, err)
	data, err = cw721abi.Pack("royaltyInfo", big.NewInt(1), big.NewInt(1000))
	require.Nil(t, err)
	txData = ethtypes.LegacyTx{
		Nonce:    1,
		GasPrice: big.NewInt(100000000000),
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
	ret, err := cw721abi.Unpack("royaltyInfo", typedMsgData.ReturnData)
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
		GasPrice: big.NewInt(100000000000),
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
		GasPrice: big.NewInt(100000000000),
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

func TestNonceIncrementsForInsufficientFunds(t *testing.T) {
	k := testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx([]byte{}).WithBlockTime(time.Now())
	privKey := testkeeper.MockPrivateKey()
	seiAddr, evmAddr := testkeeper.PrivateKeyToAddresses(privKey)
	k.SetAddressMapping(ctx, seiAddr, evmAddr)
	testPrivHex := hex.EncodeToString(privKey.Bytes())
	key, _ := crypto.HexToECDSA(testPrivHex)
	txData := ethtypes.LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(100000000000),
		Gas:      5000000,
		To:       nil,
		Data:     []byte{},
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
	require.Equal(t, uint32(5), res.Code)                 // insufficient funds has error code 5
	require.Equal(t, uint64(1), k.GetNonce(ctx, evmAddr)) // make sure nonce is incremented regardless

	// ensure that old txs cannot be used by malicious party to bump nonces
	res = testkeeper.EVMTestApp.DeliverTx(ctx, abci.RequestDeliverTx{Tx: txbz}, cosmosTx, sha256.Sum256(txbz))
	require.Equal(t, uint32(32), res.Code)                // wrong nonce has error code 32
	require.Equal(t, uint64(1), k.GetNonce(ctx, evmAddr)) // nonce should not be incremented this time because the tx is an old one
}

func TestInvalidAssociateMsg(t *testing.T) {
	// EVM associate tx
	k := testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx([]byte{}).WithBlockTime(time.Now()).WithChainID("sei-test").WithBlockHeight(1)
	privKey := testkeeper.MockPrivateKey()
	seiAddr, _ := testkeeper.PrivateKeyToAddresses(privKey)
	amt := sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(1000000)))
	k.BankKeeper().MintCoins(ctx, "evm", amt)
	k.BankKeeper().SendCoinsFromModuleToAccount(ctx, "evm", seiAddr, amt)
	testPrivHex := hex.EncodeToString(privKey.Bytes())
	key, _ := crypto.HexToECDSA(testPrivHex)
	customMsg := strings.Repeat("a", 65)
	hash := crypto.Keccak256Hash([]byte(customMsg))
	sig, err := crypto.Sign(hash[:], key)
	require.Nil(t, err)
	R, S, _, err := ethtx.DecodeSignature(sig)
	require.Nil(t, err)
	V := big.NewInt(int64(sig[64]))
	require.Nil(t, err)
	typedTx := &ethtx.AssociateTx{
		V: V.Bytes(), R: R.Bytes(), S: S.Bytes(), CustomMessage: customMsg,
	}
	msg, err := types.NewMsgEVMTransaction(typedTx)
	require.Nil(t, err)
	txBuilder := testkeeper.EVMTestApp.GetTxConfig().NewTxBuilder()
	txBuilder.SetMsgs(msg)
	cosmosTx := txBuilder.GetTx()
	txbz, err := testkeeper.EVMTestApp.GetTxConfig().TxEncoder()(cosmosTx)
	require.Nil(t, err)
	res := testkeeper.EVMTestApp.DeliverTx(ctx, abci.RequestDeliverTx{Tx: txbz}, cosmosTx, sha256.Sum256(txbz))
	require.Equal(t, uint32(21), res.Code) // tx too large

	// cosmos associate tx
	amsg := &types.MsgAssociate{
		Sender: seiAddr.String(), CustomMessage: customMsg,
	}
	txBuilder = testkeeper.EVMTestApp.GetTxConfig().NewTxBuilder()
	txBuilder.SetMsgs(amsg)
	signedTx := signTx(txBuilder, privKey, k.AccountKeeper().GetAccount(ctx, seiAddr))
	txbz, err = testkeeper.EVMTestApp.GetTxConfig().TxEncoder()(signedTx)
	require.Nil(t, err)
	res = testkeeper.EVMTestApp.DeliverTx(ctx, abci.RequestDeliverTx{Tx: txbz}, signedTx, sha256.Sum256(txbz))
	require.Equal(t, uint32(21), res.Code)

	// multiple associate msgs should charge gas (and run out of gas in this test case)
	amsg = &types.MsgAssociate{
		Sender: seiAddr.String(), CustomMessage: "",
	}
	txBuilder = testkeeper.EVMTestApp.GetTxConfig().NewTxBuilder()
	msgs := []sdk.Msg{}
	for i := 1; i <= 1000; i++ {
		msgs = append(msgs, amsg)
	}
	txBuilder.SetMsgs(msgs...)
	signedTx = signTx(txBuilder, privKey, k.AccountKeeper().GetAccount(ctx, seiAddr))
	txbz, err = testkeeper.EVMTestApp.GetTxConfig().TxEncoder()(signedTx)
	require.Nil(t, err)
	res = testkeeper.EVMTestApp.DeliverTx(ctx, abci.RequestDeliverTx{Tx: txbz}, signedTx, sha256.Sum256(txbz))
	require.Equal(t, uint32(11), res.Code) // out of gas
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
