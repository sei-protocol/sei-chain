package app_test

import (
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/keys/secp256k1"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/config"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/sei-protocol/sei-chain/x/evm/types/ethtx"
	"github.com/stretchr/testify/require"
)

// TestUnderstatedGasEstimatesExecuteBlockPastMaxGas checks that EVM txs understating their gas
// estimate are admitted past MaxGas, and that the block then executes up to MaxGasWanted gas.
func TestUnderstatedGasEstimatesExecuteBlockPastMaxGas(t *testing.T) {
	const (
		maxGas            = int64(1_000_000)
		maxGasWanted      = 4 * maxGas
		txsAtMaxGasWanted = int(maxGasWanted / maxGas)
	)
	ap, burner, senders := commitGasBurnerChain(t, maxGas, maxGasWanted, txsAtMaxGasWanted+1)
	calls := signGasBurnerCalls(t, burner, senders, uint64(maxGas))
	honest := wrapEVMTxs(t, ap, calls, uint64(maxGas))
	understated := wrapEVMTxs(t, ap, calls, app.MinGasEVMTx)

	for _, tx := range understated {
		res := ap.CheckTx(context.Background(), &abci.RequestCheckTxV2{Tx: tx})
		require.Equal(t, uint32(0), res.Code, res.Log)
		require.Equal(t, int64(app.MinGasEVMTx), res.GasEstimated)
	}

	header := &types.Header{ChainID: "sei-test", Height: 2}
	require.Equal(t, abci.ResponseProcessProposal_REJECT, processProposal(t, ap, header, honest[:txsAtMaxGasWanted]),
		"honest estimates exceed MaxGas")
	require.Equal(t, abci.ResponseProcessProposal_REJECT, processProposal(t, ap, header, understated),
		"gas wanted exceeds MaxGasWanted")
	require.Equal(t, abci.ResponseProcessProposal_ACCEPT, processProposal(t, ap, header, understated[:txsAtMaxGasWanted]),
		"understated estimates fit MaxGas and gas wanted fits MaxGasWanted")

	res, err := ap.FinalizeBlock(context.Background(), &abci.RequestFinalizeBlock{Txs: understated[:txsAtMaxGasWanted], Header: header})
	require.NoError(t, err)
	require.Len(t, res.TxResults, txsAtMaxGasWanted)
	var gasUsed int64
	for _, txResult := range res.TxResults {
		require.Equal(t, uint32(0), txResult.Code, txResult.Log)
		gasUsed += txResult.GasUsed
	}
	require.Greater(t, gasUsed, maxGas)
	require.LessOrEqual(t, gasUsed, maxGasWanted)
}

// commitGasBurnerChain returns an app whose committed state has the given block gas limits, a
// contract at the returned address that burns the gas it is given, and n funded EVM senders.
func commitGasBurnerChain(t *testing.T, maxGas, maxGasWanted int64, n int) (*app.App, common.Address, []*ecdsa.PrivateKey) {
	t.Helper()
	tw := app.NewTestWrapper(t, time.Now().UTC(), secp256k1.GenPrivKey().PubKey(), false)
	tw.App.StoreConsensusParams(tw.Ctx, &types.ConsensusParams{
		Block: &types.BlockParams{MaxGas: maxGas, MaxGasWanted: maxGasWanted},
	})
	_, burner := testkeeper.MockAddressPair()
	// Loops while gasleft() > 1000, then stops.
	tw.App.EvmKeeper.SetCode(tw.Ctx, burner, common.FromHex("0x5b6103e85a1160005700"))

	senders := make([]*ecdsa.PrivateKey, n)
	for i := range senders {
		privKey := testkeeper.MockPrivateKey()
		seiAddr, _ := testkeeper.PrivateKeyToAddresses(privKey)
		tw.FundAcc(seiAddr, sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(1_000_000_000_000))))
		key, err := crypto.HexToECDSA(hex.EncodeToString(privKey.Bytes()))
		require.NoError(t, err)
		senders[i] = key
	}

	_, err := tw.App.FinalizeBlock(context.Background(), &abci.RequestFinalizeBlock{
		Header: &types.Header{ChainID: "sei-test", Height: 1},
	})
	require.NoError(t, err)
	tw.App.SetDeliverStateToCommit()
	_, err = tw.App.Commit(context.Background())
	require.NoError(t, err)
	return tw.App, burner, senders
}

// signGasBurnerCalls returns one call to burner from each sender, each with the given gas limit.
func signGasBurnerCalls(t *testing.T, burner common.Address, senders []*ecdsa.PrivateKey, gasLimit uint64) []*ethtypes.Transaction {
	t.Helper()
	chainID := big.NewInt(config.DefaultChainID)
	signer := ethtypes.LatestSignerForChainID(chainID)
	calls := make([]*ethtypes.Transaction, len(senders))
	for i, key := range senders {
		call, err := ethtypes.SignNewTx(key, signer, &ethtypes.DynamicFeeTx{
			ChainID:   chainID,
			GasTipCap: big.NewInt(1_000_000_000),
			GasFeeCap: big.NewInt(100_000_000_000),
			Gas:       gasLimit,
			To:        &burner,
		})
		require.NoError(t, err)
		calls[i] = call
	}
	return calls
}

// wrapEVMTxs encodes each EVM tx in a Cosmos tx declaring the given gas estimate.
func wrapEVMTxs(t *testing.T, ap *app.App, txs []*ethtypes.Transaction, gasEstimate uint64) [][]byte {
	t.Helper()
	wrapped := make([][]byte, len(txs))
	for i, tx := range txs {
		txData, err := ethtx.NewTxDataFromTx(tx)
		require.NoError(t, err)
		msg, err := evmtypes.NewMsgEVMTransaction(txData)
		require.NoError(t, err)
		builder := ap.GetTxConfig().NewTxBuilder()
		require.NoError(t, builder.SetMsgs(msg))
		builder.SetGasEstimate(gasEstimate)
		wrapped[i], err = ap.GetTxConfig().TxEncoder()(builder.GetTx())
		require.NoError(t, err)
	}
	return wrapped
}

// processProposal returns the status ProcessProposal gives txs proposed at header.
func processProposal(t *testing.T, ap *app.App, header *types.Header, txs [][]byte) abci.ResponseProcessProposal_ProposalStatus {
	t.Helper()
	res, err := ap.ProcessProposal(context.Background(), &abci.RequestProcessProposal{Txs: txs, Header: header})
	require.NoError(t, err)
	return res.Status
}
