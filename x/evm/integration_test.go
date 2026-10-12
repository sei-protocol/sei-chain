package evm_test

import (
	"crypto/sha256"
	"encoding/hex"
	"math/big"
	"strings"
	"testing"
	"time"

	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client"
	clienttx "github.com/sei-protocol/sei-chain/sei-cosmos/client/tx"
	cryptotypes "github.com/sei-protocol/sei-chain/sei-cosmos/crypto/types"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/types/tx/signing"
	xauthsigning "github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/signing"
	authtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/types"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/sei-protocol/sei-chain/x/evm/types/ethtx"
	"github.com/stretchr/testify/require"
)

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
		Gas:      6000000,
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
	res := testkeeper.EVMTestApp.DeliverTx(ctx, abci.RequestDeliverTxV2{Tx: txbz}, cosmosTx, sha256.Sum256(txbz))
	require.Equal(t, uint32(5), res.Code)                 // insufficient funds has error code 5
	require.Equal(t, uint64(1), k.GetNonce(ctx, evmAddr)) // make sure nonce is incremented regardless

	// ensure that old txs cannot be used by malicious party to bump nonces
	res = testkeeper.EVMTestApp.DeliverTx(ctx, abci.RequestDeliverTxV2{Tx: txbz}, cosmosTx, sha256.Sum256(txbz))
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
	res := testkeeper.EVMTestApp.DeliverTx(ctx, abci.RequestDeliverTxV2{Tx: txbz}, cosmosTx, sha256.Sum256(txbz))
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
	res = testkeeper.EVMTestApp.DeliverTx(ctx, abci.RequestDeliverTxV2{Tx: txbz}, signedTx, sha256.Sum256(txbz))
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
	res = testkeeper.EVMTestApp.DeliverTx(ctx, abci.RequestDeliverTxV2{Tx: txbz}, signedTx, sha256.Sum256(txbz))
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
