package keeper

import (
	"encoding/hex"
	"sync"
	"testing"
	"time"

	"github.com/cosmos/go-bip39"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/hd"
	cryptotypes "github.com/sei-protocol/sei-chain/sei-cosmos/crypto/types"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"

	"github.com/sei-protocol/sei-chain/app"
	evmkeeper "github.com/sei-protocol/sei-chain/giga/deps/xevm/keeper"
	evmtypes "github.com/sei-protocol/sei-chain/giga/deps/xevm/types"
	"github.com/sei-protocol/sei-chain/occ_tests/utils"
	"github.com/stretchr/testify/require"
)

var mockKeeper *evmkeeper.Keeper
var mockCtx sdk.Context
var mtx = &sync.Mutex{}

func MockApp(t *testing.T) (*app.App, sdk.Context) {
	accts := utils.NewTestAccounts(1)
	testWrapper := app.NewGigaTestWrapper(t, time.Now(), accts[0].PublicKey, false, false)
	testApp := testWrapper.App
	ctx := testApp.GetContextForDeliverTx([]byte{}).WithBlockHeight(8).WithBlockTime(time.Now())
	ctx = ctx.WithMultiStore(ctx.MultiStore().CacheMultiStore())
	return testApp, ctx
}

func MockEVMKeeper(t *testing.T) (*evmkeeper.Keeper, sdk.Context) {
	testApp, ctx := MockApp(t)
	k := testApp.GigaEvmKeeper
	ctx = ctx.WithMultiStore(ctx.MultiStore().CacheMultiStore())
	k.InitGenesis(ctx, *evmtypes.DefaultGenesis())

	// mint some coins to a sei address
	seiAddr, err := sdk.AccAddressFromHex(common.Bytes2Hex([]byte("seiAddr")))
	if err != nil {
		panic(err)
	}
	err = testApp.BankKeeper.MintCoins(ctx, "evm", sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(10))))
	if err != nil {
		panic(err)
	}
	err = testApp.BankKeeper.SendCoinsFromModuleToAccount(ctx, "evm", seiAddr, sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(10))))
	if err != nil {
		panic(err)
	}
	return &k, ctx
}

func MockEVMKeeperPrecompiles(t *testing.T) (*evmkeeper.Keeper, sdk.Context) {
	testApp := app.Setup(t, false, true, false)
	ctx := testApp.GetContextForDeliverTx([]byte{}).WithBlockHeight(8).WithBlockTime(time.Now())
	k := testApp.GigaEvmKeeper
	k.InitGenesis(ctx, *evmtypes.DefaultGenesis())

	// mint some coins to a sei address
	seiAddr, err := sdk.AccAddressFromHex(common.Bytes2Hex([]byte("seiAddr")))
	if err != nil {
		panic(err)
	}
	err = testApp.BankKeeper.MintCoins(ctx, "evm", sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(10))))
	if err != nil {
		panic(err)
	}
	err = testApp.BankKeeper.SendCoinsFromModuleToAccount(ctx, "evm", seiAddr, sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(10))))
	if err != nil {
		panic(err)
	}
	return &k, ctx
}

func MockAddressPair() (sdk.AccAddress, common.Address) {
	return PrivateKeyToAddresses(MockPrivateKey())
}

func MockPrivateKey() cryptotypes.PrivKey {
	// Generate a new Sei private key
	entropySeed, _ := bip39.NewEntropy(256)
	mnemonic, _ := bip39.NewMnemonic(entropySeed)
	algo := hd.Secp256k1
	derivedPriv, _ := algo.Derive()(mnemonic, "", "")
	return algo.Generate()(derivedPriv)
}

func PrivateKeyToAddresses(privKey cryptotypes.PrivKey) (sdk.AccAddress, common.Address) {
	// Encode the private key to hex (i.e. what wallets do behind the scene when users reveal private keys)
	testPrivHex := hex.EncodeToString(privKey.Bytes())

	// Sign an Ethereum transaction with the hex private key
	key, _ := crypto.HexToECDSA(testPrivHex)
	msg := crypto.Keccak256([]byte("foo"))
	sig, _ := crypto.Sign(msg, key)

	// Recover the public keys from the Ethereum signature
	recoveredPub, _ := crypto.Ecrecover(msg, sig)
	pubKey, _ := crypto.UnmarshalPubkey(recoveredPub)

	return sdk.AccAddress(privKey.PubKey().Address()), crypto.PubkeyToAddress(*pubKey)
}

func UseiCoins(amount int64) sdk.Coins {
	return sdk.NewCoins(sdk.NewCoin(sdk.MustGetBaseDenom(), sdk.NewInt(amount)))
}

func WaitForReceipt(t *testing.T, k *evmkeeper.Keeper, ctx sdk.Context, txHash common.Hash) *evmtypes.Receipt {
	t.Helper()
	var receipt *evmtypes.Receipt
	require.Eventually(t, func() bool {
		var err error
		receipt, err = k.GetReceipt(ctx, txHash)
		return err == nil
	}, 2*time.Second, 10*time.Millisecond)
	return receipt
}

func WaitForReceiptFromStore(t *testing.T, k *evmkeeper.Keeper, ctx sdk.Context, txHash common.Hash) *evmtypes.Receipt {
	t.Helper()
	var receipt *evmtypes.Receipt
	require.Eventually(t, func() bool {
		var err error
		receipt, err = k.GetReceiptFromReceiptStore(ctx, txHash)
		return err == nil
	}, 2*time.Second, 10*time.Millisecond)
	return receipt
}
