package solo_test

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/params"
	pcommon "github.com/sei-protocol/sei-chain/precompiles/common"
	"github.com/sei-protocol/sei-chain/precompiles/solo"
	codectypes "github.com/sei-protocol/sei-chain/sei-cosmos/codec/types"
	cryptotypes "github.com/sei-protocol/sei-chain/sei-cosmos/crypto/types"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/types/tx/signing"
	authsigning "github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/signing"
	authtx "github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/tx"
	authtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/types"
	wasmkeeper "github.com/sei-protocol/sei-chain/sei-wasmd/x/wasm/keeper"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
)

func TestExecute(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx(nil).WithChainID("sei-test").WithIsEVM(true)
	txConfig := testkeeper.EVMTestApp.GetTxConfig()
	a := pcommon.MustGetABI(solo.F, "abi.json")
	p := solo.NewExecutor(a, k, k.BankKeeper(), k.AccountKeeper(), wasmkeeper.NewDefaultPermissionKeeper(testkeeper.EVMTestApp.WasmKeeper), testkeeper.EVMTestApp.WasmKeeper, txConfig)
	evm := vm.NewEVM(vm.BlockContext{}, nil, &params.ChainConfig{}, vm.Config{}, nil)
	_, _, err := p.Execute(ctx.WithEVMPrecompileCalledFromDelegateCall(true), &abi.Method{}, common.Address{}, common.Address{}, []interface{}{}, nil, false, evm, 0, nil)
	require.Error(t, err, "cannot delegatecall claim")
	_, _, err = p.Execute(ctx, &abi.Method{}, common.Address{}, common.Address{}, []interface{}{}, nil, false, evm, 0, nil)
	require.NoError(t, err)
	_, _, err = p.Execute(ctx.WithEVMEntryViaWasmdPrecompile(true), &abi.Method{}, common.Address{}, common.Address{}, []interface{}{}, nil, false, evm, 0, nil)
	require.Error(t, err, "cannot claim from cosmos entry")
	_, _, err = p.Execute(ctx, &abi.Method{}, common.Address{}, common.Address{}, []interface{}{}, common.Big1, false, evm, 0, nil)
	require.Error(t, err)
	_, _, err = p.Execute(ctx.WithIsEVM(false), &abi.Method{}, common.Address{}, common.Address{}, []interface{}{}, nil, false, evm, 0, nil)
	require.Error(t, err)
}

