package cmd

import (
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/config"
	"github.com/cosmos/cosmos-sdk/client/debug"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/keys"
	"github.com/cosmos/cosmos-sdk/client/pruning"
	"github.com/cosmos/cosmos-sdk/client/rpc"
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/server"
	serverconfig "github.com/cosmos/cosmos-sdk/server/config"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	"github.com/cosmos/cosmos-sdk/snapshots"
	"github.com/cosmos/cosmos-sdk/store"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/utils/tracing"
	authcmd "github.com/cosmos/cosmos-sdk/x/auth/client/cli"
	"github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/cosmos/cosmos-sdk/x/crisis"
	genutilcli "github.com/cosmos/cosmos-sdk/x/genutil/client/cli"
	"github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/app/params"
	evmrpcconfig "github.com/sei-protocol/sei-chain/evmrpc/config"
	gigaconfig "github.com/sei-protocol/sei-chain/giga/executor/config"
	seidbconfig "github.com/sei-protocol/sei-chain/sei-db/config"
	tmcfg "github.com/sei-protocol/sei-chain/sei-tendermint/config"
	tmcli "github.com/sei-protocol/sei-chain/sei-tendermint/libs/cli"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/log"
	"github.com/sei-protocol/sei-chain/sei-wasmd/x/wasm"
	wasmkeeper "github.com/sei-protocol/sei-chain/sei-wasmd/x/wasm/keeper"
	"github.com/sei-protocol/sei-chain/tools"
	"github.com/sei-protocol/sei-chain/tools/migration/ss"
	"github.com/spf13/cast"
	"github.com/spf13/cobra"
	dbm "github.com/tendermint/tm-db"
)

// Option configures root command option.
type Option func(*rootOptions)

// scaffoldingOptions keeps set of options to apply scaffolding.
//
//nolint:unused // preserving this becase don't know if it is needed.
type rootOptions struct{}

func (s *rootOptions) apply(options ...Option) { //nolint:unused // I figure this gets used later.
	for _, o := range options {
		o(s)
	}
}

// NewRootCmd creates a new root command for a Cosmos SDK application
func NewRootCmd() (*cobra.Command, params.EncodingConfig) {
	encodingConfig := app.MakeEncodingConfig()
	initClientCtx := client.Context{}.
		WithCodec(encodingConfig.Marshaler).
		WithInterfaceRegistry(encodingConfig.InterfaceRegistry).
		WithTxConfig(encodingConfig.TxConfig).
		WithLegacyAmino(encodingConfig.Amino).
		WithInput(os.Stdin).
		WithAccountRetriever(types.AccountRetriever{}).
		WithBroadcastMode(flags.BroadcastBlock).
		WithHomeDir(app.DefaultNodeHome).
		WithViper("SEI")

	rootCmd := &cobra.Command{
		Use:   "seid",
		Short: "Start sei app",
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			// set the default command outputs
			cmd.SetOut(cmd.OutOrStdout())
			cmd.SetErr(cmd.ErrOrStderr())
			initClientCtx, err := client.ReadPersistentCommandFlags(initClientCtx, cmd.Flags())
			if err != nil {
				return err
			}
			initClientCtx, err = config.ReadFromClientConfig(initClientCtx)
			if err != nil {
				return err
			}
			if err := client.SetCmdClientContextHandler(initClientCtx, cmd); err != nil {
				return err
			}

			// Skip creating config.toml/app.toml when running "init"; init creates them itself.
			// Otherwise the PreRun would create them in the init home, and init would then error
			if strings.HasPrefix(cmd.Use, "init") {
				return nil
			}

			customAppTemplate, customAppConfig := initAppConfig()

			return server.InterceptConfigsPreRunHandler(cmd, customAppTemplate, customAppConfig)
		},
	}

	initRootCmd(
		rootCmd,
		encodingConfig,
	)
	return rootCmd, encodingConfig
}

