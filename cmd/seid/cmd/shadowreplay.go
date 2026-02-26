package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/server"
	"github.com/cosmos/cosmos-sdk/store"
	storetypes "github.com/cosmos/cosmos-sdk/store/types"
	"github.com/sei-protocol/sei-chain/app"
	abciclient "github.com/sei-protocol/sei-chain/sei-tendermint/abci/client"
	tmlog "github.com/sei-protocol/sei-chain/sei-tendermint/libs/log"
	"github.com/sei-protocol/sei-chain/sei-tendermint/shadowreplay"
	gigaconfig "github.com/sei-protocol/sei-chain/giga/executor/config"
	"github.com/sei-protocol/sei-chain/sei-wasmd/x/wasm"
	wasmkeeper "github.com/sei-protocol/sei-chain/sei-wasmd/x/wasm/keeper"
	"github.com/spf13/cast"
	"github.com/spf13/cobra"
)

const (
	flagSourceRPC   = "source-rpc"
	flagStartHeight = "start-height"
	flagEndHeight   = "end-height"
)

// ShadowReplayCmd returns the cobra command for shadow replay.
func ShadowReplayCmd(defaultNodeHome string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "shadow-replay",
		Short: "Replay finalized blocks through the Giga executor and compare outcomes to the source chain",
		Long: `shadow-replay fetches finalized blocks from an archival source node (min-retain-blocks=0)
and re-executes them against a locally seeded chain state using the Giga execution engine.

For each block it compares AppHash and per-transaction results (Code, GasUsed, Data,
Codespace, Events) against the canonical results from the source node and emits a
newline-delimited JSON stream of ComparisonRecords to stdout.

The --home directory must contain chain state seeded via snapshot sync.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			serverCtx := server.GetServerContextFromCmd(cmd)
			if err := serverCtx.Viper.BindPFlags(cmd.Flags()); err != nil {
				return err
			}

			home := serverCtx.Viper.GetString(flags.FlagHome)
			sourceRPC := serverCtx.Viper.GetString(flagSourceRPC)
			startHeight := serverCtx.Viper.GetInt64(flagStartHeight)
			endHeight := serverCtx.Viper.GetInt64(flagEndHeight)

			if sourceRPC == "" {
				return fmt.Errorf("--%s is required", flagSourceRPC)
			}
			if startHeight <= 0 {
				return fmt.Errorf("--%s must be > 0", flagStartHeight)
			}

			// Force-enable Giga — shadow replay always exercises the Giga executor.
			serverCtx.Viper.Set(gigaconfig.FlagEnabled, true)
			serverCtx.Viper.Set(gigaconfig.FlagOCCEnabled, true)

			logger := tmlog.NewTMLogger(tmlog.NewSyncWriter(os.Stderr))

			db, err := openDB(home)
			if err != nil {
				return fmt.Errorf("opening app DB: %w", err)
			}

			cache := store.NewCommitKVStoreCacheManager()
			wasmGasRegisterConfig := wasmkeeper.DefaultGasRegisterConfig()
			wasmGasRegisterConfig.GasMultiplier = 21_000_000

			seiApp := app.New(
				logger,
				db,
				nil,
				true,
				map[int64]bool{},
				home,
				0,
				true,
				nil,
				app.MakeEncodingConfig(),
				wasm.EnableAllProposals,
				serverCtx.Viper,
				[]wasm.Option{
					wasmkeeper.WithGasRegister(
						wasmkeeper.NewWasmGasRegister(wasmGasRegisterConfig),
					),
				},
				app.EmptyAppOptions,
				baseapp.SetPruning(storetypes.PruneNothing),
				baseapp.SetInterBlockCache(cache),
				baseapp.SetMinRetainBlocks(cast.ToUint64(serverCtx.Viper.Get(server.FlagMinRetainBlocks))),
			)

			appConn := abciclient.NewLocalClient(logger, seiApp)

			opts := shadowreplay.Options{
				SourceRPC:   sourceRPC,
				StartHeight: startHeight,
				EndHeight:   endHeight,
			}

			dataDir := filepath.Join(home, "data")
			return shadowreplay.Run(context.Background(), dataDir, appConn, logger, opts)
		},
	}

	cmd.Flags().String(flags.FlagHome, defaultNodeHome, "Chain home directory (must contain snapshot-seeded state)")
	cmd.Flags().String(flagSourceRPC, "", "Archival source node RPC endpoint, e.g. http://pacific-replay-0:26657 (required)")
	cmd.Flags().Int64(flagStartHeight, 0, "First block height to replay; must equal snapshot height + 1 (required)")
	cmd.Flags().Int64(flagEndHeight, 0, "Last block height to replay (0 = run continuously to tip)")

	return cmd
}
