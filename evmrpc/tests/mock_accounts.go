package tests

import (
	"encoding/hex"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/sei-protocol/sei-chain/app"
	clienttx "github.com/sei-protocol/sei-chain/sei-cosmos/client/tx"
	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/hd"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/types/tx/signing"
	xauthsigning "github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/signing"
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

func signCosmosTxWithMnemonic(msg sdk.Msg, mnemonic string, accountNumber uint64, sequenceNumber uint64) (sdk.Tx, error) {
	derivedPriv, err := hd.Secp256k1.Derive()(mnemonic, "", "")
	if err != nil {
		return nil, fmt.Errorf("derive: %w", err)
	}
	privKey := hd.Secp256k1.Generate()(derivedPriv)
	txBuilder := testkeeper.EVMTestApp.GetTxConfig().NewTxBuilder()
	if err := txBuilder.SetMsgs(msg); err != nil {
		return nil, fmt.Errorf("s: %w", err)
	}
	txBuilder.SetFeeAmount(sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(1000000))))
	txBuilder.SetGasLimit(300000)
	sigV2 := signing.SignatureV2{
		PubKey: privKey.PubKey(),
		Data: &signing.SingleSignatureData{
			SignMode:  testkeeper.EVMTestApp.GetTxConfig().SignModeHandler().DefaultMode(),
			Signature: nil,
		},
		Sequence: sequenceNumber,
	}
	if err := txBuilder.SetSignatures(sigV2); err != nil {
		return nil, fmt.Errorf("setSignatures (placeholder): %w", err)
	}
	signerData := xauthsigning.SignerData{
		ChainID:       "sei-test",
		AccountNumber: accountNumber,
		Sequence:      sequenceNumber,
	}
	sigV2, err = clienttx.SignWithPrivKey(
		testkeeper.EVMTestApp.GetTxConfig().SignModeHandler().DefaultMode(),
		signerData,
		txBuilder,
		privKey,
		testkeeper.EVMTestApp.GetTxConfig(),
		sequenceNumber,
	)
	if err != nil {
		return nil, fmt.Errorf("signWithPrivKey: %w", err)
	}
	if err := txBuilder.SetSignatures(sigV2); err != nil {
		return nil, fmt.Errorf("SetSignatures (final): %w", err)
	}
	return txBuilder.GetTx(), nil
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

const letters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

func multiCoinInitializer(mnemonic string) func(ctx sdk.Context, a *app.App) {
	return func(ctx sdk.Context, a *app.App) {
		amt := sdk.NewCoins()
		for i := 0; i < 50; i++ {
			amt = append(amt, sdk.NewCoin(letters[i:i+3], sdk.OneInt()))
		}
		_ = a.BankKeeper.MintCoins(ctx, types.ModuleName, amt)
		_ = a.BankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, getSeiAddrWithMnemonic(mnemonic), amt)
	}
}
