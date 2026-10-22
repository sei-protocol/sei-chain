package server

// DONTCOVER

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	_ "net/http/pprof" //nolint:gosec
	"os"
	"path/filepath"
	"runtime/pprof"
	"sync"
	"time"

	gigaconfig "github.com/sei-protocol/sei-chain/giga/config"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client"
	clientconfig "github.com/sei-protocol/sei-chain/sei-cosmos/client/config"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client/flags"
	"github.com/sei-protocol/sei-chain/sei-cosmos/server/api"
	"github.com/sei-protocol/sei-chain/sei-cosmos/server/config"
	"github.com/sei-protocol/sei-chain/sei-cosmos/server/types"
	storetypes "github.com/sei-protocol/sei-chain/sei-cosmos/store/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/telemetry"
	genesistypes "github.com/sei-protocol/sei-chain/sei-cosmos/types/genesis"
	"github.com/sei-protocol/sei-chain/sei-cosmos/utils/tracing"
	tcmd "github.com/sei-protocol/sei-chain/sei-tendermint/cmd/tendermint/commands"
	"github.com/sei-protocol/sei-chain/sei-tendermint/node"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/client/local"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/types"
	"github.com/spf13/cobra"
	"go.opentelemetry.io/otel/sdk/trace"
)

const (
	// Tendermint full-node start flags
	flagAddress            = "address"
	flagTransport          = "transport"
	flagGRPCEnable         = "grpc.enable"
	flagGRPCAddress        = "grpc.address"
	flagGRPCWebEnable      = "grpc-web.enable"
	flagGRPCWebAddress     = "grpc-web.address"
	flagTraceStore         = "trace-store"
	flagCPUProfile         = "cpu-profile"
	FlagMinGasPrices       = "minimum-gas-prices"
	FlagHaltHeight         = "halt-height"
	FlagFreezeHeight       = "freeze-height"
	FlagHaltTime           = "halt-time"
	FlagInterBlockCache    = "inter-block-cache"
	FlagUnsafeSkipUpgrades = "unsafe-skip-upgrades"
	FlagTrace              = "trace"
	FlagProfile            = "profile"
	FlagInvCheckPeriod     = "inv-check-period"

	// Legacy pruning flags (kept for backward compatibility)
	FlagPruning           = "pruning"
	FlagPruningKeepRecent = "pruning-keep-recent"
	FlagPruningKeepEvery  = "pruning-keep-every"
	FlagPruningInterval   = "pruning-interval"

	FlagIndexEvents        = "index-events"
	FlagMinRetainBlocks    = "min-retain-blocks"
	FlagCompactionInterval = "compaction-interval"
	FlagConcurrencyWorkers = "concurrency-workers"

	// state sync-related flags
	FlagStateSyncSnapshotInterval   = "state-sync.snapshot-interval"
	FlagStateSyncSnapshotKeepRecent = "state-sync.snapshot-keep-recent"
	FlagStateSyncSnapshotDir        = "state-sync.snapshot-directory"

	// archival related flags
	FlagArchivalVersion                = "archival-version"
	FlagArchivalDBType                 = "archival-db-type"
	FlagArchivalArweaveIndexDBFullPath = "archival-arweave-index-db-full-path"
	FlagArchivalArweaveNodeURL         = "archival-arweave-node-url"

	// chain info
	FlagChainID = "chain-id"
)