func initRootCmd(
	rootCmd *cobra.Command,
	encodingConfig params.EncodingConfig,
) {
	cfg := sdk.GetConfig()
	cfg.Seal()

	// extend debug command
	debugCmd := debug.Cmd()
	debugCmd.AddCommand(DumpIavlCmd())

	rootCmd.AddCommand(
		InitCmd(app.ModuleBasics, app.DefaultNodeHome),
		genutilcli.CollectGenTxsCmd(banktypes.GenesisBalancesIterator{}, app.DefaultNodeHome),
		genutilcli.MigrateGenesisCmd(),
		genutilcli.GenTxCmd(
			app.ModuleBasics,
			encodingConfig.TxConfig,
			banktypes.GenesisBalancesIterator{},
			app.DefaultNodeHome,
		),
		genutilcli.ValidateGenesisCmd(app.ModuleBasics),
		AddGenesisAccountCmd(app.DefaultNodeHome),
		AddGenesisWasmMsgCmd(app.DefaultNodeHome),
		tmcli.NewCompletionCmd(rootCmd, true),
		debugCmd,
		config.Cmd(),
		pruning.PruningCmd(newApp),
		CompactCmd(app.DefaultNodeHome),
		tools.ToolCmd(),
		SnapshotCmd(),
	)

	tracingProviderOpts, err := tracing.GetTracerProviderOptions(tracing.DefaultTracingURL)
	if err != nil {
		panic(err)
	}
	// add server commands
	server.AddCommands(
		rootCmd,
		app.DefaultNodeHome,
		newApp,
		appExport,
		addModuleInitFlags,
		tracingProviderOpts,
	)

	// add keybase, auxiliary RPC, query, and tx child commands
	rootCmd.AddCommand(
		rpc.StatusCommand(),
		queryCommand(),
		txCommand(),
		keys.Commands(app.DefaultNodeHome),
		ReplayCmd(app.DefaultNodeHome),
		BlocktestCmd(app.DefaultNodeHome),
		ShadowReplayCmd(app.DefaultNodeHome),
	)
}

// queryCommand returns the sub-command to send queries to the app
func queryCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:                        "query",
		Aliases:                    []string{"q"},
		Short:                      "Querying subcommands",
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}

	cmd.AddCommand(
		authcmd.GetAccountCmd(),
		rpc.ValidatorCommand(),
		rpc.BlockCommand(),
		authcmd.QueryTxsByEventsCmd(),
		authcmd.QueryTxCmd(),
	)

	app.ModuleBasics.AddQueryCommands(cmd)
	cmd.PersistentFlags().String(flags.FlagChainID, "", "The network chain ID")

	return cmd
}

// txCommand returns the sub-command to send transactions to the app
func txCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:                        "tx",
		Short:                      "Transactions subcommands",
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}

	cmd.AddCommand(
		authcmd.GetSignCommand(),
		authcmd.GetSignBatchCommand(),
		authcmd.GetMultiSignCommand(),
		authcmd.GetValidateSignaturesCommand(),
		flags.LineBreak,
		authcmd.GetBroadcastCommand(),
		authcmd.GetEncodeCommand(),
		authcmd.GetDecodeCommand(),
	)

	app.ModuleBasics.AddTxCommands(cmd)
	cmd.PersistentFlags().String(flags.FlagChainID, "", "The network chain ID")

	return cmd
}

func addModuleInitFlags(startCmd *cobra.Command) {
	crisis.AddModuleInitFlags(startCmd)
	startCmd.Flags().Bool("migrate-iavl", false, "Run migration of IAVL data store to SeiDB State Store")
	startCmd.Flags().Int64("migrate-height", 0, "Height at which to start the migration")
}

