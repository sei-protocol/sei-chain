package tests

import (
	"encoding/hex"
	"math/big"

	clienttx "github.com/cosmos/cosmos-sdk/client/tx"
	"github.com/cosmos/cosmos-sdk/crypto/hd"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	xauthsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/sei-protocol/sei-chain/app"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/config"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

var chainId = big.NewInt(config.DefaultChainID)
var mnemonic1 = "fish mention unlock february marble dove vintage sand hub ordinary fade found inject room embark supply fabric improve spike stem give current similar glimpse"

func signTxWithMnemonic(txData ethtypes.TxData, mnemonic string) *ethtypes.Transaction {
	derivedPriv, _ := hd.Secp256k1.Derive()(mnemonic, "", "")
	privKey := hd.Secp256k1.Generate()(derivedPriv)
	testPrivHex := hex.EncodeToString(privKey.Bytes())
	key, _ := crypto.HexToECDSA(testPrivHex)
	ethCfg := types.DefaultChainConfig().EthereumConfig(chainId)
	signer := ethtypes.MakeSigner(ethCfg, big.NewInt(1), 1)
	tx := ethtypes.NewTx(txData)
	tx, err := ethtypes.SignTx(tx, signer, key)
	if err != nil {
		panic(err)
	}
	return tx
}

func signCosmosTxWithMnemonic(msg sdk.Msg, mnemonic string, accountNumber uint64, sequenceNumber uint64) sdk.Tx {
	derivedPriv, _ := hd.Secp256k1.Derive()(mnemonic, "", "")
	privKey := hd.Secp256k1.Generate()(derivedPriv)
	txBuilder := testkeeper.EVMTestApp.GetTxConfig().NewTxBuilder()
	_ = txBuilder.SetMsgs(msg)
	txBuilder.SetFeeAmount(sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(1000000))))
	txBuilder.SetGasLimit(300000)
	var sigsV2 []signing.SignatureV2
	sigV2 := signing.SignatureV2{
		PubKey: privKey.PubKey(),
		Data: &signing.SingleSignatureData{
			SignMode:  testkeeper.EVMTestApp.GetTxConfig().SignModeHandler().DefaultMode(),
			Signature: nil,
		},
		Sequence: sequenceNumber,
	}
	sigsV2 = append(sigsV2, sigV2)
	_ = txBuilder.SetSignatures(sigsV2...)
	sigsV2 = []signing.SignatureV2{}
	signerData := xauthsigning.SignerData{
		ChainID:       "sei-test",
		AccountNumber: accountNumber,
		Sequence:      sequenceNumber,
	}
	sigV2, _ = clienttx.SignWithPrivKey(
		testkeeper.EVMTestApp.GetTxConfig().SignModeHandler().DefaultMode(),
		signerData,
		txBuilder,
		privKey,
		testkeeper.EVMTestApp.GetTxConfig(),
		sequenceNumber,
	)
	sigsV2 = append(sigsV2, sigV2)
	_ = txBuilder.SetSignatures(sigsV2...)
	return txBuilder.GetTx()
}

func getAddrWithMnemonic(mnemonic string) common.Address {
	derivedPriv, _ := hd.Secp256k1.Derive()(mnemonic, "", "")
	privKey := hd.Secp256k1.Generate()(derivedPriv)
	_, evmAddr := testkeeper.PrivateKeyToAddresses(privKey)
	return evmAddr
}

func getSeiAddrWithMnemonic(mnemonic string) sdk.AccAddress {
	derivedPriv, _ := hd.Secp256k1.Derive()(mnemonic, "", "")
	privKey := hd.Secp256k1.Generate()(derivedPriv)
	seiAddr, _ := testkeeper.PrivateKeyToAddresses(privKey)
	return seiAddr
}

func mnemonicInitializer(mnemonic string) func(ctx sdk.Context, a *app.App) {
	return func(ctx sdk.Context, a *app.App) {
		seiAddr := getSeiAddrWithMnemonic(mnemonic)
		evmAddr := getAddrWithMnemonic(mnemonic)
		a.EvmKeeper.SetAddressMapping(ctx, seiAddr, evmAddr)
		fundSeiAddr(ctx, a, seiAddr)
	}
}

func fundSeiAddr(ctx sdk.Context, a *app.App, addr sdk.AccAddress) {
	amt := sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(10000000000)))
	_ = a.BankKeeper.MintCoins(ctx, types.ModuleName, amt)
	_ = a.BankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, addr, amt)
}
