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

// Watermarks returns the earliest and latest block heights that are safe to
// serve. Earliest is inclusive, latest is inclusive.
func (m *WatermarkManager) Watermarks(ctx context.Context) (int64, int64, error) {
	if m == nil {
		return 0, 0, errNoHeightSource
	}

	var (
		latest      int64
		latestSet   bool
		earliest    int64
		earliestSet bool
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

	setEarliest := func(candidate int64) {
		if candidate < 0 {
			return
		}
		if !earliestSet || candidate > earliest {
			earliest = candidate
			earliestSet = true
		}
	}

	// Tendermint heights
	if tmLatest, tmEarliest, err := m.fetchTendermintWatermarks(ctx); err == nil {
		setLatest(tmLatest)
		setEarliest(tmEarliest)
	} else if !errors.Is(err, errNoHeightSource) {
		return 0, 0, err
	}

	if m.ctxProvider != nil {
		if ctxHeight := m.ctxProvider(LatestCtxHeight).BlockHeight(); ctxHeight > 0 {
			setLatest(ctxHeight)
		}
	}

	// State store heights (historical state DB)
	if ssLatest, ssEarliest, ok := m.fetchStateStoreWatermarks(); ok {
		setLatest(ssLatest)
		setEarliest(ssEarliest)
	}

	// Receipt store height participates only in the latest watermark, since
	// pruning guarantees the earliest watermark is bounded by TM+SS.
	if m.receiptStore != nil {
		setLatest(m.receiptStore.GetLatestVersion())
	}

	if !latestSet {
		return 0, 0, errNoHeightSource
	}

	if !earliestSet {
		earliest = 0
	}

	if latest < earliest {
		return 0, 0, fmt.Errorf("computed latest watermark %d is behind earliest %d", latest, earliest)
	}
	return earliest, latest, nil
}

// LatestHeight returns the inclusive latest height guaranteed to have complete
// data.
func (m *WatermarkManager) LatestHeight(ctx context.Context) (int64, error) {
	_, latest, err := m.Watermarks(ctx)
	return latest, err
}

// EarliestHeight returns the earliest height that remains fully queryable.
func (m *WatermarkManager) EarliestHeight(ctx context.Context) (int64, error) {
	earliest, _, err := m.Watermarks(ctx)
	return earliest, err
}

// ResolveHeight normalizes a requested block identifier into a concrete height
// that is guaranteed to be within the available watermarks.
func (m *WatermarkManager) ResolveHeight(ctx context.Context, blockNrOrHash rpc.BlockNumberOrHash) (int64, error) {
	if m == nil {
		return 0, errNoHeightSource
	}

	earliest, latest, err := m.Watermarks(ctx)
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
		if err := m.ensureWithinWatermarks(height, earliest, latest); err != nil {
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
		return earliest, nil
	}

	heightPtr, err := getBlockNumber(ctx, m.tmClient, blockNr)
	if err != nil {
		return 0, err
	}
	if heightPtr == nil {
		return latest, nil
	}
	if err := m.ensureWithinWatermarks(*heightPtr, earliest, latest); err != nil {
		return 0, err
	}
	return *heightPtr, nil
}

// EnsureHeightAvailable verifies that the provided height falls within the
// computed watermarks.
func (m *WatermarkManager) EnsureHeightAvailable(ctx context.Context, height int64) error {
	if m == nil {
		return errNoHeightSource
	}
	earliest, latest, err := m.Watermarks(ctx)
	if err != nil {
		return err
	}
	return m.ensureWithinWatermarks(height, earliest, latest)
}

func (m *WatermarkManager) ensureWithinWatermarks(height, earliest, latest int64) error {
	if height > latest {
		return fmt.Errorf("requested block height %d is not yet available; safe latest is %d", height, latest)
	}
	if height < earliest {
		return fmt.Errorf("requested block height %d has been pruned; earliest available is %d", height, earliest)
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
	if wm == nil {
		return blockByNumberWithRetry(ctx, client, heightPtr, maxRetries)
	}
	if heightPtr == nil {
		latest, err := wm.LatestHeight(ctx)
		if err != nil {
			return nil, err
		}
		resolved := latest
		return blockByNumberWithRetry(ctx, client, &resolved, maxRetries)
	}
	if err := wm.EnsureHeightAvailable(ctx, *heightPtr); err != nil {
		return nil, err
	}
	return blockByNumberWithRetry(ctx, client, heightPtr, maxRetries)
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
