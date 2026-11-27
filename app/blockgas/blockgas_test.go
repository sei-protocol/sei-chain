package blockgas_test

import (
	"math"
	"testing"

	"github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/app/blockgas"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	banktypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/bank/types"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/sei-protocol/sei-chain/x/evm/types/ethtx"
	"github.com/stretchr/testify/require"
)

var txConfig = app.MakeEncodingConfig().TxConfig

// buildTx returns a tx of msgs declaring gasLimit as its Cosmos gas limit.
func buildTx(t *testing.T, gasLimit uint64, msgs ...sdk.Msg) sdk.Tx {
	t.Helper()
	b := txConfig.NewTxBuilder()
	require.NoError(t, b.SetMsgs(msgs...))
	b.SetGasLimit(gasLimit)
	return b.GetTx()
}

// evmTx returns an EVM tx with the given gas limit.
func evmTx(t *testing.T, gasLimit uint64) sdk.Tx {
	t.Helper()
	msg, err := evmtypes.NewMsgEVMTransaction(&ethtx.LegacyTx{GasLimit: gasLimit})
	require.NoError(t, err)
	return buildTx(t, 0, msg)
}

// cosmosTx returns a Cosmos tx with the given gas limit.
func cosmosTx(t *testing.T, gasLimit uint64) sdk.Tx {
	t.Helper()
	return buildTx(t, gasLimit, &banktypes.MsgSend{})
}

// maxGas returns consensus params whose only block gas limit is MaxGas.
func maxGas(limit int64) *tmproto.ConsensusParams {
	return &tmproto.ConsensusParams{Block: &tmproto.BlockParams{MaxGas: limit}}
}

// executed returns the result of an EVM tx that executed and used gasUsed.
func executed(gasUsed int64) *abci.ExecTxResult {
	return &abci.ExecTxResult{GasUsed: gasUsed, EvmTxInfo: &abci.EvmTxInfo{}}
}

func TestOf(t *testing.T) {
	evmMsg, err := evmtypes.NewMsgEVMTransaction(&ethtx.LegacyTx{GasLimit: 50_000})
	require.NoError(t, err)
	associateMsg, err := evmtypes.NewMsgEVMTransaction(&ethtx.AssociateTx{})
	require.NoError(t, err)

	for _, tc := range []struct {
		name  string
		tx    sdk.Tx
		kind  blockgas.Kind
		limit uint64
	}{
		{name: "nil", tx: nil, kind: blockgas.Uncharged},
		{name: "evm", tx: buildTx(t, 0, evmMsg), kind: blockgas.EVM, limit: 50_000},
		{name: "cosmos", tx: cosmosTx(t, 80_000), kind: blockgas.Cosmos, limit: 80_000},
		{name: "associate", tx: buildTx(t, 0, associateMsg), kind: blockgas.Uncharged},
		{name: "evm with another message", tx: buildTx(t, 80_000, evmMsg, &banktypes.MsgSend{}), kind: blockgas.Uncharged},
	} {
		t.Run(tc.name, func(t *testing.T) {
			kind, limit, err := blockgas.Of(tc.tx)
			require.NoError(t, err)
			require.Equal(t, tc.kind, kind)
			require.Equal(t, tc.limit, limit)
		})
	}
}

func TestOfRejectsAnEVMPayloadThatCannotBeUnpacked(t *testing.T) {
	_, _, err := blockgas.Of(buildTx(t, 0, &evmtypes.MsgEVMTransaction{}))
	require.Error(t, err)
}

func TestLimit(t *testing.T) {
	require.Equal(t, uint64(math.MaxUint64), blockgas.Limit(-1))
	require.Equal(t, uint64(0), blockgas.Limit(0))
	require.Equal(t, uint64(12_500_000), blockgas.Limit(12_500_000))
}

