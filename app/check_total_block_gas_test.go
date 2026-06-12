package app

import (
	"encoding/hex"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/keys/secp256k1"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	banktypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/bank/types"
	stakingtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/staking/types"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/sei-protocol/sei-chain/x/evm/config"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/sei-protocol/sei-chain/x/evm/types/ethtx"
	oracletypes "github.com/sei-protocol/sei-chain/x/oracle/types"
	"github.com/stretchr/testify/require"
)

// blockGasParams builds a ConsensusParams with only the block-gas fields populated.
func blockGasParams(maxGas, maxGasWanted int64) *tmproto.ConsensusParams {
	return &tmproto.ConsensusParams{
		Block: &tmproto.BlockParams{
			MaxGas:       maxGas,
			MaxGasWanted: maxGasWanted,
		},
	}
}

// newBlockGasCtx returns a fresh App context with the given block-gas caps.
func newBlockGasCtx(t *testing.T, a *App, maxGas, maxGasWanted int64) sdk.Context {
	t.Helper()
	return a.NewContext(false, tmproto.Header{
		Height:  2,
		ChainID: "sei-test",
		Time:    time.Now().UTC(),
	}).WithConsensusParams(blockGasParams(maxGas, maxGasWanted))
}

// encodeDecodeTx round-trips a tx through the App's encoder/decoder, matching what
// proposal processing sees.
func encodeDecodeTx(t *testing.T, a *App, tx sdk.Tx) sdk.Tx {
	t.Helper()
	raw, err := a.GetTxConfig().TxEncoder()(tx)
	require.NoError(t, err)
	decoded, err := a.GetTxConfig().TxDecoder()(raw)
	require.NoError(t, err)
	return decoded
}

// buildCosmosTx wraps a single Cosmos msg in a tx with the given gas limit and round-trips it.
func buildCosmosTx(t *testing.T, a *App, msg sdk.Msg, gasLimit uint64) sdk.Tx {
	t.Helper()
	txb := a.GetTxConfig().NewTxBuilder()
	require.NoError(t, txb.SetMsgs(msg))
	txb.SetGasLimit(gasLimit)
	return encodeDecodeTx(t, a, txb.GetTx())
}

// buildSignedLegacyEVMTx builds a signed legacy EVM tx wrapped as MsgEVMTransaction.
// Pass gasEstimate=0 to leave the estimate unset.
func buildSignedLegacyEVMTx(t *testing.T, a *App, nonce, gasWanted, gasEstimate uint64) sdk.Tx {
	t.Helper()
	privKey := secp256k1.GenPrivKey()
	key, err := crypto.HexToECDSA(hex.EncodeToString(privKey.Bytes()))
	require.NoError(t, err)

	to := common.HexToAddress("0x1111111111111111111111111111111111111111")
	ethCfg := evmtypes.DefaultChainConfig().EthereumConfig(big.NewInt(config.DefaultChainID))
	signer := ethtypes.MakeSigner(ethCfg, big.NewInt(1), 123)

	signedTx, err := ethtypes.SignTx(ethtypes.NewTx(&ethtypes.LegacyTx{
		Nonce:    nonce,
		GasPrice: big.NewInt(10),
		Gas:      gasWanted,
		To:       &to,
		Value:    big.NewInt(1),
	}), signer, key)
	require.NoError(t, err)

	ethtxdata, err := ethtx.NewTxDataFromTx(signedTx)
	require.NoError(t, err)
	msg, err := evmtypes.NewMsgEVMTransaction(ethtxdata)
	require.NoError(t, err)

	txb := a.GetTxConfig().NewTxBuilder()
	require.NoError(t, txb.SetMsgs(msg))
	txb.SetGasEstimate(gasEstimate)
	return encodeDecodeTx(t, a, txb.GetTx())
}

// TestCheckTotalBlockGas_MultipleEVMUnderLimit covers the fast path where valid
// single-message EVM txs skip IsTxGasless but still accumulate gas toward the block cap.
func TestCheckTotalBlockGas_MultipleEVMUnderLimit(t *testing.T) {
	a := Setup(t, false, false, false)
	ctx := newBlockGasCtx(t, a, 1_000_000, 1_000_000)

	txs := make([]sdk.Tx, 0, 3)
	for i := uint64(1); i <= 3; i++ {
		txs = append(txs, buildSignedLegacyEVMTx(t, a, i, 21000, 0))
	}
	require.True(t, a.checkTotalBlockGas(ctx, txs))
}

