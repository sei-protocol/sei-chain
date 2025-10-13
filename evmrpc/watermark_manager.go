package evmrpc

import (
	"context"
	"errors"
	"fmt"

	storetypes "github.com/cosmos/cosmos-sdk/store/types"
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
		earliestCandidates []int64
		latestCandidates   []int64
	)

	// Tendermint heights
	if latest, earliest, err := m.fetchTendermintWatermarks(ctx); err == nil {
		latestCandidates = append(latestCandidates, latest)
		earliestCandidates = append(earliestCandidates, earliest)
	} else if !errors.Is(err, errNoHeightSource) {
		return 0, 0, err
	}

	// State store heights (historical state DB)
	if ssLatest, ssEarliest, ok := m.fetchStateStoreWatermarks(); ok {
		latestCandidates = append(latestCandidates, ssLatest)
		earliestCandidates = append(earliestCandidates, ssEarliest)
	} else if msLatest, msEarliest, ok := m.fetchMultiStoreWatermarks(); ok {
		// Fall back to application multistore heights if state store unavailable
		latestCandidates = append(latestCandidates, msLatest)
		earliestCandidates = append(earliestCandidates, msEarliest)
	}

	// Receipt store height participates only in the latest watermark, since
	// pruning guarantees the earliest watermark is bounded by TM+SS.
	if m.receiptStore != nil {
		latestCandidates = append(latestCandidates, m.receiptStore.GetLatestVersion())
	}

	if len(latestCandidates) == 0 {
		return 0, 0, errNoHeightSource
	}

	latest := minInt64(latestCandidates)
	earliest := int64(0)
	if len(earliestCandidates) > 0 {
		earliest = maxInt64(earliestCandidates)
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

func (m *WatermarkManager) fetchMultiStoreWatermarks() (latest int64, earliest int64, ok bool) {
	if m.ctxProvider == nil {
		return 0, 0, false
	}
	ctx := m.ctxProvider(LatestCtxHeight)
	ms := ctx.MultiStore()
	if ms == nil {
		return 0, 0, false
	}
	defer func() {
		if r := recover(); r != nil {
			latest, earliest, ok = 0, 0, false
		}
	}()
	earliest = ms.GetEarliestVersion()
	if earliest == 0 {
		earliest = ctx.BlockHeight()
	}
	if commitStore, implements := ms.(interface{ LastCommitID() storetypes.CommitID }); implements {
		latest = commitStore.LastCommitID().Version
	}
	if latest == 0 {
		latest = ctx.BlockHeight()
	}
	return latest, earliest, true
}

func minInt64(values []int64) int64 {
	min := values[0]
	for _, v := range values[1:] {
		if v < min {
			min = v
		}
	}
	return min
}

func maxInt64(values []int64) int64 {
	max := values[0]
	for _, v := range values[1:] {
		if v > max {
			max = v
		}
	}
	return max
}
