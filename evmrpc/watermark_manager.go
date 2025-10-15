package evmrpc

import (
	"context"
	"errors"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/rpc"
	sstypes "github.com/sei-protocol/sei-db/ss/types"
	rpcclient "github.com/tendermint/tendermint/rpc/client"
	"github.com/tendermint/tendermint/rpc/coretypes"
)

var errNoHeightSource = errors.New("unable to determine height information")

// WatermarkManager coordinates access to block, state, and receipt stores to
// determine queryable block heights for RPC consumers. It ensures read-side
// requests only target heights where all backing data sources are fully
// synchronized.
type WatermarkManager struct {
	tmClient     rpcclient.Client
	ctxProvider  func(int64) sdk.Context
	stateStore   sstypes.StateStore
	receiptStore sstypes.StateStore
}

func NewWatermarkManager(
	tmClient rpcclient.Client,
	ctxProvider func(int64) sdk.Context,
	stateStore sstypes.StateStore,
	receiptStore sstypes.StateStore,
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
func (m *WatermarkManager) Watermarks(ctx context.Context) (int64, int64, int64, error) {
	var (
		latest           int64
		latestSet        bool
		blockEarliest    int64
		blockEarliestSet bool
		stateEarliest    int64
		stateEarliestSet bool
	)

	setLatest := func(candidate int64) {
		if candidate < 0 {
			return
		}
		if !latestSet || candidate < latest {
			latest = candidate
			latestSet = true
		}
	}

	setBlockEarliest := func(candidate int64) {
		if candidate < 0 {
			return
		}
		if !blockEarliestSet || candidate > blockEarliest {
			blockEarliest = candidate
			blockEarliestSet = true
		}
	}

	setStateEarliest := func(candidate int64) {
		if candidate < 0 {
			return
		}
		if !stateEarliestSet || candidate > stateEarliest {
			stateEarliest = candidate
			stateEarliestSet = true
		}
	}

	// Tendermint heights govern both block availability and the latest safe height.
	if tmLatest, tmEarliest, err := m.fetchTendermintWatermarks(ctx); err == nil {
		setLatest(tmLatest)
		setBlockEarliest(tmEarliest)
	} else if !errors.Is(err, errNoHeightSource) {
		return 0, 0, 0, err
	}

	if m.ctxProvider != nil {
		if ctxHeight := m.ctxProvider(LatestCtxHeight).BlockHeight(); ctxHeight > 0 {
			setLatest(ctxHeight)
		}
	}

	// State store heights (historical state DB) may lag behind block pruning.
	if ssLatest, ssEarliest, ok := m.fetchStateStoreWatermarks(); ok {
		setLatest(ssLatest)
		setStateEarliest(ssEarliest)
	}

	// Receipt store height participates only in the latest watermark.
	if m.receiptStore != nil {
		setLatest(m.receiptStore.GetLatestVersion())
	}

	if !latestSet {
		return 0, 0, 0, errNoHeightSource
	}

	if !blockEarliestSet {
		blockEarliest = 0
	}

	if !stateEarliestSet {
		stateEarliest = blockEarliest
	}

	if blockEarliest > latest {
		return 0, 0, 0, fmt.Errorf("computed block earliest watermark %d is beyond latest %d", blockEarliest, latest)
	}

	if stateEarliest > latest {
		return 0, 0, 0, fmt.Errorf("computed state earliest watermark %d is beyond latest %d", stateEarliest, latest)
	}

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
		if err := m.ensureWithinWatermarks(height, stateEarliest, latest); err != nil {
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
	if err := m.ensureWithinWatermarks(*heightPtr, stateEarliest, latest); err != nil {
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
	return m.ensureWithinWatermarks(height, blockEarliest, latest)
}

func (m *WatermarkManager) ensureWithinWatermarks(height, earliest, latest int64) error {
	if height > latest {
		return fmt.Errorf("requested height %d is not yet available; safe latest is %d", height, latest)
	}
	if height < earliest {
		return fmt.Errorf("requested height %d has been pruned; earliest available is %d", height, earliest)
	}
	return nil
}

func blockByNumberRespectingWatermarks(
	ctx context.Context,
	client rpcclient.Client,
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
	client rpcclient.Client,
	wm *WatermarkManager,
	hash []byte,
	maxRetries int,
) (*coretypes.ResultBlock, error) {
	block, err := blockByHashWithRetry(ctx, client, hash, maxRetries)
	if err != nil {
		return nil, err
	}
	if wm == nil {
		return block, nil
	}
	if err := wm.EnsureBlockHeightAvailable(ctx, block.Block.Height); err != nil {
		return nil, err
	}
	return block, nil
}

func (m *WatermarkManager) fetchTendermintWatermarks(ctx context.Context) (int64, int64, error) {
	if m.tmClient == nil {
		return 0, 0, errNoHeightSource
	}
	status, err := m.tmClient.Status(ctx)
	if err != nil {
		return 0, 0, err
	}
	TraceTendermintIfApplicable(ctx, "Status", []string{}, status)
	return status.SyncInfo.LatestBlockHeight, status.SyncInfo.EarliestBlockHeight, nil
}

func (m *WatermarkManager) fetchStateStoreWatermarks() (int64, int64, bool) {
	if m.stateStore == nil {
		return 0, 0, false
	}
	return m.stateStore.GetLatestVersion(), m.stateStore.GetEarliestVersion(), true
}
