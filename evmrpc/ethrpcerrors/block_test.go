package ethrpcerrors_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/evmrpc/ethrpcerrors"
)

const (
	msgHeaderNotFound        = "header not found"
	msgHeaderForHashNotFound = "header for hash not found"
	msgUnknownBlock          = "unknown block"
	msgPrunedHistory         = "pruned history unavailable"
	msgMissingTrieNode       = "missing trie node"
)

var someHash = common.HexToHash("0xabcdef")

type blockGoldenCase struct {
	name    string
	in      *ethrpcerrors.BlockUnavailable
	reason  error
	code    int
	message string
	detail  string
	missing bool
	forLogs string
}

func blockGoldenCases() []blockGoldenCase {
	return []blockGoldenCase{
		{
			name:    "above safe latest",
			in:      ethrpcerrors.BlockAboveLatest(12, 10),
			reason:  ethrpcerrors.ErrBlockAboveLatest,
			code:    -32000,
			message: msgHeaderNotFound,
			detail:  "requested height 12 is not yet available; safe latest is 10",
			missing: true,
			forLogs: msgUnknownBlock,
		},
		{
			name:    "unknown hash",
			in:      ethrpcerrors.BlockUnknownHash(someHash),
			reason:  ethrpcerrors.ErrBlockUnknownHash,
			code:    -32000,
			message: msgHeaderForHashNotFound,
			detail:  "no block with hash " + someHash.Hex(),
			missing: true,
			forLogs: msgUnknownBlock,
		},
		{
			name:    "not in block store",
			in:      ethrpcerrors.BlockNotFound(7),
			reason:  ethrpcerrors.ErrBlockNotFound,
			code:    -32000,
			message: msgHeaderNotFound,
			detail:  "no block at height 7",
			missing: true,
			forLogs: msgUnknownBlock,
		},
		{
			name:    "history pruned",
			in:      ethrpcerrors.HistoryPruned(3, 100),
			reason:  ethrpcerrors.ErrHistoryPruned,
			code:    ethrpcerrors.CodePrunedHistory,
			message: msgPrunedHistory,
			detail:  "history at height 3 has been pruned; earliest available is 100",
			forLogs: msgPrunedHistory,
		},
		{
			name:    "state pruned",
			in:      ethrpcerrors.StatePruned(3, 100),
			reason:  ethrpcerrors.ErrStatePruned,
			code:    -32000,
			message: msgMissingTrieNode + ": state at height 3 is not available; earliest available is 100",
			detail:  "state at height 3 has been pruned; earliest available is 100",
			forLogs: msgMissingTrieNode + ": state at height 3 is not available; earliest available is 100",
		},
		{
			name:    "state never written",
			in:      ethrpcerrors.StatePruned(3, 0),
			reason:  ethrpcerrors.ErrStatePruned,
			code:    -32000,
			message: msgMissingTrieNode + ": state at height 3 is not available",
			detail:  "state at height 3 does not exist",
			forLogs: msgMissingTrieNode + ": state at height 3 is not available",
		},
	}
}

func TestBlockUnavailableGolden(t *testing.T) {
	for _, tc := range blockGoldenCases() {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.code, tc.in.ErrorCode())
			require.Equal(t, tc.message, tc.in.Error())
			require.Equal(t, tc.detail, tc.in.Detail())
			require.Nil(t, tc.in.ErrorData())
			require.ErrorIs(t, tc.in, tc.reason)
			require.Equal(t, tc.missing, ethrpcerrors.IsBlockMissing(tc.in))
			require.Equal(t, tc.forLogs, ethrpcerrors.ForLogs(tc.in).Error())
		})
	}
}

// TestBlockUnavailableWrappedStillSelectable pins that the reason survives fmt.Errorf wrapping,
// so an internal caller may wrap for its own logging and still select with errors.Is.
func TestBlockUnavailableWrappedStillSelectable(t *testing.T) {
	wrapped := fmt.Errorf("scan: %w", ethrpcerrors.BlockAboveLatest(12, 10))
	require.True(t, ethrpcerrors.IsBlockMissing(wrapped))
	require.ErrorIs(t, wrapped, ethrpcerrors.ErrBlockAboveLatest)
	require.False(t, ethrpcerrors.IsBlockMissing(errors.New("header not found")))
	require.False(t, ethrpcerrors.IsBlockMissing(nil))
}

