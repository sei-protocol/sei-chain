package app_test

import (
	"testing"
	"time"

	types "github.com/SiloMEV/silo-mev-protobuf-go/mev/v1"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/sei-protocol/sei-chain/app"
	"github.com/stretchr/testify/require"
	abci "github.com/tendermint/tendermint/abci/types"
)

func TestBundleSubmissionSuccess(t *testing.T) {
	tm := time.Now().UTC()
	valPub := secp256k1.GenPrivKey().PubKey()
	secondAcc := secp256k1.GenPrivKey().PubKey()

	testWrapper := app.NewTestWrapper(t, tm, valPub, false, true)

	account := sdk.AccAddress(valPub.Address()).String()
	account2 := sdk.AccAddress(secondAcc.Address()).String()

	// Create a bank message (same as TestProcessOracleAndOtherTxsSuccess)
	bankMsg := &banktypes.MsgSend{
		FromAddress: account,
		ToAddress:   account2,
		Amount:      sdk.NewCoins(sdk.NewInt64Coin("usei", 2)),
	}

	// Create transaction (using same pattern as lines 228-244)
	txBuilder := app.MakeEncodingConfig().TxConfig.NewTxBuilder()
	err := txBuilder.SetMsgs(bankMsg)
	require.NoError(t, err)
	txBuilder.SetGasLimit(100000)
	txBuilder.SetFeeAmount(sdk.NewCoins(sdk.NewInt64Coin("usei", 10000)))
	tx, err := app.MakeEncodingConfig().TxConfig.TxEncoder()(txBuilder.GetTx())
	require.NoError(t, err)

	// Create and submit bundle
	height := int64(1)
	bundle := &types.Bundle{
		Transactions: [][]byte{tx},
		BlockHeight:  uint64(height),
	}

	res := testWrapper.App.MevKeeper.SetBundles(height, []*types.Bundle{bundle})
	require.True(t, res)

	// Verify bundle was stored immediately after submission
	queryRes := testWrapper.App.MevKeeper.PendingBundles(height)
	require.Equal(t, 1, len(queryRes))
	require.Equal(t, bundle.Transactions, queryRes[0].Transactions)

	// Verify bundle was stored immediately after submission
	queryRes2 := testWrapper.App.MevKeeper.PendingBundles(height)
	require.Equal(t, 1, len(queryRes2))
	require.Equal(t, bundle.Transactions, queryRes2[0].Transactions)

	// Call PrepareProposal
	prepareProposalReq := abci.RequestPrepareProposal{
		MaxTxBytes: 1000000,
		Height:     height,
		Time:       testWrapper.Ctx.BlockTime(),
	}

	prepareProposalRes, err := testWrapper.App.PrepareProposal(sdk.WrapSDKContext(testWrapper.Ctx), &prepareProposalReq)
	require.NoError(t, err)
	require.NotNil(t, prepareProposalRes)

	t.Log("prepareProposalRes", prepareProposalRes)

	// Extract tx bytes from TxRecords
	txBytes := [][]byte{}
	for _, record := range prepareProposalRes.TxRecords {
		txBytes = append(txBytes, record.Tx)
	}
	require.Contains(t, txBytes, tx)

	// Process block (same pattern as lines 251-261)
	req := &abci.RequestFinalizeBlock{
		Height: height,
		Txs:    txBytes,
	}
	_, txResults, _, _ := testWrapper.App.ProcessBlock(
		testWrapper.Ctx.WithBlockHeight(height),
		txBytes,
		req,
		req.DecidedLastCommit,
		false,
	)

	t.Log("txResults", txResults)

	// Verify results
	require.Equal(t, 1, len(txResults))
	// We expect insufficient funds.
	require.Equal(t, uint32(5), txResults[0].Code)
}

// If tx is in a bundle, it should not be included from the mempool
func TestTransactionDuplicatesRemoved(t *testing.T) {
	tm := time.Now().UTC()
	valPub := secp256k1.GenPrivKey().PubKey()
	testWrapper := app.NewTestWrapper(t, tm, valPub, false, true)

	tx1 := []byte{0x2, 0x1, 0x3, 0x7}
	tx2 := []byte("magic_tx")
	tx3 := []byte("useless bytes")

	// Create and submit bundle
	height := int64(1)
	bundle := &types.Bundle{
		Transactions: [][]byte{tx1, tx2, tx3},
		BlockHeight:  uint64(height),
	}

	res := testWrapper.App.MevKeeper.SetBundles(height, []*types.Bundle{bundle})
	require.True(t, res)

	// Verify bundle was stored immediately after submission
	queryRes := testWrapper.App.MevKeeper.PendingBundles(height)
	require.Equal(t, 1, len(queryRes))
	require.Equal(t, bundle.Transactions, queryRes[0].Transactions)

	// Call PrepareProposal
	prepareProposalReq := abci.RequestPrepareProposal{
		Txs: [][]byte{
			tx2,
		},
		MaxTxBytes: 10000000,
		Height:     height,
		Time:       testWrapper.Ctx.BlockTime(),
	}

	prepareProposalRes, err := testWrapper.App.PrepareProposal(sdk.WrapSDKContext(testWrapper.Ctx), &prepareProposalReq)
	require.NoError(t, err)
	require.NotNil(t, prepareProposalRes)

	t.Log("prepareProposalRes", prepareProposalRes)

	// Extract tx bytes from TxRecords
	txBytes := [][]byte{}
	for _, record := range prepareProposalRes.TxRecords {
		txBytes = append(txBytes, record.Tx)
	}
	require.Len(t, prepareProposalRes.TxRecords, 3)
}
