package utils

import (
	"crypto/ecdsa"
	"encoding/hex"
	"fmt"
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
	"github.com/sei-protocol/sei-load/generator"
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
	CW20CodeID     uint64
	Validator      TestAcct
	TestAccounts   []TestAcct
	ContractKeeper *wasmkeeper.PermissionedKeeper
	TestApp        *app.App
	CW20Addrs      []string
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

// GetSeiAddress converts an EVM address to a Sei address (direct cast, same as GetSeiAddressOrDefault)
func GetSeiAddress(evmAddr common.Address) sdk.AccAddress {
	return sdk.AccAddress(evmAddr[:])
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

// deployCW20Token deploys a CW20 token contract for testing
func deployCW20Token(tCtx *TestContext, i int) (string, error) {
	// CW20 instantiate message with initial balances for the test accounts
	instantiateMsgCW20 := fmt.Sprintf(`{
		"name": "TestToken",
		"symbol": "TTK",
		"decimals": 6,
		"initial_balances": [
			{"address": "%s", "amount": "1000000"},
			{"address": "%s", "amount": "2000000"}
		],
		"mint": {
			"minter": "%s",
			"cap": "10000000"
		}
	}`, tCtx.TestAccounts[0].AccountAddress.String(), tCtx.TestAccounts[1].AccountAddress.String(), tCtx.TestAccounts[0].AccountAddress.String())

	// Execute the message to instantiate the contract
	contractAddr, _, err := tCtx.ContractKeeper.Instantiate(
		tCtx.Ctx,
		tCtx.CW20CodeID,
		tCtx.TestAccounts[0].AccountAddress,
		tCtx.TestAccounts[0].AccountAddress,
		[]byte(instantiateMsgCW20),
		fmt.Sprintf("test-cw20-%d", i),
		Funds(100000),
	)

	if err != nil {
		return "", err
	}

	return contractAddr.String(), nil
}

// NewTestContext initializes a new TestContext with a new app and a new contract
func NewTestContext(t *testing.T, testAccts []TestAcct, blockTime time.Time, workers int, occEnabled bool) *TestContext {
	return newTestContext(t, testAccts, blockTime, workers, occEnabled, false)
}

// NewTestContextWithTracing initializes a new TestContext with tracing enabled
func NewTestContextWithTracing(t *testing.T, testAccts []TestAcct, blockTime time.Time, workers int, occEnabled bool) *TestContext {
	return newTestContext(t, testAccts, blockTime, workers, occEnabled, true)
}

func newTestContext(t *testing.T, testAccts []TestAcct, blockTime time.Time, workers int, occEnabled bool, enableTracing bool) *TestContext {
	contractFile := "../integration_test/contracts/mars.wasm"
	cw20ContractFile := "../contracts/wasm/cw20_base.wasm"

	var wrapper *app.TestWrapper
	if enableTracing {
		wrapper = app.NewTestWrapperWithTracing(t, blockTime, testAccts[0].PublicKey, true, func(ba *baseapp.BaseApp) {
			ba.SetOccEnabled(occEnabled)
			ba.SetConcurrencyWorkers(workers)
		})
	} else {
		wrapper = app.NewTestWrapper(t, blockTime, testAccts[0].PublicKey, true, func(ba *baseapp.BaseApp) {
			ba.SetOccEnabled(occEnabled)
			ba.SetConcurrencyWorkers(workers)
		})
	}
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

	// Upload the CW20 contract
	cw20Wasm, err := os.ReadFile(cw20ContractFile)
	panicIfErr(err)
	cw20CodeID, err := contractKeeper.Create(ctx, testAccts[0].AccountAddress, cw20Wasm, perm)
	panicIfErr(err)

	for _, ta := range testAccts {
		panicIfErr(bankkeeper.MintCoins(ctx, minttypes.ModuleName, amounts))
		panicIfErr(bankkeeper.SendCoinsFromModuleToAccount(ctx, minttypes.ModuleName, ta.AccountAddress, amounts))
	}

	tctx := &TestContext{
		Ctx:            ctx,
		CodeID:         codeID,
		CW20CodeID:     cw20CodeID,
		Validator:      testAccts[0],
		TestAccounts:   testAccts,
		ContractKeeper: contractKeeper,
		TestApp:        testApp,
	}

	for i := 0; i < 10; i++ {
		addr, err := deployCW20Token(tctx, i)
		panicIfErr(err)
		tctx.CW20Addrs = append(tctx.CW20Addrs, addr)
	}

	return tctx
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
			_, ok := testCtx.TestApp.EvmKeeper.GetSeiAddress(testCtx.Ctx, tm.EVMSigner.EvmAddress)
			if !ok {
				fmt.Println("funding", tm.EVMSigner.EvmAddress.Hex())
				seiAddr := testCtx.TestApp.EvmKeeper.GetSeiAddressOrDefault(testCtx.Ctx, tm.EVMSigner.EvmAddress)
				amounts := sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(1000000000000000000)), sdk.NewCoin("uusdc", sdk.NewInt(1000000000000000)))

				// fund account so it has funds
				if err := testCtx.TestApp.BankKeeper.MintCoins(testCtx.Ctx, minttypes.ModuleName, amounts); err != nil {
					panic(err)
				}
				if err := testCtx.TestApp.BankKeeper.SendCoinsFromModuleToAccount(testCtx.Ctx, minttypes.ModuleName, seiAddr, amounts); err != nil {
					panic(err)
				}
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
func RunWithOCC(testCtx *TestContext, msgs []*TestMessage) ([]types.Event, []*types.ExecTxResult, types.ResponseEndBlock, time.Duration, error) {
	return runTxs(testCtx, msgs, true)
}

// RunWithoutOCC runs the given messages without OCC enabled
func RunWithoutOCC(testCtx *TestContext, msgs []*TestMessage) ([]types.Event, []*types.ExecTxResult, types.ResponseEndBlock, time.Duration, error) {
	return runTxs(testCtx, msgs, false)
}

func runTxs(testCtx *TestContext, msgs []*TestMessage, occ bool) ([]types.Event, []*types.ExecTxResult, types.ResponseEndBlock, time.Duration, error) {
	app.EnableOCC = occ
	txs := toTxBytes(testCtx, msgs)
	req := &types.RequestFinalizeBlock{
		Txs:    txs,
		Height: testCtx.Ctx.BlockHeader().Height,
	}

	start := time.Now()
	evts, res, reb, err := testCtx.TestApp.ProcessBlock(testCtx.Ctx, txs, req, req.DecidedLastCommit, false)
	duration := time.Since(start)
	return evts, res, reb, duration, err
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
	defer func() { _ = iexpected.Close() }()

	iactual := actual.Iterator(nil, nil)
	defer func() { _ = iactual.Close() }()

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

// InitAccounts provisions all accounts from a generator with initial balances
func (tc *TestContext) InitAccounts(g generator.Generator) {
	if g == nil {
		return // Skip if no generator provided
	}

	// Use same amounts as test accounts
	amounts := sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(1000000000000000)), sdk.NewCoin("uusdc", sdk.NewInt(1000000000000000)))
	bankkeeper := tc.TestApp.BankKeeper

	// Track seen addresses to avoid duplicates
	seen := make(map[common.Address]bool)

	// Iterate over all account pools
	for _, pool := range g.GetAccountPools() {
		var count int
		for {
			acct := pool.NextAccount()
			if _, ok := seen[acct.Address]; ok {
				break
			}
			fmt.Printf("funding[%d]: %s\n", count, acct.Address.Hex())
			count++
			seen[acct.Address] = true

			// Convert EVM address to Sei address (same as GetSeiAddressOrDefault)
			seiAddr := sdk.AccAddress(acct.Address[:])

			// Mint and send coins - account will be created automatically by bank keeper
			if err := bankkeeper.MintCoins(tc.Ctx, minttypes.ModuleName, amounts); err != nil {
				panic(fmt.Sprintf("failed to mint coins for %s: %v", seiAddr, err))
			}
			if err := bankkeeper.SendCoinsFromModuleToAccount(tc.Ctx, minttypes.ModuleName, seiAddr, amounts); err != nil {
				panic(fmt.Sprintf("failed to send coins to %s: %v", seiAddr, err))
			}
		}
	}
}
