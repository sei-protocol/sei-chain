package app_test

import (
	"context"
	"testing"
	"time"

	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkacltypes "github.com/cosmos/cosmos-sdk/types/accesscontrol"
	acltypes "github.com/cosmos/cosmos-sdk/x/accesscontrol/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/k0kubun/pp/v3"
	"github.com/sei-protocol/sei-chain/app"
	oracletypes "github.com/sei-protocol/sei-chain/x/oracle/types"
	"github.com/stretchr/testify/require"
	abci "github.com/tendermint/tendermint/abci/types"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
)

func TestEmptyBlockIdempotency(t *testing.T) {
	commitData := [][]byte{}
	tm := time.Now().UTC()
	valPub := secp256k1.GenPrivKey().PubKey()

	for i := 1; i <= 10; i++ {
		testWrapper := app.NewTestWrapper(t, tm, valPub)
		res, _ := testWrapper.App.FinalizeBlock(context.Background(), &abci.RequestFinalizeBlock{Height: 1})
		testWrapper.App.Commit(context.Background())
		data := res.AppHash
		commitData = append(commitData, data)
	}

	referenceData := commitData[0]
	for _, data := range commitData[1:] {
		require.Equal(t, len(referenceData), len(data))
	}
}

func TestGetChannelsFromSignalMapping(t *testing.T) {
	dag := acltypes.NewDag()
	commit := *acltypes.CommitAccessOp()
	writeA := sdkacltypes.AccessOperation{
		AccessType:         sdkacltypes.AccessType_WRITE,
		ResourceType:       sdkacltypes.ResourceType_KV,
		IdentifierTemplate: "ResourceA",
	}
	readA := sdkacltypes.AccessOperation{
		AccessType:         sdkacltypes.AccessType_READ,
		ResourceType:       sdkacltypes.ResourceType_KV,
		IdentifierTemplate: "ResourceA",
	}
	readAll := sdkacltypes.AccessOperation{
		AccessType:         sdkacltypes.AccessType_READ,
		ResourceType:       sdkacltypes.ResourceType_ANY,
		IdentifierTemplate: "*",
	}

	dag.AddNodeBuildDependency(0, 0, writeA)
	dag.AddNodeBuildDependency(0, 0, commit)
	dag.AddNodeBuildDependency(1, 0, readAll)
	dag.AddNodeBuildDependency(1, 0, commit)
	dag.AddNodeBuildDependency(2, 0, writeA)
	dag.AddNodeBuildDependency(2, 0, commit)
	dag.AddNodeBuildDependency(3, 0, writeA)
	dag.AddNodeBuildDependency(3, 0, commit)

	dag.AddNodeBuildDependency(0, 1, writeA)
	dag.AddNodeBuildDependency(0, 1, commit)
	dag.AddNodeBuildDependency(1, 1, readA)
	dag.AddNodeBuildDependency(1, 1, commit)

	completionSignalsMap, blockingSignalsMap := dag.CompletionSignalingMap, dag.BlockingSignalsMap

	pp.Default.SetColoringEnabled(false)

	resultCompletionSignalsMap := app.GetChannelsFromSignalMapping(completionSignalsMap[0])
	resultBlockingSignalsMap := app.GetChannelsFromSignalMapping(blockingSignalsMap[1])

	require.True(t, len(resultCompletionSignalsMap) > 1)
	require.True(t, len(resultBlockingSignalsMap) > 1)
}

// Mock method to fail
func MockProcessBlockConcurrentFunctionFail(
	ctx sdk.Context,
	txs [][]byte,
	completionSignalingMap map[int]acltypes.MessageCompletionSignalMapping,
	blockingSignalsMap map[int]acltypes.MessageCompletionSignalMapping,
	txMsgAccessOpMapping map[int]acltypes.MsgIndexToAccessOpMapping,
) ([]*abci.ExecTxResult, bool) {
	return []*abci.ExecTxResult{}, false
}

func MockProcessBlockConcurrentFunctionSuccess(
	ctx sdk.Context,
	txs [][]byte,
	completionSignalingMap map[int]acltypes.MessageCompletionSignalMapping,
	blockingSignalsMap map[int]acltypes.MessageCompletionSignalMapping,
	txMsgAccessOpMapping map[int]acltypes.MsgIndexToAccessOpMapping,
) ([]*abci.ExecTxResult, bool) {
	return []*abci.ExecTxResult{}, true
}

