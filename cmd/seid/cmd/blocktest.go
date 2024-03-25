package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/CosmWasm/wasmd/x/wasm"
	wasmkeeper "github.com/CosmWasm/wasmd/x/wasm/keeper"
	"github.com/spf13/cast"
	"github.com/spf13/cobra"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/server"
	"github.com/cosmos/cosmos-sdk/store"
	storetypes "github.com/cosmos/cosmos-sdk/store/types"
	aclkeeper "github.com/cosmos/cosmos-sdk/x/accesscontrol/keeper"
	ethtests "github.com/ethereum/go-ethereum/tests"
	"github.com/sei-protocol/sei-chain/app"
	"github.com/tendermint/tendermint/libs/log"

	//nolint:gosec,G108
	_ "net/http/pprof"
)

//nolint:gosec
func BlocktestCmd(defaultNodeHome string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "blocktest",
		Short: "run EF blocktest",
		Long:  "run EF blocktest",
		RunE: func(cmd *cobra.Command, _ []string) error {
			blockTestFileName, err := cmd.Flags().GetString("block-test")
			if err != nil {
				panic(fmt.Sprintf("Error with retrieving block test path: %v", err.Error()))
			}
			testName, err := cmd.Flags().GetString("test-name")
			if err != nil {
				panic(fmt.Sprintf("Error with retrieving test name: %v", err.Error()))
			}
			if blockTestFileName == "" || testName == "" {
				panic("block test file name or test name not set")
			}

			serverCtx := server.GetServerContextFromCmd(cmd)
			if err := serverCtx.Viper.BindPFlags(cmd.Flags()); err != nil {
				return err
			}
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
			bt := testIngester(blockTestFileName, testName)
			app.BlockTest(a, bt)
			return nil
		},
	}

	cmd.Flags().String(flags.FlagHome, defaultNodeHome, "The database home directory")
	cmd.Flags().String(flags.FlagChainID, "sei-chain", "chain ID")
	cmd.Flags().String("block-test", "", "path to a block test json file")
	cmd.Flags().String("test-name", "", "individual test name")

	return cmd
}

func testIngester(testFilePath string, testName string) *ethtests.BlockTest {
	file, err := os.Open(testFilePath)
	if err != nil {
		panic(err)
	}
	var tests map[string]ethtests.BlockTest
	decoder := json.NewDecoder(file)
	err = decoder.Decode(&tests)
	if err != nil {
		panic(err)
	}
	for name, bt := range tests {
		btP := &bt
		if name == testName {
			return btP
		}
	}
	panic(fmt.Sprintf("Unable to find test name %v at test file path %v", testName, testFilePath))
}
