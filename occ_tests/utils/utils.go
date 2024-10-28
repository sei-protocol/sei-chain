package utils

import (
	"crypto/ecdsa"
	"encoding/hex"
	"math/big"
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
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
	"github.com/tendermint/tendermint/abci/types"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"

	"github.com/sei-protocol/sei-chain/app"
	utils2 "github.com/sei-protocol/sei-chain/utils"
	"github.com/sei-protocol/sei-chain/x/evm/config"
	types2 "github.com/sei-protocol/sei-chain/x/evm/types"
	minttypes "github.com/sei-protocol/sei-chain/x/mint/types"
)

// ignoreStoreKeys are store keys that are not compared
var ignoredStoreKeys = map[string]struct{}{
	"mem_capability": {},
	"epoch":          {},
	"deferredcache":  {},
}

type TestMessage struct {
	Msg       sdk.Msg
	Type      string
	EVMSigner TestAcct
	IsEVM     bool
}

type TestContext struct {
	Ctx            sdk.Context
	CodeID         uint64
	Validator      TestAcct
	TestAccounts   []TestAcct
	ContractKeeper *wasmkeeper.PermissionedKeeper
	TestApp        *app.App
}

type TestAcct struct {
	ValidatorAddress sdk.ValAddress
	AccountAddress   sdk.AccAddress
	PrivateKey       cryptotypes.PrivKey
	PublicKey        cryptotypes.PubKey
	EvmAddress       common.Address
	EvmSigner        ethtypes.Signer
	EvmPrivateKey    *ecdsa.PrivateKey
}

func NewTestAccounts(count int) []TestAcct {
	testAccounts := make([]TestAcct, 0, count)
	for i := 0; i < count; i++ {
		testAccounts = append(testAccounts, NewSigner())
	}
	return testAccounts
}

func NewSigner() TestAcct {
	priv1, pubKey, acct := testdata.KeyTestPubAddr()
	val := addressToValAddress(acct)

	pvKeyHex := hex.EncodeToString(priv1.Bytes())
	key, _ := crypto.HexToECDSA(pvKeyHex)
	ethCfg := types2.DefaultChainConfig().EthereumConfig(big.NewInt(config.DefaultChainID))
	signer := ethtypes.MakeSigner(ethCfg, utils2.Big1, 1)
	address := crypto.PubkeyToAddress(key.PublicKey)

	return TestAcct{
		ValidatorAddress: val,
		AccountAddress:   acct,
		PrivateKey:       priv1,
		PublicKey:        pubKey,
		EvmAddress:       address,
		EvmSigner:        signer,
		EvmPrivateKey:    key,
	}
}

func Funds(amount int64) sdk.Coins {
	return sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(amount)))
}

func panicIfErr(err error) {
	if err != nil {
		panic(err)
	}
}

func addressToValAddress(addr sdk.AccAddress) sdk.ValAddress {
	bech, err := sdk.Bech32ifyAddressBytes(sdk.GetConfig().GetBech32ValidatorAddrPrefix(), addr.Bytes())
	panicIfErr(err)
	valAddr, err := sdk.ValAddressFromBech32(bech)
	panicIfErr(err)
	return valAddr
}

