package server

// DONTCOVER

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path"
	"runtime/pprof"
	"time"

	clientconfig "github.com/cosmos/cosmos-sdk/client/config"

	genesistypes "github.com/cosmos/cosmos-sdk/types/genesis"
	"github.com/spf13/cobra"
	abciclient "github.com/tendermint/tendermint/abci/client"
	"github.com/tendermint/tendermint/abci/server"
	tcmd "github.com/tendermint/tendermint/cmd/tendermint/commands"
	"github.com/tendermint/tendermint/libs/service"
	"github.com/tendermint/tendermint/node"
	"github.com/tendermint/tendermint/rpc/client/local"
	tmtypes "github.com/tendermint/tendermint/types"
	"go.opentelemetry.io/otel/sdk/trace"
	"google.golang.org/grpc"

	//nolint:gosec,G108
	_ "net/http/pprof"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/server/api"
	"github.com/cosmos/cosmos-sdk/server/config"
	servergrpc "github.com/cosmos/cosmos-sdk/server/grpc"
	"github.com/cosmos/cosmos-sdk/server/rosetta"
	crgserver "github.com/cosmos/cosmos-sdk/server/rosetta/lib/server"
	"github.com/cosmos/cosmos-sdk/server/types"
	storetypes "github.com/cosmos/cosmos-sdk/store/types"
	"github.com/cosmos/cosmos-sdk/telemetry"
	"github.com/cosmos/cosmos-sdk/utils/tracing"
)

const (
	// Tendermint full-node start flags
	flagWithTendermint     = "with-tendermint"
	flagAddress            = "address"
	flagTransport          = "transport"
	flagTraceStore         = "trace-store"
	flagCPUProfile         = "cpu-profile"
	FlagMinGasPrices       = "minimum-gas-prices"
	FlagHaltHeight         = "halt-height"
	FlagHaltTime           = "halt-time"
	FlagInterBlockCache    = "inter-block-cache"
	FlagUnsafeSkipUpgrades = "unsafe-skip-upgrades"
	FlagTrace              = "trace"
	FlagProfile            = "profile"
	FlagInvCheckPeriod     = "inv-check-period"

	FlagPruning                      = "pruning"
	FlagPruningKeepRecent            = "pruning-keep-recent"
	FlagPruningKeepEvery             = "pruning-keep-every"
	FlagPruningInterval              = "pruning-interval"
	FlagIndexEvents                  = "index-events"
	FlagMinRetainBlocks              = "min-retain-blocks"
	FlagIAVLCacheSize                = "iavl-cache-size"
	FlagIAVLFastNode                 = "iavl-disable-fastnode"
	FlagCompactionInterval           = "compaction-interval"
	FlagSeparateOrphanStorage        = "separate-orphan-storage"
	FlagSeparateOrphanVersionsToKeep = "separate-orphan-versions-to-keep"
	FlagNumOrphanPerFile             = "num-orphan-per-file"
	FlagOrphanDirectory              = "orphan-dir"
	FlagConcurrencyWorkers           = "concurrency-workers"

	// state sync-related flags
	FlagStateSyncSnapshotInterval   = "state-sync.snapshot-interval"
	FlagStateSyncSnapshotKeepRecent = "state-sync.snapshot-keep-recent"
	FlagStateSyncSnapshotDir        = "state-sync.snapshot-directory"

	// gRPC-related flags
	flagGRPCOnly       = "grpc-only"
	flagGRPCEnable     = "grpc.enable"
	flagGRPCAddress    = "grpc.address"
	flagGRPCWebEnable  = "grpc-web.enable"
	flagGRPCWebAddress = "grpc-web.address"

	// archival related flags
	FlagArchivalVersion                = "archival-version"
	FlagArchivalDBType                 = "archival-db-type"
	FlagArchivalArweaveIndexDBFullPath = "archival-arweave-index-db-full-path"
	FlagArchivalArweaveNodeURL         = "archival-arweave-node-url"

	// chain info
	FlagChainID = "chain-id"
)