// newApp creates a new Cosmos SDK app
func newApp(
	logger log.Logger,
	db dbm.DB,
	traceStore io.Writer,
	tmConfig *tmcfg.Config,
	appOpts servertypes.AppOptions,
) servertypes.Application {
	var cache sdk.MultiStorePersistentCache

	if cast.ToBool(appOpts.Get(server.FlagInterBlockCache)) {
		cache = store.NewCommitKVStoreCacheManager()
	}

	skipUpgradeHeights := make(map[int64]bool)
	for _, h := range cast.ToIntSlice(appOpts.Get(server.FlagUnsafeSkipUpgrades)) {
		skipUpgradeHeights[int64(h)] = true
	}

	pruningOpts, err := server.GetPruningOptionsFromFlags(appOpts)
	if err != nil {
		panic(err)
	}

	snapshotDirectory := cast.ToString(appOpts.Get(server.FlagStateSyncSnapshotDir))
	if snapshotDirectory == "" {
		snapshotDirectory = filepath.Join(cast.ToString(appOpts.Get(flags.FlagHome)), "data", "snapshots")
	}

	snapshotDB, err := sdk.NewLevelDB("metadata", snapshotDirectory)
	if err != nil {
		panic(err)
	}
	snapshotStore, err := snapshots.NewStore(snapshotDB, snapshotDirectory)
	if err != nil {
		panic(err)
	}

	wasmGasRegisterConfig := wasmkeeper.DefaultGasRegisterConfig()
	// This varies from the default value of 140_000_000 because we would like to appropriately represent the
	// compute time required as a proportion of block gas used for a wasm contract that performs a lot of compute
	// This makes it such that the wasm VM gas converts to sdk gas at a 6.66x rate vs that of the previous multiplier
	wasmGasRegisterConfig.GasMultiplier = 21_000_000

	app := app.New(
		logger,
		db,
		traceStore,
		true,
		skipUpgradeHeights,
		cast.ToString(appOpts.Get(flags.FlagHome)),
		cast.ToUint(appOpts.Get(server.FlagInvCheckPeriod)),
		true,
		tmConfig,
		app.MakeEncodingConfig(),
		wasm.EnableAllProposals,
		appOpts,
		[]wasm.Option{
			wasmkeeper.WithGasRegister(
				wasmkeeper.NewWasmGasRegister(
					wasmGasRegisterConfig,
				),
			),
		},
		app.EmptyAppOptions,
		baseapp.SetPruning(pruningOpts),
		baseapp.SetMinGasPrices(cast.ToString(appOpts.Get(server.FlagMinGasPrices))),
		baseapp.SetMinRetainBlocks(cast.ToUint64(appOpts.Get(server.FlagMinRetainBlocks))),
		baseapp.SetHaltHeight(cast.ToUint64(appOpts.Get(server.FlagHaltHeight))),
		baseapp.SetHaltTime(cast.ToUint64(appOpts.Get(server.FlagHaltTime))),
		baseapp.SetInterBlockCache(cache),
		baseapp.SetTrace(cast.ToBool(appOpts.Get(server.FlagTrace))),
		baseapp.SetIndexEvents(cast.ToStringSlice(appOpts.Get(server.FlagIndexEvents))),
		baseapp.SetSnapshotStore(snapshotStore),
		baseapp.SetSnapshotInterval(cast.ToUint64(appOpts.Get(server.FlagStateSyncSnapshotInterval))),
		baseapp.SetSnapshotKeepRecent(cast.ToUint32(appOpts.Get(server.FlagStateSyncSnapshotKeepRecent))),
		baseapp.SetSnapshotDirectory(cast.ToString(appOpts.Get(server.FlagStateSyncSnapshotDir))),
		baseapp.SetOccEnabled(cast.ToBool(appOpts.Get(baseapp.FlagOccEnabled))),
	)

	// Start migration if --migrate flag is set
	if cast.ToBool(appOpts.Get("migrate-iavl")) {
		go func() {
			homeDir := cast.ToString(appOpts.Get(flags.FlagHome))
			stateStore := app.GetStateStore()
			migrationHeight := cast.ToInt64(appOpts.Get("migrate-height"))
			migrator := ss.NewMigrator(db, stateStore)
			if err := migrator.Migrate(migrationHeight, homeDir); err != nil {
				panic(err)
			}
		}()
	}

	return app
}

// appExport creates a new simapp (optionally at a given height)
func appExport(
	logger log.Logger,
	db dbm.DB,
	traceStore io.Writer,
	height int64,
	forZeroHeight bool,
	jailAllowedAddrs []string,
	appOpts servertypes.AppOptions,
	file *os.File,
) (servertypes.ExportedApp, error) {
	exportableApp, err := getExportableApp(
		logger,
		db,
		traceStore,
		height,
		appOpts,
	)
	if err != nil {
		return servertypes.ExportedApp{}, err
	}

	if file == nil {
		return exportableApp.ExportAppStateAndValidators(forZeroHeight, jailAllowedAddrs)
	} else {
		return exportableApp.ExportAppToFileStateAndValidators(forZeroHeight, jailAllowedAddrs, file)
	}
}

func getExportableApp(
	logger log.Logger,
	db dbm.DB,
	traceStore io.Writer,
	height int64,
	appOpts servertypes.AppOptions,
) (*app.App, error) {
	encCfg := app.MakeEncodingConfig()
	encCfg.Marshaler = codec.NewProtoCodec(encCfg.InterfaceRegistry)

	var exportableApp *app.App

	homePath, ok := appOpts.Get(flags.FlagHome).(string)
	if !ok || homePath == "" {
		return nil, errors.New("application home not set")
	}

	if height != -1 {
		exportableApp = app.New(logger, db, traceStore, false, map[int64]bool{}, cast.ToString(appOpts.Get(flags.FlagHome)), uint(1), true, nil, encCfg, app.GetWasmEnabledProposals(), appOpts, app.EmptyWasmOpts, app.EmptyAppOptions)
		if err := exportableApp.LoadHeight(height); err != nil {
			return nil, err
		}
	} else {
		exportableApp = app.New(logger, db, traceStore, true, map[int64]bool{}, cast.ToString(appOpts.Get(flags.FlagHome)), uint(1), true, nil, encCfg, app.GetWasmEnabledProposals(), appOpts, app.EmptyWasmOpts, app.EmptyAppOptions)
	}
	return exportableApp, nil

}

