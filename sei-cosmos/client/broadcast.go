package client

import (
	"context"
	"fmt"
	"strings"

	"github.com/pkg/errors"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/sei-protocol/sei-chain/sei-cosmos/client/flags"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	sdkerrors "github.com/sei-protocol/sei-chain/sei-cosmos/types/errors"
	"github.com/sei-protocol/sei-chain/sei-cosmos/types/tx"
)

var ErrTxInCache = errors.New("tx already exists in cache")

type ErrTxTooLarge struct {
	Max    int
	Actual int
}

func (e ErrTxTooLarge) Error() string {
	return fmt.Sprintf("Tx too large. Max size is %d, but got %d", e.Max, e.Actual)
}

type ErrMempoolIsFull struct {
	NumTxs      int
	MaxTxs      int
	TxsBytes    int64
	MaxTxsBytes int64
}

func (e ErrMempoolIsFull) Error() string {
	return fmt.Sprintf(
		"mempool is full: number of txs %d (max: %d), total txs bytes %d (max: %d)",
		e.NumTxs,
		e.MaxTxs,
		e.TxsBytes,
		e.MaxTxsBytes,
	)
}

// BroadcastTx broadcasts a transactions either synchronously or asynchronously. The result of the broadcast is parsed into
// an intermediate structure which is logged if the context has a logger
// defined.
func BroadcastTx(ctx context.Context, node Client, broadcastMode string, txBytes []byte) (*sdk.TxResponse, error) {
	switch broadcastMode {
	case flags.BroadcastSync:
		res, err := node.BroadcastTxSync(ctx, txBytes)
		if errRes := CheckTendermintError(err, txBytes); errRes != nil {
			return errRes, nil
		}
		return sdk.NewResponseFormatBroadcastTx(res), err
	case flags.BroadcastAsync:
		res, err := node.BroadcastTxAsync(ctx, txBytes)
		if errRes := CheckTendermintError(err, txBytes); errRes != nil {
			return errRes, nil
		}
		return sdk.NewResponseFormatBroadcastTx(res), err
	case flags.BroadcastBlock:
		res, err := node.BroadcastTxCommit(ctx, txBytes)
		if errRes := CheckTendermintError(err, txBytes); errRes != nil {
			return errRes, nil
		}
		return sdk.NewResponseFormatBroadcastTxCommit(res), err
	default:
		return nil, fmt.Errorf("unsupported return type %s; supported types: sync, async, block", broadcastMode)
	}
}

// CheckTendermintError checks if the error returned from BroadcastTx is a
// Tendermint error that is returned before the tx is submitted due to
// precondition checks that failed. If an Tendermint error is detected, this
// function returns the correct code back in TxResponse.
//
// TODO: Avoid brittle string matching in favor of error matching. This requires
// a change to Tendermint's RPCError type to allow retrieval or matching against
// a concrete error type.
func CheckTendermintError(err error, tx tmtypes.Tx) *sdk.TxResponse {
	if err == nil {
		return nil
	}

	errStr := strings.ToLower(err.Error())
	txHash := fmt.Sprintf("%X", tx.Hash())

	switch {
	case strings.Contains(errStr, strings.ToLower(ErrTxInCache.Error())):
		return &sdk.TxResponse{
			Code:      sdkerrors.ErrTxInMempoolCache.ABCICode(),
			Codespace: sdkerrors.ErrTxInMempoolCache.Codespace(),
			TxHash:    txHash,
		}

	case strings.Contains(errStr, "mempool is full"):
		return &sdk.TxResponse{
			Code:      sdkerrors.ErrMempoolIsFull.ABCICode(),
			Codespace: sdkerrors.ErrMempoolIsFull.Codespace(),
			TxHash:    txHash,
		}

	case strings.Contains(errStr, "tx too large"):
		return &sdk.TxResponse{
			Code:      sdkerrors.ErrTxTooLarge.ABCICode(),
			Codespace: sdkerrors.ErrTxTooLarge.Codespace(),
			TxHash:    txHash,
		}

	default:
		return nil
	}
}

// TxServiceBroadcast is a helper function to broadcast a Tx with the correct gRPC types
// from the tx service. Calls `clientCtx.BroadcastTx` under the hood.
func TxServiceBroadcast(grpcCtx context.Context, node Client, req *tx.BroadcastTxRequest) (*tx.BroadcastTxResponse, error) {
	if req == nil || req.TxBytes == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid empty tx")
	}
	resp, err := BroadcastTx(grpcCtx, node, normalizeBroadcastMode(req.Mode), req.TxBytes)
	if err != nil {
		return nil, err
	}
	return &tx.BroadcastTxResponse{TxResponse: resp}, nil
}

// normalizeBroadcastMode converts a broadcast mode into a normalized string
// to be passed into the clientCtx.
func normalizeBroadcastMode(mode tx.BroadcastMode) string {
	switch mode {
	case tx.BroadcastMode_BROADCAST_MODE_ASYNC:
		return "async"
	case tx.BroadcastMode_BROADCAST_MODE_BLOCK:
		return "block"
	case tx.BroadcastMode_BROADCAST_MODE_SYNC:
		return "sync"
	default:
		return "unspecified"
	}
}
