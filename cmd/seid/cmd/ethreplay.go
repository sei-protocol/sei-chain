package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/CosmWasm/wasmd/x/wasm"
	wasmkeeper "github.com/CosmWasm/wasmd/x/wasm/keeper"
	"github.com/spf13/cast"
	"github.com/spf13/cobra"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/server"
	"github.com/cosmos/cosmos-sdk/store"
	storetypes "github.com/cosmos/cosmos-sdk/store/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	aclkeeper "github.com/cosmos/cosmos-sdk/x/accesscontrol/keeper"
	"github.com/sei-protocol/sei-chain/app"
	"github.com/tendermint/tendermint/libs/log"
	dbm "github.com/tendermint/tm-db"

	"net/http"
	//nolint:gosec,G108
	_ "net/http/pprof"
)

//nolint:gosec
func ReplayCmd(defaultNodeHome string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ethreplay",
		Short: "replay EVM transactions",
		Long:  "replay EVM transactions",
		RunE: func(cmd *cobra.Command, _ []string) error {
			blockTestFileName, _ := cmd.Flags().GetString("block-test")
			fmt.Println("blockTestFileName: ", blockTestFileName)

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
				[]aclkeeper.Option{},
				baseapp.SetPruning(storetypes.PruneEverything),
				baseapp.SetMinGasPrices(cast.ToString(serverCtx.Viper.Get(server.FlagMinGasPrices))),
				baseapp.SetMinRetainBlocks(cast.ToUint64(serverCtx.Viper.Get(server.FlagMinRetainBlocks))),
				baseapp.SetInterBlockCache(cache),
			)
			if blockTestFileName != "" {
				// tm := new(TestMatcher)

				// // need to injest the test case using TestMatcher

				// runTest := func(name string, bt *ethtest.BlockTest) {
				// 	fmt.Println("In runTest, bt = ", bt)
				// 	if runtime.GOARCH == "386" && runtime.GOOS == "windows" && rand.Int63()%2 == 0 {
				// 		return
				// 	}
				// 	// app.ReplayBlockTest(a, bt)
				// }
				// tm.walk2(blockTestDir, runTest)

				// bt := testInjester(blockTestFileName)
				bt := emptyBlockTest
				app.ReplayBlockTest(a, bt)
				return nil
			}
			app.Replay(a)
			return nil

		},
	}

	cmd.Flags().String(flags.FlagHome, defaultNodeHome, "The database home directory")
	cmd.Flags().String(flags.FlagChainID, "sei-chain", "chain ID")
	cmd.Flags().String("block-test", "", "path to a block test json file")

	return cmd
}

func openDB(rootDir string) (dbm.DB, error) {
	dataDir := filepath.Join(rootDir, "data")
	return sdk.NewLevelDB("application", dataDir)
}
