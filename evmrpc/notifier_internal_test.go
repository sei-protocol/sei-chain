package evmrpc

import (
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
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
	var n *BlockHeaderNotifier
	// Must not panic.
	n.OnBlockCommitted(nil, &tmproto.Header{}, &abci.ResponseFinalizeBlock{})
}

func TestEncodeCommittedBlock(t *testing.T) {
	hash := common.HexToHash("0x1111111111111111111111111111111111111111111111111111111111111111").Bytes()
	proposer := common.HexToAddress("0x2222222222222222222222222222222222222222").Bytes()
	appHash := common.HexToHash("0x3333333333333333333333333333333333333333333333333333333333333333").Bytes()
	ts := time.Unix(1_700_000_000, 0).UTC()
	evt := blockHeaderEvent{
		hash: hash,
		header: &tmproto.Header{
			Height:          12345,
			Time:            ts,
			ProposerAddress: proposer,
			AppHash:         appHash,
		},
		response: &abci.ResponseFinalizeBlock{
			TxResults: []*abci.ExecTxResult{
				{GasUsed: 21000},
				{GasUsed: 100000},
			},
		},
	}

	out := encodeCommittedBlock(evt, big.NewInt(42), 10_000_000)

	require.Equal(t, common.BytesToHash(hash), out["hash"])
	require.Equal(t, (*hexutil.Big)(big.NewInt(12345)), out["number"])
	require.Equal(t, common.BytesToAddress(proposer), out["miner"])
	require.Equal(t, common.BytesToHash(appHash), out["stateRoot"])
	require.Equal(t, hexutil.Uint64(ts.Unix()), out["timestamp"])
	require.Equal(t, hexutil.Uint64(121000), out["gasUsed"])
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
	out := encodeCommittedBlock(evt, big.NewInt(0), 0)
	require.Equal(t, hexutil.Uint64(0), out["gasLimit"])
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

	got := pickHeadBaseFee(getNextBaseFee, ctxProvider, 5)

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

	got := pickHeadBaseFee(getNextBaseFee, ctxProvider, 1)

	require.Equal(t, evmtypes.DefaultMinFeePerGas.TruncateInt().BigInt(), got)
	require.Empty(t, captured, "ctxProvider must not be called at height 1")
}