func getPrimeNums(lo int, hi int) []int {
	var primeNums []int

	for lo <= hi {
		isPrime := true
		for i := 2; i <= int(math.Sqrt(float64(lo))); i++ {
			if lo%i == 0 {
				isPrime = false
				break
			}
		}
		if isPrime {
			primeNums = append(primeNums, lo)
		}
		lo++
	}
	return primeNums
}

// initAppConfig helps to override default appConfig template and configs.
// return "", nil if no custom configuration is required for the application.
// nolint: staticcheck
func initAppConfig() (string, interface{}) {
	// Optionally allow the chain developer to overwrite the SDK's default
	// server config.
	srvCfg := serverconfig.DefaultConfig()
	// The SDK's default minimum gas price is set to "" (empty value) inside
	// app.toml. If left empty by validators, the node will halt on startup.
	// However, the chain developer can set a default app.toml value for their
	// validators here.
	//
	// In summary:
	// - if you leave srvCfg.MinGasPrices = "", all validators MUST tweak their
	//   own app.toml config,
	// - if you set srvCfg.MinGasPrices non-empty, validators CAN tweak their
	//   own app.toml to override, or use this default value.
	//
	// In simapp, we set the min gas prices to 0.
	srvCfg.MinGasPrices = "0.01usei"
	srvCfg.API.Enable = true

	// Pruning configs
	srvCfg.Pruning = "default"
	// Randomly generate pruning interval. Note this only takes affect if using custom pruning. We want the following properties:
	//   - random: if everyone has the same value, the block that everyone prunes will be slow
	//   - prime: no overlap
	primes := getPrimeNums(2500, 4000)
	r := rand.New(rand.NewSource(time.Now().Unix()))
	pruningInterval := primes[r.Intn(len(primes))]
	srvCfg.PruningInterval = fmt.Sprintf("%d", pruningInterval)

	// Metrics
	srvCfg.Telemetry.Enabled = true
	srvCfg.Telemetry.PrometheusRetentionTime = 60

	// Use shared CustomAppConfig from app_config.go
	customAppConfig := NewCustomAppConfig(srvCfg, evmrpcconfig.DefaultConfig)

	customAppTemplate := serverconfig.ManualConfigTemplate +
		seidbconfig.StateCommitConfigTemplate +
		seidbconfig.StateStoreConfigTemplate +
		evmrpcconfig.ConfigTemplate +
		gigaconfig.ConfigTemplate +
		serverconfig.AutoManagedConfigTemplate + `
###############################################################################
###                        WASM Configuration (Auto-managed)                ###
###############################################################################

[wasm]
# This is the maximum sdk gas (wasm and storage) that we allow for any x/wasm "smart" queries
query_gas_limit = 300000
# This is the number of wasm vm instances we keep cached in memory for speed-up
# Warning: this is currently unstable and may lead to crashes, best to keep for 0 unless testing locally
lru_size = 0

###############################################################################
###                     ETH Replay Configuration (Auto-managed)             ###
###############################################################################

[eth_replay]
eth_replay_enabled = {{ .ETHReplay.Enabled }}
eth_rpc = "{{ .ETHReplay.EthRPC }}"
eth_data_dir = "{{ .ETHReplay.EthDataDir }}"
eth_replay_contract_state_checks = {{ .ETHReplay.ContractStateChecks }}

###############################################################################
###                   ETH Block Test Configuration (Auto-managed)           ###
###############################################################################

[eth_blocktest]
eth_blocktest_enabled = {{ .ETHBlockTest.Enabled }}
eth_blocktest_test_data_path = "{{ .ETHBlockTest.TestDataPath }}"

###############################################################################
###                    EVM Query Configuration (Auto-managed)               ###
###############################################################################

[evm_query]
evm_query_gas_limit = {{ .EvmQuery.GasLimit }}

###############################################################################
###                 Light Invariance Configuration (Auto-managed)           ###
###############################################################################

[light_invariance]
supply_enabled = {{ .LightInvariance.SupplyEnabled }}
`

	return customAppTemplate, customAppConfig
}
