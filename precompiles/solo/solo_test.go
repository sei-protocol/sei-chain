package solo_test

import (
	"testing"

	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/ethereum/go-ethereum/common"
	pcommon "github.com/sei-protocol/sei-chain/precompiles/common"
	"github.com/sei-protocol/sei-chain/precompiles/solo"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
)

func TestClaim(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	origCtx := testkeeper.EVMTestApp.GetContextForDeliverTx(nil).WithChainID("sei-test")
	txConfig := testkeeper.EVMTestApp.GetTxConfig()
	a := pcommon.MustGetABI(solo.F, "abi.json")
	method := a.Methods["claim"]
	p := solo.NewExecutor(a, k, k.BankKeeper(), k.AccountKeeper(), txConfig)
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
	_, remainingGas, err := p.Claim(ctx, claimer, &method, []interface{}{signClaimMsg(t, claimee, claimer, acc, claimeeKey)}, false)
	require.NoError(t, err)
	require.Equal(t, uint64(964834), remainingGas)
	require.Equal(t, sdk.NewInt(2), k.BankKeeper().GetBalance(ctx, k.GetSeiAddressOrDefault(ctx, claimer), "abc").Amount)
	require.Equal(t, sdk.NewInt(3), k.BankKeeper().GetBalance(ctx, k.GetSeiAddressOrDefault(ctx, claimer), "def").Amount)
	// from staticcall
	ctx, _ = origCtx.CacheContext()
	ctx = ctx.WithGasMeter(sdk.NewGasMeter(1000000, 1, 1))
	_, remainingGas, err = p.Claim(ctx, claimer, &method, []interface{}{signClaimMsg(t, claimee, claimer, acc, claimeeKey)}, true)
	require.Error(t, err, "cannot call send from staticcall")
	require.Equal(t, uint64(0), remainingGas)
	// incorrect number of args
	ctx, _ = origCtx.CacheContext()
	ctx = ctx.WithGasMeter(sdk.NewGasMeter(1000000, 1, 1))
	_, remainingGas, err = p.Claim(ctx, claimer, &method, []interface{}{signClaimMsg(t, claimee, claimer, acc, claimeeKey), ""}, false)
	require.Error(t, err, "expected 1 arguments but got 2")
	require.Equal(t, uint64(0), remainingGas)
	// bad payload
	ctx, _ = origCtx.CacheContext()
	ctx = ctx.WithGasMeter(sdk.NewGasMeter(1000000, 1, 1))
	bz := signClaimMsg(t, claimee, claimer, acc, claimeeKey)
	_, remainingGas, err = p.Claim(ctx, claimer, &method, []interface{}{bz[:10]}, false)
	require.Error(t, err, "failed to decode claim tx due to")
	require.Equal(t, uint64(0), remainingGas)
	// imposter
	ctx, _ = origCtx.CacheContext()
	ctx = ctx.WithGasMeter(sdk.NewGasMeter(1000000, 1, 1))
	_, imposter := testkeeper.MockAddressPair()
	_, remainingGas, err = p.Claim(ctx, imposter, &method, []interface{}{signClaimMsg(t, claimee, claimer, acc, claimeeKey)}, false)
	require.Error(t, err, "claim tx is meant for")
	require.Equal(t, uint64(0), remainingGas)
	// account does not exist
	ctx, _ = origCtx.CacheContext()
	ctx = ctx.WithGasMeter(sdk.NewGasMeter(1000000, 1, 1))
	k.AccountKeeper().RemoveAccount(ctx, acc)
	_, remainingGas, err = p.Claim(ctx, claimer, &method, []interface{}{signClaimMsg(t, claimee, claimer, acc, claimeeKey)}, false)
	require.Error(t, err, "does not exist")
	require.Equal(t, uint64(0), remainingGas)
	// sequence number mismatch
	ctx, _ = origCtx.CacheContext()
	ctx = ctx.WithGasMeter(sdk.NewGasMeter(1000000, 1, 1))
	acc.Sequence++
	_, remainingGas, err = p.Claim(ctx, claimer, &method, []interface{}{signClaimMsg(t, claimee, claimer, acc, claimeeKey)}, false)
	require.Error(t, err, "account sequence mismatch")
	require.Equal(t, uint64(0), remainingGas)
	acc.Sequence--
	// insufficient gas
	ctx, _ = origCtx.CacheContext()
	ctx = ctx.WithGasMeter(sdk.NewGasMeter(9000, 1, 1))
	require.PanicsWithValue(t, sdk.ErrorOutOfGas{Descriptor: "ante verify: secp256k1"}, func() {
		_, _, _ = p.Claim(ctx, claimer, &method, []interface{}{signClaimMsg(t, claimee, claimer, acc, claimeeKey)}, false)
	})
	// signature verification failure
	ctx, _ = origCtx.CacheContext()
	ctx = ctx.WithGasMeter(sdk.NewGasMeter(1000000, 1, 1)).WithChainID("bad chain")
	_, remainingGas, err = p.Claim(ctx, claimer, &method, []interface{}{signClaimMsg(t, claimee, claimer, acc, claimeeKey)}, false)
	require.Error(t, err, "failed to verify signature for claim tx")
	require.Equal(t, uint64(0), remainingGas)
}

func signClaimMsg(t *testing.T, claimee sdk.AccAddress, claimer common.Address, acc authtypes.AccountI, signingKey cryptotypes.PrivKey) []byte {
	claimMsg := evmtypes.NewMsgClaim(claimee, claimer)
	tb := testkeeper.EVMTestApp.GetTxConfig().NewTxBuilder()
	tb.SetMsgs(claimMsg)
	tb.SetSignatures(signing.SignatureV2{
		PubKey: signingKey.PubKey(),
		Data: &signing.SingleSignatureData{
			SignMode:  testkeeper.EVMTestApp.GetTxConfig().SignModeHandler().DefaultMode(),
			Signature: nil,
		},
		Sequence: acc.GetSequence(),
	})
	signerData := authsigning.SignerData{
		ChainID:       "sei-test",
		AccountNumber: acc.GetAccountNumber(),
		Sequence:      acc.GetSequence(),
	}
	signBytes, err := testkeeper.EVMTestApp.GetTxConfig().SignModeHandler().GetSignBytes(testkeeper.EVMTestApp.GetTxConfig().SignModeHandler().DefaultMode(), signerData, tb.GetTx())
	require.Nil(t, err)
	sig, err := signingKey.Sign(signBytes)
	require.Nil(t, err)
	sigs := make([]signing.SignatureV2, 1)
	sigs[0] = signing.SignatureV2{
		PubKey: signingKey.PubKey(),
		Data: &signing.SingleSignatureData{
			SignMode:  testkeeper.EVMTestApp.GetTxConfig().SignModeHandler().DefaultMode(),
			Signature: sig,
		},
		Sequence: acc.GetSequence(),
	}
	require.Nil(t, tb.SetSignatures(sigs...))
	sdktx := tb.GetTx()
	txbz, err := testkeeper.EVMTestApp.GetTxConfig().TxEncoder()(sdktx)
	require.Nil(t, err)
	return txbz
}