func TestProcessTxsSuccess(t *testing.T) {
	tm := time.Now().UTC()
	valPub := secp256k1.GenPrivKey().PubKey()

	testWrapper := app.NewTestWrapper(t, tm, valPub)
	dag := acltypes.NewDag()

	// Set some test context mem cache values
	testWrapper.Ctx.ContextMemCache().UpsertDeferredSends("Some Account", sdk.NewCoins(sdk.Coin{
		Denom:  "test",
		Amount: sdk.NewInt(1),
	}))
	testWrapper.Ctx.ContextMemCache().UpsertDeferredWithdrawals("Some Other Account", sdk.NewCoins(sdk.Coin{
		Denom:  "test",
		Amount: sdk.NewInt(1),
	}))
	require.Equal(t, 1, len(testWrapper.Ctx.ContextMemCache().GetDeferredSends().GetSortedKeys()))
	testWrapper.App.ProcessTxs(
		testWrapper.Ctx,
		[][]byte{},
		&dag,
		MockProcessBlockConcurrentFunctionSuccess,
	)

	// It should be reset if it fails to prevent any values from being written
	require.Equal(t, 1, len(testWrapper.Ctx.ContextMemCache().GetDeferredWithdrawals().GetSortedKeys()))
	require.Equal(t, 1, len(testWrapper.Ctx.ContextMemCache().GetDeferredSends().GetSortedKeys()))
}

func TestProcessTxsClearCacheOnFail(t *testing.T) {
	tm := time.Now().UTC()
	valPub := secp256k1.GenPrivKey().PubKey()

	testWrapper := app.NewTestWrapper(t, tm, valPub)
	dag := acltypes.NewDag()

	// Set some test context mem cache values
	testWrapper.Ctx.ContextMemCache().UpsertDeferredSends("Some Account", sdk.NewCoins(sdk.Coin{
		Denom:  "test",
		Amount: sdk.NewInt(1),
	}))
	testWrapper.Ctx.ContextMemCache().UpsertDeferredWithdrawals("Some Account", sdk.NewCoins(sdk.Coin{
		Denom:  "test",
		Amount: sdk.NewInt(1),
	}))
	testWrapper.App.ProcessTxs(
		testWrapper.Ctx,
		[][]byte{},
		&dag,
		MockProcessBlockConcurrentFunctionFail,
	)

	// It should be reset if it fails to prevent any values from being written
	require.Equal(t, 0, len(testWrapper.Ctx.ContextMemCache().GetDeferredWithdrawals().GetSortedKeys()))
	require.Equal(t, 0, len(testWrapper.Ctx.ContextMemCache().GetDeferredSends().GetSortedKeys()))
}

func TestPartitionOracleTxs(t *testing.T) {
	tm := time.Now().UTC()
	valPub := secp256k1.GenPrivKey().PubKey()

	testWrapper := app.NewTestWrapper(t, tm, valPub)

	account := sdk.AccAddress(valPub.Address()).String()
	validator := sdk.ValAddress(valPub.Address()).String()

	oracleMsg := &oracletypes.MsgAggregateExchangeRateVote{
		ExchangeRates: "1.2uatom",
		Feeder:        account,
		Validator:     validator,
	}

	otherMsg := &stakingtypes.MsgDelegate{
		DelegatorAddress: account,
		ValidatorAddress: validator,
		Amount:           sdk.NewCoin("usei", sdk.NewInt(1)),
	}

	txEncoder := app.MakeEncodingConfig().TxConfig.TxEncoder()
	oracleTxBuilder := app.MakeEncodingConfig().TxConfig.NewTxBuilder()
	otherTxBuilder := app.MakeEncodingConfig().TxConfig.NewTxBuilder()
	mixedTxBuilder := app.MakeEncodingConfig().TxConfig.NewTxBuilder()

	err := oracleTxBuilder.SetMsgs(oracleMsg)
	require.NoError(t, err)
	oracleTx, err := txEncoder(oracleTxBuilder.GetTx())
	require.NoError(t, err)

	err = otherTxBuilder.SetMsgs(otherMsg)
	require.NoError(t, err)
	otherTx, err := txEncoder(otherTxBuilder.GetTx())
	require.NoError(t, err)

	// this should be treated as non-oracle vote
	err = mixedTxBuilder.SetMsgs([]sdk.Msg{oracleMsg, otherMsg}...)
	require.NoError(t, err)
	mixedTx, err := txEncoder(mixedTxBuilder.GetTx())
	require.NoError(t, err)

	txs := [][]byte{
		oracleTx,
		otherTx,
		mixedTx,
	}

	oracleTxs, otherTxs := testWrapper.App.PartitionOracleVoteTxs(testWrapper.Ctx, txs)
	require.Equal(t, oracleTxs, [][]byte{oracleTx})
	require.Equal(t, otherTxs, [][]byte{otherTx, mixedTx})
}

