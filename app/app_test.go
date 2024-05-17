package app_test

import (
	"context"
	"encoding/hex"
	"fmt"
	"math/big"
	"testing"
	"time"

	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkacltypes "github.com/cosmos/cosmos-sdk/types/accesscontrol"
	acltypes "github.com/cosmos/cosmos-sdk/x/accesscontrol/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/k0kubun/pp/v3"
	"github.com/sei-protocol/sei-chain/app"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/config"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/sei-protocol/sei-chain/x/evm/types/ethtx"
	oracletypes "github.com/sei-protocol/sei-chain/x/oracle/types"
	"github.com/stretchr/testify/require"
	abci "github.com/tendermint/tendermint/abci/types"
	"github.com/tendermint/tendermint/proto/tendermint/types"
)

func TestEmptyBlockIdempotency(t *testing.T) {
	commitData := [][]byte{}
	tm := time.Now().UTC()
	valPub := secp256k1.GenPrivKey().PubKey()

	for i := 1; i <= 10; i++ {
		testWrapper := app.NewTestWrapper(t, tm, valPub, false)
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

func TestPartitionPrioritizedTxs(t *testing.T) {
	tm := time.Now().UTC()
	valPub := secp256k1.GenPrivKey().PubKey()

	testWrapper := app.NewTestWrapper(t, tm, valPub, false)

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
	typedTxs := []sdk.Tx{
		oracleTxBuilder.GetTx(),
		otherTxBuilder.GetTx(),
		mixedTxBuilder.GetTx(),
	}

	prioritizedTxs, otherTxs, prioritizedTypedTxs, otherTypedTxs, prioIdxs, otherIdxs := testWrapper.App.PartitionPrioritizedTxs(testWrapper.Ctx, txs, typedTxs)
	require.Equal(t, [][]byte{oracleTx}, prioritizedTxs)
	require.Equal(t, [][]byte{otherTx, mixedTx}, otherTxs)
	require.Equal(t, []int{0}, prioIdxs)
	require.Equal(t, []int{1, 2}, otherIdxs)
	require.Equal(t, 4, len(prioritizedTypedTxs))
	require.Equal(t, 2, len(otherTypedTxs))

	diffOrderTxs := [][]byte{
		otherTx,
		oracleTx,
		mixedTx,
	}
	differOrderTypedTxs := []sdk.Tx{
		otherTxBuilder.GetTx(),
		oracleTxBuilder.GetTx(),
		mixedTxBuilder.GetTx(),
	}

	prioritizedTxs, otherTxs, prioritizedTypedTxs, otherTypedTxs, prioIdxs, otherIdxs = testWrapper.App.PartitionPrioritizedTxs(testWrapper.Ctx, diffOrderTxs, differOrderTypedTxs)
	require.Equal(t, [][]byte{oracleTx}, prioritizedTxs)
	require.Equal(t, [][]byte{otherTx, mixedTx}, otherTxs)
	require.Equal(t, []int{1}, prioIdxs)
	require.Equal(t, []int{0, 2}, otherIdxs)
	require.Equal(t, 4, len(prioritizedTypedTxs))
	require.Equal(t, 2, len(otherTypedTxs))
}

func TestProcessOracleAndOtherTxsSuccess(t *testing.T) {
	tm := time.Now().UTC()
	valPub := secp256k1.GenPrivKey().PubKey()
	secondAcc := secp256k1.GenPrivKey().PubKey()

	testWrapper := app.NewTestWrapper(t, tm, valPub, false)

	account := sdk.AccAddress(valPub.Address()).String()
	account2 := sdk.AccAddress(secondAcc.Address()).String()
	validator := sdk.ValAddress(valPub.Address()).String()

	oracleMsg := &oracletypes.MsgAggregateExchangeRateVote{
		ExchangeRates: "1.2uatom",
		Feeder:        account,
		Validator:     validator,
	}

	otherMsg := &banktypes.MsgSend{
		FromAddress: account,
		ToAddress:   account2,
		Amount:      sdk.NewCoins(sdk.NewInt64Coin("usei", 2)),
	}

	oracleTxBuilder := app.MakeEncodingConfig().TxConfig.NewTxBuilder()
	otherTxBuilder := app.MakeEncodingConfig().TxConfig.NewTxBuilder()
	txEncoder := app.MakeEncodingConfig().TxConfig.TxEncoder()

	err := oracleTxBuilder.SetMsgs(oracleMsg)
	require.NoError(t, err)
	oracleTxBuilder.SetGasLimit(200000)
	oracleTxBuilder.SetFeeAmount(sdk.NewCoins(sdk.NewInt64Coin("usei", 20000)))
	oracleTx, err := txEncoder(oracleTxBuilder.GetTx())
	require.NoError(t, err)

	err = otherTxBuilder.SetMsgs(otherMsg)
	require.NoError(t, err)
	otherTxBuilder.SetGasLimit(100000)
	otherTxBuilder.SetFeeAmount(sdk.NewCoins(sdk.NewInt64Coin("usei", 10000)))
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
		),
		txs,
		req,
		req.DecidedLastCommit,
	)
	fmt.Println("txResults1", txResults)

	require.Equal(t, 2, len(txResults))
	require.Equal(t, uint32(3), txResults[0].Code)
	require.Equal(t, uint32(5), txResults[1].Code)

	diffOrderTxs := [][]byte{
		otherTx,
		oracleTx,
	}

	req = &abci.RequestFinalizeBlock{
		Height: 1,
	}
	_, txResults2, _, _ := testWrapper.App.ProcessBlock(
		testWrapper.Ctx.WithBlockHeight(
			1,
		),
		diffOrderTxs,
		req,
		req.DecidedLastCommit,
	)
	fmt.Println("txResults2", txResults2)

	require.Equal(t, 2, len(txResults2))
	// opposite ordering due to true index ordering
	require.Equal(t, uint32(5), txResults2[0].Code)
	require.Equal(t, uint32(3), txResults2[1].Code)
}

