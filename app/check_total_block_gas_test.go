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

func testCheckTotalBlockGasCtx(t *testing.T, a *App) sdk.Context {
	t.Helper()
	cp := &tmproto.ConsensusParams{
		Block: &tmproto.BlockParams{
			MaxGas:       1_000_000,
			MaxGasWanted: 1_000_000,
		},
	}
	return a.NewContext(false, tmproto.Header{
		Height:  2,
		ChainID: "sei-test",
		Time:    time.Now().UTC(),
	}).WithConsensusParams(cp)
}

func buildSignedLegacyEVMTx(t *testing.T, a *App, nonce uint64, gasWanted, gasEstimate uint64) sdk.Tx {
	t.Helper()
	privKey := secp256k1.GenPrivKey()
	key, err := crypto.HexToECDSA(hex.EncodeToString(privKey.Bytes()))
	require.NoError(t, err)
	to := common.HexToAddress("0x1111111111111111111111111111111111111111")
	txData := ethtypes.LegacyTx{
		Nonce:    nonce,
		GasPrice: big.NewInt(10),
		Gas:      gasWanted,
		To:       &to,
		Value:    big.NewInt(1),
	}
	chainCfg := evmtypes.DefaultChainConfig()
	ethCfg := chainCfg.EthereumConfig(big.NewInt(config.DefaultChainID))
	signer := ethtypes.MakeSigner(ethCfg, big.NewInt(1), uint64(123))
	signedTx, err := ethtypes.SignTx(ethtypes.NewTx(&txData), signer, key)
	require.NoError(t, err)
	ethtxdata, err := ethtx.NewTxDataFromTx(signedTx)
	require.NoError(t, err)
	msg, err := evmtypes.NewMsgEVMTransaction(ethtxdata)
	require.NoError(t, err)
	txb := a.GetTxConfig().NewTxBuilder()
	require.NoError(t, txb.SetMsgs(msg))
	txb.SetGasEstimate(gasEstimate)
	tx, err := a.GetTxConfig().TxEncoder()(txb.GetTx())
	require.NoError(t, err)
	decoded, err := a.GetTxConfig().TxDecoder()(tx)
	require.NoError(t, err)
	return decoded
}

// TestCheckTotalBlockGas_MultipleEVMUnderLimit covers the fast path where valid
// single-message EVM txs skip IsTxGasless but still accumulate gas toward the block cap.
func TestCheckTotalBlockGas_MultipleEVMUnderLimit(t *testing.T) {
	a := Setup(t, false, false, false)
	ctx := testCheckTotalBlockGasCtx(t, a)

	var txs []sdk.Tx
	for i := 0; i < 3; i++ {
		txs = append(txs, buildSignedLegacyEVMTx(t, a, uint64(i+1), 21000, 0))
	}
	require.True(t, a.checkTotalBlockGas(ctx, txs))
}

// TestCheckTotalBlockGas_MultipleEVMExceedsMaxGas ensures EVM gas accounting is unchanged
// when cumulative gas wanted exceeds MaxGas (no gas estimate path).
func TestCheckTotalBlockGas_MultipleEVMExceedsMaxGas(t *testing.T) {
	a := Setup(t, false, false, false)
	cp := &tmproto.ConsensusParams{
		Block: &tmproto.BlockParams{
			MaxGas:       50_000,
			MaxGasWanted: 1_000_000,
		},
	}
	ctx := a.NewContext(false, tmproto.Header{
		Height:  2,
		ChainID: "sei-test",
		Time:    time.Now().UTC(),
	}).WithConsensusParams(cp)

	txs := []sdk.Tx{
		buildSignedLegacyEVMTx(t, a, 1, 30_000, 0),
		buildSignedLegacyEVMTx(t, a, 2, 30_000, 0),
	}
	require.False(t, a.checkTotalBlockGas(ctx, txs))
}

// TestCheckTotalBlockGas_CosmosBankSendWithoutGaslessTypes exercises txs that are not
// EVM and not oracle/associate: couldBeGaslessTransaction is false so IsTxGasless is skipped.
func TestCheckTotalBlockGas_CosmosBankSendWithoutGaslessTypes(t *testing.T) {
	a := Setup(t, false, false, false)
	ctx := testCheckTotalBlockGasCtx(t, a)

	fromAddr := sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address())
	toAddr := sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address())
	txb := a.GetTxConfig().NewTxBuilder()
	require.NoError(t, txb.SetMsgs(&banktypes.MsgSend{
		FromAddress: fromAddr.String(),
		ToAddress:   toAddr.String(),
		Amount:      sdk.NewCoins(sdk.NewInt64Coin("usei", 1)),
	}))
	txb.SetGasLimit(80_000)
	raw, err := a.GetTxConfig().TxEncoder()(txb.GetTx())
	require.NoError(t, err)
	decoded, err := a.GetTxConfig().TxDecoder()(raw)
	require.NoError(t, err)

	require.True(t, a.checkTotalBlockGas(ctx, []sdk.Tx{decoded}))
}