// StartCmd runs the service passed in with Tendermint in-process.
func StartCmd(appCreator types.AppCreator, defaultNodeHome string, tracerProviderOptions []trace.TracerProviderOption) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Run the full node",
		Long: `Run the full node application with Tendermint in process.
Pruning options can be provided via the '--pruning' flag or alternatively with '--pruning-keep-recent',
'pruning-keep-every', and 'pruning-interval' together.
For '--pruning' the options are as follows:
default: the last 100 states are kept in addition to every 500th state; pruning at 10 block intervals
nothing: all historic states will be saved, nothing will be deleted (i.e. archiving node)
everything: all saved states will be deleted, storing only the current and previous state; pruning at 10 block intervals
custom: allow pruning options to be manually specified through 'pruning-keep-recent', 'pruning-keep-every', and 'pruning-interval'
Node halting configurations exist in the form of two flags: '--halt-height' and '--halt-time'. During
the ABCI Commit phase, the node will check if the current block height is greater than or equal to
the halt-height or if the current block time is greater than or equal to the halt-time. If so, the
node will attempt to gracefully shutdown and the block will not be committed. In addition, the node
will not be able to commit subsequent blocks.
The '--freeze-height' flag puts a full node in read-only freeze mode. Query RPC remains available,
but transaction and evidence submission, mempool gossip, and state sync are disabled from startup.
Block sync and consensus stop before executing the configured height.
For profiling and benchmarking purposes, CPU profiling can be enabled via the '--cpu-profile' flag
which accepts a path for the resulting pprof file.
`,
		PreRunE: func(cmd *cobra.Command, _ []string) error {
			serverCtx := GetServerContextFromCmd(cmd)

			// Bind flags to the Context's Viper so the app construction can set
			// options accordingly.
			if err := serverCtx.Viper.BindPFlags(cmd.Flags()); err != nil {
				return err
			}

			_, err := GetPruningOptionsFromFlags(serverCtx.Viper)
			return err
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			serverCtx := GetServerContextFromCmd(cmd)

			if enableProfile, _ := cmd.Flags().GetBool(FlagProfile); enableProfile {
				go func() {
					logger.Info("Listening for profiling at http://localhost:6060/debug/pprof/")
					// TODO: Should this be bound to all interfaces?
					server := &http.Server{
						Addr:              "localhost:6060",
						ReadHeaderTimeout: 10 * time.Second,
						//nolint:gosec // no read/write timeout to allow long running pprofs for debugging
					}
					err := server.ListenAndServe()
					if err != nil {
						logger.Error("Error from profiling server", "error", err)
					}
				}()
			}

			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
			clientCtx, err = clientconfig.ReadFromClientConfig(clientCtx)
			if err != nil {
				return err
			}

			chainID := clientCtx.ChainID
			flagChainID, _ := cmd.Flags().GetString(FlagChainID)
			if flagChainID != "" {
				if flagChainID != chainID {
					panic(fmt.Sprintf("chain-id mismatch: %s vs %s. The chain-id passed in is different from the value in ~/.sei/config/client.toml \n", flagChainID, chainID))
				}
				chainID = flagChainID
			}

			serverCtx.Viper.Set(flags.FlagChainID, chainID)

			if enableTracing, _ := cmd.Flags().GetBool(tracing.FlagTracing); !enableTracing {
				logger.Info("--tracing not passed in, tracing is not enabled")
				tracerProviderOptions = []trace.TracerProviderOption{}
			}

			config, err := config.GetConfig(serverCtx.Viper)
			if err != nil {
				return err
			}
			config.Telemetry = injectTelemetryChainID(config.Telemetry, clientCtx.ChainID)
			apiMetrics, err := telemetry.New(config.Telemetry)
			if err != nil {
				return fmt.Errorf("failed to initialize telemetry: %w", err)
			}
			if !config.Genesis.StreamImport {
				genesisFile, _ := tmtypes.GenesisDocFromFile(serverCtx.Config.GenesisFile())
				if genesisFile.ChainID != clientCtx.ChainID {
					panic(fmt.Sprintf("genesis file chain-id=%s does not equal config.toml chain-id=%s", genesisFile.ChainID, clientCtx.ChainID))
				}
			}

			logger.Info("Starting Process")
			for {
				err := startInProcess(
					serverCtx,
					clientCtx,
					appCreator,
					tracerProviderOptions,
					apiMetrics,
				)
				if !errors.Is(err, ErrShouldRestart) {
					return err
				}
				logger.Info("restarting node...")
			}
		},
	}

	addStartNodeFlags(cmd, defaultNodeHome)
	return cmd
}

