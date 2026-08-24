package p2p

import (
	"testing"
	"time"

	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/eventbus"
	tmpubsub "github.com/sei-protocol/sei-chain/sei-tendermint/internal/pubsub"
	tmquery "github.com/sei-protocol/sei-chain/sei-tendermint/internal/pubsub/query"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

func testGlobalBlock(t *testing.T, rng utils.Rng, height atypes.GlobalBlockNumber, txs [][]byte) *atypes.GlobalBlock {
	t.Helper()
	payload, err := atypes.PayloadBuilder{
		CreatedAt: time.Now(),
		Txs:       txs,
	}.Build()
	require.NoError(t, err)
	block := atypes.NewBlock(atypes.GenLaneID(rng), 1, atypes.GenBlockHeaderHash(rng), payload)
	return &atypes.GlobalBlock{
		Header:       block.Header(),
		Timestamp:    time.Now(),
		GlobalNumber: height,
		Payload:      payload,
	}
}

func subscribe(t *testing.T, bus *eventbus.EventBus, clientID string, query *tmquery.Query, limit int) eventbus.Subscription {
	t.Helper()
	sub, err := bus.SubscribeWithArgs(t.Context(), tmpubsub.SubscribeArgs{
		ClientID: clientID,
		Query:    query,
		Limit:    limit,
	})
	require.NoError(t, err)
	return sub
}

func waitForN(t *testing.T, sub eventbus.Subscription, n int) {
	t.Helper()
	for range n {
		_, err := sub.Next(t.Context())
		require.NoError(t, err)
	}
}

func TestPublishExecutedBlockEventsIndexesTxs(t *testing.T) {
	rng := utils.TestRng()

	// Setup: EventBus, KV indexer, and subscriptions for header + tx events.
	genDoc := &types.GenesisDoc{ChainID: "giga-index-test", InitialHeight: 1}
	require.NoError(t, genDoc.ValidateAndComplete())

	bus := startedEventBus(t)
	sink := startedTxIndexer(t, bus)
	txSub := subscribe(t, bus, "txs", types.EventQueryTx, 8)
	headerSub := subscribe(t, bus, "headers", types.EventQueryNewBlockHeader, 8)

	// Setup: committed Autobahn block with two txs and matching FinalizeBlock results.
	txs := [][]byte{[]byte("tx-a"), []byte("tx-b")}
	height := atypes.GlobalBlockNumber(7)
	gb := testGlobalBlock(t, rng, height, txs)
	resp := &abci.ResponseFinalizeBlock{
		TxResults: []*abci.ExecTxResult{
			{Code: abci.CodeTypeOK, GasUsed: 11},
			{Code: abci.CodeTypeOK, GasUsed: 22},
		},
	}

	// Test: publish NewBlock / NewBlockHeader / per-tx events as executeBlock does.
	r := &gigaRouterCommon{cfg: &GigaRouterCommonConfig{GenDoc: genDoc, EventBus: bus}}
	r.publishExecutedBlockEvents(gb, nil, resp)

	// Validate: indexer has the block and each tx by hash, with height/index/gas.
	waitForN(t, headerSub, 1)
	waitForN(t, txSub, len(txs))

	ok, err := sink.HasBlock(int64(height))
	require.NoError(t, err)
	require.True(t, ok)

	for i, tx := range txs {
		got, err := sink.GetTxByHash(types.Tx(tx).Hash().Bytes())
		require.NoError(t, err)
		require.NotNil(t, got)
		require.Equal(t, int64(height), got.Height)
		require.Equal(t, uint32(i), got.Index)
		require.Equal(t, tx, []byte(got.Tx))
		require.Equal(t, resp.TxResults[i].GasUsed, got.Result.GasUsed)
	}
}

func TestPublishExecutedBlockEventsIndexesEmptyBlock(t *testing.T) {
	rng := utils.TestRng()

	// Setup: EventBus, KV indexer, and a header subscription.
	genDoc := &types.GenesisDoc{ChainID: "giga-index-empty", InitialHeight: 1}
	require.NoError(t, genDoc.ValidateAndComplete())

	bus := startedEventBus(t)
	sink := startedTxIndexer(t, bus)
	headerSub := subscribe(t, bus, "headers", types.EventQueryNewBlockHeader, 4)

	// Test: publish events for a committed block with no txs.
	height := atypes.GlobalBlockNumber(3)
	gb := testGlobalBlock(t, rng, height, nil)
	r := &gigaRouterCommon{cfg: &GigaRouterCommonConfig{GenDoc: genDoc, EventBus: bus}}
	r.publishExecutedBlockEvents(gb, nil, &abci.ResponseFinalizeBlock{})

	// Validate: indexer recorded the empty block.
	waitForN(t, headerSub, 1)

	ok, err := sink.HasBlock(int64(height))
	require.NoError(t, err)
	require.True(t, ok)
}

func TestPublishExecutedBlockEventsPanicsBeforeDispatchOnTxCountMismatch(t *testing.T) {
	rng := utils.TestRng()

	// Setup: counting EventBus and a one-tx block whose TxResults length does not match.
	genDoc := &types.GenesisDoc{ChainID: "giga-index-mismatch", InitialHeight: 1}
	require.NoError(t, genDoc.ValidateAndComplete())

	bus := &countingBlockEvents{}
	gb := testGlobalBlock(t, rng, 1, [][]byte{[]byte("tx-a")})
	r := &gigaRouterCommon{cfg: &GigaRouterCommonConfig{GenDoc: genDoc, EventBus: bus}}

	// Test: publish with empty TxResults.
	require.Panics(t, func() {
		r.publishExecutedBlockEvents(gb, nil, &abci.ResponseFinalizeBlock{})
	})

	// Validate: no EventBus methods ran before the panic.
	require.Equal(t, 0, bus.n)
}

type countingBlockEvents struct{ n int }

func (c *countingBlockEvents) PublishEventNewBlock(types.EventDataNewBlock) error {
	c.n++
	return nil
}
func (c *countingBlockEvents) PublishEventNewBlockHeader(types.EventDataNewBlockHeader) error {
	c.n++
	return nil
}
func (c *countingBlockEvents) PublishEventNewEvidence(types.EventDataNewEvidence) error {
	c.n++
	return nil
}
func (c *countingBlockEvents) PublishEventTx(types.EventDataTx) error {
	c.n++
	return nil
}
func (c *countingBlockEvents) PublishEventValidatorSetUpdates(types.EventDataValidatorSetUpdates) error {
	c.n++
	return nil
}
