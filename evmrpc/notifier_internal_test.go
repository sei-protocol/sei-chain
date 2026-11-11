package evmrpc

import (
	"errors"
	"math/big"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/types"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
)

func TestBlockHeaderNotifier_DeliversEvent(t *testing.T) {
	n := NewBlockHeaderNotifier(4)

	hash := []byte{0x01, 0x02, 0x03}
	header := &tmproto.Header{Height: 42}
	resp := &abci.ResponseFinalizeBlock{}

	n.OnBlockCommitted(hash, header, resp)

	select {
	case evt := <-n.recv():
		require.Equal(t, hash, evt.hash)
		require.Equal(t, header, evt.header)
		require.Equal(t, resp, evt.response)
	case <-time.After(time.Second):
		t.Fatal("expected event on notifier channel")
	}
}

func TestBlockHeaderNotifier_OverwritesWhenFull(t *testing.T) {
	n := NewBlockHeaderNotifier(1)

	// Fill the buffer with a stale event.
	n.OnBlockCommitted([]byte{1}, &tmproto.Header{Height: 1}, &abci.ResponseFinalizeBlock{})

	// A second call must not block and must replace the buffered event.
	done := make(chan struct{})
	go func() {
		n.OnBlockCommitted([]byte{2}, &tmproto.Header{Height: 2}, &abci.ResponseFinalizeBlock{})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("OnBlockCommitted blocked when buffer was full")
	}

	// The newest event must survive; the stale one must be gone.
	evt := <-n.recv()
	require.EqualValues(t, 2, evt.header.Height, "expected newest event to win on overwrite")
	select {
	case extra := <-n.recv():
		t.Fatalf("expected stale event to be dropped, got %+v", extra)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestBlockHeaderNotifier_NilReceiverIsNoOp(t *testing.T) {
	// Setup: nil notifier, matching Autobahn-disabled App calls.
	var n *BlockHeaderNotifier

	// Test: every entry point, including subscribe.
	n.OnBlockCommitted(nil, &tmproto.Header{}, &abci.ResponseFinalizeBlock{})
	n.Stash(&abci.RequestFinalizeBlock{Header: &tmproto.Header{}}, &abci.ResponseFinalizeBlock{})
	n.ClearStash()
	n.subscribe(func(blockHeaderEvent) {})

	// Verify: no panic, and PublishStashed reports nothing to publish.
	require.False(t, n.PublishStashed())
}

func TestBlockHeaderNotifier_SubscribeGetsEveryEvent(t *testing.T) {
	// Setup: capacity-1 notifier so newHeads would overwrite.
	n := NewBlockHeaderNotifier(1)
	// Setup: subscribe listener records every height.
	var heights []int64
	n.subscribe(func(evt blockHeaderEvent) {
		heights = append(heights, evt.header.Height)
	})

	// Test: publish two blocks without draining recv().
	n.OnBlockCommitted([]byte{1}, &tmproto.Header{Height: 1}, &abci.ResponseFinalizeBlock{})
	n.OnBlockCommitted([]byte{2}, &tmproto.Header{Height: 2}, &abci.ResponseFinalizeBlock{})

	// Verify: subscribe saw both heights.
	require.Equal(t, []int64{1, 2}, heights, "subscribe must not drop hashes the way newHeads overwrite does")
	// Verify: recv() kept only the newest.
	evt := <-n.recv()
	require.EqualValues(t, 2, evt.header.Height, "newHeads channel still overwrite-on-full")
}

func TestBlockHeaderNotifier_SubscribeDoesNotStealFromRecv(t *testing.T) {
	// Setup: notifier with a no-op subscribe listener alongside recv().
	n := NewBlockHeaderNotifier(4)
	n.subscribe(func(blockHeaderEvent) {})

	// Test: publish one event.
	n.OnBlockCommitted([]byte{9}, &tmproto.Header{Height: 9}, &abci.ResponseFinalizeBlock{})

	// Verify: recv() still gets the event; subscribe did not consume the channel.
	select {
	case evt := <-n.recv():
		require.EqualValues(t, 9, evt.header.Height)
	case <-time.After(time.Second):
		t.Fatal("subscribe listener must not consume the newHeads channel")
	}
}

func TestBlockHeaderNotifier_SubscribePanicDoesNotSkipLaterListeners(t *testing.T) {
	// Setup: first listener panics; second records that it ran.
	n := NewBlockHeaderNotifier(4)
	n.subscribe(func(blockHeaderEvent) { panic("listener boom") })
	var second bool
	n.subscribe(func(blockHeaderEvent) { second = true })

	// Test: one publish must fan out to every listener.
	n.OnBlockCommitted([]byte{1}, &tmproto.Header{Height: 1}, &abci.ResponseFinalizeBlock{})

	// Verify: the second listener still ran.
	require.True(t, second)
}

// TestBlockHeaderNotifier_StashThenPublish covers the happy path used
// by the App: Stash captures a tuple, PublishStashed publishes it and
// clears the stash, second PublishStashed reports nothing to publish.
func TestBlockHeaderNotifier_StashThenPublish(t *testing.T) {
	n := NewBlockHeaderNotifier(4)
	req := &abci.RequestFinalizeBlock{
		Hash:   []byte{0xaa},
		Header: &tmproto.Header{Height: 7},
	}
	resp := &abci.ResponseFinalizeBlock{}

	n.Stash(req, resp)
	require.True(t, n.PublishStashed(), "first publish should report a published event")

	select {
	case evt := <-n.recv():
		require.Equal(t, req.Hash, evt.hash)
		require.Equal(t, req.Header, evt.header)
		require.Equal(t, resp, evt.response)
	case <-time.After(time.Second):
		t.Fatal("expected stashed event on notifier channel")
	}

	require.False(t, n.PublishStashed(), "second publish should report nothing to publish")
}

// TestBlockHeaderNotifier_ClearStashDropsPending covers FinalizeBlocker's
// defensive entry-clear: a stash from a prior block that never reached
// Commit must not be republished by a later Commit.
func TestBlockHeaderNotifier_ClearStashDropsPending(t *testing.T) {
	n := NewBlockHeaderNotifier(4)

	n.Stash(&abci.RequestFinalizeBlock{
		Hash:   []byte{0xaa},
		Header: &tmproto.Header{Height: 7},
	}, &abci.ResponseFinalizeBlock{})
	n.ClearStash()
	require.False(t, n.PublishStashed(), "ClearStash must drop the pending event")

	select {
	case evt := <-n.recv():
		t.Fatalf("expected channel to stay empty after ClearStash, got %+v", evt)
	case <-time.After(50 * time.Millisecond):
	}
}

// TestBlockHeaderNotifier_StashOverwrites guards the invariant that a
// second Stash without an intervening Publish replaces the first — i.e.
// only the latest FinalizeBlock outputs reach subscribers.
func TestBlockHeaderNotifier_StashOverwrites(t *testing.T) {
	n := NewBlockHeaderNotifier(4)

	n.Stash(&abci.RequestFinalizeBlock{
		Hash:   []byte{0x01},
		Header: &tmproto.Header{Height: 1},
	}, &abci.ResponseFinalizeBlock{})
	n.Stash(&abci.RequestFinalizeBlock{
		Hash:   []byte{0x02},
		Header: &tmproto.Header{Height: 2},
	}, &abci.ResponseFinalizeBlock{})
	require.True(t, n.PublishStashed())

	select {
	case evt := <-n.recv():
		require.EqualValues(t, 2, evt.header.Height, "second Stash must win")
	case <-time.After(time.Second):
		t.Fatal("expected event")
	}
}

func TestEncodeCommittedBlock(t *testing.T) {
	hash := common.HexToHash("0x1111111111111111111111111111111111111111111111111111111111111111").Bytes()
	proposer := common.HexToAddress("0x2222222222222222222222222222222222222222").Bytes()
	appHash := common.HexToHash("0x3333333333333333333333333333333333333333333333333333333333333333").Bytes()
	// Fractional second on purpose: it is the part "timestamp" drops and
	// "milliTimestamp" keeps, so a whole-second fixture would pass even if
	// milliTimestamp were derived as timestamp*1000.
	ts := time.Unix(1_700_000_000, 750_000_000).UTC()
	evt := blockHeaderEvent{
		hash: hash,
		header: &tmproto.Header{
			Height:          12345,
			Time:            ts,
			ProposerAddress: proposer,
		},
		response: &abci.ResponseFinalizeBlock{
			AppHash: appHash,
			TxResults: []*abci.ExecTxResult{
				{GasUsed: 21000},
				{GasUsed: 100000},
			},
		},
	}

	logsBloom := ethtypes.CreateBloom(&ethtypes.Receipt{Logs: []*ethtypes.Log{{
		Address: common.HexToAddress("0x4444444444444444444444444444444444444444"),
	}}})
	out := encodeCommittedBlock(evt, big.NewInt(42), 10_000_000, receiptTotals{logsBloom: logsBloom, gasUsed: 21000})

	require.Equal(t, common.BytesToHash(hash), out["hash"])
	require.Equal(t, (*hexutil.Big)(big.NewInt(12345)), out["number"])
	require.Equal(t, common.BytesToAddress(proposer), out["miner"])
	require.Equal(t, common.BytesToHash(appHash), out["stateRoot"])
	require.Equal(t, hexutil.Uint64(ts.Unix()), out["timestamp"])
	require.Equal(t, hexutil.Uint64(ts.UnixMilli()), out["milliTimestamp"])
	// gasUsed and logsBloom come from the receipts, not from TxResults, which also cover Cosmos transactions.
	require.Equal(t, hexutil.Uint64(21000), out["gasUsed"])
	require.Equal(t, logsBloom, out["logsBloom"])
	require.Equal(t, hexutil.Uint64(10_000_000), out["gasLimit"])
	require.Equal(t, (*hexutil.Big)(big.NewInt(42)), out["baseFeePerGas"])
	// Fields not surfaced by the Autobahn path must be zero, but present.
	require.Equal(t, common.Hash{}, out["parentHash"])
	require.Equal(t, common.Hash{}, out["receiptsRoot"])
	require.Equal(t, common.Hash{}, out["transactionsRoot"])
}

func TestEncodeCommittedBlock_ZeroGasLimit(t *testing.T) {
	evt := blockHeaderEvent{
		hash:     []byte{0xab},
		header:   &tmproto.Header{Height: 1, Time: time.Unix(0, 0)},
		response: &abci.ResponseFinalizeBlock{},
	}
	out := encodeCommittedBlock(evt, big.NewInt(0), 0, receiptTotals{})
	require.Equal(t, hexutil.Uint64(0), out["gasLimit"])
	// Nil response.AppHash must serialize as the zero hash, not panic.
	require.Equal(t, common.Hash{}, out["stateRoot"])
}

// TestPickHeadBaseFee_UsesParentCtx pins down the off-by-one fix:
// GetNextBaseFeePerGas(ctx_at_N) returns the fee for block N+1, so the
// base fee for the newHeads notification of block N must come from
// ctxProvider(N-1) — NOT ctxProvider(N). We spy on the ctxProvider to
// assert which height was queried.
func TestPickHeadBaseFee_UsesParentCtx(t *testing.T) {
	var captured []int64
	ctxProvider := func(h int64) sdk.Context {
		captured = append(captured, h)
		return sdk.Context{}
	}
	getNextBaseFee := func(sdk.Context) sdk.Dec {
		return sdk.NewDec(42)
	}

	got, err := pickHeadBaseFee(getNextBaseFee, ctxProvider, 5)

	require.NoError(t, err)
	require.Equal(t, big.NewInt(42), got, "should forward getNextBaseFee result")
	require.Equal(t, []int64{4}, captured, "ctxProvider must be called with parent height (height-1)")
}

// TestPickHeadBaseFee_GenesisFallback verifies that at height 1 we skip
// the keeper call entirely (there is no parent block) and return the
// configured default min fee.
func TestPickHeadBaseFee_GenesisFallback(t *testing.T) {
	var captured []int64
	ctxProvider := func(h int64) sdk.Context {
		captured = append(captured, h)
		return sdk.Context{}
	}
	getNextBaseFee := func(sdk.Context) sdk.Dec {
		t.Fatal("getNextBaseFee must not be called at genesis")
		return sdk.ZeroDec()
	}

	got, err := pickHeadBaseFee(getNextBaseFee, ctxProvider, 1)

	require.NoError(t, err)
	require.Equal(t, evmtypes.DefaultMinFeePerGas.TruncateInt().BigInt(), got)
	require.Empty(t, captured, "ctxProvider must not be called at height 1")
}

// TestPickHeadBaseFee_ParentStateUnavailable verifies that a parent height
// the node holds no state for is an error rather than a panic, which would
// end the goroutine serving every newHeads subscriber.
func TestPickHeadBaseFee_ParentStateUnavailable(t *testing.T) {
	ctxProvider := func(h int64) sdk.Context {
		panic(errors.New("unable to load historical state"))
	}
	getNextBaseFee := func(sdk.Context) sdk.Dec {
		t.Fatal("getNextBaseFee must not be called without the parent's state")
		return sdk.ZeroDec()
	}

	_, err := pickHeadBaseFee(getNextBaseFee, ctxProvider, 5)

	require.ErrorContains(t, err, "state at height 4 is unavailable")
}

// TestEncodeTmHeader_TakesTotalsFromReceipts verifies that the CometBFT
// newHeads header reports gasUsed and logsBloom from the receipt totals, not
// from the TxResults of the finalized block.
func TestEncodeTmHeader_TakesTotalsFromReceipts(t *testing.T) {
	logsBloom := ethtypes.CreateBloom(&ethtypes.Receipt{Logs: []*ethtypes.Log{{
		Address: common.HexToAddress("0x5555555555555555555555555555555555555555"),
	}}})
	header := tmtypes.EventDataNewBlockHeader{
		Header: tmtypes.Header{Height: 7, Time: time.Unix(1_700_000_000, 0)},
		ResultFinalizeBlock: abci.ResponseFinalizeBlock{
			TxResults: []*abci.ExecTxResult{{GasUsed: 21000}, {GasUsed: 90000}},
			ConsensusParamUpdates: &tmproto.ConsensusParams{
				Block: &tmproto.BlockParams{MaxGas: 10_000_000},
			},
		},
	}

	out, err := encodeTmHeader(header, big.NewInt(42), receiptTotals{logsBloom: logsBloom, gasUsed: 21000})

	require.NoError(t, err)
	require.Equal(t, hexutil.Uint64(21000), out["gasUsed"])
	require.Equal(t, logsBloom, out["logsBloom"])
	require.Equal(t, (*hexutil.Big)(big.NewInt(42)), out["baseFeePerGas"])
}

// TestUnreadReceiptTotals verifies the totals a newHeads header falls back
// to without its block's receipts: a bloom that no log filter can rule out,
// never an empty one, and the gas of every TxResult.
func TestUnreadReceiptTotals(t *testing.T) {
	totals := unreadReceiptTotals([]*abci.ExecTxResult{{GasUsed: 21000}, {GasUsed: 90000}})

	require.Equal(t, FullBloom(), totals.logsBloom)
	require.Equal(t, uint64(111000), totals.gasUsed)
}

// versionedReceiptStore is a receipt.ReceiptStore whose only working method
// is LatestVersion.
type versionedReceiptStore struct {
	receipt.ReceiptStore
	latest atomic.Int64
}

func (s *versionedReceiptStore) LatestVersion() int64 {
	return s.latest.Load()
}

func TestAwaitReceipts(t *testing.T) {
	t.Run("store already at height", func(t *testing.T) {
		store := &versionedReceiptStore{}
		store.latest.Store(10)
		require.NoError(t, awaitReceipts(store, 10, time.Second))
	})

	t.Run("store reaches height while waiting", func(t *testing.T) {
		store := &versionedReceiptStore{}
		store.latest.Store(9)
		go func() {
			time.Sleep(20 * time.Millisecond)
			store.latest.Store(10)
		}()
		require.NoError(t, awaitReceipts(store, 10, 5*time.Second))
	})

	t.Run("store stays behind height", func(t *testing.T) {
		store := &versionedReceiptStore{}
		store.latest.Store(9)
		err := awaitReceipts(store, 10, 20*time.Millisecond)
		require.ErrorContains(t, err, "receipts of height 10 not written")
	})

	t.Run("no receipt store", func(t *testing.T) {
		require.ErrorIs(t, awaitReceipts(nil, 10, time.Second), receipt.ErrNotConfigured)
	})
}
