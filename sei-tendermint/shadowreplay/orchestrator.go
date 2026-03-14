// Package shadowreplay replays finalized main-chain blocks through a shadow
// Sei app (with the Giga executor enabled) and compares outcomes against the
// canonical results stored by the source archival node.
//
// Because this package lives under sei-tendermint/ it is permitted to import
// the sei-tendermint/internal packages that contain the state store primitives.
package shadowreplay

import (
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"time"

	abciclient "github.com/sei-protocol/sei-chain/sei-tendermint/abci/client"
	sm "github.com/sei-protocol/sei-chain/sei-tendermint/internal/state"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/log"
	httpclient "github.com/sei-protocol/sei-chain/sei-tendermint/rpc/client/http"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/types"
	dbm "github.com/tendermint/tm-db"
)

// Options configures a shadow replay run.
type Options struct {
	// SourceRPC is the HTTP RPC endpoint of the archival source node.
	SourceRPC string

	// StartHeight is the first block to replay.
	// Ignored when resuming from a checkpoint.
	StartHeight int64

	// EndHeight is the last block height to replay (inclusive).
	// Set to 0 to run continuously, polling for new blocks.
	EndHeight int64

	// CheckpointPath, when set, enables crash-recovery checkpointing.
	CheckpointPath string

	// OutputDir, when set, enables NDJSON file output with rotation.
	// Divergence files are written to OutputDir/divergences/.
	OutputDir string

	// MetricsAddr is the address for the Prometheus metrics HTTP server
	// (e.g. ":9090"). Empty disables metrics.
	MetricsAddr string

	// ChainID is used as a Prometheus label. Defaults to "unknown".
	ChainID string
}