func TestInvalidProposalWithExcessiveGasWanted(t *testing.T) {
	tm := time.Now().UTC()
	valPub := secp256k1.GenPrivKey().PubKey()

	testWrapper := app.NewTestWrapper(t, tm, valPub, false)
	ap := testWrapper.App
	ctx := testWrapper.Ctx.WithConsensusParams(&types.ConsensusParams{
		Block: &types.BlockParams{MaxGas: 10},
	})
	emptyTxBuilder := app.MakeEncodingConfig().TxConfig.NewTxBuilder()
	txEncoder := app.MakeEncodingConfig().TxConfig.TxEncoder()
	emptyTxBuilder.SetGasLimit(10)
	emptyTx, _ := txEncoder(emptyTxBuilder.GetTx())

	badProposal := abci.RequestProcessProposal{
		Txs:    [][]byte{emptyTx, emptyTx},
		Height: 1,
	}
	res, err := ap.ProcessProposalHandler(ctx, &badProposal)
	require.Nil(t, err)
	require.Equal(t, abci.ResponseProcessProposal_REJECT, res.Status)
}

func TestDecodeTransactionsConcurrently(t *testing.T) {
	tm := time.Now().UTC()
	valPub := secp256k1.GenPrivKey().PubKey()

	testWrapper := app.NewTestWrapper(t, tm, valPub, false)
	privKey := testkeeper.MockPrivateKey()
	testPrivHex := hex.EncodeToString(privKey.Bytes())
	key, _ := crypto.HexToECDSA(testPrivHex)
	to := new(common.Address)
	copy(to[:], []byte("0x1234567890abcdef1234567890abcdef12345678"))
	txData := ethtypes.LegacyTx{
		Nonce:    1,
		GasPrice: big.NewInt(10),
		Gas:      1000,
		To:       to,
		Value:    big.NewInt(1000),
		Data:     []byte("abc"),
	}
	chainCfg := evmtypes.DefaultChainConfig()
	ethCfg := chainCfg.EthereumConfig(big.NewInt(config.DefaultChainID))
	signer := ethtypes.MakeSigner(ethCfg, big.NewInt(1), uint64(123))
	tx, err := ethtypes.SignTx(ethtypes.NewTx(&txData), signer, key)
	ethtxdata, _ := ethtx.NewTxDataFromTx(tx)
	if err != nil {
		return
	}
	msg, _ := evmtypes.NewMsgEVMTransaction(ethtxdata)
	txBuilder := testWrapper.App.GetTxConfig().NewTxBuilder()
	txBuilder.SetMsgs(msg)
	evmtxbz, _ := testWrapper.App.GetTxConfig().TxEncoder()(txBuilder.GetTx())

	bankMsg := &banktypes.MsgSend{
		FromAddress: "",
		ToAddress:   "",
		Amount:      sdk.NewCoins(sdk.NewInt64Coin("usei", 2)),
	}

	bankTxBuilder := testWrapper.App.GetTxConfig().NewTxBuilder()
	bankTxBuilder.SetMsgs(bankMsg)
	bankTxBuilder.SetGasLimit(200000)
	bankTxBuilder.SetFeeAmount(sdk.NewCoins(sdk.NewInt64Coin("usei", 20000)))
	banktxbz, _ := testWrapper.App.GetTxConfig().TxEncoder()(bankTxBuilder.GetTx())

	invalidbz := []byte("abc")

	typedTxs := testWrapper.App.DecodeTransactionsConcurrently(testWrapper.Ctx, [][]byte{evmtxbz, invalidbz, banktxbz})
	require.NotNil(t, typedTxs[0])
	require.NotNil(t, typedTxs[0].GetMsgs()[0].(*evmtypes.MsgEVMTransaction).Derived)
	require.Nil(t, typedTxs[1])
	require.NotNil(t, typedTxs[2])
}
