package app_test

import (
	"fmt"
	"math/big"
	"os"
	"testing"
	"time"

	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/precompiles"
	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/keys/secp256k1"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	sdkerrors "github.com/sei-protocol/sei-chain/sei-cosmos/types/errors"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/signing"
	banktypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/bank/types"
	stakingtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/staking/types"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	wasmkeeper "github.com/sei-protocol/sei-chain/sei-wasmd/x/wasm/keeper"
	wasmtypes "github.com/sei-protocol/sei-chain/sei-wasmd/x/wasm/types"
	"github.com/sei-protocol/sei-chain/testutil/processblock"
	"github.com/stretchr/testify/require"
)

var (
	msgSendTypeURL     = sdk.MsgTypeURL(&banktypes.MsgSend{})
	msgDelegateTypeURL = sdk.MsgTypeURL(&stakingtypes.MsgDelegate{})
)

// commitDisabledMsgTypeURLs commits typeURLs as the disabled message type URLs, which the
// next block and CheckTx both read.
func commitDisabledMsgTypeURLs(t *testing.T, a *processblock.App, typeURLs ...string) {
	t.Helper()
	a.RunBlock(nil)
	// CheckTx reads the committed state, so the parameter goes to the commit multistore and
	// the next block commits it.
	ctx, write := a.NewUncachedContext(false, a.Ctx().BlockHeader()).CacheContext()
	a.ParamsKeeper.SetDisabledMsgTypeURLs(ctx, typeURLs)
	write()
	a.RunBlock(nil)
}

// TestDisabledMsgTypeURLsRefuseCosmosTxsWithoutCharging checks and delivers a bank send, and a
// transaction pairing a bank send with an enabled message, while MsgSend is disabled, then
// checks and delivers the same two transactions once the list is cleared.
func TestDisabledMsgTypeURLsRefuseCosmosTxsWithoutCharging(t *testing.T) {
	a := processblock.NewTestApp(t)
	p := processblock.CommonPreset(a)
	senders := []sdk.AccAddress{a.NewSignableAccount("send"), a.NewSignableAccount("delegate-and-send")}
	for _, sender := range senders {
		a.FundAccount(sender, 100000000)
	}
	recipient := a.NewAccount()
	commitDisabledMsgTypeURLs(t, a, msgSendTypeURL)
	amount := sdk.NewCoins(sdk.NewInt64Coin("usei", 100))
	txs := []signing.Tx{
		a.Sign(senders[0], 200000, banktypes.NewMsgSend(senders[0], recipient, amount)),
		a.Sign(senders[1], 200000,
			stakingtypes.NewMsgDelegate(senders[1], p.AllValidators[0], sdk.NewInt64Coin("usei", 1)),
			banktypes.NewMsgSend(senders[1], recipient, amount)),
	}
	balances := make([]sdk.Coin, len(senders))
	sequences := make([]uint64, len(senders))
	for i, sender := range senders {
		balances[i] = a.BankKeeper.GetBalance(a.Ctx(), sender, "usei")
		sequences[i] = a.AccountKeeper.GetAccount(a.Ctx(), sender).GetSequence()
	}

	for _, check := range checkTxs(t, a, txs) {
		require.Equal(t, sdkerrors.ErrMsgTypeDisabled.ABCICode(), check.Code, check.Log)
		require.Equal(t, sdkerrors.RootCodespace, check.Codespace)
	}
	results := a.RunBlockDetailed(txs)
	require.Len(t, results, len(txs))
	for i, result := range results {
		require.Equal(t, sdkerrors.ErrMsgTypeDisabled.ABCICode(), result.Code, result.Log)
		require.Equal(t, sdkerrors.RootCodespace, result.Codespace)
		require.Contains(t, result.Log, msgSendTypeURL)
		require.Equal(t, balances[i], a.BankKeeper.GetBalance(a.Ctx(), senders[i], "usei"),
			"the refused transaction was charged a fee")
		require.Equal(t, sequences[i], a.AccountKeeper.GetAccount(a.Ctx(), senders[i]).GetSequence())
	}
	require.True(t, a.BankKeeper.GetBalance(a.Ctx(), recipient, "usei").IsZero())

	commitDisabledMsgTypeURLs(t, a)
	for _, check := range checkTxs(t, a, txs) {
		require.Equal(t, uint32(abci.CodeTypeOK), check.Code, check.Log)
	}
	results = a.RunBlockDetailed(txs)
	require.Len(t, results, len(txs))
	for _, result := range results {
		require.Equal(t, uint32(abci.CodeTypeOK), result.Code, result.Log)
	}
	require.Equal(t, sdk.NewInt64Coin("usei", 200), a.BankKeeper.GetBalance(a.Ctx(), recipient, "usei"))
}

// checkTxs runs CheckTx on each of txs.
func checkTxs(t *testing.T, a *processblock.App, txs []signing.Tx) []*abci.ResponseCheckTxV2 {
	t.Helper()
	checks := make([]*abci.ResponseCheckTxV2, len(txs))
	for i, tx := range txs {
		txBytes, err := processblock.TxConfig.TxEncoder()(tx)
		require.NoError(t, err)
		checks[i] = a.CheckTx(t.Context(), &abci.RequestCheckTxV2{Tx: txBytes})
	}
	return checks
}

