package cmd

import (
	"encoding/json"
	"fmt"
	_ "net/http/pprof" //nolint:gosec
	"os"
	"path/filepath"
	"strings"

	"github.com/sei-protocol/sei-chain/sei-wasmd/x/wasm"
	wasmkeeper "github.com/sei-protocol/sei-chain/sei-wasmd/x/wasm/keeper"
	"github.com/spf13/cast"
	"github.com/spf13/cobra"

	ethtests "github.com/ethereum/go-ethereum/tests"
	"github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/sei-cosmos/baseapp"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client/flags"
	"github.com/sei-protocol/sei-chain/sei-cosmos/server"
	"github.com/sei-protocol/sei-chain/sei-cosmos/store"
	storetypes "github.com/sei-protocol/sei-chain/sei-cosmos/store/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/log"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
)

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

			logger := log.NewTMLogger(os.Stdout)
			cache := store.NewCommitKVStoreCacheManager()
			wasmGasRegisterConfig := wasmkeeper.DefaultGasRegisterConfig()
			wasmGasRegisterConfig.GasMultiplier = 21_000_000
			// turn on Cancun for block test
			evmtypes.CancunTime = 0
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
	file, err := os.Open(filepath.Clean(testFilePath))
	if err != nil {
		panic(err)
	}
	var tests map[string]ethtests.BlockTest
	decoder := json.NewDecoder(file)
	err = decoder.Decode(&tests)
	if err != nil {
		panic(err)
	}

	fullTestname := fmt.Sprintf(
		"%s::%s",
		strings.TrimPrefix(testFilePath, "./ethtests/"), testName,
	)
	res, ok := tests[fullTestname]
	if !ok {
		panic(fmt.Sprintf("Unable to find test name %v at test file path %v", fullTestname, testFilePath))
	}

	return &res
}