func addStartNodeFlags(cmd *cobra.Command, defaultNodeHome string) {
	cmd.Flags().String(flags.FlagHome, defaultNodeHome, "The application home directory")
	cmd.Flags().String(flagTraceStore, "", "Enable KVStore tracing to an output file")
	cmd.Flags().String(FlagMinGasPrices, "", "Minimum gas prices to accept for transactions; Any fee in a tx must meet this minimum (e.g. 0.01photino;0.0001stake)")
	cmd.Flags().IntSlice(FlagUnsafeSkipUpgrades, []int{}, "Skip a set of upgrade heights to continue the old binary")
	cmd.Flags().Uint64(FlagHaltHeight, 0, "Block height at which to gracefully halt the chain and shutdown the node")
	cmd.Flags().Uint64(FlagFreezeHeight, 0, "Block height at which a full node stops executing while continuing to serve query RPC")
	cmd.Flags().Uint64(FlagHaltTime, 0, "Minimum block time (in Unix seconds) at which to gracefully halt the chain and shutdown the node")
	cmd.Flags().Bool(FlagInterBlockCache, true, "Enable inter-block caching")
	cmd.Flags().String(flagCPUProfile, "", "Enable CPU profiling and write to the provided file")
	cmd.Flags().Bool(FlagTrace, false, "Provide full stack traces for errors in ABCI Log")
	cmd.Flags().Bool(tracing.FlagTracing, false, "Enable Tracing for the app")
	cmd.Flags().Bool(FlagProfile, false, "Enable Profiling in the application")
	cmd.Flags().String(FlagPruning, storetypes.PruningOptionDefault, "Pruning strategy (default|nothing|everything|custom)")
	cmd.Flags().Uint64(FlagPruningKeepRecent, 0, "Number of recent heights to keep on disk (ignored if pruning is not 'custom')")
	cmd.Flags().Uint64(FlagPruningKeepEvery, 0, "Offset heights to keep on disk after 'keep-every' (ignored if pruning is not 'custom')")
	cmd.Flags().Uint64(FlagPruningInterval, 0, "Height interval at which pruned heights are removed from disk (ignored if pruning is not 'custom')")
	cmd.Flags().Uint(FlagInvCheckPeriod, 0, "Assert registered invariants every N blocks")
	cmd.Flags().Uint64(FlagMinRetainBlocks, 0, "Minimum block height offset during ABCI commit to prune Tendermint blocks")
	cmd.Flags().Uint64(FlagCompactionInterval, 0, "Time interval in between forced levelDB compaction. 0 means no forced compaction.")
	cmd.Flags().Int(FlagConcurrencyWorkers, config.DefaultConcurrencyWorkers, "Number of workers to process concurrent transactions")

	cmd.Flags().Uint64(FlagStateSyncSnapshotInterval, 0, "State sync snapshot interval")
	cmd.Flags().Uint32(FlagStateSyncSnapshotKeepRecent, 2, "State sync snapshot to keep")

	cmd.Flags().Int64(FlagArchivalVersion, 0, "Application data before this version is stored in archival DB")
	cmd.Flags().String(FlagArchivalDBType, "", "Archival DB type. Valid options: arweave")
	cmd.Flags().String(FlagArchivalArweaveIndexDBFullPath, "", "Full local path to the levelDB used for indexing arweave data")
	cmd.Flags().String(FlagArchivalArweaveNodeURL, "", "Arweave Node URL that stores archived data")
	cmd.Flags().String(FlagChainID, "", "Chain ID")

	// add support for all Tendermint-specific command line options
	tcmd.AddNodeFlags(cmd, NewDefaultContext().Config)
	mustMarkDeprecated(cmd, flagAddress, "out-of-process ABCI has been removed; this flag is ignored")
	mustMarkDeprecated(cmd, flagTransport, "out-of-process ABCI has been removed; this flag is ignored")

	const grpcRemoved = "the gRPC and gRPC-Web servers have been removed; this flag is ignored"
	mustMarkDeprecatedBool(cmd, flagGRPCEnable, grpcRemoved)
	mustMarkDeprecated(cmd, flagGRPCAddress, grpcRemoved)
	mustMarkDeprecatedBool(cmd, flagGRPCWebEnable, grpcRemoved)
	mustMarkDeprecated(cmd, flagGRPCWebAddress, grpcRemoved)
}

func injectTelemetryChainID(cfg telemetry.Config, chainID string) telemetry.Config {
	if chainID == "" {
		return cfg
	}
	for _, label := range cfg.GlobalLabels {
		if len(label) > 0 && label[0] == "chain_id" {
			return cfg
		}
	}
	cfg.GlobalLabels = append(cfg.GlobalLabels, []string{"chain_id", chainID})
	return cfg
}

func mustMarkDeprecated(cmd *cobra.Command, name, message string) {
	cmd.Flags().String(name, "", "")
	if err := cmd.Flags().MarkDeprecated(name, message); err != nil {
		panic(err)
	}
}

func mustMarkDeprecatedBool(cmd *cobra.Command, name, message string) {
	cmd.Flags().Bool(name, false, "")
	if err := cmd.Flags().MarkDeprecated(name, message); err != nil {
		panic(err)
	}
}

