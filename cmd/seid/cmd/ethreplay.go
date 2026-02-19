package cmd

import (
	"net/http"
	//nolint:gosec
	_ "net/http/pprof"
	"os"
	"path/filepath"

	"github.com/sei-protocol/sei-chain/sei-wasmd/x/wasm"
	wasmkeeper "github.com/sei-protocol/sei-chain/sei-wasmd/x/wasm/keeper"
	"github.com/spf13/cast"
	"github.com/spf13/cobra"

	"github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/sei-cosmos/baseapp"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client/flags"
	"github.com/sei-protocol/sei-chain/sei-cosmos/server"
	"github.com/sei-protocol/sei-chain/sei-cosmos/store"
	storetypes "github.com/sei-protocol/sei-chain/sei-cosmos/store/types"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/log"
	dbm "github.com/tendermint/tm-db"
)

//nolint:gosec
func ReplayCmd(defaultNodeHome string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ethreplay",
		Short: "replay EVM transactions",
		Long:  "replay EVM transactions",
		RunE: func(cmd *cobra.Command, _ []string) error {

			serverCtx := server.GetServerContextFromCmd(cmd)
			if err := serverCtx.Viper.BindPFlags(cmd.Flags()); err != nil {
				return err
			}
			go func() {
				serverCtx.Logger.Info("Listening for profiling at http://localhost:6060/debug/pprof/")
				err := http.ListenAndServe(":6060", nil)
				if err != nil {
					serverCtx.Logger.Error("Error from profiling server", "error", err)
				}
			}()

			home := serverCtx.Viper.GetString(flags.FlagHome)
			db, err := openDB(home)
			if err != nil {
				return err
			}

			logger := log.NewTMLogger(log.NewSyncWriter(os.Stdout))
			cache := store.NewCommitKVStoreCacheManager()
			wasmGasRegisterConfig := wasmkeeper.DefaultGasRegisterConfig()
			wasmGasRegisterConfig.GasMultiplier = 21_000_000
			a := app.New(
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
						wasmkeeper.NewWasmGasRegister(
							wasmGasRegisterConfig,
						),
					),
				},
				app.EmptyAppOptions,
				baseapp.SetPruning(storetypes.PruneEverything),
				baseapp.SetMinGasPrices(cast.ToString(serverCtx.Viper.Get(server.FlagMinGasPrices))),
				baseapp.SetMinRetainBlocks(cast.ToUint64(serverCtx.Viper.Get(server.FlagMinRetainBlocks))),
				baseapp.SetInterBlockCache(cache),
			)
			app.Replay(a)
			return nil
		},
	}

	cmd.Flags().String(flags.FlagHome, defaultNodeHome, "The database home directory")
	cmd.Flags().String(flags.FlagChainID, "sei-chain", "chain ID")

	return cmd
}

func openDB(rootDir string) (dbm.DB, error) {
	dataDir := filepath.Join(rootDir, "data")
	return sdk.NewLevelDB("application", dataDir)
}