// Run executes shadow replay from opts.StartHeight to opts.EndHeight (or forever).
func Run(ctx context.Context, dbDir string, appConn abciclient.Client, logger log.Logger, opts Options) error {
	if opts.ChainID == "" {
		opts.ChainID = "unknown"
	}

	// Set up metrics.
	var metrics *Metrics
	if opts.MetricsAddr != "" {
		metrics = NewMetrics(opts.ChainID)
		if err := metrics.Serve(opts.MetricsAddr); err != nil {
			return fmt.Errorf("starting metrics server: %w", err)
		}
		defer metrics.Stop()
		logger.Info("metrics server started", "addr", opts.MetricsAddr)
	} else {
		metrics = NoopMetrics()
	}

	// Set up output.
	output, err := NewOutputWriter(opts.OutputDir, os.Stdout)
	if err != nil {
		return fmt.Errorf("creating output writer: %w", err)
	}
	defer output.Close()

	// Open the state DB from the snapshot-seeded chain home.
	stateDB, err := dbm.NewDB("state", dbm.GoLevelDBBackend, dbDir)
	if err != nil {
		return fmt.Errorf("opening state DB at %q: %w", dbDir, err)
	}
	defer stateDB.Close() //nolint:errcheck

	stateStore := sm.NewStore(stateDB)

	state, err := stateStore.Load()
	if err != nil {
		return fmt.Errorf("loading state: %w", err)
	}
	initialHeight := state.InitialHeight

	// Determine start height: checkpoint takes priority.
	startHeight := opts.StartHeight
	epoch := NewEpochState()
	startedAt := time.Now()
	var blocksReplayed int64
	var totalDivergences int64

	if opts.CheckpointPath != "" {
		cp, err := LoadCheckpoint(opts.CheckpointPath)
		if err != nil {
			return fmt.Errorf("loading checkpoint: %w", err)
		}
		if cp != nil {
			startHeight = cp.LastHeight + 1
			blocksReplayed = cp.BlocksReplayed
			totalDivergences = cp.Divergences
			startedAt = cp.StartedAt
			if cp.Epoch == EpochDiverged {
				epoch.Current = EpochDiverged
				epoch.OriginHeight = cp.EpochOrigin
			}
			logger.Info("resuming from checkpoint",
				"height", startHeight,
				"blocks_replayed", blocksReplayed,
				"epoch", epoch.Current,
			)
		}
	}

	if state.LastBlockHeight != startHeight-1 {
		return fmt.Errorf("height mismatch: state is at %d, but start height is %d (expected state = start-1)",
			state.LastBlockHeight, startHeight)
	}

	// Connect to source archival node.
	src, err := httpclient.New(opts.SourceRPC)
	if err != nil {
		return fmt.Errorf("creating source RPC client for %q: %w", opts.SourceRPC, err)
	}

	logger.Info("shadow replay starting",
		"start_height", startHeight,
		"end_height", opts.EndHeight,
		"source_rpc", opts.SourceRPC,
		"output_dir", opts.OutputDir,
		"checkpoint", opts.CheckpointPath,
	)

	bpsStart := time.Now()
	bpsCount := int64(0)

	for height := startHeight; ; height++ {
		if opts.EndHeight > 0 && height > opts.EndHeight {
			logger.Info("shadow-replay complete", "heights_replayed", blocksReplayed)
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		block, err := fetchBlockWithRetry(ctx, src, height, logger)
		if err != nil {
			return err
		}

		resultsResp, err := src.BlockResults(ctx, &height)
		if err != nil {
			return fmt.Errorf("fetching block_results at height %d: %w", height, err)
		}

		nextHeight := height + 1
		nextBlock, err := fetchBlockWithRetry(ctx, src, nextHeight, logger)
		if err != nil {
			return err
		}
		expectedAppHash := nextBlock.AppHash

		start := time.Now()

		gotAppHash, txResults, err := sm.ExecCommitBlockFull(ctx, appConn, block, logger, stateStore, initialHeight)
		if err != nil {
			return fmt.Errorf("ExecCommitBlockFull at height %d: %w", height, err)
		}

		state.AppHash = gotAppHash
		state.LastBlockHeight = height
		if err := stateStore.Save(state); err != nil {
			return fmt.Errorf("saving state at height %d: %w", height, err)
		}

		elapsedMs := time.Since(start).Milliseconds()
		appHashMatch := string(gotAppHash) == string(expectedAppHash)

		// Epoch tracking.
		isNewOrigin := epoch.Transition(appHashMatch, height)

		// Build tx hashes for comparison.
		txHashes := make([]string, len(block.Txs))
		for i, tx := range block.Txs {
			txHashes[i] = hex.EncodeToString(tx.Hash())
		}

		// Compare.
		var divs []Divergence

		if !appHashMatch && isNewOrigin {
			divs = append(divs, Divergence{
				Scope:     ScopeBlock,
				Severity:  SeverityCritical,
				Field:     "app_hash",
				Canonical: hex.EncodeToString(expectedAppHash),
				Replay:    hex.EncodeToString(gotAppHash),
			})
			logger.Error("app hash divergence (new origin)", "height", height)
		}

		// Tx-level comparison runs in both clean and diverged epochs to
		// detect independent divergences even when app hash is cascading.
		txDivs := CompareTxResults(resultsResp.TxsResults, txResults, txHashes)
		divs = append(divs, txDivs...)

		// Compute total gas.
		var gasTotal int64
		for _, tx := range txResults {
			gasTotal += tx.GasUsed
		}

		comp := &BlockComparison{
			Height:       height,
			Timestamp:    time.Now().UTC().Format(time.RFC3339),
			AppHashMatch: appHashMatch,
			CanonicalApp: hex.EncodeToString(expectedAppHash),
			ReplayApp:    hex.EncodeToString(gotAppHash),
			TxCount:      len(txResults),
			GasUsedTotal: gasTotal,
			ElapsedMs:    elapsedMs,
			Epoch:        epoch.Current,
			Divergences:  divs,
		}

		if epoch.Current == EpochDiverged {
			comp.DivergenceOrigin = epoch.OriginHeight
			comp.BlocksSinceDivergent = epoch.BlocksSince(height)
		}

		blocksReplayed++
		totalDivergences += int64(len(divs))

		// Metrics.
		metrics.RecordBlock(comp)

		bpsCount++
		if elapsed := time.Since(bpsStart).Seconds(); elapsed >= 10 {
			metrics.BlocksPerSecond.Set(float64(bpsCount) / elapsed)
			bpsStart = time.Now()
			bpsCount = 0
		}

		// Output.
		if err := output.WriteBlock(comp); err != nil {
			return fmt.Errorf("writing output at height %d: %w", height, err)
		}

		// Checkpoint every 100 blocks.
		if opts.CheckpointPath != "" && blocksReplayed%100 == 0 {
			cp := &Checkpoint{
				LastHeight:     height,
				LastAppHash:    hex.EncodeToString(gotAppHash),
				StartedAt:      startedAt,
				BlocksReplayed: blocksReplayed,
				Divergences:    totalDivergences,
				Epoch:          epoch.Current,
				EpochOrigin:    epoch.OriginHeight,
			}
			if err := SaveCheckpoint(opts.CheckpointPath, cp); err != nil {
				logger.Error("failed to save checkpoint", "err", err)
			}
		}

		if len(divs) > 0 {
			logger.Info("block divergences",
				"height", height,
				"count", len(divs),
				"max_severity", MaxSeverity(divs),
				"epoch", epoch.Current,
			)
		}
	}
}

// fetchBlockWithRetry fetches the block at height from src, retrying every
// second until the block is available or ctx is cancelled.
func fetchBlockWithRetry(ctx context.Context, src *httpclient.HTTP, height int64, logger log.Logger) (*tmtypes.Block, error) {
	for {
		resp, err := src.Block(ctx, &height)
		if err == nil && resp != nil && resp.Block != nil {
			return resp.Block, nil
		}
		logger.Info("waiting for block from source", "height", height)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(1 * time.Second):
		}
	}
}
