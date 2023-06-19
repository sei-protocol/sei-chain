package cmd

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	input "github.com/cosmos/cosmos-sdk/client/input"
	"github.com/mitchellh/mapstructure"

	"github.com/gorilla/mux"
	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"

	"github.com/sei-protocol/sei-chain/oracle/price-feeder/config"
	"github.com/sei-protocol/sei-chain/oracle/price-feeder/oracle"
	"github.com/sei-protocol/sei-chain/oracle/price-feeder/oracle/client"
	v1 "github.com/sei-protocol/sei-chain/oracle/price-feeder/router/v1"

	"github.com/cosmos/cosmos-sdk/telemetry"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

const (
	logLevelJSON = "json"
	logLevelText = "text"

	flagLogLevel  = "log-level"
	flagLogFormat = "log-format"

	envVariablePass = "PRICE_FEEDER_PASS"
)

var rootCmd = &cobra.Command{
	Use:   "price-feeder [config-file]",
	Args:  cobra.ExactArgs(1),
	Short: "price-feeder is a side-car process for providing an on-chain oracle with price data",
	Long: `A side-car process that validators must run in order to provide
an on-chain price oracle with price information. The price-feeder performs
two primary functions. First, it is responsible for obtaining price information
from various reliable data sources, e.g. exchanges, and exposing this data via
an API. Secondly, the price-feeder consumes this data and periodically submits
vote messages following the oracle voting procedure.`,
	RunE: priceFeederCmdHandler,
}