func TestClaim(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	origCtx := testkeeper.EVMTestApp.GetContextForDeliverTx(nil).WithChainID("sei-test")
	txConfig := testkeeper.EVMTestApp.GetTxConfig()
	a := pcommon.MustGetABI(solo.F, "abi.json")
	method := a.Methods["claim"]
	p := solo.NewExecutor(a, k, k.BankKeeper(), k.AccountKeeper(), wasmkeeper.NewDefaultPermissionKeeper(testkeeper.EVMTestApp.WasmKeeper), testkeeper.EVMTestApp.WasmKeeper, txConfig)
	claimeeKey := testkeeper.MockPrivateKey()
	claimee, _ := testkeeper.PrivateKeyToAddresses(claimeeKey)
	claimerKey := testkeeper.MockPrivateKey()
	_, claimer := testkeeper.PrivateKeyToAddresses(claimerKey)
	acc := authtypes.NewBaseAccount(claimee, claimeeKey.PubKey(), 10, 0)
	k.AccountKeeper().SetAccount(origCtx, acc)
	require.NoError(t, k.BankKeeper().AddCoins(origCtx, claimee, sdk.NewCoins(sdk.NewCoin("abc", sdk.NewInt(2)), sdk.NewCoin("def", sdk.NewInt(3))), false))
	// happy path
	ctx, _ := origCtx.CacheContext()
	ctx = ctx.WithGasMeter(sdk.NewGasMeter(1000000, 1, 1))
	signedMsg := signClaimMsg(t, evmtypes.NewMsgClaim(claimee, claimer), claimee, claimer, acc, claimeeKey)
	_, remainingGas, err := p.Claim(ctx, claimer, &method, []interface{}{signedMsg}, false)
	require.NoError(t, err)
	require.Greater(t, remainingGas, uint64(900000))
	require.Equal(t, sdk.NewInt(2), k.BankKeeper().GetBalance(ctx, k.GetSeiAddressOrDefault(ctx, claimer), "abc").Amount)
	require.Equal(t, sdk.NewInt(3), k.BankKeeper().GetBalance(ctx, k.GetSeiAddressOrDefault(ctx, claimer), "def").Amount)
	// ensure a replay isn't possible
	_, _, err = p.Claim(ctx, claimer, &method, []interface{}{signedMsg}, false)
	require.Error(t, err, "failed to verify signature for claim tx")
	// from staticcall
	ctx, _ = origCtx.CacheContext()
	ctx = ctx.WithGasMeter(sdk.NewGasMeter(1000000, 1, 1))
	_, remainingGas, err = p.Claim(ctx, claimer, &method, []interface{}{signClaimMsg(t, evmtypes.NewMsgClaim(claimee, claimer), claimee, claimer, acc, claimeeKey)}, true)
	require.Error(t, err, "cannot call send from staticcall")
	require.Equal(t, uint64(0), remainingGas)
	// incorrect number of args
	ctx, _ = origCtx.CacheContext()
	ctx = ctx.WithGasMeter(sdk.NewGasMeter(1000000, 1, 1))
	_, remainingGas, err = p.Claim(ctx, claimer, &method, []interface{}{signClaimMsg(t, evmtypes.NewMsgClaim(claimee, claimer), claimee, claimer, acc, claimeeKey), ""}, false)
	require.Error(t, err, "expected 1 arguments but got 2")
	require.Equal(t, uint64(0), remainingGas)
	// bad payload
	ctx, _ = origCtx.CacheContext()
	ctx = ctx.WithGasMeter(sdk.NewGasMeter(1000000, 1, 1))
	bz := signClaimMsg(t, evmtypes.NewMsgClaim(claimee, claimer), claimee, claimer, acc, claimeeKey)
	_, remainingGas, err = p.Claim(ctx, claimer, &method, []interface{}{bz[:10]}, false)
	require.Error(t, err, "failed to decode claim tx due to")
	require.Equal(t, uint64(0), remainingGas)
	// imposter
	ctx, _ = origCtx.CacheContext()
	ctx = ctx.WithGasMeter(sdk.NewGasMeter(1000000, 1, 1))
	_, imposter := testkeeper.MockAddressPair()
	_, remainingGas, err = p.Claim(ctx, imposter, &method, []interface{}{signClaimMsg(t, evmtypes.NewMsgClaim(claimee, claimer), claimee, claimer, acc, claimeeKey)}, false)
	require.Error(t, err, "claim tx is meant for")
	require.Equal(t, uint64(0), remainingGas)
	// imposter on ClaimMsg
	ctx, _ = origCtx.CacheContext()
	ctx = ctx.WithGasMeter(sdk.NewGasMeter(1000000, 1, 1))
	imposterKey := testkeeper.MockPrivateKey()
	_, remainingGas, err = p.Claim(ctx, imposter, &method, []interface{}{signClaimMsg(t, evmtypes.NewMsgClaim(claimee, imposter), claimee, imposter, acc, imposterKey)}, false)
	require.Error(t, err, "claim message is for")
	require.Equal(t, uint64(0), remainingGas)
	// account does not exist
	ctx, _ = origCtx.CacheContext()
	ctx = ctx.WithGasMeter(sdk.NewGasMeter(1000000, 1, 1))
	k.AccountKeeper().RemoveAccount(ctx, acc)
	_, remainingGas, err = p.Claim(ctx, claimer, &method, []interface{}{signClaimMsg(t, evmtypes.NewMsgClaim(claimee, claimer), claimee, claimer, acc, claimeeKey)}, false)
	require.Error(t, err, "does not exist")
	require.Equal(t, uint64(0), remainingGas)
	// sequence number mismatch
	ctx, _ = origCtx.CacheContext()
	ctx = ctx.WithGasMeter(sdk.NewGasMeter(1000000, 1, 1))
	acc.Sequence++
	_, remainingGas, err = p.Claim(ctx, claimer, &method, []interface{}{signClaimMsg(t, evmtypes.NewMsgClaim(claimee, claimer), claimee, claimer, acc, claimeeKey)}, false)
	require.Error(t, err, "account sequence mismatch")
	require.Equal(t, uint64(0), remainingGas)
	acc.Sequence--
	// insufficient gas
	ctx, _ = origCtx.CacheContext()
	ctx = ctx.WithGasMeter(sdk.NewGasMeter(9000, 1, 1))
	require.PanicsWithValue(t, sdk.ErrorOutOfGas{Descriptor: "ante verify: secp256k1"}, func() {
		_, _, _ = p.Claim(ctx, claimer, &method, []interface{}{signClaimMsg(t, evmtypes.NewMsgClaim(claimee, claimer), claimee, claimer, acc, claimeeKey)}, false)
	})
	// signature verification failure
	ctx, _ = origCtx.CacheContext()
	ctx = ctx.WithGasMeter(sdk.NewGasMeter(1000000, 1, 1)).WithChainID("bad chain")
	_, remainingGas, err = p.Claim(ctx, claimer, &method, []interface{}{signClaimMsg(t, evmtypes.NewMsgClaim(claimee, claimer), claimee, claimer, acc, claimeeKey)}, false)
	require.Error(t, err, "failed to verify signature for claim tx")
	require.Equal(t, uint64(0), remainingGas)
	// wrapping a claimSpecific message should fail in Claim call
	ctx, _ = origCtx.CacheContext()
	ctx = ctx.WithGasMeter(sdk.NewGasMeter(1000000, 1, 1))
	signedMsg = signClaimMsg(t, evmtypes.NewMsgClaimSpecific(claimee, claimer, &evmtypes.Asset{AssetType: evmtypes.AssetType_TYPECW20, ContractAddress: ""}), claimee, claimer, acc, claimeeKey)
	_, _, err = p.Claim(ctx, claimer, &method, []interface{}{signedMsg}, false)
	require.Error(t, err, "message for Claim must not be MsgClaimSpecific type")
}