// NewTestContext initializes a new TestContext with a new app and a new contract
func NewTestContext(t *testing.T, testAccts []TestAcct, blockTime time.Time, workers int, occEnabled bool) *TestContext {
	contractFile := "../integration_test/contracts/mars.wasm"
	wrapper := app.NewTestWrapper(t, blockTime, testAccts[0].PublicKey, false, func(ba *baseapp.BaseApp) {
		ba.SetOccEnabled(occEnabled)
		ba.SetConcurrencyWorkers(workers)
	})
	testApp := wrapper.App
	ctx := wrapper.Ctx
	ctx = ctx.WithBlockHeader(tmproto.Header{Height: ctx.BlockHeader().Height, ChainID: ctx.BlockHeader().ChainID, Time: blockTime})
	amounts := sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(1000000000000000)), sdk.NewCoin("uusdc", sdk.NewInt(1000000000000000)))
	bankkeeper := testApp.BankKeeper
	wasmKeeper := testApp.WasmKeeper
	contractKeeper := wasmkeeper.NewDefaultPermissionKeeper(&wasmKeeper)

	// deploy a contract so we can use it
	wasm, err := os.ReadFile(contractFile)
	panicIfErr(err)
	var perm *wasmxtypes.AccessConfig
	codeID, err := contractKeeper.Create(ctx, testAccts[0].AccountAddress, wasm, perm)
	panicIfErr(err)

	for _, ta := range testAccts {
		panicIfErr(bankkeeper.MintCoins(ctx, minttypes.ModuleName, amounts))
		panicIfErr(bankkeeper.SendCoinsFromModuleToAccount(ctx, minttypes.ModuleName, ta.AccountAddress, amounts))
	}

	return &TestContext{
		Ctx:            ctx,
		CodeID:         codeID,
		Validator:      testAccts[0],
		TestAccounts:   testAccts,
		ContractKeeper: contractKeeper,
		TestApp:        testApp,
	}
}

func toTxBytes(testCtx *TestContext, msgs []*TestMessage) [][]byte {
	txs := make([][]byte, 0, len(msgs))
	tc := app.MakeEncodingConfig().TxConfig

	priv := testCtx.TestAccounts[0].PrivateKey
	acct := testCtx.TestApp.AccountKeeper.GetAccount(testCtx.Ctx, testCtx.TestAccounts[0].AccountAddress)

	for _, tm := range msgs {
		m := tm.Msg
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
					Payer:    testCtx.TestAccounts[0].AccountAddress.String(),
					Granter:  testCtx.TestAccounts[0].AccountAddress.String(),
				},
			},
		})

		if tm.IsEVM {
			amounts := sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(1000000000000000000)), sdk.NewCoin("uusdc", sdk.NewInt(1000000000000000)))

			// fund account so it has funds
			if err := testCtx.TestApp.BankKeeper.MintCoins(testCtx.Ctx, minttypes.ModuleName, amounts); err != nil {
				panic(err)
			}
			if err := testCtx.TestApp.BankKeeper.SendCoinsFromModuleToAccount(testCtx.Ctx, minttypes.ModuleName, tm.EVMSigner.AccountAddress, amounts); err != nil {
				panic(err)
			}

			b, err := tc.TxEncoder()(tBuilder.GetTx())
			if err != nil {
				panic(err)
			}
			txs = append(txs, b)
			continue
		}

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
func RunWithOCC(testCtx *TestContext, msgs []*TestMessage) ([]types.Event, []*types.ExecTxResult, types.ResponseEndBlock, error) {
	return runTxs(testCtx, msgs, true)
}

// RunWithoutOCC runs the given messages without OCC enabled
func RunWithoutOCC(testCtx *TestContext, msgs []*TestMessage) ([]types.Event, []*types.ExecTxResult, types.ResponseEndBlock, error) {
	return runTxs(testCtx, msgs, false)
}

func runTxs(testCtx *TestContext, msgs []*TestMessage, occ bool) ([]types.Event, []*types.ExecTxResult, types.ResponseEndBlock, error) {
	app.EnableOCC = occ
	txs := toTxBytes(testCtx, msgs)
	req := &types.RequestFinalizeBlock{
		Txs:    txs,
		Height: testCtx.Ctx.BlockHeader().Height,
	}

	return testCtx.TestApp.ProcessBlock(testCtx.Ctx, txs, req, req.DecidedLastCommit)
}

func JoinMsgs(msgsList ...[]*TestMessage) []*TestMessage {
	var result []*TestMessage
	for _, testMsg := range msgsList {
		result = append(result, testMsg...)
	}
	return result
}

func Shuffle(msgs []*TestMessage) []*TestMessage {
	result := make([]*TestMessage, 0, len(msgs))
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