// TestCheckTotalBlockGas_NilDecodedTxSkipped ensures nil entries (decode failures) are ignored.
func TestCheckTotalBlockGas_NilDecodedTxSkipped(t *testing.T) {
	a := Setup(t, false, false, false)
	ctx := testCheckTotalBlockGasCtx(t, a)
	evmTx := buildSignedLegacyEVMTx(t, a, 1, 21000, 0)
	require.True(t, a.checkTotalBlockGas(ctx, []sdk.Tx{nil, evmTx}))
}

// TestCheckTotalBlockGas_AssociateTxIsGasless verifies that a MsgAssociate from an
// unassociated address is excluded from block gas accounting (treated as gasless).
func TestCheckTotalBlockGas_AssociateTxIsGasless(t *testing.T) {
	a := Setup(t, false, false, false)
	cp := &tmproto.ConsensusParams{
		Block: &tmproto.BlockParams{
			MaxGas:       100,
			MaxGasWanted: 1_000_000,
		},
	}
	ctx := a.NewContext(false, tmproto.Header{
		Height:  2,
		ChainID: "sei-test",
		Time:    time.Now().UTC(),
	}).WithConsensusParams(cp)

	sender := sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address())
	txb := a.GetTxConfig().NewTxBuilder()
	require.NoError(t, txb.SetMsgs(&evmtypes.MsgAssociate{
		Sender:        sender.String(),
		CustomMessage: "test",
	}))
	txb.SetGasLimit(1_000) // exceeds MaxGas=100 if counted
	raw, err := a.GetTxConfig().TxEncoder()(txb.GetTx())
	require.NoError(t, err)
	decoded, err := a.GetTxConfig().TxDecoder()(raw)
	require.NoError(t, err)

	// MsgAssociate for an unassociated address is gasless: not counted toward block gas
	require.True(t, a.checkTotalBlockGas(ctx, []sdk.Tx{decoded}))
}

// TestCheckTotalBlockGas_OracleVoteIsGasless verifies that a valid oracle aggregate vote
// from a bonded validator with no prior vote is excluded from block gas accounting.
func TestCheckTotalBlockGas_OracleVoteIsGasless(t *testing.T) {
	tm := time.Now().UTC()
	valPub := secp256k1.GenPrivKey().PubKey()
	testWrapper := NewTestWrapper(t, tm, valPub, false)

	// Promote validator to Bonded so ValidateFeeder succeeds
	valAddr := sdk.ValAddress(valPub.Address())
	val, found := testWrapper.App.StakingKeeper.GetValidator(testWrapper.Ctx, valAddr)
	require.True(t, found)
	val = val.UpdateStatus(stakingtypes.Bonded)
	testWrapper.App.StakingKeeper.SetValidator(testWrapper.Ctx, val)

	// Self-feeder oracle vote; no prior aggregate vote exists in fresh state
	oracleMsg := &oracletypes.MsgAggregateExchangeRateVote{
		ExchangeRates: "1.2uatom",
		Feeder:        sdk.AccAddress(valAddr).String(),
		Validator:     valAddr.String(),
	}
	txb := testWrapper.App.GetTxConfig().NewTxBuilder()
	require.NoError(t, txb.SetMsgs(oracleMsg))
	txb.SetGasLimit(1_000) // exceeds MaxGas=100 if counted
	raw, err := testWrapper.App.GetTxConfig().TxEncoder()(txb.GetTx())
	require.NoError(t, err)
	decoded, err := testWrapper.App.GetTxConfig().TxDecoder()(raw)
	require.NoError(t, err)

	cp := &tmproto.ConsensusParams{
		Block: &tmproto.BlockParams{
			MaxGas:       100,
			MaxGasWanted: 1_000_000,
		},
	}
	ctx := testWrapper.Ctx.WithConsensusParams(cp)

	// Oracle vote is gasless: not counted toward block gas
	require.True(t, testWrapper.App.checkTotalBlockGas(ctx, []sdk.Tx{decoded}))
}