func TestProcessOracleAndOtherTxsSuccess(t *testing.T) {
	tm := time.Now().UTC()
	valPub := secp256k1.GenPrivKey().PubKey()

	testWrapper := app.NewTestWrapper(t, tm, valPub)

	account := sdk.AccAddress(valPub.Address()).String()
	validator := sdk.ValAddress(valPub.Address()).String()

	oracleMsg := &oracletypes.MsgAggregateExchangeRateVote{
		ExchangeRates: "1.2uatom",
		Feeder:        account,
		Validator:     validator,
	}

	otherMsg := &stakingtypes.MsgDelegate{
		DelegatorAddress: account,
		ValidatorAddress: validator,
		Amount:           sdk.NewCoin("usei", sdk.NewInt(1)),
	}

	oracleTxBuilder := app.MakeEncodingConfig().TxConfig.NewTxBuilder()
	otherTxBuilder := app.MakeEncodingConfig().TxConfig.NewTxBuilder()
	txEncoder := app.MakeEncodingConfig().TxConfig.TxEncoder()

	err := oracleTxBuilder.SetMsgs(oracleMsg)
	require.NoError(t, err)
	oracleTx, err := txEncoder(oracleTxBuilder.GetTx())
	require.NoError(t, err)

	err = otherTxBuilder.SetMsgs(otherMsg)
	require.NoError(t, err)
	otherTx, err := txEncoder(otherTxBuilder.GetTx())
	require.NoError(t, err)

	txs := [][]byte{
		oracleTx,
		otherTx,
	}

	req := &abci.RequestFinalizeBlock{
		Height: 1,
	}
	_, txResults, _, _ := testWrapper.App.ProcessBlock(
		testWrapper.Ctx.WithBlockHeight(
			1,
		).WithBlockGasMeter(
			sdk.NewInfiniteGasMeter(),
		),
		txs,
		req,
		req.DecidedLastCommit,
	)

	require.Equal(t, 2, len(txResults))
}

// we want to test all possible deadlock paths in this test, i.e. we will test every scenario followed a different one 
func TestOptimisticProcessingDeadlocks(t *testing.T) {
	tm := time.Now().UTC()
	valPub := secp256k1.GenPrivKey().PubKey()
	testWrapper := app.NewTestWrapper(t, tm, valPub)

	testCtx := testWrapper.App.BaseApp.NewContext(false, tmproto.Header{Height: 1, ChainID: "sei-test", Time: tm})

	// scenario 1, optimisticProcessingInfo is not nil
	testWrapper.PopulateOptimisticProcessingInfo(testWrapper.Ctx, &abci.RequestProcessProposal{
		Height: 1,
	})
	testWrapper.App.ProcessProposalHandler(testCtx.WithBlockHeight(4), &abci.RequestProcessProposal{
		Height: 1,
	})
	testWrapper.App.FinalizeBlocker(testWrapper.Ctx, &abci.RequestFinalizeBlock{
		Height: 1,
	})

	// scenario 2, optimisticProcessingInfo is not nil but hash doesn't match
	testWrapper.PopulateOptimisticProcessingInfo(testWrapper.Ctx, &abci.RequestProcessProposal{
		Height: 1,
	})
	testWrapper.App.ProcessProposalHandler(testCtx.WithBlockHeight(4), &abci.RequestProcessProposal{
		Hash: []byte("test_hash"),
		Height: 1,
	})
	testWrapper.App.FinalizeBlocker(testWrapper.Ctx, &abci.RequestFinalizeBlock{
		Height: 1,
	})

	// scenario 3, optimisticProcessingInfo is nil
	testWrapper.App.ProcessProposalHandler(testCtx.WithBlockHeight(4), &abci.RequestProcessProposal{
		Height: 1,
	})
	testWrapper.App.FinalizeBlocker(testWrapper.Ctx, &abci.RequestFinalizeBlock{
		Height: 1,
	})

	// scenario 1, optimisticProcessingInfo is not nil
	testWrapper.PopulateOptimisticProcessingInfo(testWrapper.Ctx, &abci.RequestProcessProposal{
		Height: 1,
	})
	testWrapper.App.ProcessProposalHandler(testCtx.WithBlockHeight(4), &abci.RequestProcessProposal{
		Height: 1,
	})
	testWrapper.App.FinalizeBlocker(testWrapper.Ctx, &abci.RequestFinalizeBlock{
		Height: 1,
	})

	// scenario 3, optimisticProcessingInfo is nil
	testWrapper.App.ProcessProposalHandler(testCtx.WithBlockHeight(4), &abci.RequestProcessProposal{
		Height: 1,
	})
	testWrapper.App.FinalizeBlocker(testWrapper.Ctx, &abci.RequestFinalizeBlock{
		Height: 1,
	})

	// scenario 2, optimisticProcessingInfo is not nil but hash doesn't match
	testWrapper.PopulateOptimisticProcessingInfo(testWrapper.Ctx, &abci.RequestProcessProposal{
		Height: 1,
	})
	testWrapper.App.ProcessProposalHandler(testCtx.WithBlockHeight(4), &abci.RequestProcessProposal{
		Hash: []byte("test_hash"),
		Height: 1,
	})
	testWrapper.App.FinalizeBlocker(testWrapper.Ctx, &abci.RequestFinalizeBlock{
		Height: 1,
	})

	// scenario 1, optimisticProcessingInfo is not nil
	testWrapper.PopulateOptimisticProcessingInfo(testWrapper.Ctx, &abci.RequestProcessProposal{
		Height: 1,
	})
	testWrapper.App.ProcessProposalHandler(testCtx.WithBlockHeight(4), &abci.RequestProcessProposal{
		Height: 1,
	})
	testWrapper.App.FinalizeBlocker(testWrapper.Ctx, &abci.RequestFinalizeBlock{
		Height: 1,
	})
}

