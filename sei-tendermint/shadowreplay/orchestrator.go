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
	"encoding/json"
	"fmt"
	"io"
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
	// Must retain FinalizeBlockResponses (min-retain-blocks=0).
	SourceRPC string

	// StartHeight is the first block to replay.
	// Must equal snapshot height + 1; validated on startup.
	StartHeight int64

	// EndHeight is the last block height to replay (inclusive).
	// Set to 0 to run continuously, polling for new blocks.
	EndHeight int64

	// Output receives newline-delimited ComparisonRecord JSON.
	// Defaults to os.Stdout when nil.
	Output io.Writer
}

// ComparisonRecord is the per-block output written to Options.Output as NDJSON.
type ComparisonRecord struct {
	Height    int64  `json:"height"`
	Status    string `json:"status"` // "match" or "diverge"
	AppHash   string `json:"app_hash,omitempty"`
	TxCount   int    `json:"tx_count,omitempty"`
	ElapsedMs int64  `json:"elapsed_ms,omitempty"`

	// Divergence detail fields (only when Status=="diverge")
	Kind              string      `json:"kind,omitempty"`    // "AppHash" | "tx"
	TxIndex           int         `json:"tx_index,omitempty"`
	TxHash            string      `json:"tx_hash,omitempty"`
	Field             string      `json:"field,omitempty"`
	Want              interface{} `json:"want,omitempty"`
	Got               interface{} `json:"got,omitempty"`
	BlockAppHashMatch bool        `json:"block_app_hash_match,omitempty"`
}

// Run executes shadow replay from opts.StartHeight to opts.EndHeight (or forever).
//
// dbDir must be the chain home's data directory (e.g. $CHAIN_HOME/data).
// appConn must be a started local ABCI client wrapping the shadow Sei app
// (already loaded from the same dbDir via snapshot sync).
func Run(ctx context.Context, dbDir string, appConn abciclient.Client, logger log.Logger, opts Options) error {
	if opts.Output == nil {
		opts.Output = os.Stdout
	}

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
	if state.LastBlockHeight != opts.StartHeight-1 {
		return fmt.Errorf("height mismatch: snapshot state is at %d, but StartHeight is %d (expected snapshot height+1)",
			state.LastBlockHeight, opts.StartHeight)
	}
	initialHeight := state.InitialHeight

	// Connect to source archival node.
	src, err := httpclient.New(opts.SourceRPC)
	if err != nil {
		return fmt.Errorf("creating source RPC client for %q: %w", opts.SourceRPC, err)
	}

	enc := json.NewEncoder(opts.Output)

	for height := opts.StartHeight; ; height++ {
		if opts.EndHeight > 0 && height > opts.EndHeight {
			logger.Info("shadow-replay complete", "heights_replayed", height-opts.StartHeight)
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Fetch the block at this height from the source, polling until available.
		block, err := fetchBlockWithRetry(ctx, src, height, logger)
		if err != nil {
			return err
		}

		// Fetch canonical tx results.
		resultsResp, err := src.BlockResults(ctx, &height)
		if err != nil {
			return fmt.Errorf("fetching block_results at height %d: %w", height, err)
		}

		// Fetch next block to extract the expected AppHash.
		// (AppHash for block N is stored in block N+1's header.)
		nextHeight := height + 1
		nextBlock, err := fetchBlockWithRetry(ctx, src, nextHeight, logger)
		if err != nil {
			return err
		}
		expectedAppHash := nextBlock.AppHash

		start := time.Now()

		// Execute and commit the block on the shadow app.
		gotAppHash, txResults, err := sm.ExecCommitBlockFull(ctx, appConn, block, logger, stateStore, initialHeight)
		if err != nil {
			return fmt.Errorf("ExecCommitBlockFull at height %d: %w", height, err)
		}

		// Advance the state so buildLastCommitInfo finds correct validators next iteration.
		// Note: full validator set updates require ApplyBlock; this is sufficient for
		// mainnet replay where validator set changes are infrequent.
		state.AppHash = gotAppHash
		state.LastBlockHeight = height
		if err := stateStore.Save(state); err != nil {
			return fmt.Errorf("saving state at height %d: %w", height, err)
		}

		elapsedMs := time.Since(start).Milliseconds()

		rec := ComparisonRecord{
			Height:    height,
			Status:    "match",
			AppHash:   hex.EncodeToString(gotAppHash),
			TxCount:   len(txResults),
			ElapsedMs: elapsedMs,
		}

		// Block-level AppHash comparison.
		if string(gotAppHash) != string(expectedAppHash) {
			rec.Status = "diverge"
			rec.Kind = "AppHash"
			rec.Want = hex.EncodeToString(expectedAppHash)
			rec.Got = hex.EncodeToString(gotAppHash)
			logger.Error("AppHash divergence", "height", height, "want", rec.Want, "got", rec.Got)
			_ = enc.Encode(rec)
			continue
		}

		// Per-transaction comparison.
		diverged := false
		for i, got := range txResults {
			if i >= len(resultsResp.TxsResults) {
				break
			}
			want := resultsResp.TxsResults[i]
			txHash := hex.EncodeToString(block.Txs[i].Hash())

			var field string
			var wantVal, gotVal interface{}

			switch {
			case got.Code != want.Code:
				field, wantVal, gotVal = "Code", want.Code, got.Code
			case got.GasUsed != want.GasUsed:
				field, wantVal, gotVal = "GasUsed", want.GasUsed, got.GasUsed
			case string(got.Data) != string(want.Data):
				field, wantVal, gotVal = "Data", hex.EncodeToString(want.Data), hex.EncodeToString(got.Data)
			case got.Codespace != want.Codespace:
				field, wantVal, gotVal = "Codespace", want.Codespace, got.Codespace
			case len(got.Events) != len(want.Events):
				field = "Events"
				wantVal = fmt.Sprintf("count=%d", len(want.Events))
				gotVal = fmt.Sprintf("count=%d", len(got.Events))
			}

			if field != "" {
				txRec := ComparisonRecord{
					Height:            height,
					Status:            "diverge",
					Kind:              "tx",
					TxIndex:           i,
					TxHash:            txHash,
					Field:             field,
					Want:              wantVal,
					Got:               gotVal,
					BlockAppHashMatch: true,
				}
				logger.Error("tx divergence", "height", height, "tx_index", i, "field", field)
				_ = enc.Encode(txRec)
				diverged = true
			}
		}

		if !diverged {
			_ = enc.Encode(rec)
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