func TestClaimSpecificCW20(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	origCtx := testkeeper.EVMTestApp.GetContextForDeliverTx(nil).WithChainID("sei-test").WithBlockTime(time.Now())
	txConfig := testkeeper.EVMTestApp.GetTxConfig()
	a := pcommon.MustGetABI(solo.F, "abi.json")
	method := a.Methods["claimSpecific"]
	wKeeper := wasmkeeper.NewDefaultPermissionKeeper(testkeeper.EVMTestApp.WasmKeeper)
	p := solo.NewExecutor(a, k, k.BankKeeper(), k.AccountKeeper(), wKeeper, testkeeper.EVMTestApp.WasmKeeper, txConfig)
	claimeeKey := testkeeper.MockPrivateKey()
	claimee, _ := testkeeper.PrivateKeyToAddresses(claimeeKey)
	claimerKey := testkeeper.MockPrivateKey()
	_, claimer := testkeeper.PrivateKeyToAddresses(claimerKey)
	acc := authtypes.NewBaseAccount(claimee, claimeeKey.PubKey(), 10, 0)
	k.AccountKeeper().SetAccount(origCtx, acc)
	contractAddr := setupCW20Contract(origCtx, claimeeKey, *wKeeper)
	ctx, _ := origCtx.CacheContext()
	ctx = ctx.WithGasMeter(sdk.NewGasMeter(1000000, 1, 1))
	_, remainingGas, err := p.ClaimSpecific(ctx, claimer, &method, []interface{}{signClaimMsg(t, evmtypes.NewMsgClaimSpecific(claimee, claimer, &evmtypes.Asset{AssetType: evmtypes.AssetType_TYPECW20, ContractAddress: contractAddr.String()}), claimee, claimer, acc, claimeeKey)}, false)
	require.NoError(t, err)
	require.Greater(t, remainingGas, uint64(800000))
	require.Equal(t, sdk.ZeroInt(), queryCW20Balance(ctx, testkeeper.EVMTestApp.WasmKeeper, contractAddr, claimee))
	require.Equal(t, sdk.NewInt(1000000000), queryCW20Balance(ctx, testkeeper.EVMTestApp.WasmKeeper, contractAddr, k.GetSeiAddressOrDefault(ctx, claimer)))
}

func TestClaimSpecificCW721(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	origCtx := testkeeper.EVMTestApp.GetContextForDeliverTx(nil).WithChainID("sei-test").WithBlockTime(time.Now())
	txConfig := testkeeper.EVMTestApp.GetTxConfig()
	a := pcommon.MustGetABI(solo.F, "abi.json")
	method := a.Methods["claimSpecific"]
	wKeeper := wasmkeeper.NewDefaultPermissionKeeper(testkeeper.EVMTestApp.WasmKeeper)
	p := solo.NewExecutor(a, k, k.BankKeeper(), k.AccountKeeper(), wKeeper, testkeeper.EVMTestApp.WasmKeeper, txConfig)
	claimeeKey := testkeeper.MockPrivateKey()
	claimee, _ := testkeeper.PrivateKeyToAddresses(claimeeKey)
	claimerKey := testkeeper.MockPrivateKey()
	_, claimer := testkeeper.PrivateKeyToAddresses(claimerKey)
	acc := authtypes.NewBaseAccount(claimee, claimeeKey.PubKey(), 10, 0)
	k.AccountKeeper().SetAccount(origCtx, acc)
	contractAddr := setupCW721Contract(origCtx, claimeeKey, *wKeeper)
	ctx, _ := origCtx.CacheContext()
	ctx = ctx.WithGasMeter(sdk.NewGasMeter(3000000, 1, 1))
	_, remainingGas, err := p.ClaimSpecific(ctx, claimer, &method, []interface{}{signClaimMsg(t, evmtypes.NewMsgClaimSpecific(claimee, claimer, &evmtypes.Asset{AssetType: evmtypes.AssetType_TYPECW721, ContractAddress: contractAddr.String()}), claimee, claimer, acc, claimeeKey)}, false)
	ctx = ctx.WithGasMeter(sdk.NewInfiniteGasMeterWithMultiplier(ctx))
	require.NoError(t, err)
	require.Greater(t, remainingGas, uint64(500000))
	for i := 0; i < 15; i++ {
		require.Equal(t, k.GetSeiAddressOrDefault(ctx, claimer).String(), queryCW721Owner(ctx, testkeeper.EVMTestApp.WasmKeeper, contractAddr, fmt.Sprintf("%d", i)))
	}
}

