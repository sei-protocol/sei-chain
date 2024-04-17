package keeper

import (
	"encoding/hex"
	"math/big"

	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/crypto/hd"
	"github.com/cosmos/cosmos-sdk/store"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authkeeper "github.com/cosmos/cosmos-sdk/x/auth/keeper"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	bankkeeper "github.com/cosmos/cosmos-sdk/x/bank/keeper"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	paramskeeper "github.com/cosmos/cosmos-sdk/x/params/keeper"
	typesparams "github.com/cosmos/cosmos-sdk/x/params/types"
	"github.com/cosmos/go-bip39"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/tendermint/tendermint/libs/log"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	tmdb "github.com/tendermint/tm-db"

	"github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

func MockEVMKeeper() (*Keeper, sdk.Context) {
	evmStoreKey := sdk.NewKVStoreKey(types.StoreKey)
	authStoreKey := sdk.NewKVStoreKey(authtypes.StoreKey)
	bankStoreKey := sdk.NewKVStoreKey(banktypes.StoreKey)
	keyParams := sdk.NewKVStoreKey(typesparams.StoreKey)
	tKeyParams := sdk.NewTransientStoreKey(typesparams.TStoreKey)

	db := tmdb.NewMemDB()
	stateStore := store.NewCommitMultiStore(db)
	stateStore.MountStoreWithDB(evmStoreKey, sdk.StoreTypeIAVL, db)
	stateStore.MountStoreWithDB(authStoreKey, sdk.StoreTypeIAVL, db)
	stateStore.MountStoreWithDB(bankStoreKey, sdk.StoreTypeIAVL, db)
	stateStore.MountStoreWithDB(tKeyParams, sdk.StoreTypeTransient, db)
	stateStore.MountStoreWithDB(keyParams, sdk.StoreTypeIAVL, db)
	_ = stateStore.LoadLatestVersion()

	cdc := codec.NewProtoCodec(app.MakeEncodingConfig().InterfaceRegistry)

	paramsKeeper := paramskeeper.NewKeeper(cdc, codec.NewLegacyAmino(), keyParams, tKeyParams)
	accountKeeper := authkeeper.NewAccountKeeper(cdc, authStoreKey, paramsKeeper.Subspace(authtypes.ModuleName), authtypes.ProtoBaseAccount, map[string][]string{types.ModuleName: {authtypes.Minter}})
	bankKeeper := bankkeeper.NewBaseKeeper(cdc, bankStoreKey, accountKeeper, paramsKeeper.Subspace(banktypes.ModuleName), map[string]bool{})

	ctx := sdk.NewContext(stateStore, tmproto.Header{}, false, log.NewNopLogger())
	k := NewKeeper(evmStoreKey, paramsKeeper.Subspace(types.ModuleName), big.NewInt(1), bankKeeper, &accountKeeper)
	k.SetParams(ctx, types.DefaultParams())
	return k, ctx
}

func MockAddressPair() (sdk.AccAddress, common.Address) {
	// Generate a new Sei private key
	entropySeed, _ := bip39.NewEntropy(256)
	mnemonic, _ := bip39.NewMnemonic(entropySeed)
	algo := hd.Secp256k1
	derivedPriv, _ := algo.Derive()(mnemonic, "", "")
	privKey := algo.Generate()(derivedPriv)

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
