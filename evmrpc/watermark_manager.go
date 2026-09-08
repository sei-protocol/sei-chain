package evmrpc

import (
	"context"
	"errors"

	"github.com/ethereum/go-ethereum/rpc"
	"github.com/sei-protocol/sei-chain/evmrpc/ethrpcerrors"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	genesistypes "github.com/sei-protocol/sei-chain/sei-cosmos/types/genesis"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
)

var errNoHeightSource = errors.New("unable to determine height information")

// WatermarkManager coordinates access to block, state, and receipt stores to
// determine queryable block heights for RPC consumers. It ensures read-side
// requests only target heights where all backing data sources are fully
// synchronized.
type WatermarkManager struct {
	tmClient     client.LocalClient
	ctxProvider  func(int64) sdk.Context
	stateStore   types.StateStore     // nil if SS is disabled.
	receiptStore receipt.ReceiptStore // nil if the receipt store is disabled.
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

// genesisInitialHeight returns the chain's genesis InitialHeight, sourced from
// the local client's in-memory genesis doc, falling back to
// genesis.DefaultGenesisInitialHeight when the client cannot supply it.
func (m *WatermarkManager) genesisInitialHeight() int64 {
	if h := m.tmClient.GenesisInitialHeight(); h > 0 {
		return h
	}
	return genesistypes.DefaultGenesisInitialHeight
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
	)

	// A node keeping no receipts has no receipt height to cap the safe latest with; its receipt
	// queries fail on their own rather than by shrinking the window every other query serves.
	if m.receiptStore != nil {
		latest = min(latest, m.receiptStore.LatestVersion())
	}

	// State store heights (historical state DB) may lag behind block pruning.
	stateEarliest := latest // no historical storage => just the current state.
	if m.stateStore != nil {
		latest = min(latest, m.stateStore.GetLatestVersion())
		stateEarliest = m.stateStore.GetEarliestVersion()
	}

	// Floor the earliest state height at genesis. A never-pruned store reports
	// its earliest version as 0 (the earliest-version key is only written by
	// pruning or state-sync), which would otherwise resolve `earliest` to the
	// tip via CreateQueryContext's height-0 coercion. Guarding on
	// stateEarliest < latest leaves the pre-commit window (no blocks yet, where
	// stateEarliest == latest) reading the genesis checkState, and the clamp to
	// latest keeps the floor from ever exceeding the safe latest. Using the real
	// InitialHeight (not a literal 1) keeps this correct for chains started
	// above height 1, whose version 1 was never committed.
	if stateEarliest < latest {
		stateEarliest = max(stateEarliest, min(m.genesisInitialHeight(), latest))
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

// ResolveHeight normalizes a requested block identifier into a concrete height whose
// state can be served. A height outside the state watermarks, or a hash no block carries,
// is reported as an *ethrpcerrors.BlockUnavailable.
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
		if err := ensureWithinWatermarks(height, stateEarliest, latest, ethrpcerrors.StatePruned); err != nil {
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
	if err := ensureWithinWatermarks(*heightPtr, stateEarliest, latest, ethrpcerrors.StatePruned); err != nil {
		return 0, err
	}
	return *heightPtr, nil
}

// EnsureBlockHeightAvailable verifies that the block at height is within the block
// watermarks, reporting an *ethrpcerrors.BlockUnavailable otherwise.
func (m *WatermarkManager) EnsureBlockHeightAvailable(ctx context.Context, height int64) error {
	blockEarliest, _, latest, err := m.Watermarks(ctx)
	if err != nil {
		return err
	}
	return ensureWithinWatermarks(height, blockEarliest, latest, ethrpcerrors.HistoryPruned)
}

// EnsureReceiptHeightAvailable verifies that the receipts at height are still in the receipt
// store, which may keep fewer heights than the block or state stores, reporting an
// *ethrpcerrors.BlockUnavailable otherwise.
func (m *WatermarkManager) EnsureReceiptHeightAvailable(height int64) error {
	if m.receiptStore == nil {
		return receipt.ErrNotConfigured
	}
	earliest := m.receiptStore.EarliestVersion()
	if height < earliest {
		return ethrpcerrors.HistoryPruned(height, earliest)
	}
	return nil
}

// ensureWithinWatermarks reports a height outside [earliest, latest]. pruned names the store the
// height fell out of, since state and block history are pruned independently and go-ethereum
// renders the two differently.
func ensureWithinWatermarks(height, earliest, latest int64, pruned func(height, earliest int64) *ethrpcerrors.BlockUnavailable) error {
	if height > latest {
		return ethrpcerrors.BlockAboveLatest(height, latest)
	}
	if height < earliest {
		return pruned(height, earliest)
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

// blockByNumberOrNullForJSONRPC is blockByNumberRespectingWatermarks for the endpoints that
// return a block or something inside one (eth_getBlockByNumber, eth_getBlockReceipts,
// eth_getTransactionByBlockNumberAndIndex, ...). A block that does not exist from the caller's
// point of view is (nil, nil), which the endpoint answers with null; pruned history stays an
// error, as in go-ethereum.
func blockByNumberOrNullForJSONRPC(
	ctx context.Context,
	c client.LocalClient,
	wm *WatermarkManager,
	heightPtr *int64,
	maxRetries int,
) (*coretypes.ResultBlock, error) {
	block, err := blockByNumberRespectingWatermarks(ctx, c, wm, heightPtr, maxRetries)
	if ethrpcerrors.IsBlockMissing(err) {
		return nil, nil
	}
	return block, err
}

// blockByHashOrNullForJSONRPC is the by-hash counterpart of blockByNumberOrNullForJSONRPC.
func blockByHashOrNullForJSONRPC(
	ctx context.Context,
	c client.LocalClient,
	wm *WatermarkManager,
	hash []byte,
	maxRetries int,
) (*coretypes.ResultBlock, error) {
	block, err := blockByHashRespectingWatermarks(ctx, c, wm, hash, maxRetries)
	if ethrpcerrors.IsBlockMissing(err) {
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