func TestForStateRelabelsPrunedHistory(t *testing.T) {
	err := ethrpcerrors.ForState(ethrpcerrors.HistoryPruned(3, 100))
	require.ErrorIs(t, err, ethrpcerrors.ErrStatePruned)
	require.Equal(t, msgMissingTrieNode+": state at height 3 is not available; earliest available is 100", err.Error())
	rpcErr, ok := err.(rpc.Error)
	require.True(t, ok)
	require.Equal(t, -32000, rpcErr.ErrorCode())

	future := ethrpcerrors.BlockAboveLatest(12, 10)
	require.Same(t, future, ethrpcerrors.ForState(future))
	other := errors.New("tendermint down")
	require.Same(t, other, ethrpcerrors.ForState(other))
	require.Nil(t, ethrpcerrors.ForState(nil))
}

func TestForLogsPassesOtherErrorsThrough(t *testing.T) {
	other := errors.New("tendermint down")
	require.Same(t, other, ethrpcerrors.ForLogs(other))
	require.Nil(t, ethrpcerrors.ForLogs(nil))
	unknown := ethrpcerrors.ForLogs(ethrpcerrors.BlockUnknownHash(someHash))
	rpcErr, ok := unknown.(rpc.Error)
	require.True(t, ok)
	require.Equal(t, -32000, rpcErr.ErrorCode())
}

func TestBeyondHead(t *testing.T) {
	err := ethrpcerrors.BeyondHead(4294967295, 812)
	require.Equal(t, "request beyond head block: requested 4294967295, head 812", err.Error())
	rpcErr, ok := err.(rpc.Error)
	require.True(t, ok)
	require.Equal(t, -32000, rpcErr.ErrorCode())
}

// TestBlockUnavailableNoSeiVocabulary keeps the wire text free of the Sei-side words the detail
// uses, so a client never has to know how Sei stores blocks.
func TestBlockUnavailableNoSeiVocabulary(t *testing.T) {
	banned := []string{"watermark", "safe latest", "tendermint", "module", "receipt store", "pruned;"}
	for _, tc := range blockGoldenCases() {
		t.Run(tc.name, func(t *testing.T) {
			msg := tc.in.Error()
			require.False(t, strings.HasPrefix(msg, ": "))
			for _, b := range banned {
				require.NotContains(t, msg, b)
			}
		})
	}
}

type blockRoundTripService struct{}

func (*blockRoundTripService) Future(context.Context) (string, error) {
	return "", ethrpcerrors.BlockAboveLatest(12, 10)
}

func (*blockRoundTripService) Pruned(context.Context) (string, error) {
	return "", ethrpcerrors.HistoryPruned(3, 100)
}

func (*blockRoundTripService) Wrapped(context.Context) (string, error) {
	return "", fmt.Errorf("outer: %w", ethrpcerrors.HistoryPruned(3, 100))
}

func TestBlockUnavailableJSONRPCRoundTrip(t *testing.T) {
	srv := rpc.NewServer()
	t.Cleanup(srv.Stop)
	require.NoError(t, srv.RegisterName("test", &blockRoundTripService{}))
	client := rpc.DialInProc(srv)
	t.Cleanup(client.Close)

	t.Run("future", func(t *testing.T) {
		var res string
		err := client.CallContext(t.Context(), &res, "test_future")
		rpcErr, ok := err.(rpc.Error)
		require.True(t, ok)
		require.Equal(t, -32000, rpcErr.ErrorCode())
		require.Equal(t, msgHeaderNotFound, err.Error())
		dataErr, ok := err.(rpc.DataError)
		require.True(t, ok)
		require.Nil(t, dataErr.ErrorData())
	})
	t.Run("pruned keeps go-ethereum's 4444", func(t *testing.T) {
		var res string
		err := client.CallContext(t.Context(), &res, "test_pruned")
		rpcErr, ok := err.(rpc.Error)
		require.True(t, ok)
		require.Equal(t, ethrpcerrors.CodePrunedHistory, rpcErr.ErrorCode())
		require.Equal(t, msgPrunedHistory, err.Error())
	})
	t.Run("wrapped loses the code", func(t *testing.T) {
		var res string
		err := client.CallContext(t.Context(), &res, "test_wrapped")
		rpcErr, ok := err.(rpc.Error)
		require.True(t, ok)
		// go-ethereum type-asserts the top-level value, so a wrapper reverts to -32000 and
		// prefixes the text. BlockUnavailable must be returned as is.
		require.Equal(t, -32000, rpcErr.ErrorCode())
		require.Equal(t, "outer: "+msgPrunedHistory, err.Error())
	})
}