func TestClaimSingleCW721(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	origCtx := testkeeper.EVMTestApp.GetContextForDeliverTx(nil).WithChainID("sei-test").WithBlockTime(time.Now())
	txConfig := testkeeper.EVMTestApp.GetTxConfig()
	a := pcommon.MustGetABI(solo.F, "abi.json")
	method := a.Methods["claimSpecific"]
	wKeeper := wasmkeeper.NewDefaultPermissionKeeper(testkeeper.EVMTestApp.WasmKeeper)
	p := solo.NewExecutor(a, k, k.BankKeeper(), k.AccountKeeper(), wKeeper, testkeeper.EVMTestApp.WasmKeeper, txConfig)
	claimeeKey := testkeeper.MockPrivateKey()
	claimee, _ := testkeeper.PrivateKeyToAddresses(claimeeKey)
	claimerKey := testkeeper.MockPrivateKey()
	_, claimer := testkeeper.PrivateKeyToAddresses(claimerKey)
	acc := authtypes.NewBaseAccount(claimee, claimeeKey.PubKey(), 10, 0)
	k.AccountKeeper().SetAccount(origCtx, acc)
	contractAddr := setupCW721Contract(origCtx, claimeeKey, *wKeeper)
	ctx, _ := origCtx.CacheContext()
	ctx = ctx.WithGasMeter(sdk.NewGasMeter(2000000, 1, 1))
	_, remainingGas, err := p.ClaimSpecific(ctx, claimer, &method, []interface{}{signClaimMsg(t, evmtypes.NewMsgClaimSpecific(claimee, claimer, &evmtypes.Asset{AssetType: evmtypes.AssetType_TYPECW721, ContractAddress: contractAddr.String(), Denom: "5"}), claimee, claimer, acc, claimeeKey)}, false)
	ctx = ctx.WithGasMeter(sdk.NewInfiniteGasMeterWithMultiplier(ctx))
	require.NoError(t, err)
	require.Equal(t, uint64(0x1c0bc6), remainingGas)
	for i := 0; i < 15; i++ {
		if i == 5 {
			require.Equal(t, k.GetSeiAddressOrDefault(ctx, claimer).String(), queryCW721Owner(ctx, testkeeper.EVMTestApp.WasmKeeper, contractAddr, fmt.Sprintf("%d", i)))
		} else {
			require.Equal(t, claimee.String(), queryCW721Owner(ctx, testkeeper.EVMTestApp.WasmKeeper, contractAddr, fmt.Sprintf("%d", i)))
		}
	}
}

func TestClaimSpecificNative(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	origCtx := testkeeper.EVMTestApp.GetContextForDeliverTx(nil).WithChainID("sei-test").WithBlockTime(time.Now())
	txConfig := testkeeper.EVMTestApp.GetTxConfig()
	a := pcommon.MustGetABI(solo.F, "abi.json")
	method := a.Methods["claimSpecific"]
	p := solo.NewExecutor(a, k, k.BankKeeper(), k.AccountKeeper(), nil, testkeeper.EVMTestApp.WasmKeeper, txConfig)
	claimeeKey := testkeeper.MockPrivateKey()
	claimee, _ := testkeeper.PrivateKeyToAddresses(claimeeKey)
	claimerKey := testkeeper.MockPrivateKey()
	_, claimer := testkeeper.PrivateKeyToAddresses(claimerKey)
	acc := authtypes.NewBaseAccount(claimee, claimeeKey.PubKey(), 10, 0)
	k.AccountKeeper().SetAccount(origCtx, acc)
	_ = k.BankKeeper().AddCoins(origCtx, claimee, sdk.NewCoins(sdk.NewCoin("foo", sdk.OneInt())), false)
	ctx, _ := origCtx.CacheContext()
	ctx = ctx.WithGasMeter(sdk.NewGasMeter(1000000, 1, 1))
	_, remainingGas, err := p.ClaimSpecific(ctx, claimer, &method, []interface{}{signClaimMsg(t, evmtypes.NewMsgClaimSpecific(claimee, claimer, &evmtypes.Asset{AssetType: evmtypes.AssetType_TYPENATIVE, Denom: "foo"}), claimee, claimer, acc, claimeeKey)}, false)
	require.NoError(t, err)
	require.Greater(t, remainingGas, uint64(900000))
	require.Equal(t, sdk.OneInt(), k.BankKeeper().GetBalance(ctx, k.GetSeiAddressOrDefault(ctx, claimer), "foo").Amount)
	require.Equal(t, sdk.ZeroInt(), k.BankKeeper().GetBalance(ctx, claimee, "foo").Amount)
}