func TestPoolChargesEVMTxsTheGasTheyUse(t *testing.T) {
	txs := []sdk.Tx{evmTx(t, 60_000), evmTx(t, 60_000)}
	pool := blockgas.NewPool(maxGas(100_000), txs)
	require.True(t, pool.MayReject(), "the gas limits sum past MaxGas")

	require.NoError(t, pool.Admit(0))
	pool.Use(0, executed(21_000))
	require.NoError(t, pool.Admit(1), "the first tx gave back the gas it did not use")
}

func TestPoolRefusesAnEVMTxWhoseGasLimitExceedsTheGasLeft(t *testing.T) {
	txs := []sdk.Tx{evmTx(t, 60_000), evmTx(t, 60_000), evmTx(t, 21_000)}
	pool := blockgas.NewPool(maxGas(100_000), txs)

	require.NoError(t, pool.Admit(0))
	pool.Use(0, executed(60_000))
	require.ErrorIs(t, pool.Admit(1), evmtypes.ErrBlockGasLimitReached)
	require.NoError(t, pool.Admit(2), "a later tx with a smaller gas limit still fits")
}

func TestPoolReservesTheGasLimitsOfCosmosTxs(t *testing.T) {
	txs := []sdk.Tx{evmTx(t, 50_000), cosmosTx(t, 60_000)}
	pool := blockgas.NewPool(maxGas(100_000), txs)
	require.True(t, pool.MayReject())
	require.ErrorIs(t, pool.Admit(0), evmtypes.ErrBlockGasLimitReached,
		"the Cosmos tx leaves 40_000 gas, though it comes later in the block")
	require.NoError(t, pool.Admit(1), "a Cosmos tx is never refused")
}

func TestPoolUsesOnlyGasThatEVMTxsUsedExecuting(t *testing.T) {
	txs := []sdk.Tx{cosmosTx(t, 10_000), evmTx(t, 50_000), evmTx(t, 50_000), evmTx(t, 140_000), evmTx(t, 140_001)}
	pool := blockgas.NewPool(maxGas(200_000), txs)

	pool.Use(0, &abci.ExecTxResult{GasUsed: 10_000}) // a Cosmos tx, whose gas limit is reserved up front
	pool.Use(1, &abci.ExecTxResult{GasUsed: 50_000}) // an EVM tx that failed before executing
	pool.Use(2, executed(math.MaxInt64))             // an EVM tx, which uses at most its gas limit
	require.NoError(t, pool.Admit(3))
	require.ErrorIs(t, pool.Admit(4), evmtypes.ErrBlockGasLimitReached)
}

func TestPoolRefusesNothingWhenTheGasLimitsFit(t *testing.T) {
	txs := []sdk.Tx{cosmosTx(t, 40_000), evmTx(t, 30_000), evmTx(t, 30_000)}
	pool := blockgas.NewPool(maxGas(100_000), txs)
	require.False(t, pool.MayReject())
	for i := range txs {
		require.NoError(t, pool.Admit(i))
		pool.Use(i, executed(math.MaxInt64))
	}
}

func TestPoolNeverRefusesATxThatCannotBeUnpacked(t *testing.T) {
	pool := blockgas.NewPool(maxGas(0), []sdk.Tx{buildTx(t, 0, &evmtypes.MsgEVMTransaction{})})
	require.False(t, pool.MayReject())
	require.NoError(t, pool.Admit(0))
}

func TestPoolWithoutConsensusParamsHasNoLimit(t *testing.T) {
	pool := blockgas.NewPool(nil, []sdk.Tx{evmTx(t, math.MaxUint64)})
	require.False(t, pool.MayReject())
	require.NoError(t, pool.Admit(0))
}

func TestRejected(t *testing.T) {
	pool := blockgas.NewPool(maxGas(10_000), []sdk.Tx{evmTx(t, 21_000)})
	res := blockgas.Rejected(pool.Admit(0))
	require.Equal(t, evmtypes.ModuleName, res.Codespace)
	require.Equal(t, evmtypes.ErrBlockGasLimitReached.ABCICode(), res.Code)
	require.NotEmpty(t, res.Log)
	require.Zero(t, res.GasWanted)
	require.Zero(t, res.GasUsed)
	require.Nil(t, res.EvmTxInfo)
}
