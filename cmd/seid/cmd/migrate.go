package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"net/http"
	_ "net/http/pprof"

	"github.com/cosmos/cosmos-sdk/client"
	clientconfig "github.com/cosmos/cosmos-sdk/client/config"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/server"
	"github.com/cosmos/cosmos-sdk/server/config"
	"github.com/cosmos/cosmos-sdk/server/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authkeeper "github.com/cosmos/cosmos-sdk/x/auth/keeper"
	distributionkeeper "github.com/cosmos/cosmos-sdk/x/distribution/keeper"
	stakingkeeper "github.com/cosmos/cosmos-sdk/x/staking/keeper"
	"github.com/sei-protocol/sei-chain/app"
	"github.com/spf13/cobra"
	abci "github.com/tendermint/tendermint/abci/types"
	"github.com/tendermint/tendermint/libs/cli"
	tmtypes "github.com/tendermint/tendermint/types"
	dbm "github.com/tendermint/tm-db"
)

func MigrateCmd(appCreator types.AppCreator, defaultNodeHome string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "migrate_oneoff",
		Short: "",
		Long:  "",
		PreRunE: func(cmd *cobra.Command, _ []string) error {
			serverCtx := server.GetServerContextFromCmd(cmd)

			// Bind flags to the Context's Viper so the app construction can set
			// options accordingly.
			serverCtx.Viper.BindPFlags(cmd.Flags())

			_, err := server.GetPruningOptionsFromFlags(serverCtx.Viper)
			return err
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			serverCtx := server.GetServerContextFromCmd(cmd)
			go func() {
				err := http.ListenAndServe(":6060", nil)
				if err != nil {
					serverCtx.Logger.Error("Error from profiling server", "error", err)
				}
			}()
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
			clientCtx, err = clientconfig.ReadFromClientConfig(clientCtx)
			if err != nil {
				return err
			}

			chainID := clientCtx.ChainID
			flagChainID, _ := cmd.Flags().GetString(server.FlagChainID)
			if flagChainID != "" {
				if flagChainID != chainID {
					panic(fmt.Sprintf("chain-id mismatch: %s vs %s. The chain-id passed in is different from the value in ~/.sei/config/client.toml \n", flagChainID, chainID))
				}
				chainID = flagChainID
			}

			serverCtx.Viper.Set(flags.FlagChainID, chainID)

			genesisFile, _ := tmtypes.GenesisDocFromFile(serverCtx.Config.GenesisFile())
			if genesisFile.ChainID != clientCtx.ChainID {
				panic(fmt.Sprintf("genesis file chain-id=%s does not equal config.toml chain-id=%s", genesisFile.ChainID, clientCtx.ChainID))
			}

			cfg := serverCtx.Config
			home := cfg.RootDir

			traceWriterFile := serverCtx.Viper.GetString("trace-store")
			db, err := openDB(home)
			if err != nil {
				return err
			}

			traceWriter, err := openTraceWriter(traceWriterFile)
			if err != nil {
				return err
			}

			config, err := config.GetConfig(serverCtx.Viper)
			if err != nil {
				return err
			}

			if err := config.ValidateBasic(serverCtx.Config); err != nil {
				serverCtx.Logger.Error("WARNING: The minimum-gas-prices config in app.toml is set to the empty string. " +
					"This defaults to 0 in the current version, but will error in the next version " +
					"(SDK v0.45). Please explicitly put the desired minimum-gas-prices in your app.toml.")
			}
			a := appCreator(serverCtx.Logger, db, traceWriter, serverCtx.Config, serverCtx.Viper)
			seiApp := a.(*app.App)
			setCtx(seiApp)
			stakingMigrator := stakingkeeper.NewMigrator(seiApp.StakingKeeper)
			authMigrator := authkeeper.NewMigrator(seiApp.AccountKeeper, seiApp.GRPCQueryRouter())
			distMigrator := distributionkeeper.NewMigrator(seiApp.DistrKeeper)
			stakingMigrator.Migrate2to3(seiApp.GetContextForDeliverTx([]byte{}))
			fmt.Println("migrated staking")
			authMigrator.Migrate2to3(seiApp.GetContextForDeliverTx([]byte{}))
			fmt.Println("migrated auth")
			distMigrator.Migrate2to3(seiApp.GetContextForDeliverTx([]byte{}))
			fmt.Println("migrated distribution")
			seiApp.SetDeliverStateToCommit()
			_, err = seiApp.Commit(context.Background())
			if err != nil {
				panic(err)
			}
			return nil
		},
	}
	cmd.Flags().String(cli.HomeFlag, defaultNodeHome, "node's home directory")
	cmd.Flags().String(server.FlagChainID, "", "Chain ID")
	return cmd
}

func setCtx(seiApp *app.App) {
	defer func() {
		if err := recover(); err != nil {

		}
	}()
	seiApp.FinalizeBlock(context.Background(), &abci.RequestFinalizeBlock{})
}

func openDB(rootDir string) (dbm.DB, error) {
	dataDir := filepath.Join(rootDir, "data")
	return sdk.NewLevelDB("application", dataDir)
}

func openTraceWriter(traceWriterFile string) (w io.Writer, err error) {
	if traceWriterFile == "" {
		return
	}
	return os.OpenFile(
		traceWriterFile,
		os.O_WRONLY|os.O_APPEND|os.O_CREATE,
		0666,
	)
}