// TestClaimLegacyAminoJSON covers claim txs signed with
// SIGN_MODE_LEGACY_AMINO_JSON — the only sign mode Ledger devices support. The
// precompile does not pin a sign mode: verification dispatches on the mode
// declared in the signature data, so amino-signed claims must verify exactly
// like direct-signed ones.
func TestClaimLegacyAminoJSON(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	origCtx := testkeeper.EVMTestApp.GetContextForDeliverTx(nil).WithChainID("sei-test")
	txConfig := testkeeper.EVMTestApp.GetTxConfig()
	a := pcommon.MustGetABI(solo.F, "abi.json")
	method := a.Methods["claim"]
	p := solo.NewExecutor(a, k, k.BankKeeper(), k.AccountKeeper(), wasmkeeper.NewDefaultPermissionKeeper(testkeeper.EVMTestApp.WasmKeeper), testkeeper.EVMTestApp.WasmKeeper, txConfig)
	claimeeKey := testkeeper.MockPrivateKey()
	claimee, _ := testkeeper.PrivateKeyToAddresses(claimeeKey)
	claimerKey := testkeeper.MockPrivateKey()
	_, claimer := testkeeper.PrivateKeyToAddresses(claimerKey)
	acc := authtypes.NewBaseAccount(claimee, claimeeKey.PubKey(), 10, 0)
	k.AccountKeeper().SetAccount(origCtx, acc)
	require.NoError(t, k.BankKeeper().AddCoins(origCtx, claimee, sdk.NewCoins(sdk.NewCoin("abc", sdk.NewInt(2)), sdk.NewCoin("def", sdk.NewInt(3))), false))
	aminoMode := signing.SignMode_SIGN_MODE_LEGACY_AMINO_JSON
	directMode := signing.SignMode_SIGN_MODE_DIRECT
	// happy path
	ctx, _ := origCtx.CacheContext()
	ctx = ctx.WithGasMeter(sdk.NewGasMeter(1000000, 1, 1))
	signedMsg := signClaimMsgWithModes(t, evmtypes.NewMsgClaim(claimee, claimer), acc, claimeeKey, aminoMode, aminoMode, acc.GetSequence())
	_, remainingGas, err := p.Claim(ctx, claimer, &method, []interface{}{signedMsg}, false)
	require.NoError(t, err)
	require.Greater(t, remainingGas, uint64(900000))
	require.Equal(t, sdk.NewInt(2), k.BankKeeper().GetBalance(ctx, k.GetSeiAddressOrDefault(ctx, claimer), "abc").Amount)
	require.Equal(t, sdk.NewInt(3), k.BankKeeper().GetBalance(ctx, k.GetSeiAddressOrDefault(ctx, claimer), "def").Amount)
	// the claim must bump the sequence so the amino tx cannot be replayed
	require.Equal(t, acc.GetSequence()+1, k.AccountKeeper().GetAccount(ctx, claimee).GetSequence())
	_, _, err = p.Claim(ctx, claimer, &method, []interface{}{signedMsg}, false)
	require.ErrorContains(t, err, "account sequence mismatch")
	// the amino sign doc pins the chain ID: verification on another chain ID must fail
	ctx, _ = origCtx.CacheContext()
	ctx = ctx.WithGasMeter(sdk.NewGasMeter(1000000, 1, 1)).WithChainID("bad chain")
	_, remainingGas, err = p.Claim(ctx, claimer, &method, []interface{}{signClaimMsgWithModes(t, evmtypes.NewMsgClaim(claimee, claimer), acc, claimeeKey, aminoMode, aminoMode, acc.GetSequence())}, false)
	require.ErrorContains(t, err, "unable to verify single signer signature")
	require.Equal(t, uint64(0), remainingGas)
	// the amino sign doc pins the sequence: a signature over sequence+1 must fail
	// even though the declared sequence matches the account
	ctx, _ = origCtx.CacheContext()
	ctx = ctx.WithGasMeter(sdk.NewGasMeter(1000000, 1, 1))
	_, _, err = p.Claim(ctx, claimer, &method, []interface{}{signClaimMsgWithModes(t, evmtypes.NewMsgClaim(claimee, claimer), acc, claimeeKey, aminoMode, aminoMode, acc.GetSequence()+1)}, false)
	require.ErrorContains(t, err, "unable to verify single signer signature")
	// declared amino but signed over direct-mode bytes must fail
	ctx, _ = origCtx.CacheContext()
	ctx = ctx.WithGasMeter(sdk.NewGasMeter(1000000, 1, 1))
	_, _, err = p.Claim(ctx, claimer, &method, []interface{}{signClaimMsgWithModes(t, evmtypes.NewMsgClaim(claimee, claimer), acc, claimeeKey, aminoMode, directMode, acc.GetSequence())}, false)
	require.ErrorContains(t, err, "unable to verify single signer signature")
	// declared direct but signed over amino bytes must fail
	ctx, _ = origCtx.CacheContext()
	ctx = ctx.WithGasMeter(sdk.NewGasMeter(1000000, 1, 1))
	_, _, err = p.Claim(ctx, claimer, &method, []interface{}{signClaimMsgWithModes(t, evmtypes.NewMsgClaim(claimee, claimer), acc, claimeeKey, directMode, aminoMode, acc.GetSequence())}, false)
	require.ErrorContains(t, err, "unable to verify single signer signature")
	// a declared mode outside DefaultSignModes must be rejected by the handler map
	ctx, _ = origCtx.CacheContext()
	ctx = ctx.WithGasMeter(sdk.NewGasMeter(1000000, 1, 1))
	_, _, err = p.Claim(ctx, claimer, &method, []interface{}{signClaimMsgWithModes(t, evmtypes.NewMsgClaim(claimee, claimer), acc, claimeeKey, signing.SignMode_SIGN_MODE_TEXTUAL, aminoMode, acc.GetSequence())}, false)
	require.ErrorContains(t, err, "can't verify sign mode")
	// txs carrying extension options cannot be verified under amino
	ctx, _ = origCtx.CacheContext()
	ctx = ctx.WithGasMeter(sdk.NewGasMeter(1000000, 1, 1))
	extTb := txConfig.NewTxBuilder()
	require.NoError(t, extTb.SetMsgs(evmtypes.NewMsgClaim(claimee, claimer)))
	extAny, err := codectypes.NewAnyWithValue(evmtypes.NewMsgClaim(claimee, claimer))
	require.NoError(t, err)
	extTb.(authtx.ExtensionOptionsTxBuilder).SetExtensionOptions(extAny)
	require.NoError(t, extTb.SetSignatures(signing.SignatureV2{
		PubKey:   claimeeKey.PubKey(),
		Data:     &signing.SingleSignatureData{SignMode: aminoMode, Signature: nil},
		Sequence: acc.GetSequence(),
	}))
	signerData := authsigning.SignerData{ChainID: "sei-test", AccountNumber: acc.GetAccountNumber(), Sequence: acc.GetSequence()}
	// amino sign bytes cannot even be produced for such a tx
	_, err = txConfig.SignModeHandler().GetSignBytes(aminoMode, signerData, extTb.GetTx())
	require.ErrorContains(t, err, "does not support protobuf extension options")
	// sign over direct-mode bytes just to carry a well-formed signature on the wire
	extSignBytes, err := txConfig.SignModeHandler().GetSignBytes(directMode, signerData, extTb.GetTx())
	require.NoError(t, err)
	extSig, err := claimeeKey.Sign(extSignBytes)
	require.NoError(t, err)
	require.NoError(t, extTb.SetSignatures(signing.SignatureV2{
		PubKey:   claimeeKey.PubKey(),
		Data:     &signing.SingleSignatureData{SignMode: aminoMode, Signature: extSig},
		Sequence: acc.GetSequence(),
	}))
	extTxBz, err := txConfig.TxEncoder()(extTb.GetTx())
	require.NoError(t, err)
	_, _, err = p.Claim(ctx, claimer, &method, []interface{}{extTxBz}, false)
	require.ErrorContains(t, err, "does not support protobuf extension options")
}

