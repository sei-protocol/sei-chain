package ante_test

import (
	"encoding/hex"
	"fmt"
	"math/big"
	"testing"
	"time"

	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkacltypes "github.com/cosmos/cosmos-sdk/types/accesscontrol"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	bankkeeper "github.com/cosmos/cosmos-sdk/x/bank/keeper"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/ante"
	"github.com/sei-protocol/sei-chain/x/evm/derived"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/sei-protocol/sei-chain/x/evm/types/ethtx"
	"github.com/stretchr/testify/require"
)

func TestPreprocessAnteHandler(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper()
	handler := ante.NewEVMPreprocessDecorator(k, k.AccountKeeper())
	privKey := testkeeper.MockPrivateKey()
	seiAddr, evmAddr := testkeeper.PrivateKeyToAddresses(privKey)
	require.Nil(t, k.BankKeeper().AddCoins(ctx, sdk.AccAddress(evmAddr[:]), sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(100))), true))
	require.Nil(t, k.BankKeeper().AddWei(ctx, sdk.AccAddress(evmAddr[:]), sdk.NewInt(10)))
	testPrivHex := hex.EncodeToString(privKey.Bytes())
	key, _ := crypto.HexToECDSA(testPrivHex)
	to := new(common.Address)
	copy(to[:], []byte("0x1234567890abcdef1234567890abcdef12345678"))
	txData := ethtypes.LegacyTx{
		Nonce:    1,
		GasPrice: big.NewInt(10),
		Gas:      1000,
		To:       to,
		Value:    big.NewInt(1000),
		Data:     []byte("abc"),
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
	ctx, err = handler.AnteHandle(ctx, mockTx{msgs: []sdk.Msg{msg}}, false, func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		return ctx, nil
	})
	require.Nil(t, err)
	require.Equal(t, sdk.AccAddress(privKey.PubKey().Address()), sdk.AccAddress(msg.Derived.SenderSeiAddr))
	require.Equal(t, sdk.NewInt(100), k.BankKeeper().GetBalance(ctx, seiAddr, "usei").Amount)
	require.Equal(t, sdk.NewInt(10), k.BankKeeper().GetWeiBalance(ctx, seiAddr))
	require.Equal(t, sdk.ZeroInt(), k.BankKeeper().GetBalance(ctx, sdk.AccAddress(evmAddr[:]), "usei").Amount)
	require.Equal(t, sdk.ZeroInt(), k.BankKeeper().GetWeiBalance(ctx, sdk.AccAddress(evmAddr[:])))
}

func TestPreprocessAnteHandlerUnprotected(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper()
	handler := ante.NewEVMPreprocessDecorator(k, k.AccountKeeper())
	gasPrice := sdk.NewInt(73141930316)
	amt := sdk.NewInt(270000000000000000)
	v, _ := hex.DecodeString("1c")
	s, _ := hex.DecodeString("16842c738042c72834d256b8aaf4e8cf14beb03c9e2e98bc29bedf29ef7d1ccf")
	r, _ := hex.DecodeString("f7ab1c21ab782e07bc680f3a42972e38d6b42ee9a4cce76ac4c182fe54b08ae7")
	txData := ethtx.LegacyTx{
		Nonce:    62908,
		GasPrice: &gasPrice,
		GasLimit: 93638,
		To:       "0xbb19ce0c0ad13cca2a75f73f163edc8bdae7fb70",
		Amount:   &amt,
		Data:     []byte{},
		V:        v,
		S:        s,
		R:        r,
	}
	msg, err := types.NewMsgEVMTransaction(&txData)
	require.Nil(t, err)
	_, err = handler.AnteHandle(ctx, mockTx{msgs: []sdk.Msg{msg}}, false, func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		return ctx, nil
	})
	require.Nil(t, err)
	require.Equal(t, "0xc39BDF685F289B1F261EE9b0b1B2Bf9eae4C1980", msg.Derived.SenderEVMAddr.Hex())
}

