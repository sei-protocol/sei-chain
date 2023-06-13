package processblock

import (
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/crypto/hd"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	xauthsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	"github.com/cosmos/cosmos-sdk/x/auth/tx"
)

var InterfaceReg = types.NewInterfaceRegistry()
var Marshaler = codec.NewProtoCodec(InterfaceReg)
var TxConfig = tx.NewTxConfig(Marshaler, tx.DefaultSignModes)

func (a *App) Sign(account sdk.AccAddress, fee int64, msgs ...sdk.Msg) xauthsigning.Tx {
	txBuilder := TxConfig.NewTxBuilder()
	if err := txBuilder.SetMsgs(msgs...); err != nil {
		panic(err)
	}
	txBuilder.SetGasLimit(1000000)
	txBuilder.SetFeeAmount([]sdk.Coin{
		sdk.NewCoin("usei", sdk.NewInt(fee)),
	})

	acc := a.AccountKeeper.GetAccount(a.Ctx(), account)
	seqNum := acc.GetSequence()
	if delta, ok := a.accToSeqDelta[account.String()]; ok {
		seqNum += delta
	}
	privKey := GetKey(a.accToMnemonic[account.String()])

	signerData := xauthsigning.SignerData{
		ChainID:       "tendermint_test",
		AccountNumber: acc.GetAccountNumber(),
		Sequence:      seqNum,
	}
	sigData := signing.SingleSignatureData{
		SignMode:  TxConfig.SignModeHandler().DefaultMode(),
		Signature: nil,
	}
	sig := signing.SignatureV2{
		PubKey:   privKey.PubKey(),
		Data:     &sigData,
		Sequence: seqNum,
	}
	if err := txBuilder.SetSignatures(sig); err != nil {
		panic(err)
	}
	bytesToSign, err := TxConfig.SignModeHandler().GetSignBytes(TxConfig.SignModeHandler().DefaultMode(), signerData, txBuilder.GetTx())
	if err != nil {
		panic(err)
	}
	sigBytes, err := privKey.Sign(bytesToSign)
	if err != nil {
		panic(err)
	}
	sigData = signing.SingleSignatureData{
		SignMode:  TxConfig.SignModeHandler().DefaultMode(),
		Signature: sigBytes,
	}
	sig = signing.SignatureV2{
		PubKey:   privKey.PubKey(),
		Data:     &sigData,
		Sequence: seqNum,
	}

	err = txBuilder.SetSignatures(sig)
	if err != nil {
		panic(err)
	}
	if _, ok := a.accToSeqDelta[account.String()]; ok {
		a.accToSeqDelta[account.String()]++
	} else {
		a.accToSeqDelta[account.String()] = 1
	}
	return txBuilder.GetTx()
}

func GetKey(mnemonic string) cryptotypes.PrivKey {
	algo := hd.Secp256k1
	hdpath := hd.CreateHDPath(sdk.GetConfig().GetCoinType(), 0, 0).String()
	derivedPriv, _ := algo.Derive()(mnemonic, "", hdpath)
	privKey := algo.Generate()(derivedPriv)

	return privKey
}