// TestClaimSpecificLegacyAminoJSON claims CW20, CW721, and native assets with a
// single amino-signed MsgClaimSpecific. This exercises the amino JSON encoding
// of the repeated assets field, the asset_type enum, and the optional denom in
// one sign doc — the parts of the message most likely to drift between client
// and chain serialization.
func TestClaimSpecificLegacyAminoJSON(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	origCtx := testkeeper.EVMTestApp.GetContextForDeliverTx(nil).WithChainID("sei-test").WithBlockTime(time.Now())
	txConfig := testkeeper.EVMTestApp.GetTxConfig()
	a := pcommon.MustGetABI(solo.F, "abi.json")
	method := a.Methods["claimSpecific"]
	wKeeper := wasmkeeper.NewDefaultPermissionKeeper(testkeeper.EVMTestApp.WasmKeeper)
	p := solo.NewExecutor(a, k, k.BankKeeper(), k.AccountKeeper(), wKeeper, testkeeper.EVMTestApp.WasmKeeper, txConfig)
	claimeeKey := testkeeper.MockPrivateKey()
	claimee, _ := testkeeper.PrivateKeyToAddresses(claimeeKey)
	claimerKey := testkeeper.MockPrivateKey()
	_, claimer := testkeeper.PrivateKeyToAddresses(claimerKey)
	acc := authtypes.NewBaseAccount(claimee, claimeeKey.PubKey(), 10, 0)
	k.AccountKeeper().SetAccount(origCtx, acc)
	cw20Addr := setupCW20Contract(origCtx, claimeeKey, *wKeeper)
	cw721Addr := setupCW721Contract(origCtx, claimeeKey, *wKeeper)
	require.NoError(t, k.BankKeeper().AddCoins(origCtx, claimee, sdk.NewCoins(sdk.NewCoin("foo", sdk.OneInt())), false))
	aminoMode := signing.SignMode_SIGN_MODE_LEGACY_AMINO_JSON
	msg := evmtypes.NewMsgClaimSpecific(claimee, claimer,
		&evmtypes.Asset{AssetType: evmtypes.AssetType_TYPECW20, ContractAddress: cw20Addr.String()},
		&evmtypes.Asset{AssetType: evmtypes.AssetType_TYPECW721, ContractAddress: cw721Addr.String(), Denom: "5"},
		&evmtypes.Asset{AssetType: evmtypes.AssetType_TYPENATIVE, Denom: "foo"},
	)
	ctx, _ := origCtx.CacheContext()
	ctx = ctx.WithGasMeter(sdk.NewGasMeter(5000000, 1, 1))
	_, _, err := p.ClaimSpecific(ctx, claimer, &method, []interface{}{signClaimMsgWithModes(t, msg, acc, claimeeKey, aminoMode, aminoMode, acc.GetSequence())}, false)
	ctx = ctx.WithGasMeter(sdk.NewInfiniteGasMeterWithMultiplier(ctx))
	require.NoError(t, err)
	claimerSei := k.GetSeiAddressOrDefault(ctx, claimer)
	require.Equal(t, sdk.ZeroInt(), queryCW20Balance(ctx, testkeeper.EVMTestApp.WasmKeeper, cw20Addr, claimee))
	require.Equal(t, sdk.NewInt(1000000000), queryCW20Balance(ctx, testkeeper.EVMTestApp.WasmKeeper, cw20Addr, claimerSei))
	for i := 0; i < 15; i++ {
		expectedOwner := claimee.String()
		if i == 5 {
			expectedOwner = claimerSei.String()
		}
		require.Equal(t, expectedOwner, queryCW721Owner(ctx, testkeeper.EVMTestApp.WasmKeeper, cw721Addr, fmt.Sprintf("%d", i)))
	}
	require.Equal(t, sdk.OneInt(), k.BankKeeper().GetBalance(ctx, claimerSei, "foo").Amount)
	require.Equal(t, sdk.ZeroInt(), k.BankKeeper().GetBalance(ctx, claimee, "foo").Amount)
}