func TestPreprocessAssociateTx(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper()
	handler := ante.NewEVMPreprocessDecorator(k, k.AccountKeeper())
	privKey := testkeeper.MockPrivateKey()
	testPrivHex := hex.EncodeToString(privKey.Bytes())
	key, _ := crypto.HexToECDSA(testPrivHex)

	emptyData := make([]byte, 32)
	prefixedMessage := fmt.Sprintf("\x19Ethereum Signed Message:\n%d", len(emptyData)) + string(emptyData)
	hash := crypto.Keccak256Hash([]byte(prefixedMessage))
	sig, err := crypto.Sign(hash.Bytes(), key)
	require.Nil(t, err)
	R, S, _, _ := ethtx.DecodeSignature(sig)
	V := big.NewInt(int64(sig[64]))

	txData := ethtx.AssociateTx{V: V.Bytes(), R: R.Bytes(), S: S.Bytes(), CustomMessage: prefixedMessage}
	msg, err := types.NewMsgEVMTransaction(&txData)
	require.Nil(t, err)
	ctx, err = handler.AnteHandle(ctx, mockTx{msgs: []sdk.Msg{msg}}, false, func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		panic("should not be called")
	})
	// not enough balance
	require.NotNil(t, err)
	seiAddr := sdk.AccAddress(privKey.PubKey().Address())
	evmAddr := crypto.PubkeyToAddress(key.PublicKey)
	amt := sdk.NewCoins(sdk.NewCoin(k.GetBaseDenom(ctx), sdk.NewInt(int64(ante.BalanceThreshold))))
	k.BankKeeper().MintCoins(ctx, types.ModuleName, amt)
	k.BankKeeper().SendCoinsFromModuleToAccount(ctx, types.ModuleName, seiAddr, amt)
	ctx, err = handler.AnteHandle(ctx, mockTx{msgs: []sdk.Msg{msg}}, false, func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		panic("should not be called")
	})
	require.Nil(t, err)
	associated, ok := k.GetEVMAddress(ctx, seiAddr)
	require.True(t, ok)
	require.Equal(t, evmAddr, associated)

	ctx, err = handler.AnteHandle(ctx, mockTx{msgs: []sdk.Msg{msg}}, false, func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		panic("should not be called")
	})
	// already associated
	require.NotNil(t, err)
}

func TestPreprocessAssociateTxWithWeiBalance(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper()
	handler := ante.NewEVMPreprocessDecorator(k, k.AccountKeeper())
	privKey := testkeeper.MockPrivateKey()
	testPrivHex := hex.EncodeToString(privKey.Bytes())
	key, _ := crypto.HexToECDSA(testPrivHex)

	emptyData := make([]byte, 32)
	prefixedMessage := fmt.Sprintf("\x19Ethereum Signed Message:\n%d", len(emptyData)) + string(emptyData)
	hash := crypto.Keccak256Hash([]byte(prefixedMessage))
	sig, err := crypto.Sign(hash.Bytes(), key)
	require.Nil(t, err)
	R, S, _, _ := ethtx.DecodeSignature(sig)
	V := big.NewInt(int64(sig[64]))

	txData := ethtx.AssociateTx{V: V.Bytes(), R: R.Bytes(), S: S.Bytes(), CustomMessage: prefixedMessage}
	msg, err := types.NewMsgEVMTransaction(&txData)
	require.Nil(t, err)
	seiAddr := sdk.AccAddress(privKey.PubKey().Address())
	evmAddr := crypto.PubkeyToAddress(key.PublicKey)
	k.BankKeeper().AddCoins(ctx, seiAddr, sdk.NewCoins(sdk.NewCoin(k.GetBaseDenom(ctx), sdk.NewInt(int64(ante.BalanceThreshold-1)))), true)
	k.BankKeeper().AddWei(ctx, sdk.AccAddress(evmAddr[:]), bankkeeper.OneUseiInWei.Sub(sdk.OneInt()))
	ctx, err = handler.AnteHandle(ctx, mockTx{msgs: []sdk.Msg{msg}}, false, func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		panic("should not be called")
	})
	// not enough balance (0.9999999999999999 wei only)
	require.NotNil(t, err)
	k.BankKeeper().AddWei(ctx, seiAddr, sdk.OneInt())
	ctx, err = handler.AnteHandle(ctx, mockTx{msgs: []sdk.Msg{msg}}, false, func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		panic("should not be called")
	})
	require.Nil(t, err)
	associated, ok := k.GetEVMAddress(ctx, seiAddr)
	require.True(t, ok)
	require.Equal(t, evmAddr, associated)
}

func TestGetVersion(t *testing.T) {
	ethCfg := &params.ChainConfig{}
	ctx := sdk.Context{}.WithBlockHeight(10).WithBlockTime(time.Now())
	zero := uint64(0)

	ethCfg.LondonBlock = big.NewInt(0)
	ethCfg.CancunTime = &zero
	require.Equal(t, derived.Cancun, ante.GetVersion(ctx, ethCfg))

	ethCfg.CancunTime = nil
	require.Equal(t, derived.London, ante.GetVersion(ctx, ethCfg))
}