// TestCheckTotalBlockGas_MultipleEVMExceedsMaxGas ensures EVM gas accounting is unchanged
// when cumulative gas wanted exceeds MaxGas (no gas estimate path).
func TestCheckTotalBlockGas_MultipleEVMExceedsMaxGas(t *testing.T) {
	a := Setup(t, false, false, false)
	ctx := newBlockGasCtx(t, a, 50_000, 1_000_000)

	txs := []sdk.Tx{
		buildSignedLegacyEVMTx(t, a, 1, 30_000, 0),
		buildSignedLegacyEVMTx(t, a, 2, 30_000, 0),
	}
	require.False(t, a.checkTotalBlockGas(ctx, txs))
}

// TestCheckTotalBlockGas_GasEstimatePreferredOverGasWanted exercises the branch where a
// valid gas estimate (>= MinGasEVMTx and <= gasWanted) is charged instead of full gasWanted.
// With MaxGas=80_000, three txs at gasWanted=100_000 would clearly exceed if counted by
// gasWanted (300_000) but fit comfortably when counted by estimate (63_000).
func TestCheckTotalBlockGas_GasEstimatePreferredOverGasWanted(t *testing.T) {
	a := Setup(t, false, false, false)
	ctx := newBlockGasCtx(t, a, 80_000, 1_000_000)

	txs := make([]sdk.Tx, 0, 3)
	for i := uint64(1); i <= 3; i++ {
		txs = append(txs, buildSignedLegacyEVMTx(t, a, i, 100_000, 21_000))
	}
	require.True(t, a.checkTotalBlockGas(ctx, txs))
}

// TestCheckTotalBlockGas_CosmosBankSendWithoutGaslessTypes exercises txs that are not
// EVM and not oracle/associate: couldBeGaslessTransaction is false so IsTxGasless is skipped.
func TestCheckTotalBlockGas_CosmosBankSendWithoutGaslessTypes(t *testing.T) {
	a := Setup(t, false, false, false)
	ctx := newBlockGasCtx(t, a, 1_000_000, 1_000_000)

	send := &banktypes.MsgSend{
		FromAddress: sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address()).String(),
		ToAddress:   sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address()).String(),
		Amount:      sdk.NewCoins(sdk.NewInt64Coin("usei", 1)),
	}
	require.True(t, a.checkTotalBlockGas(ctx, []sdk.Tx{buildCosmosTx(t, a, send, 80_000)}))
}

// TestCheckTotalBlockGas_NilDecodedTx exercises the nil-entry branch of the accounting loop.
func TestCheckTotalBlockGas_NilDecodedTx(t *testing.T) {
	a := Setup(t, false, false, false)
	ctx := newBlockGasCtx(t, a, 1_000_000, 1_000_000)
	evmTx := buildSignedLegacyEVMTx(t, a, 1, 21000, 0)
	require.False(t, a.checkTotalBlockGas(ctx, []sdk.Tx{nil, evmTx}))
}

// TestCheckTotalBlockGas_AssociateTxIsGasless verifies that a MsgAssociate from an
// unassociated address is excluded from block gas accounting. MaxGas is set below the
// tx's gas limit so the test fails iff the tx is incorrectly counted.
func TestCheckTotalBlockGas_AssociateTxIsGasless(t *testing.T) {
	a := Setup(t, false, false, false)
	ctx := newBlockGasCtx(t, a, 100, 1_000_000)

	msg := &evmtypes.MsgAssociate{
		Sender:        sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address()).String(),
		CustomMessage: "test",
	}
	tx := buildCosmosTx(t, a, msg, 1_000) // 1_000 > MaxGas=100 if counted
	require.True(t, a.checkTotalBlockGas(ctx, []sdk.Tx{tx}))
}

// TestCheckTotalBlockGas_OracleVoteIsGasless verifies that a valid oracle aggregate vote
// from a bonded validator with no prior vote is excluded from block gas accounting.
func TestCheckTotalBlockGas_OracleVoteIsGasless(t *testing.T) {
	valPub := secp256k1.GenPrivKey().PubKey()
	tw := NewTestWrapper(t, time.Now().UTC(), valPub, false)

	// Promote the validator to Bonded so ValidateFeeder succeeds.
	valAddr := sdk.ValAddress(valPub.Address())
	val, found := tw.App.StakingKeeper.GetValidator(tw.Ctx, valAddr)
	require.True(t, found)
	tw.App.StakingKeeper.SetValidator(tw.Ctx, val.UpdateStatus(stakingtypes.Bonded))

	// Self-feeder oracle vote; no prior aggregate vote exists in fresh state.
	vote := &oracletypes.MsgAggregateExchangeRateVote{
		ExchangeRates: "1.2uatom",
		Feeder:        sdk.AccAddress(valAddr).String(),
		Validator:     valAddr.String(),
	}
	tx := buildCosmosTx(t, tw.App, vote, 1_000) // 1_000 > MaxGas=100 if counted

	ctx := tw.Ctx.WithConsensusParams(blockGasParams(100, 1_000_000))
	require.True(t, tw.App.checkTotalBlockGas(ctx, []sdk.Tx{tx}))
}