// TestClaimAminoSignDocCanonicalBytes locks the exact SIGN_MODE_LEGACY_AMINO_JSON
// sign-doc bytes the chain computes for claim txs. Wallet integrations must
// reproduce these bytes byte-for-byte for signatures to verify, so any diff here
// is a breaking change for amino signers (e.g. Ledger) even if Go-side tests
// still pass. Notable encoding facts locked in:
//   - msgs are wrapped as {"type":"evm/MsgClaim(Specific)","value":{...}}
//   - asset_type serializes as a JSON number, and zero values (asset_type
//     TYPEUNKNOWN, empty denom/contract_address) are omitted entirely
//   - account_number, sequence, and gas serialize as strings; a nil fee amount
//     normalizes to []; memo is always present
func TestClaimAminoSignDocCanonicalBytes(t *testing.T) {
	txConfig := testkeeper.EVMTestApp.GetTxConfig()
	aminoMode := signing.SignMode_SIGN_MODE_LEGACY_AMINO_JSON
	claimee := sdk.AccAddress([]byte("claimee_____________"))
	claimer := common.HexToAddress("0x0102030405060708090A0B0C0D0E0F1011121314")
	cw20 := sdk.AccAddress([]byte("cw20________________"))
	cw721 := sdk.AccAddress([]byte("cw721_______________"))
	signerData := authsigning.SignerData{ChainID: "sei-test", AccountNumber: 7, Sequence: 3}

	tb := txConfig.NewTxBuilder()
	require.NoError(t, tb.SetMsgs(evmtypes.NewMsgClaim(claimee, claimer)))
	bz, err := txConfig.SignModeHandler().GetSignBytes(aminoMode, signerData, tb.GetTx())
	require.NoError(t, err)
	require.Equal(t,
		`{"account_number":"7","chain_id":"sei-test","fee":{"amount":[],"gas":"0"},"memo":"","msgs":[{"type":"evm/MsgClaim","value":{"claimer":"0x0102030405060708090a0B0c0d0e0f1011121314","sender":"sei1vdkxz6tdv4j47h6lta047h6lta047h6l9yjahw"}}],"sequence":"3"}`,
		string(bz))

	tb = txConfig.NewTxBuilder()
	require.NoError(t, tb.SetMsgs(evmtypes.NewMsgClaimSpecific(claimee, claimer,
		&evmtypes.Asset{AssetType: evmtypes.AssetType_TYPECW20, ContractAddress: cw20.String()},
		&evmtypes.Asset{AssetType: evmtypes.AssetType_TYPECW721, ContractAddress: cw721.String(), Denom: "5"},
		&evmtypes.Asset{AssetType: evmtypes.AssetType_TYPENATIVE, Denom: "foo"},
	)))
	bz, err = txConfig.SignModeHandler().GetSignBytes(aminoMode, signerData, tb.GetTx())
	require.NoError(t, err)
	require.Equal(t,
		`{"account_number":"7","chain_id":"sei-test","fee":{"amount":[],"gas":"0"},"memo":"","msgs":[{"type":"evm/MsgClaimSpecific","value":{"assets":[{"asset_type":1,"contract_address":"sei1vdmnyvzlta047h6lta047h6lta047h6l9kc6zy"},{"asset_type":2,"contract_address":"sei1vdmnwv33ta047h6lta047h6lta047h6l03xmyj","denom":"5"},{"asset_type":3,"denom":"foo"}],"claimer":"0x0102030405060708090a0B0c0d0e0f1011121314","sender":"sei1vdkxz6tdv4j47h6lta047h6lta047h6l9yjahw"}}],"sequence":"3"}`,
		string(bz))

	// zero-valued enum fields are dropped from the sign doc: clients emitting
	// "asset_type":0 for TYPEUNKNOWN would produce different bytes and fail
	// signature verification
	tb = txConfig.NewTxBuilder()
	require.NoError(t, tb.SetMsgs(evmtypes.NewMsgClaimSpecific(claimee, claimer,
		&evmtypes.Asset{AssetType: evmtypes.AssetType_TYPEUNKNOWN, ContractAddress: cw20.String()},
	)))
	bz, err = txConfig.SignModeHandler().GetSignBytes(aminoMode, signerData, tb.GetTx())
	require.NoError(t, err)
	require.Equal(t,
		`{"account_number":"7","chain_id":"sei-test","fee":{"amount":[],"gas":"0"},"memo":"","msgs":[{"type":"evm/MsgClaimSpecific","value":{"assets":[{"contract_address":"sei1vdmnyvzlta047h6lta047h6lta047h6l9kc6zy"}],"claimer":"0x0102030405060708090a0B0c0d0e0f1011121314","sender":"sei1vdkxz6tdv4j47h6lta047h6lta047h6l9yjahw"}}],"sequence":"3"}`,
		string(bz))
}

func signClaimMsg(t *testing.T, msg sdk.Msg, claimee sdk.AccAddress, claimer common.Address, acc authtypes.AccountI, signingKey cryptotypes.PrivKey) []byte {
	defaultMode := testkeeper.EVMTestApp.GetTxConfig().SignModeHandler().DefaultMode()
	return signClaimMsgWithModes(t, msg, acc, signingKey, defaultMode, defaultMode, acc.GetSequence())
}