func init() {
	rootCmd.PersistentFlags().String(flagLogLevel, zerolog.InfoLevel.String(), "logging level")
	rootCmd.PersistentFlags().String(flagLogFormat, logLevelText, "logging format; must be either json or text")

	rootCmd.AddCommand(getVersionCmd())
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func priceFeederCmdHandler(cmd *cobra.Command, args []string) error {
	logLvlStr, err := cmd.Flags().GetString(flagLogLevel)
	if err != nil {
		return err
	}

	logLvl, err := zerolog.ParseLevel(logLvlStr)
	if err != nil {
		return err
	}

	logFormatStr, err := cmd.Flags().GetString(flagLogFormat)
	if err != nil {
		return err
	}

	var logWriter io.Writer
	switch strings.ToLower(logFormatStr) {
	case logLevelJSON:
		logWriter = os.Stderr

	case logLevelText:
		logWriter = zerolog.ConsoleWriter{Out: os.Stderr}

	default:
		return fmt.Errorf("invalid logging format: %s", logFormatStr)
	}

	logger := zerolog.New(logWriter).Level(logLvl).With().Timestamp().Logger()

	cfg, err := config.ParseConfig(args[0])
	if err != nil {
		return err
	}

	// Set prefixes
	accountPubKeyPrefix := cfg.Account.Prefix + "pub"
	validatorAddressPrefix := cfg.Account.Prefix + "valoper"
	validatorPubKeyPrefix := cfg.Account.Prefix + "valoperpub"
	consNodeAddressPrefix := cfg.Account.Prefix + "valcons"
	consNodePubKeyPrefix := cfg.Account.Prefix + "valconspub"

	// Set and seal config
	sdkConfig := sdk.GetConfig()
	sdkConfig.SetBech32PrefixForAccount(cfg.Account.Prefix, accountPubKeyPrefix)
	sdkConfig.SetBech32PrefixForValidator(validatorAddressPrefix, validatorPubKeyPrefix)
	sdkConfig.SetBech32PrefixForConsensusNode(consNodeAddressPrefix, consNodePubKeyPrefix)
	sdkConfig.Seal()

	ctx, cancel := context.WithCancel(cmd.Context())
	g, ctx := errgroup.WithContext(ctx)

	// listen for and trap any OS signal to gracefully shutdown and exit
	trapSignal(cancel, logger)

	rpcTimeout, err := time.ParseDuration(cfg.RPC.RPCTimeout)
	if err != nil {
		return fmt.Errorf("failed to parse RPC timeout: %w", err)
	}

	// Gather pass via env variable || std input
	keyringPass, err := getKeyringPassword()
	if err != nil {
		return err
	}

	// Retry creating oracle client for 5 seconds
	var oracleClient client.OracleClient
	for i := 0; i < 5; i++ {
		oracleClient, err = client.NewOracleClient(
			ctx,
			logger,
			cfg.Account.ChainID,
			cfg.Keyring.Backend,
			cfg.Keyring.Dir,
			keyringPass,
			cfg.RPC.TMRPCEndpoint,
			rpcTimeout,
			cfg.Account.Address,
			cfg.Account.Validator,
			cfg.Account.FeeGranter,
			cfg.RPC.GRPCEndpoint,
			cfg.GasAdjustment,
			cfg.GasPrices,
		)
		if err != nil {
			// sleep for a second before retrying
			time.Sleep(1 * time.Second)
			continue
		}
		break
	}

	if err != nil {
		return fmt.Errorf("error creating oracle client: %w", err)
	}
	providerTimeout, err := time.ParseDuration(cfg.ProviderTimeout)
	if err != nil {
		return fmt.Errorf("failed to parse provider timeout: %w", err)
	}

	deviations := make(map[string]sdk.Dec, len(cfg.Deviations))
	for _, deviation := range cfg.Deviations {
		threshold, err := sdk.NewDecFromStr(deviation.Threshold)
		if err != nil {
			return err
		}
		deviations[deviation.Base] = threshold
	}

	endpoints := make(map[string]config.ProviderEndpoint, len(cfg.ProviderEndpoints))
	for _, endpoint := range cfg.ProviderEndpoints {
		endpoints[endpoint.Name] = endpoint
	}

	oracle := oracle.New(
		logger,
		oracleClient,
		cfg.CurrencyPairs,
		providerTimeout,
		deviations,
		endpoints,
		cfg.Healthchecks,
	)

	telemetryCfg := telemetry.Config{}
	err = mapstructure.Decode(cfg.Telemetry, &telemetryCfg)
	if err != nil {
		return err
	}
	metrics, err := telemetry.New(telemetryCfg)
	if err != nil {
		return err
	}

	if cfg.EnableServer {
		g.Go(func() error {
			// start the process that observes and publishes exchange prices
			return startPriceFeeder(ctx, logger, cfg, oracle, metrics)
		})
	}

	if cfg.EnableVoter {
		g.Go(func() error {
			// start the process that calculates oracle prices and votes
			return startPriceOracle(ctx, logger, oracle)
		})
	}

	// Block main process until all spawned goroutines have gracefully exited and
	// signal has been captured in the main process or if an error occurs.
	return g.Wait()
}

func getKeyringPassword() (string, error) {
	reader := bufio.NewReader(os.Stdin)

	pass := os.Getenv(envVariablePass)
	if pass == "" {
		return input.GetString("Enter keyring password", reader)
	}
	return pass, nil
}

// trapSignal will listen for any OS signal and invoke Done on the main
// WaitGroup allowing the main process to gracefully exit.
func trapSignal(cancel context.CancelFunc, logger zerolog.Logger) {
	sigCh := make(chan os.Signal, 1)

	signal.Notify(sigCh, syscall.SIGTERM)
	signal.Notify(sigCh, syscall.SIGINT)

	go func() {
		sig := <-sigCh
		logger.Info().Str("signal", sig.String()).Msg("caught signal; shutting down...")
		cancel()
	}()
}

func startPriceFeeder(
	ctx context.Context,
	logger zerolog.Logger,
	cfg config.Config,
	oracle *oracle.Oracle,
	metrics *telemetry.Metrics,
) error {
	rtr := mux.NewRouter()
	v1Router := v1.New(logger, cfg, oracle, metrics)
	v1Router.RegisterRoutes(rtr, v1.APIPathPrefix)

	writeTimeout, err := time.ParseDuration(cfg.Server.WriteTimeout)
	if err != nil {
		return err
	}
	readTimeout, err := time.ParseDuration(cfg.Server.ReadTimeout)
	if err != nil {
		return err
	}

	srvErrCh := make(chan error, 1)
	srv := &http.Server{
		Handler:           rtr,
		Addr:              cfg.Server.ListenAddr,
		WriteTimeout:      writeTimeout,
		ReadTimeout:       readTimeout,
		ReadHeaderTimeout: readTimeout,
	}

	go func() {
		logger.Info().Str("listen_addr", cfg.Server.ListenAddr).Msg("starting price-feeder server...")
		srvErrCh <- srv.ListenAndServe()
	}()

	for {
		select {
		case <-ctx.Done():
			shutdownCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
			defer cancel()

			logger.Info().Str("listen_addr", cfg.Server.ListenAddr).Msg("shutting down price-feeder server...")
			if err := srv.Shutdown(shutdownCtx); err != nil {
				logger.Error().Err(err).Msg("failed to gracefully shutdown price-feeder server")
				return err
			}

			return nil

		case err := <-srvErrCh:
			logger.Error().Err(err).Msg("failed to start price-feeder server")
			return err
		}
	}
}

func startPriceOracle(ctx context.Context, logger zerolog.Logger, oracle *oracle.Oracle) error {
	srvErrCh := make(chan error, 1)

	go func() {
		logger.Info().Msg("starting price-feeder oracle...")
		srvErrCh <- oracle.Start(ctx)
	}()

	for {
		select {
		case <-ctx.Done():
			logger.Info().Msg("shutting down price-feeder oracle...")
			return nil

		case err := <-srvErrCh:
			logger.Err(err).Msg("error starting the price-feeder oracle")
			oracle.Stop()
			return err
		}
	}
}
