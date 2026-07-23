package evmrpc

import (
	"context"
	"errors"
	"fmt"

	"github.com/ethereum/go-ethereum/rpc"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
)

var errNoHeightSource = errors.New("unable to determine height information")

// ErrBlockHeightNotYetAvailable is returned when a concrete block height is above the
// node's safe latest watermark. eth_getBlockByNumber maps this to result null (Ethereum spec).
var ErrBlockHeightNotYetAvailable = errors.New("block height not yet available")

// WatermarkManager coordinates access to block, state, and receipt stores to
// determine queryable block heights for RPC consumers. It ensures read-side
// requests only target heights where all backing data sources are fully
// synchronized.
type WatermarkManager struct {
	tmClient     client.LocalClient
	ctxProvider  func(int64) sdk.Context
	stateStore   types.StateStore // nil if SS is disabled.
	receiptStore receipt.ReceiptStore
}

func NewWatermarkManager(
	tmClient client.LocalClient,
	ctxProvider func(int64) sdk.Context,
	stateStore types.StateStore,
	receiptStore receipt.ReceiptStore,
) *WatermarkManager {
	return &WatermarkManager{
		tmClient:     tmClient,
		ctxProvider:  ctxProvider,
		stateStore:   stateStore,
		receiptStore: receiptStore,
	}
}

// Watermarks returns the earliest block height, earliest state height, and
// latest height that are safe to serve. Earliest heights are inclusive.
// It is possible that block latest < block earliest, in case there are no blocks yet.
func (m *WatermarkManager) Watermarks(ctx context.Context) (int64, int64, int64, error) {
	// Tendermint heights govern both block availability and the latest safe height.
	tmLatest, tmEarliest, err := m.fetchTendermintWatermarks(ctx)
	if err != nil {
		return 0, 0, 0, err
	}
	blockEarliest := tmEarliest
	latest := min(
		tmLatest,
		m.ctxProvider(LatestCtxHeight).BlockHeight(),
		m.receiptStore.LatestVersion(),
	)

	// State store heights (historical state DB) may lag behind block pruning.
	stateEarliest := latest // SS disabled: only the tip is servable.
	if m.stateStore != nil {
		latest = min(latest, m.stateStore.GetLatestVersion())
		// GetEarliestVersion() is 0 until the store first prunes or state-syncs.
		// An unpruned store retains state from genesis, so its earliest servable
		// state is height 1. A nonzero value is the real state-prune floor, used
		// as-is: it is a state-availability boundary independent of block pruning
		// and is not coupled to blockEarliest.
		stateEarliest = m.stateStore.GetEarliestVersion()
		if stateEarliest == 0 {
			stateEarliest = 1
		}
	}
	// State is never servable above the tip, and before the first commit
	// blockEarliest can lead latest. Clamp so `earliest` stays on the servable
	// (pre-commit checkState) path rather than a future height.
	stateEarliest = min(stateEarliest, latest)
	return blockEarliest, stateEarliest, latest, nil
}

// LatestHeight returns the inclusive latest height guaranteed to have complete
// data.
func (m *WatermarkManager) LatestHeight(ctx context.Context) (int64, error) {
	_, _, latest, err := m.Watermarks(ctx)
	return latest, err
}

// EarliestHeight returns the earliest height that remains fully queryable.
func (m *WatermarkManager) EarliestHeight(ctx context.Context) (int64, error) {
	blockEarliest, _, _, err := m.Watermarks(ctx)
	return blockEarliest, err
}

// EarliestStateHeight returns the earliest height with state availability.
func (m *WatermarkManager) EarliestStateHeight(ctx context.Context) (int64, error) {
	_, stateEarliest, _, err := m.Watermarks(ctx)
	return stateEarliest, err
}

// ResolveHeight normalizes a requested block identifier into a concrete height.
// If the resolved height sits outside the tracked watermarks, the method returns
// an error explaining whether it is too old (pruned) or too new (not yet
// available).
func (m *WatermarkManager) ResolveHeight(ctx context.Context, blockNrOrHash rpc.BlockNumberOrHash) (int64, error) {
	_, stateEarliest, latest, err := m.Watermarks(ctx)
	if err != nil {
		return 0, err
	}

	if blockNrOrHash.BlockHash != nil {
		if m.tmClient == nil {
			return 0, errNoHeightSource
		}
		res, err := blockByHash(ctx, m.tmClient, blockNrOrHash.BlockHash[:])
		if err != nil {
			return 0, err
		}
		height := res.Block.Height
		if err := ensureWithinWatermarks(height, stateEarliest, latest); err != nil {
			return 0, err
		}
		return height, nil
	}

	if blockNrOrHash.BlockNumber == nil {
		return latest, nil
	}

	blockNr := *blockNrOrHash.BlockNumber
	switch blockNr {
	case rpc.SafeBlockNumber, rpc.FinalizedBlockNumber, rpc.LatestBlockNumber, rpc.PendingBlockNumber:
		return latest, nil
	case rpc.EarliestBlockNumber:
		return stateEarliest, nil
	}

	heightPtr, err := getBlockNumber(ctx, m.tmClient, blockNr)
	if err != nil {
		return 0, err
	}
	if heightPtr == nil {
		return latest, nil
	}
	if err := ensureWithinWatermarks(*heightPtr, stateEarliest, latest); err != nil {
		return 0, err
	}
	return *heightPtr, nil
}