func startInProcess(
	ctx *Context,
	clientCtx client.Context,
	appCreator types.AppCreator,
	tracerProviderOptions []trace.TracerProviderOption,
	apiMetrics *telemetry.Metrics,
) error {
	cfg := ctx.Config
	home := cfg.RootDir
	goCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if cpuProfile := ctx.Viper.GetString(flagCPUProfile); cpuProfile != "" {
		f, err := os.Create(filepath.Clean(cpuProfile))
		if err != nil {
			return fmt.Errorf("failed to create cpuProfile file %w", err)
		}

		if err := pprof.StartCPUProfile(f); err != nil {
			return fmt.Errorf("failed to start CPU Profiler %w", err)
		}

		defer func() {
			logger.Info("stopping CPU profiler", "profile", cpuProfile)
			pprof.StopCPUProfile()
			_ = f.Close()
		}()
	}

	traceWriterFile := ctx.Viper.GetString(flagTraceStore)

	traceWriter, err := openTraceWriter(traceWriterFile)
	if err != nil {
		return err
	}

	config, err := config.GetConfig(ctx.Viper)
	if err != nil {
		return err
	}
	if err := config.ValidateFreeze(); err != nil {
		return err
	}
	if err := config.ValidateBasic(ctx.Config); err != nil {
		logger.Error("WARNING: The minimum-gas-prices config in app.toml is set to the empty string. " +
			"This defaults to 0 in the current version, but will error in the next version " +
			"(SDK v0.45). Please explicitly put the desired minimum-gas-prices in your app.toml.")
	}
	if config.Rosetta.Enable {
		logger.Error("WARNING: rosetta.enable is set in app.toml, but seid no longer starts the Rosetta API server; the setting is ignored")
	}
	app := appCreator(nil, traceWriter, ctx.Config, ctx.Viper)
	defer func() {
		logger.Info("close any other open resource...")
		if err := app.Close(); err != nil {
			logger.Error("error closing database", "err", err)
		}
	}()

	var restartMtx sync.Mutex
	restartCh := make(chan struct{})
	restartEvent := func() {
		restartMtx.Lock()
		defer restartMtx.Unlock()
		select {
		case <-restartCh:
		default:
			close(restartCh)
		}
	}

	logger.Info("starting node with ABCI Tendermint in-process")
	var gen *tmtypes.GenesisDoc
	if config.Genesis.StreamImport {
		lines := genesistypes.IngestGenesisFileLineByLine(config.Genesis.GenesisStreamFile)
		for line := range lines {
			genDoc, err := tmtypes.GenesisDocFromJSON([]byte(line))
			if err != nil {
				return err
			}
			if gen != nil {
				return fmt.Errorf("error: multiple genesis docs found in stream")
			}
			gen = genDoc
		}
	}
	gigaCfg := gigaconfig.DefaultConfig
	if ctx.Config.AutobahnConfigFile != "" {
		if gigaCfg, err = gigaconfig.ReadConfig(ctx.Viper); err != nil {
			return fmt.Errorf("read [giga] config: %w", err)
		}
	}
	tmNode, err := node.New(
		goCtx,
		ctx.Config,
		restartEvent,
		app,
		gen,
		tracerProviderOptions,
		tmtypes.DefaultConsensusPolicy(),
		node.WithFreezeHeight(config.FreezeHeight),
		node.WithGigaConfig(gigaCfg),
	)
	if err != nil {
		return fmt.Errorf("error creating node: %w", err)
	}
	if err := tmNode.Start(goCtx); err != nil {
		return fmt.Errorf("error starting node: %w", err)
	}
	defer func() {
		if tmNode.IsRunning() {
			tmNode.Stop()
		}
	}()
	// Local services include the EVM JSON-RPC and admin servers, each gated by
	// its own config, so they start whether or not the REST API is enabled.
	localClient, err := local.New(tmNode)
	if err != nil {
		return err
	}
	clientCtx = clientCtx.WithClient(localClient)
	app.RegisterLocalServices(localClient, clientCtx.TxConfig)

	if config.API.Enable {
		var apiSrv *api.Server
		clientCtx := clientCtx.WithHomeDir(home).WithChainID(clientCtx.ChainID)
		apiSrv = api.New(clientCtx)
		defer func() { _ = apiSrv.Close() }()
		app.RegisterAPIRoutes(apiSrv, config.API)
		errCh := make(chan error)

		go func() {
			if err := apiSrv.Start(config, apiMetrics); err != nil {
				errCh <- err
			}
		}()

		select {
		case err := <-errCh:
			return fmt.Errorf("error starting api server: %w", err)

		case <-time.After(types.ServerStartTime): // assume server started successfully
		}
	}

	// wait for signal capture and gracefully return
	return WaitForQuitSignals(goCtx, restartCh)
}
