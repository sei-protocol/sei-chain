package cmd

import (
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
	"github.com/sei-protocol/sei-chain/app"
	"github.com/tendermint/tendermint/libs/log"
)

//nolint:gosec
func ScriptCmd(defaultNodeHome string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "script",
		Short: "run adhoc read-only script",
		Long:  "run adhoc read-only script",
		RunE: func(cmd *cobra.Command, _ []string) error {
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
				app.EmptyAppOptions,
				baseapp.SetPruning(storetypes.PruneEverything),
				baseapp.SetMinGasPrices(cast.ToString(serverCtx.Viper.Get(server.FlagMinGasPrices))),
				baseapp.SetMinRetainBlocks(cast.ToUint64(serverCtx.Viper.Get(server.FlagMinRetainBlocks))),
				baseapp.SetInterBlockCache(cache),
			)
			ctx, err := a.CreateQueryContext(a.LastBlockHeight(), false)
			if err != nil {
				panic(err)
			}
			// WRITE YOUR READ-ONLY SCRIPT BELOW
			_ = ctx
			// ctx = ctx.WithBlockTime(time.Now())
			// f, err := os.Create("/home/ubuntu/all_cw721_addresses.txt")
			// if err != nil {
			// 	panic(err)
			// }
			// defer f.Close()
			// a.WasmKeeper.IterateContractInfo(ctx, func(contractAddress sdk.AccAddress, _ wasmtypes.ContractInfo) bool {
			// 	_, err := a.WasmKeeper.QuerySmart(ctx, contractAddress, []byte("{\"num_tokens\":{}}"))
			// 	if err == nil {
			// 		f.WriteString(fmt.Sprintf("%s\n", contractAddress.String()))
			// 	}
			// 	return false
			// })
			// f.Sync()
			return nil
		},
	}

	cmd.Flags().String(flags.FlagHome, defaultNodeHome, "The database home directory")
	cmd.Flags().String(flags.FlagChainID, "sei-chain", "chain ID")

	return cmd
}