// StartCmd runs the service passed in, either stand-alone or in-process with
// Tendermint.
func StartCmd(appCreator types.AppCreator, defaultNodeHome string, tracerProviderOptions []trace.TracerProviderOption) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Run the full node",
		Long: `Run the full node application with Tendermint in or out of process. By
default, the application will run with Tendermint in process.

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

For profiling and benchmarking purposes, CPU profiling can be enabled via the '--cpu-profile' flag
which accepts a path for the resulting pprof file.

The node may be started in a 'query only' mode where only the gRPC and JSON HTTP
API services are enabled via the 'grpc-only' flag. In this mode, Tendermint is
bypassed and can be used when legacy queries are needed after an on-chain upgrade
is performed. Note, when enabled, gRPC will also be automatically enabled.
`,
		PreRunE: func(cmd *cobra.Command, _ []string) error {
			serverCtx := GetServerContextFromCmd(cmd)

			// Bind flags to the Context's Viper so the app construction can set
			// options accordingly.
			serverCtx.Viper.BindPFlags(cmd.Flags())

			_, err := GetPruningOptionsFromFlags(serverCtx.Viper)
			return err
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			serverCtx := GetServerContextFromCmd(cmd)

			if enableProfile, _ := cmd.Flags().GetBool(FlagProfile); enableProfile {
				go func() {
					serverCtx.Logger.Info("Listening for profiling at http://localhost:6060/debug/pprof/")
					err := http.ListenAndServe(":6060", nil)
					if err != nil {
						serverCtx.Logger.Error("Error from profiling server", "error", err)
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
				serverCtx.Logger.Info("--tracing not passed in, tracing is not enabled")
				tracerProviderOptions = []trace.TracerProviderOption{}
			}

			withTM, _ := cmd.Flags().GetBool(flagWithTendermint)
			if !withTM {
				serverCtx.Logger.Info("starting ABCI without Tendermint")
				return startStandAlone(serverCtx, appCreator)
			}

			// amino is needed here for backwards compatibility of REST routes
			exitCode := RestartErrorCode

			serverCtx.Logger.Info("Creating node metrics provider")
			nodeMetricsProvider := node.DefaultMetricsProvider(serverCtx.Config.Instrumentation)(clientCtx.ChainID)

			config, _ := config.GetConfig(serverCtx.Viper)
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

			restartCoolDownDuration := time.Second * time.Duration(serverCtx.Config.SelfRemediation.RestartCooldownSeconds)
			// Set the first restart time to be now - restartCoolDownDuration so that the first restart can trigger whenever
			canRestartAfter := time.Now().Add(-restartCoolDownDuration)

			serverCtx.Logger.Info("Starting Process")
			for {
				err = startInProcess(
					serverCtx,
					clientCtx,
					appCreator,
					tracerProviderOptions,
					nodeMetricsProvider,
					apiMetrics,
					canRestartAfter,
				)
				errCode, ok := err.(ErrorCode)
				exitCode = errCode.Code
				if !ok {
					return err
				}
				if exitCode != RestartErrorCode {
					break
				}
				serverCtx.Logger.Info("restarting node...")
				canRestartAfter = time.Now().Add(restartCoolDownDuration)
			}
			return nil
		},
	}

	addStartNodeFlags(cmd, defaultNodeHome)
	return cmd
}

func addStartNodeFlags(cmd *cobra.Command, defaultNodeHome string) {
	cmd.Flags().String(flags.FlagHome, defaultNodeHome, "The application home directory")
	cmd.Flags().Bool(flagWithTendermint, true, "Run abci app embedded in-process with tendermint")
	cmd.Flags().String(flagAddress, "tcp://0.0.0.0:26658", "Listen address")
	cmd.Flags().String(flagTransport, "socket", "Transport protocol: socket, grpc")
	cmd.Flags().String(flagTraceStore, "", "Enable KVStore tracing to an output file")
	cmd.Flags().String(FlagMinGasPrices, "", "Minimum gas prices to accept for transactions; Any fee in a tx must meet this minimum (e.g. 0.01photino;0.0001stake)")
	cmd.Flags().IntSlice(FlagUnsafeSkipUpgrades, []int{}, "Skip a set of upgrade heights to continue the old binary")
	cmd.Flags().Uint64(FlagHaltHeight, 0, "Block height at which to gracefully halt the chain and shutdown the node")
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
	cmd.Flags().Bool(FlagSeparateOrphanStorage, false, "Whether to store orphans outside main application levelDB")
	cmd.Flags().Int64(FlagSeparateOrphanVersionsToKeep, 2, "Number of versions to keep if storing orphans separately")
	cmd.Flags().Int(FlagNumOrphanPerFile, 100000, "Number of orphans to store on each file if storing orphans separately")
	cmd.Flags().String(FlagOrphanDirectory, path.Join(defaultNodeHome, "orphans"), "Directory to store orphan files if storing orphans separately")
	cmd.Flags().Int(FlagConcurrencyWorkers, config.DefaultConcurrencyWorkers, "Number of workers to process concurrent transactions")

	cmd.Flags().Bool(flagGRPCOnly, false, "Start the node in gRPC query only mode (no Tendermint process is started)")
	cmd.Flags().Bool(flagGRPCEnable, true, "Define if the gRPC server should be enabled")
	cmd.Flags().String(flagGRPCAddress, config.DefaultGRPCAddress, "the gRPC server address to listen on")

	cmd.Flags().Bool(flagGRPCWebEnable, true, "Define if the gRPC-Web server should be enabled. (Note: gRPC must also be enabled.)")
	cmd.Flags().String(flagGRPCWebAddress, config.DefaultGRPCWebAddress, "The gRPC-Web server address to listen on")

	cmd.Flags().Uint64(FlagStateSyncSnapshotInterval, 0, "State sync snapshot interval")
	cmd.Flags().Uint32(FlagStateSyncSnapshotKeepRecent, 2, "State sync snapshot to keep")

	cmd.Flags().Int64(FlagArchivalVersion, 0, "Application data before this version is stored in archival DB")
	cmd.Flags().String(FlagArchivalDBType, "", "Archival DB type. Valid options: arweave")
	cmd.Flags().String(FlagArchivalArweaveIndexDBFullPath, "", "Full local path to the levelDB used for indexing arweave data")
	cmd.Flags().String(FlagArchivalArweaveNodeURL, "", "Arweave Node URL that stores archived data")
	cmd.Flags().Bool(FlagIAVLFastNode, true, "Enable fast node for IAVL tree")

	cmd.Flags().String(FlagChainID, "", "Chain ID")

	// add support for all Tendermint-specific command line options
	tcmd.AddNodeFlags(cmd, NewDefaultContext().Config)
}