// signClaimMsgWithModes builds and encodes a single-message claim tx. declaredMode
// is the sign mode carried in the tx's signer info; signedMode is the mode whose
// sign bytes are actually signed; signDocSequence is the sequence baked into the
// sign doc. Happy paths pass matching values; negative tests pass divergent values
// to prove verification binds the signature to the declared mode and sign-doc
// contents.
func signClaimMsgWithModes(t *testing.T, msg sdk.Msg, acc authtypes.AccountI, signingKey cryptotypes.PrivKey, declaredMode signing.SignMode, signedMode signing.SignMode, signDocSequence uint64) []byte {
	txConfig := testkeeper.EVMTestApp.GetTxConfig()
	tb := txConfig.NewTxBuilder()
	require.NoError(t, tb.SetMsgs(msg))
	// set the (unsigned) signature first: direct-mode sign bytes cover the
	// signer info, so the declared mode must be in place before signing
	require.NoError(t, tb.SetSignatures(signing.SignatureV2{
		PubKey: signingKey.PubKey(),
		Data: &signing.SingleSignatureData{
			SignMode:  declaredMode,
			Signature: nil,
		},
		Sequence: acc.GetSequence(),
	}))
	signerData := authsigning.SignerData{
		ChainID:       "sei-test",
		AccountNumber: acc.GetAccountNumber(),
		Sequence:      signDocSequence,
	}
	signBytes, err := txConfig.SignModeHandler().GetSignBytes(signedMode, signerData, tb.GetTx())
	require.NoError(t, err)
	sig, err := signingKey.Sign(signBytes)
	require.NoError(t, err)
	require.NoError(t, tb.SetSignatures(signing.SignatureV2{
		PubKey: signingKey.PubKey(),
		Data: &signing.SingleSignatureData{
			SignMode:  declaredMode,
			Signature: sig,
		},
		Sequence: acc.GetSequence(),
	}))
	txbz, err := txConfig.TxEncoder()(tb.GetTx())
	require.NoError(t, err)
	return txbz
}

func setupCW20Contract(ctx sdk.Context, creatorKey cryptotypes.PrivKey, wKeeper wasmkeeper.PermissionedKeeper) sdk.AccAddress {
	code, err := os.ReadFile("../../contracts/wasm/cw20_base.wasm")
	if err != nil {
		panic(err)
	}
	creator, _ := testkeeper.PrivateKeyToAddresses(creatorKey)
	codeID, err := wKeeper.Create(ctx, creator, code, nil)
	if err != nil {
		panic(err)
	}
	contractAddr, _, err := wKeeper.Instantiate(ctx, codeID, creator, creator, []byte(fmt.Sprintf("{\"name\":\"test\",\"symbol\":\"test\",\"decimals\":6,\"initial_balances\":[{\"address\":\"%s\",\"amount\":\"1000000000\"}]}", creator.String())), "test", sdk.NewCoins())
	if err != nil {
		panic(err)
	}
	return contractAddr
}

func queryCW20Balance(ctx sdk.Context, wKeeper wasmkeeper.Keeper, contractAddr sdk.AccAddress, addr sdk.AccAddress) sdk.Int {
	bz, err := wKeeper.QuerySmart(ctx, contractAddr, solo.CW20BalanceQueryPayload(addr))
	if err != nil {
		panic(bz)
	}
	res, err := solo.ParseCW20BalanceQueryResponse(bz)
	if err != nil {
		panic(bz)
	}
	return res
}

func setupCW721Contract(ctx sdk.Context, creatorKey cryptotypes.PrivKey, wKeeper wasmkeeper.PermissionedKeeper) sdk.AccAddress {
	code, err := os.ReadFile("../../contracts/wasm/cw721_base.wasm")
	if err != nil {
		panic(err)
	}
	creator, _ := testkeeper.PrivateKeyToAddresses(creatorKey)
	codeID, err := wKeeper.Create(ctx, creator, code, nil)
	if err != nil {
		panic(err)
	}
	contractAddr, _, err := wKeeper.Instantiate(ctx, codeID, creator, creator, []byte(fmt.Sprintf("{\"name\":\"test\",\"symbol\":\"test\",\"minter\":\"%s\"}", creator.String())), "test", sdk.NewCoins())
	if err != nil {
		panic(err)
	}
	type mintRequest struct {
		Token string `json:"token_id"`
		Owner string `json:"owner"`
	}
	for i := 0; i < 15; i++ {
		raw := mintRequest{Token: fmt.Sprintf("%d", i), Owner: creator.String()}
		bz, err := json.Marshal(map[string]interface{}{"mint": raw})
		if err != nil {
			panic(err)
		}
		_, err = wKeeper.Execute(ctx, contractAddr, creator, bz, sdk.NewCoins())
		if err != nil {
			panic(err)
		}
	}
	return contractAddr
}

func queryCW721Owner(ctx sdk.Context, wKeeper wasmkeeper.Keeper, contractAddr sdk.AccAddress, token string) string {
	bz, err := wKeeper.QuerySmart(ctx, contractAddr, CW721OwnerOfQueryPayload(token))
	if err != nil {
		panic(bz)
	}
	res, err := ParseCW721OwnerOfQueryResponse(bz)
	if err != nil {
		panic(bz)
	}
	return res
}

func CW721OwnerOfQueryPayload(token string) []byte {
	raw := map[string]interface{}{"token_id": token}
	bz, err := json.Marshal(map[string]interface{}{"owner_of": raw})
	if err != nil {
		// should be impossible
		panic(err)
	}
	return bz
}

func ParseCW721OwnerOfQueryResponse(res []byte) (string, error) {
	type response struct {
		Owner string `json:"owner"`
	}
	typed := response{}
	if err := json.Unmarshal(res, &typed); err != nil {
		return "", err
	}
	return typed.Owner, nil
}