// TestDisabledMsgTypeURLsLeaveContractDispatchOpen executes a contract that pays out through a
// bank send and one that delegates, while MsgSend and MsgDelegate are disabled.
func TestDisabledMsgTypeURLsLeaveContractDispatchOpen(t *testing.T) {
	a := processblock.NewTestApp(t)
	p := processblock.CommonPreset(a)
	beneficiary := a.NewAccount()
	validator := p.AllValidators[0]
	escrow := instantiateTestContract(t, a, p.Admin, "../sei-wasmd/x/wasm/keeper/testdata/hackatom.wasm",
		fmt.Sprintf(`{"verifier":%q,"beneficiary":%q}`, p.Admin.String(), beneficiary.String()), sdk.NewCoins(sdk.NewInt64Coin("usei", 1000)))
	derivative := instantiateTestContract(t, a, p.Admin, "../sei-wasmd/x/wasm/keeper/testdata/staking.wasm",
		fmt.Sprintf(`{"name":"Staking Derivatives","symbol":"DRV","decimals":0,"validator":%q,"exit_tax":"0.10","min_withdrawal":"100"}`, validator.String()), nil)
	commitDisabledMsgTypeURLs(t, a, msgSendTypeURL, msgDelegateTypeURL)

	results := a.RunBlockDetailed([]signing.Tx{
		p.AdminSign(a, &wasmtypes.MsgExecuteContract{
			Sender: p.Admin.String(), Contract: escrow.String(), Msg: []byte(`{"release":{}}`),
		}),
		p.AdminSign(a, &wasmtypes.MsgExecuteContract{
			Sender: p.Admin.String(), Contract: derivative.String(), Msg: []byte(`{"bond":{}}`),
			Funds: sdk.NewCoins(sdk.NewInt64Coin("usei", 1000)),
		}),
	})

	require.Len(t, results, 2)
	for _, result := range results {
		require.Equal(t, uint32(abci.CodeTypeOK), result.Code, result.Log)
	}
	require.Equal(t, sdk.NewInt64Coin("usei", 1000), a.BankKeeper.GetBalance(a.Ctx(), beneficiary, "usei"))
	_, found := a.StakingKeeper.GetDelegation(a.Ctx(), derivative, validator)
	require.True(t, found, "the delegation the contract dispatched was refused")
}

// instantiateTestContract stores the wasm code at path and instantiates it from creator.
func instantiateTestContract(t *testing.T, a *processblock.App, creator sdk.AccAddress, path, initMsg string, funds sdk.Coins) sdk.AccAddress {
	t.Helper()
	code, err := os.ReadFile(path)
	require.NoError(t, err)
	contractKeeper := wasmkeeper.NewDefaultPermissionKeeper(&a.WasmKeeper)
	codeID, err := contractKeeper.Create(a.Ctx(), creator, code, nil)
	require.NoError(t, err)
	contract, _, err := contractKeeper.Instantiate(a.Ctx(), codeID, creator, creator, []byte(initMsg), "test", funds)
	require.NoError(t, err)
	return contract
}

// TestDisabledMsgTypeURLsLeavePrecompilesOpen delegates through the staking precompile while
// MsgDelegate is disabled.
func TestDisabledMsgTypeURLsLeavePrecompilesOpen(t *testing.T) {
	valPub := secp256k1.GenPrivKey().PubKey()
	testWrapper := app.NewTestWrapper(t, time.Now().UTC(), valPub, true)
	a := testWrapper.App
	ctx := testWrapper.Ctx.WithBlockHeight(1)
	a.ParamsKeeper.SetDisabledMsgTypeURLs(ctx, []string{msgDelegateTypeURL})
	validator := sdk.ValAddress(valPub.Address())

	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	delegator := sdk.AccAddress((&secp256k1.PrivKey{Key: crypto.FromECDSA(key)}).PubKey().Address())
	testWrapper.FundAcc(delegator, sdk.NewCoins(sdk.NewInt64Coin("usei", 10_000_000)))
	staking := precompiles.GetPrecompileInfo("staking")
	data, err := staking.ABI.Pack("delegate", validator.String())
	require.NoError(t, err)
	stakingAddress := staking.Address
	delegateTx, err := ethtypes.SignTx(ethtypes.NewTx(&ethtypes.LegacyTx{
		GasPrice: big.NewInt(100_000_000_000),
		Gas:      500_000,
		To:       &stakingAddress,
		// 100usei, in wei.
		Value: new(big.Int).Mul(big.NewInt(100), big.NewInt(1_000_000_000_000)),
		Data:  data,
	}), ethtypes.LatestSignerForChainID(a.EvmKeeper.ChainID(ctx)), key)
	require.NoError(t, err)

	req := &abci.RequestFinalizeBlock{Header: &tmproto.Header{ChainID: "sei-test", Height: 1}}
	_, txResults, _, err := a.ProcessBlock(ctx, [][]byte{signAndEncodeEVMTx(t, a, delegateTx)}, finalizeToBlockProcessReq(req), req.DecidedLastCommit, nil)

	require.NoError(t, err)
	require.Len(t, txResults, 1)
	require.Equal(t, uint32(abci.CodeTypeOK), txResults[0].Code, txResults[0].Log)
	delegation, found := a.StakingKeeper.GetDelegation(ctx, delegator, validator)
	require.True(t, found, "the staking precompile did not delegate")
	require.Equal(t, sdk.NewDec(100), delegation.Shares)
}
