package utils

import (
	"context"

	"math/rand"
	"os"
	"testing"
	"time"

	wasmkeeper "github.com/CosmWasm/wasmd/x/wasm/keeper"
	wasmxtypes "github.com/CosmWasm/wasmd/x/wasm/types"
	"github.com/cosmos/cosmos-sdk/baseapp"
	clienttx "github.com/cosmos/cosmos-sdk/client/tx"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	"github.com/cosmos/cosmos-sdk/store"
	"github.com/cosmos/cosmos-sdk/testutil/testdata"
	sdk "github.com/cosmos/cosmos-sdk/types"
	txtype "github.com/cosmos/cosmos-sdk/types/tx"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	"github.com/cosmos/cosmos-sdk/x/auth/tx"
	"github.com/stretchr/testify/require"
	"github.com/tendermint/tendermint/abci/types"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"

	"github.com/sei-protocol/sei-chain/app"
	dexcache "github.com/sei-protocol/sei-chain/x/dex/cache"
	dextypes "github.com/sei-protocol/sei-chain/x/dex/types"
	dexutils "github.com/sei-protocol/sei-chain/x/dex/utils"
	minttypes "github.com/sei-protocol/sei-chain/x/mint/types"
)

// ignoreStoreKeys are store keys that are not compared
var ignoredStoreKeys = map[string]struct{}{
	"mem_capability": {},
	"epoch":          {},
	"deferredcache":  {},
}

type TestContext struct {
	Ctx    sdk.Context
	CodeID uint64

	Signer         Signer
	TestAccount1   sdk.AccAddress
	TestAccount2   sdk.AccAddress
	ContractKeeper *wasmkeeper.PermissionedKeeper
	TestApp        *app.App
}

type Signer struct {
	Sender     sdk.AccAddress
	PrivateKey cryptotypes.PrivKey
	PublicKey  cryptotypes.PubKey
}

func NewSigner() Signer {
	priv1, pubKey, sender := testdata.KeyTestPubAddr()
	return Signer{
		Sender:     sender,
		PrivateKey: priv1,
		PublicKey:  pubKey,
	}
}

func Funds(amount int64) sdk.Coins {
	return sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(amount)))
}

// NewTestContext initializes a new TestContext with a new app and a new contract
func NewTestContext(signer Signer, blockTime time.Time, workers int) *TestContext {
	contractFile := "../integration_test/contracts/mars.wasm"
	testApp := app.Setup(false, func(ba *baseapp.BaseApp) {
		ba.SetConcurrencyWorkers(workers)
	})
	ctx := testApp.BaseApp.NewContext(false, tmproto.Header{Time: time.Now(), Height: 1})
	ctx = ctx.WithChainID("chainId")
	ctx = ctx.WithContext(context.WithValue(ctx.Context(), dexutils.DexMemStateContextKey, dexcache.NewMemState(testApp.GetMemKey(dextypes.MemStoreKey))))
	ctx = ctx.WithBlockGasMeter(sdk.NewGasMeter(100000000))
	ctx = ctx.WithBlockHeader(tmproto.Header{Height: ctx.BlockHeader().Height, ChainID: ctx.BlockHeader().ChainID, Time: blockTime})
	testAccount, _ := sdk.AccAddressFromBech32("sei1h9yjz89tl0dl6zu65dpxcqnxfhq60wxx8s5kag")
	depositAccount, _ := sdk.AccAddressFromBech32("sei1yezq49upxhunjjhudql2fnj5dgvcwjj87pn2wx")
	amounts := sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(1000000000000000)), sdk.NewCoin("uusdc", sdk.NewInt(1000000000000000)))
	bankkeeper := testApp.BankKeeper
	bankkeeper.MintCoins(ctx, minttypes.ModuleName, amounts)
	bankkeeper.SendCoinsFromModuleToAccount(ctx, minttypes.ModuleName, testAccount, amounts)
	bankkeeper.MintCoins(ctx, minttypes.ModuleName, amounts)
	bankkeeper.SendCoinsFromModuleToAccount(ctx, minttypes.ModuleName, depositAccount, amounts)
	bankkeeper.MintCoins(ctx, minttypes.ModuleName, amounts)
	bankkeeper.SendCoinsFromModuleToAccount(ctx, minttypes.ModuleName, signer.Sender, amounts)

	wasm, err := os.ReadFile(contractFile)
	if err != nil {
		panic(err)
	}
	wasmKeeper := testApp.WasmKeeper
	contractKeeper := wasmkeeper.NewDefaultPermissionKeeper(&wasmKeeper)
	var perm *wasmxtypes.AccessConfig
	codeID, err := contractKeeper.Create(ctx, testAccount, wasm, perm)
	if err != nil {
		panic(err)
	}

	return &TestContext{
		Ctx:            ctx,
		CodeID:         codeID,
		Signer:         signer,
		TestAccount1:   testAccount,
		TestAccount2:   depositAccount,
		ContractKeeper: contractKeeper,
		TestApp:        testApp,
	}
}