func startStandAlone(ctx *Context, appCreator types.AppCreator) error {
	addr := ctx.Viper.GetString(flagAddress)
	transport := ctx.Viper.GetString(flagTransport)
	home := ctx.Viper.GetString(flags.FlagHome)

	db, err := openDB(home)
	if err != nil {
		return err
	}

	traceWriterFile := ctx.Viper.GetString(flagTraceStore)
	traceWriter, err := openTraceWriter(traceWriterFile)
	if err != nil {
		return err
	}

	app := appCreator(ctx.Logger, db, traceWriter, nil, ctx.Viper)

	svr, err := server.NewServer(ctx.Logger.With("module", "abci-server"), addr, transport, app)
	if err != nil {
		return fmt.Errorf("error creating listener: %v", err)
	}

	goCtx, cancel := context.WithCancel(context.Background())
	err = svr.Start(goCtx)
	if err != nil {
		fmt.Printf(err.Error() + "\n")
		os.Exit(1)
	}

	defer func() {
		cancel()
		svr.Wait()
	}()

	restartCh := make(chan struct{})

	// Wait for SIGINT or SIGTERM signal
	return WaitForQuitSignals(ctx, restartCh, time.Now())
}

func startInProcess(
	ctx *Context,
	clientCtx client.Context,
	appCreator types.AppCreator,
	tracerProviderOptions []trace.TracerProviderOption,
	nodeMetricsProvider *node.NodeMetrics,
	apiMetrics *telemetry.Metrics,
	canRestartAfter time.Time,
) error {
	cfg := ctx.Config
	home := cfg.RootDir
	goCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var cpuProfileCleanup func()
	if cpuProfile := ctx.Viper.GetString(flagCPUProfile); cpuProfile != "" {
		f, err := os.Create(cpuProfile)
		if err != nil {
			return fmt.Errorf("failed to create cpuProfile file %w", err)
		}

		if err := pprof.StartCPUProfile(f); err != nil {
			return fmt.Errorf("failed to start CPU Profiler %w", err)
		}

		cpuProfileCleanup = func() {
			ctx.Logger.Info("stopping CPU profiler", "profile", cpuProfile)
			pprof.StopCPUProfile()
			f.Close()
		}
	}

	traceWriterFile := ctx.Viper.GetString(flagTraceStore)
	db, err := openDB(home)
	if err != nil {
		return err
	}

	traceWriter, err := openTraceWriter(traceWriterFile)
	if err != nil {
		return err
	}

	config, err := config.GetConfig(ctx.Viper)
	if err != nil {
		return err
	}

	if err := config.ValidateBasic(ctx.Config); err != nil {
		ctx.Logger.Error("WARNING: The minimum-gas-prices config in app.toml is set to the empty string. " +
			"This defaults to 0 in the current version, but will error in the next version " +
			"(SDK v0.45). Please explicitly put the desired minimum-gas-prices in your app.toml.")
	}
	app := appCreator(ctx.Logger, db, traceWriter, ctx.Config, ctx.Viper)

	var (
		tmNode    service.Service
		restartCh chan struct{}
		gRPCOnly  = ctx.Viper.GetBool(flagGRPCOnly)
	)

	restartCh = make(chan struct{})

	if gRPCOnly {
		ctx.Logger.Info("starting node in gRPC only mode; Tendermint is disabled")
		config.GRPC.Enable = true
	} else {
		ctx.Logger.Info("starting node with ABCI Tendermint in-process")
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
		tmNode, err = node.New(
			goCtx,
			ctx.Config,
			ctx.Logger,
			restartCh,
			abciclient.NewLocalClient(ctx.Logger, app),
			gen,
			tracerProviderOptions,
			nodeMetricsProvider,
		)
		if err != nil {
			return fmt.Errorf("error creating node: %w", err)
		}
		if err := tmNode.Start(goCtx); err != nil {
			return fmt.Errorf("error starting node: %w", err)
		}
	}

	// Add the tx service to the gRPC router. We only need to register this
	// service if API or gRPC is enabled, and avoid doing so in the general
	// case, because it spawns a new local tendermint RPC client.
	if (config.API.Enable || config.GRPC.Enable) && tmNode != nil {
		localClient, err := local.New(ctx.Logger, tmNode.(local.NodeService))
		if err != nil {
			return err
		}
		clientCtx = clientCtx.WithClient(localClient)

		app.RegisterTxService(clientCtx)
		app.RegisterTendermintService(clientCtx)
	}

	var apiSrv *api.Server
	if config.API.Enable {
		clientCtx := clientCtx.WithHomeDir(home).WithChainID(clientCtx.ChainID)
		apiSrv = api.New(clientCtx, ctx.Logger.With("module", "api-server"))
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

	var (
		grpcSrv    *grpc.Server
		grpcWebSrv *http.Server
	)

	if config.GRPC.Enable {
		grpcSrv, err = servergrpc.StartGRPCServer(clientCtx, app, config.GRPC.Address)
		if err != nil {
			return err
		}

		if config.GRPCWeb.Enable {
			grpcWebSrv, err = servergrpc.StartGRPCWeb(grpcSrv, config)
			if err != nil {
				ctx.Logger.Error("failed to start grpc-web http server: ", err)
				return err
			}
		}
	}

	// At this point it is safe to block the process if we're in gRPC only mode as
	// we do not need to start Rosetta or handle any Tendermint related processes.
	if gRPCOnly {
		// wait for signal capture and gracefully return
		return WaitForQuitSignals(ctx, restartCh, canRestartAfter)
	}

	var rosettaSrv crgserver.Server
	if config.Rosetta.Enable {
		offlineMode := config.Rosetta.Offline

		// If GRPC is not enabled rosetta cannot work in online mode, so it works in
		// offline mode.
		if !config.GRPC.Enable {
			offlineMode = true
		}

		conf := &rosetta.Config{
			Blockchain:        config.Rosetta.Blockchain,
			Network:           config.Rosetta.Network,
			TendermintRPC:     ctx.Config.RPC.ListenAddress,
			GRPCEndpoint:      config.GRPC.Address,
			Addr:              config.Rosetta.Address,
			Retries:           config.Rosetta.Retries,
			Offline:           offlineMode,
			Codec:             clientCtx.Codec.(*codec.ProtoCodec),
			InterfaceRegistry: clientCtx.InterfaceRegistry,
		}

		rosettaSrv, err = rosetta.ServerFromConfig(conf)
		if err != nil {
			return err
		}

		errCh := make(chan error)
		go func() {
			if err := rosettaSrv.Start(); err != nil {
				errCh <- err
			}
		}()

		select {
		case err := <-errCh:
			return err

		case <-time.After(types.ServerStartTime): // assume server started successfully
		}
	}

	defer func() {
		cancel()
		if tmNode.IsRunning() {
			tmNode.Wait()
		}

		if cpuProfileCleanup != nil {
			cpuProfileCleanup()
		}

		if apiSrv != nil {
			_ = apiSrv.Close()
		}

		if grpcSrv != nil {
			grpcSrv.Stop()
			if grpcWebSrv != nil {
				grpcWebSrv.Close()
			}
		}

		ctx.Logger.Info("close any other open resource...")
		if err := app.Close(); err != nil {
			ctx.Logger.Error("error closing database", "err", err)
		}
	}()

	// wait for signal capture and gracefully return
	return WaitForQuitSignals(ctx, restartCh, canRestartAfter)
}
