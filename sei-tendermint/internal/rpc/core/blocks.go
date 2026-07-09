package core

import (
	"context"
	"fmt"
	"sort"

	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	tmquery "github.com/sei-protocol/sei-chain/sei-tendermint/internal/pubsub/query"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/state/indexer"
	tmmath "github.com/sei-protocol/sei-chain/sei-tendermint/libs/math"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

// BlockchainInfo gets block headers for minHeight <= height <= maxHeight.
//
// If maxHeight does not yet exist, blocks up to the current height will be
// returned. If minHeight does not exist (due to pruning), earliest existing
// height will be used.
//
// At most 20 items will be returned. Block headers are returned in descending
// order (highest first).
//
// More: https://docs.tendermint.com/master/rpc/#/Info/blockchain
func (env *Environment) BlockchainInfo(ctx context.Context, req *coretypes.RequestBlockchainInfo) (*coretypes.ResultBlockchainInfo, error) {
	const limit = 20
	minHeight, maxHeight, err := filterMinMax(
		env.BlockStore.Base(),
		env.BlockStore.Height(),
		int64(req.MinHeight),
		int64(req.MaxHeight),
		limit,
	)
	if err != nil {
		return nil, err
	}
	logger.Debug("BlockchainInfo", "maxHeight", maxHeight, "minHeight", minHeight)

	blockMetas := make([]*types.BlockMeta, 0, maxHeight-minHeight+1)
	for height := maxHeight; height >= minHeight; height-- {
		blockMeta := env.BlockStore.LoadBlockMeta(height)
		if blockMeta != nil {
			blockMetas = append(blockMetas, blockMeta)
		}
	}

	return &coretypes.ResultBlockchainInfo{
		LastHeight: env.BlockStore.Height(),
		BlockMetas: blockMetas,
	}, nil
}

// error if either min or max are negative or min > max
// if 0, use blockstore base for min, latest block height for max
// enforce limit.
func filterMinMax(base, height, min, max, limit int64) (int64, int64, error) {
	// filter negatives
	if min < 0 || max < 0 {
		return min, max, coretypes.ErrZeroOrNegativeHeight
	}

	// adjust for default values
	if min == 0 {
		min = 1
	}
	if max == 0 {
		max = height
	}

	// limit max to the height
	max = tmmath.MinInt64(height, max)

	// limit min to the base
	min = tmmath.MaxInt64(base, min)

	// limit min to within `limit` of max
	// so the total number of blocks returned will be `limit`
	min = tmmath.MaxInt64(min, max-limit+1)

	if min > max {
		return min, max, fmt.Errorf("%w: min height %d can't be greater than max height %d",
			coretypes.ErrInvalidRequest, min, max)
	}
	return min, max, nil
}

// Block gets block at a given height.
// If no height is provided, it will fetch the latest block.
// More: https://docs.tendermint.com/master/rpc/#/Info/block
//
// Under Autobahn the CometBFT BlockStore is not populated; route through
// GigaRouter, which reads the finalized global block from data.State and
// returns it in the same coretypes.ResultBlock shape. This keeps every
// downstream consumer (evmrpc, /block HTTP, seid q block) working without
// individually branching on consensus mode.
func (env *Environment) Block(ctx context.Context, req *coretypes.RequestBlockInfo) (*coretypes.ResultBlock, error) {
	if giga, ok := env.gigaRouter().Get(); ok {
		height, err := env.autobahnCheckAndGetHeight(ctx, (*int64)(req.Height))
		if err != nil {
			return nil, err
		}
		// Cast happens at the boundary so giga.BlockByNumber stays
		// strongly typed on atypes.GlobalBlockNumber. autobahnCheckAndGetHeight
		// has already validated height is positive and within chain head.
		gbn, ok := utils.SafeCast[atypes.GlobalBlockNumber](height)
		if !ok {
			return nil, fmt.Errorf("invalid height %d", height)
		}
		return giga.BlockByNumber(ctx, gbn)
	}

	height, err := env.getHeight(env.BlockStore.Height(), (*int64)(req.Height))
	if err != nil {
		return nil, err
	}

	blockMeta := env.BlockStore.LoadBlockMeta(height)
	if blockMeta == nil {
		return &coretypes.ResultBlock{BlockID: types.BlockID{}, Block: nil}, nil
	}

	block := env.BlockStore.LoadBlock(height)
	return &coretypes.ResultBlock{BlockID: blockMeta.BlockID, Block: block}, nil
}

// autobahnCheckAndGetHeight resolves a caller-supplied height pointer to a
// concrete int64 and validates it against the current ABCI head. nil (or
// zero) means "latest". Returns the same error sentinels as env.getHeight
// (ErrZeroOrNegativeHeight, ErrHeightExceedsChainHead, ErrHeightNotAvailable)
// so downstream consumers like evmrpc — which translate
// ErrHeightExceedsChainHead-class errors into the Ethereum-spec `null`
// response for non-existent blocks — see consistent shapes under both
// consensus engines.
//
// TODO(autobahn): wire a real lower bound and pass it as `base` to
// env.getHeight. We currently pass env.BlockStore.Base() (always 0 under
// Autobahn), which means any positive height < chain head passes validation
// here and is rejected one layer down (data.GlobalBlock returns
// data.ErrPruned, which BlockByNumber maps to ErrHeightNotAvailable). With
// a real lower bound the rejection happens at this layer instead. The
// natural source becomes available once sei-db/ledger_db/block.BlockDB is
// wired into block execution: switch this and BlockByNumber to read from
// BlockDB, and source `base` from BlockDB.GetLowestBlockHeight.
func (env *Environment) autobahnCheckAndGetHeight(ctx context.Context, heightPtr *int64) (int64, error) {
	info, err := env.ABCIInfo(ctx)
	if err != nil {
		return 0, err
	}
	return env.getHeight(info.Response.LastBlockHeight, heightPtr)
}

// BlockByHash gets block by hash.
// More: https://docs.tendermint.com/master/rpc/#/Info/block_by_hash
//
// Under Autobahn the CometBFT BlockStore is not populated, so we route
// through the GigaRouter's temporary in-memory hash index. Match CometBFT
// semantics: an unknown hash returns &ResultBlock{Block: nil} with no
// error, never an error response — external tools (block explorers,
// monitoring) treat that as "no such block" rather than a failure.
//
// The Tendermint RPC boundary (req.Hash is bytes.HexBytes — a []byte alias
// for wire-format flexibility) gets converted to the strongly-typed
// atypes.BlockHeaderHash here, before reaching GigaRouter.BlockByHash.
// Wrong-size inputs short-circuit to the same zero-result CometBFT
// returns for an unknown hash.
func (env *Environment) BlockByHash(ctx context.Context, req *coretypes.RequestBlockByHash) (*coretypes.ResultBlock, error) {
	if giga, ok := env.gigaRouter().Get(); ok {
		if len(req.Hash) != len(atypes.BlockHeaderHash{}) {
			return &coretypes.ResultBlock{}, nil
		}
		return giga.BlockByHash(ctx, atypes.BlockHeaderHash(req.Hash))
	}
	block := env.BlockStore.LoadBlockByHash(req.Hash)
	if block == nil {
		return &coretypes.ResultBlock{BlockID: types.BlockID{}, Block: nil}, nil
	}
	// If block is not nil, then blockMeta can't be nil.
	blockMeta := env.BlockStore.LoadBlockMeta(block.Height)
	return &coretypes.ResultBlock{BlockID: blockMeta.BlockID, Block: block}, nil
}

// Header gets block header at a given height.
// If no height is provided, it will fetch the latest header.
// More: https://docs.tendermint.com/master/rpc/#/Info/header
func (env *Environment) Header(ctx context.Context, req *coretypes.RequestBlockInfo) (*coretypes.ResultHeader, error) {
	height, err := env.getHeight(env.BlockStore.Height(), (*int64)(req.Height))
	if err != nil {
		return nil, err
	}

	blockMeta := env.BlockStore.LoadBlockMeta(height)
	if blockMeta == nil {
		return &coretypes.ResultHeader{}, nil
	}

	return &coretypes.ResultHeader{Header: &blockMeta.Header}, nil
}

// HeaderByHash gets header by hash.
// More: https://docs.tendermint.com/master/rpc/#/Info/header_by_hash
func (env *Environment) HeaderByHash(ctx context.Context, req *coretypes.RequestBlockByHash) (*coretypes.ResultHeader, error) {
	blockMeta := env.BlockStore.LoadBlockMetaByHash(req.Hash)
	if blockMeta == nil {
		return &coretypes.ResultHeader{}, nil
	}

	return &coretypes.ResultHeader{Header: &blockMeta.Header}, nil
}

// Commit gets block commit at a given height.
// If no height is provided, it will fetch the commit for the latest block.
// More: https://docs.tendermint.com/master/rpc/#/Info/commit
func (env *Environment) Commit(ctx context.Context, req *coretypes.RequestBlockInfo) (*coretypes.ResultCommit, error) {
	height, err := env.getHeight(env.BlockStore.Height(), (*int64)(req.Height))
	if err != nil {
		return nil, err
	}

	blockMeta := env.BlockStore.LoadBlockMeta(height)
	if blockMeta == nil {
		return nil, nil
	}
	header := blockMeta.Header

	// If the next block has not been committed yet,
	// use a non-canonical commit
	if height == env.BlockStore.Height() {
		commit := env.BlockStore.LoadSeenCommit()
		// NOTE: we can't yet ensure atomicity of operations in asserting
		// whether this is the latest height and retrieving the seen commit
		if commit != nil && commit.Height == height {
			return coretypes.NewResultCommit(&header, commit, false), nil
		}
	}

	// Return the canonical commit (comes from the block at height+1)
	commit := env.BlockStore.LoadBlockCommit(height)
	if commit == nil {
		return nil, nil
	}
	return coretypes.NewResultCommit(&header, commit, true), nil
}

// BlockResults gets ABCIResults at a given height.
// If no height is provided, it will fetch results for the latest block.
//
// Results are for the height of the block containing the txs.
// More: https://docs.tendermint.com/master/rpc/#/Info/block_results
//
// Under Autobahn, FinalizeBlock responses are not persisted to StateStore
// (giga_router.executeBlock never calls SaveFinalizeBlockResponses), so this
// returns a valid-but-empty ResultBlockResults at the requested height. That
// lets downstream consumers (evmrpc's eth_getBlockByNumber, which looks up
// BlockResults to enrich the response) keep working — the block envelope
// renders correctly, just without per-tx ExecTxResult details. Properly
// populating these under Autobahn is a separate follow-up.
func (env *Environment) BlockResults(ctx context.Context, req *coretypes.RequestBlockInfo) (*coretypes.ResultBlockResults, error) {
	if giga, ok := env.gigaRouter().Get(); ok {
		height, err := env.autobahnCheckAndGetHeight(ctx, (*int64)(req.Height))
		if err != nil {
			return nil, err
		}
		// evmrpc's EncodeTmBlock reads ConsensusParamUpdates.Block.MaxGas to
		// populate the eth_getBlockByNumber gasLimit field. Populate it from
		// Autobahn's producer config so that path doesn't nil-deref.
		return &coretypes.ResultBlockResults{
			Height: height,
			ConsensusParamUpdates: &tmproto.ConsensusParams{
				Block: &tmproto.BlockParams{
					MaxGas: utils.Clamp[int64](giga.MaxGasEstimatedPerBlock()),
				},
			},
		}, nil
	}

	height, err := env.getHeight(env.BlockStore.Height(), (*int64)(req.Height))
	if err != nil {
		return nil, err
	}

	results, err := env.StateStore.LoadFinalizeBlockResponses(height)
	if err != nil {
		return nil, err
	}

	var totalGasUsed int64
	for _, res := range results.GetTxResults() {
		totalGasUsed += res.GetGasUsed()
	}

	return &coretypes.ResultBlockResults{
		Height:                height,
		TxsResults:            results.TxResults,
		TotalGasUsed:          totalGasUsed,
		FinalizeBlockEvents:   results.Events,
		ValidatorUpdates:      results.ValidatorUpdates,
		ConsensusParamUpdates: results.ConsensusParamUpdates,
	}, nil
}

const AscendingOrder = "asc"
const DescendingOrder = "desc"

// BlockSearch searches for a paginated set of blocks matching the provided query.
func (env *Environment) BlockSearch(ctx context.Context, req *coretypes.RequestBlockSearch) (*coretypes.ResultBlockSearch, error) {
	if !indexer.KVSinkEnabled(env.EventSinks) {
		return nil, fmt.Errorf("block searching is disabled due to no kvEventSink")
	}

	q, err := tmquery.New(req.Query)
	if err != nil {
		return nil, err
	}

	var kvsink indexer.EventSink
	for _, sink := range env.EventSinks {
		if sink.Type() == indexer.KV {
			kvsink = sink
		}
	}

	// Validate order_by up front so we can push the ordering (and the result
	// cap) down into the indexer; a broad query is then bounded at the scan
	// path rather than after materializing and sorting the full match set.
	var orderDesc bool
	switch req.OrderBy {
	case DescendingOrder, "":
		orderDesc = true

	case AscendingOrder:
		orderDesc = false

	default:
		return nil, fmt.Errorf("expected order_by to be either `asc` or `desc` or empty: %w", coretypes.ErrInvalidRequest)
	}

	results, err := kvsink.SearchBlockEvents(ctx, q, indexer.SearchOptions{
		Limit:     env.Config.MaxTxSearchResults,
		OrderDesc: orderDesc,
		MaxScan:   env.Config.MaxEventSearchScan,
	})
	if err != nil {
		return nil, err
	}

	// sort results (must be done before cap and pagination)
	if orderDesc {
		sort.Slice(results, func(i, j int) bool { return results[i] > results[j] })
	} else {
		sort.Slice(results, func(i, j int) bool { return results[i] < results[j] })
	}

	// Safety net: the kv indexer already bounds to MaxTxSearchResults, but keep
	// the cap so the response stays bounded for any sink that ignores the limit.
	if maxResults := env.Config.MaxTxSearchResults; maxResults > 0 && len(results) > maxResults {
		results = results[:maxResults]
	}

	// paginate results
	totalCount := len(results)
	perPage := env.validatePerPage(req.PerPage.IntPtr())

	page, err := validatePage(req.Page.IntPtr(), perPage, totalCount)
	if err != nil {
		return nil, err
	}

	skipCount := validateSkipCount(page, perPage)
	pageSize := tmmath.MinInt(perPage, totalCount-skipCount)

	apiResults := make([]*coretypes.ResultBlock, 0, pageSize)
	for i := skipCount; i < skipCount+pageSize; i++ {
		block := env.BlockStore.LoadBlock(results[i])
		if block != nil {
			blockMeta := env.BlockStore.LoadBlockMeta(block.Height)
			if blockMeta != nil {
				apiResults = append(apiResults, &coretypes.ResultBlock{
					Block:   block,
					BlockID: blockMeta.BlockID,
				})
			}
		}
	}

	return &coretypes.ResultBlockSearch{Blocks: apiResults, TotalCount: totalCount}, nil
}