func toTxBytes(testCtx *TestContext, msgs []sdk.Msg) [][]byte {
	var txs [][]byte
	tc := app.MakeEncodingConfig().TxConfig

	priv := testCtx.Signer.PrivateKey
	acct := testCtx.TestApp.AccountKeeper.GetAccount(testCtx.Ctx, testCtx.Signer.Sender)

	for _, m := range msgs {
		a, err := codectypes.NewAnyWithValue(m)
		if err != nil {
			panic(err)
		}

		tBuilder := tx.WrapTx(&txtype.Tx{
			Body: &txtype.TxBody{
				Messages: []*codectypes.Any{a},
			},
			AuthInfo: &txtype.AuthInfo{
				Fee: &txtype.Fee{
					Amount:   Funds(10000000000),
					GasLimit: 10000000000,
					Payer:    testCtx.Signer.Sender.String(),
					Granter:  testCtx.Signer.Sender.String(),
				},
			},
		})

		err = tBuilder.SetSignatures(signing.SignatureV2{
			PubKey: priv.PubKey(),
			Data: &signing.SingleSignatureData{
				SignMode:  tc.SignModeHandler().DefaultMode(),
				Signature: nil,
			},
			Sequence: acct.GetSequence(),
		})
		if err != nil {
			panic(err)
		}

		signerData := authsigning.SignerData{
			ChainID:       testCtx.Ctx.ChainID(),
			Sequence:      acct.GetSequence(),
			AccountNumber: acct.GetAccountNumber(),
		}

		sigV2, err := clienttx.SignWithPrivKey(
			tc.SignModeHandler().DefaultMode(), signerData,
			tBuilder, priv, tc, acct.GetSequence())

		if err != nil {
			panic(err)
		}

		err = tBuilder.SetSignatures(sigV2)
		if err != nil {
			panic(err)
		}

		b, err := tc.TxEncoder()(tBuilder.GetTx())
		if err != nil {
			panic(err)
		}
		txs = append(txs, b)

		if err := acct.SetSequence(acct.GetSequence() + 1); err != nil {
			panic(err)
		}
	}
	return txs
}

// RunWithOCC runs the given messages with OCC enabled, number of workers is configured via context
func RunWithOCC(testCtx *TestContext, msgs []sdk.Msg) ([]types.Event, []*types.ExecTxResult, types.ResponseEndBlock, error) {
	return runTxs(testCtx, msgs, true)
}

// RunWithoutOCC runs the given messages without OCC enabled
func RunWithoutOCC(testCtx *TestContext, msgs []sdk.Msg) ([]types.Event, []*types.ExecTxResult, types.ResponseEndBlock, error) {
	return runTxs(testCtx, msgs, false)
}

func runTxs(testCtx *TestContext, msgs []sdk.Msg, occ bool) ([]types.Event, []*types.ExecTxResult, types.ResponseEndBlock, error) {
	app.EnableOCC = occ
	txs := toTxBytes(testCtx, msgs)
	req := &types.RequestFinalizeBlock{
		Txs:    txs,
		Height: testCtx.Ctx.BlockHeader().Height,
	}

	return testCtx.TestApp.ProcessBlock(testCtx.Ctx, txs, req, req.DecidedLastCommit)
}

func JoinMsgs(msgsList ...[]sdk.Msg) []sdk.Msg {
	var result []sdk.Msg
	for _, msgs := range msgsList {
		result = append(result, msgs...)
	}
	return result
}

func Shuffle(msgs []sdk.Msg) []sdk.Msg {
	var result []sdk.Msg
	for _, i := range rand.Perm(len(msgs)) {
		result = append(result, msgs[i])
	}
	return result
}

func CompareStores(t *testing.T, storeKey sdk.StoreKey, expected store.KVStore, actual store.KVStore, testName string) {
	if _, ok := ignoredStoreKeys[storeKey.Name()]; ok {
		return
	}

	iexpected := expected.Iterator(nil, nil)
	defer iexpected.Close()

	iactual := actual.Iterator(nil, nil)
	defer iactual.Close()

	// Iterate over the expected store
	for ; iexpected.Valid(); iexpected.Next() {
		key := iexpected.Key()
		expectedValue := iexpected.Value()

		// Ensure the key exists in the actual store
		actualValue := actual.Get(key)
		require.NotNil(t, actualValue, "%s: key not found in the %s store: %s", testName, storeKey.Name(), string(key))

		// Compare the values for the current key
		require.Equal(t, string(expectedValue), string(actualValue), "%s: %s value mismatch for key: %s", testName, storeKey.Name(), string(key))

		// Move to the next key in the actual store for the upcoming iteration
		iactual.Next()
	}

	// Ensure there are no extra keys in the actual store
	require.False(t, iactual.Valid(), "%s: Extra key found in the actual store: %s", testName, storeKey.Name())
}