func TestAnteDeps(t *testing.T) {
	k, _ := testkeeper.MockEVMKeeper()
	handler := ante.NewEVMPreprocessDecorator(k, k.AccountKeeper())
	msg, _ := types.NewMsgEVMTransaction(&ethtx.LegacyTx{GasLimit: 100})
	msg.Derived = &derived.Derived{
		SenderEVMAddr: common.BytesToAddress([]byte("senderevm")),
		SenderSeiAddr: []byte("sendersei"),
		PubKey:        &secp256k1.PubKey{Key: []byte("pubkey")},
	}
	deps, err := handler.AnteDeps(nil, mockTx{msgs: []sdk.Msg{msg}}, 0, func(txDeps []sdkacltypes.AccessOperation, tx sdk.Tx, txIndex int) ([]sdkacltypes.AccessOperation, error) {
		return txDeps, nil
	})
	require.Nil(t, err)
	require.Equal(t, 12, len(deps))
}

func TestEVMAddressDecorator(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper()
	privKey := testkeeper.MockPrivateKey()
	sender, evmAddr := testkeeper.PrivateKeyToAddresses(privKey)
	recipient, _ := testkeeper.MockAddressPair()
	handler := ante.NewEVMAddressDecorator(k, k.AccountKeeper())
	msg := banktypes.NewMsgSend(sender, recipient, sdk.NewCoins(sdk.NewCoin("usei", sdk.OneInt())))
	k.AccountKeeper().SetAccount(ctx, authtypes.NewBaseAccount(sender, privKey.PubKey(), 1, 1))
	ctx, err := handler.AnteHandle(ctx, mockTx{msgs: []sdk.Msg{msg}, signers: []sdk.AccAddress{sender}}, false, func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		return ctx, nil
	})
	require.Nil(t, err)
	associatedEvmAddr, associated := k.GetEVMAddress(ctx, sender)
	require.True(t, associated)
	require.Equal(t, evmAddr, associatedEvmAddr)
}

// MockTxNotSigVerifiable is a simple mock transaction type that implements sdk.Tx but not SigVerifiableTx
type MockTxIncompatible struct {
	msgs []sdk.Msg
}

func (m MockTxIncompatible) GetMsgs() []sdk.Msg {
	return m.msgs
}

func (m MockTxIncompatible) ValidateBasic() error {
	return nil
}

func TestEVMAddressDecoratorContinueDespiteErrors(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper()
	handler := ante.NewEVMAddressDecorator(k, k.AccountKeeper())

	_, err := handler.AnteHandle(ctx, MockTxIncompatible{}, false, func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		return ctx, nil
	})
	require.NotNil(t, err)
	require.Contains(t, err.Error(), "invalid tx type")

	// Prepare a SigVerifiableTx with no public key
	privKey := testkeeper.MockPrivateKey()
	sender, _ := testkeeper.PrivateKeyToAddresses(privKey)
	k.AccountKeeper().SetAccount(ctx, authtypes.NewBaseAccount(sender, &secp256k1.PubKey{}, 1, 1)) // deliberately no pubkey set
	msg := banktypes.NewMsgSend(sender, sender, sdk.NewCoins(sdk.NewCoin("usei", sdk.OneInt())))   // to self to simplify
	ctx, err = handler.AnteHandle(ctx, mockTx{msgs: []sdk.Msg{msg}, signers: []sdk.AccAddress{sender}}, false, func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		return ctx, nil
	})
	// Since the handler logs the error but does not stop processing, we expect no error returned
	require.Nil(t, err, "Expected no error from AnteHandle despite missing public key")

	k.AccountKeeper().SetAccount(ctx, authtypes.NewBaseAccount(sender, nil, 1, 1))              // deliberately no pubkey set
	msg = banktypes.NewMsgSend(sender, sender, sdk.NewCoins(sdk.NewCoin("usei", sdk.OneInt()))) // to self to simplify
	ctx, err = handler.AnteHandle(ctx, mockTx{msgs: []sdk.Msg{msg}, signers: []sdk.AccAddress{sender}}, false, func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		return ctx, nil
	})

	// Since the handler logs the error but does not stop processing, we expect no error returned
	require.Nil(t, err, "Expected no error from AnteHandle despite nil public key")

	// Prepare a SigVerifiableTx with a pubkey that fails to parse
	brokenPubKey := &secp256k1.PubKey{Key: []byte{1, 2, 3}} // deliberately too short to be valid
	k.AccountKeeper().SetAccount(ctx, authtypes.NewBaseAccount(sender, brokenPubKey, 1, 1))
	ctx, err = handler.AnteHandle(ctx, mockTx{msgs: []sdk.Msg{msg}, signers: []sdk.AccAddress{sender}}, false, func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		return ctx, nil
	})

	// Since the handler logs the error but does not stop processing, we expect no error returned
	require.Nil(t, err, "Expected no error from AnteHandle despite inability to parse public key")
}