// EnsureBlockHeightAvailable verifies that the provided block height falls within
// the computed watermarks.
func (m *WatermarkManager) EnsureBlockHeightAvailable(ctx context.Context, height int64) error {
	blockEarliest, _, latest, err := m.Watermarks(ctx)
	if err != nil {
		return err
	}
	return ensureWithinWatermarks(height, blockEarliest, latest)
}

// EnsureReceiptHeightAvailable verifies that receipts for the given block height
// have not been pruned from the receipt store. This is a separate check from
// EnsureBlockHeightAvailable because the receipt store can be configured with a
// smaller KeepRecent than the block or state stores.
func (m *WatermarkManager) EnsureReceiptHeightAvailable(height int64) error {
	earliest := m.receiptStore.EarliestVersion()
	if height < earliest {
		return fmt.Errorf("requested height %d receipts have been pruned; earliest available is %d", height, earliest)
	}
	return nil
}

func ensureWithinWatermarks(height, earliest, latest int64) error {
	if height > latest {
		return fmt.Errorf("requested height %d is not yet available; safe latest is %d: %w", height, latest, ErrBlockHeightNotYetAvailable)
	}
	if height < earliest {
		return fmt.Errorf("requested height %d has been pruned; earliest available is %d", height, earliest)
	}
	return nil
}

func blockByNumberRespectingWatermarks(
	ctx context.Context,
	client client.LocalClient,
	wm *WatermarkManager,
	heightPtr *int64,
	maxRetries int,
) (*coretypes.ResultBlock, error) {
	if heightPtr == nil {
		latest, err := wm.LatestHeight(ctx)
		if err != nil {
			return nil, err
		}
		resolved := latest
		return blockByNumberWithRetry(ctx, client, &resolved, maxRetries)
	}
	if err := wm.EnsureBlockHeightAvailable(ctx, *heightPtr); err != nil {
		return nil, err
	}
	return blockByNumberWithRetry(ctx, client, heightPtr, maxRetries)
}

func blockByHashRespectingWatermarks(
	ctx context.Context,
	client client.LocalClient,
	wm *WatermarkManager,
	hash []byte,
	maxRetries int,
) (*coretypes.ResultBlock, error) {
	if wm == nil {
		return nil, errNoHeightSource
	}
	block, err := blockByHashWithRetry(ctx, client, hash, maxRetries)
	if err != nil {
		return nil, err
	}
	if err := wm.EnsureBlockHeightAvailable(ctx, block.Block.Height); err != nil {
		return nil, err
	}
	return block, nil
}

// blockByNumberOrNullForJSONRPC wraps blockByNumberRespectingWatermarks for
// Ethereum JSON-RPC endpoints that must return null (not an error) when the
// requested block sits above the safe-latest watermark — i.e. the block does
// not yet exist from the caller's perspective. This is the spec contract for
// endpoints that take a block identifier and return null for non-existent
// blocks (eth_getBlockByNumber, eth_getBlockByHash, eth_getBlockReceipts,
// eth_getTransactionByHash, eth_getTransactionByBlock*AndIndex, etc.).
//
// Internal call sites that genuinely need the error (state queries that must
// reject invalid heights, simulation paths bound to a specific block) keep
// using blockByNumberRespectingWatermarks directly.
func blockByNumberOrNullForJSONRPC(
	ctx context.Context,
	c client.LocalClient,
	wm *WatermarkManager,
	heightPtr *int64,
	maxRetries int,
) (*coretypes.ResultBlock, error) {
	block, err := blockByNumberRespectingWatermarks(ctx, c, wm, heightPtr, maxRetries)
	if errors.Is(err, ErrBlockHeightNotYetAvailable) {
		return nil, nil
	}
	return block, err
}

// blockByHashOrNullForJSONRPC is the by-hash counterpart of
// blockByNumberOrNullForJSONRPC. In addition to the above-watermark case it
// also converts ErrBlockNotFoundByHash to (nil, nil) — both are forms of
// "block doesn't exist from the caller's perspective" and the Ethereum
// JSON-RPC spec maps both to null.
func blockByHashOrNullForJSONRPC(
	ctx context.Context,
	c client.LocalClient,
	wm *WatermarkManager,
	hash []byte,
	maxRetries int,
) (*coretypes.ResultBlock, error) {
	block, err := blockByHashRespectingWatermarks(ctx, c, wm, hash, maxRetries)
	if errors.Is(err, ErrBlockHeightNotYetAvailable) || errors.Is(err, ErrBlockNotFoundByHash) {
		return nil, nil
	}
	return block, err
}

func (m *WatermarkManager) fetchTendermintWatermarks(ctx context.Context) (int64, int64, error) {
	status, err := m.tmClient.Status(ctx)
	if err != nil {
		return 0, 0, err
	}
	TraceTendermintIfApplicable(ctx, "Status", []string{}, status)
	latest := status.SyncInfo.LatestBlockHeight
	earliest := status.SyncInfo.EarliestBlockHeight
	return latest, earliest, nil
}
