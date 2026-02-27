package network

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/codec"
	tmtime "github.com/sei-protocol/sei-chain/sei-cosmos/std"
	"github.com/sei-protocol/sei-chain/sei-cosmos/telemetry"
	tmos "github.com/sei-protocol/sei-chain/sei-tendermint/libs/os"
	"github.com/sei-protocol/sei-chain/sei-tendermint/node"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/client/local"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
	"go.opentelemetry.io/otel/sdk/trace"

	"github.com/sei-protocol/sei-chain/sei-cosmos/server/api"
	servergrpc "github.com/sei-protocol/sei-chain/sei-cosmos/server/grpc"
	srvtypes "github.com/sei-protocol/sei-chain/sei-cosmos/server/types"
	authtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/types"
	banktypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/bank/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/genutil"
	genutiltypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/genutil/types"
)

func startInProcess(cfg Config, val *Validator) error {
	logger := val.Ctx.Logger
	tmCfg := val.Ctx.Config
	tmCfg.Instrumentation.Prometheus = false

	if err := val.AppConfig.ValidateBasic(tmCfg); err != nil {
		return err
	}

	app := cfg.AppConstructor(*val)

	defaultGensis, err := types.GenesisDocFromFile(tmCfg.GenesisFile())
	if err != nil {
		return err
	}
	tmPubKey, err := codec.ToTmPubKeyInterface(val.PubKey)
	if err != nil {
		return err
	}
	defaultGensis.Validators = []types.GenesisValidator{
		{
			PubKey:  tmPubKey,
			Address: tmPubKey.Address(),
			Name:    val.Moniker,
			Power:   100,
		},
	}
	tmNode, err := node.New(
		val.GoCtx,
		tmCfg,
		logger,
		func() {},
		app,
		defaultGensis,
		[]trace.TracerProviderOption{},
		node.NoOpMetricsProvider(),
	)

	if err != nil {
		return err
	}

	if err := tmNode.Start(val.GoCtx); err != nil {
		return err
	}

	val.tmNode = tmNode

	if val.RPCAddress != "" {
		localClient, _ := local.New(logger, tmNode.(local.NodeService))
		val.RPCClient = localClient
	}

	// We'll need a RPC client if the validator exposes a gRPC or REST endpoint.
	if val.APIAddress != "" || val.AppConfig.GRPC.Enable {
		val.ClientCtx = val.ClientCtx.
			WithClient(val.RPCClient)

		// Add the tx service in the gRPC router.
		app.RegisterTxService(val.ClientCtx)

		// Add the tendermint queries service in the gRPC router.
		app.RegisterTendermintService(val.ClientCtx)
	}

	if val.APIAddress != "" {
		apiSrv := api.New(val.ClientCtx, logger.With("module", "api-server"))
		app.RegisterAPIRoutes(apiSrv, val.AppConfig.API)

		errCh := make(chan error)

		go func() {
			if err := apiSrv.Start(*val.AppConfig, &telemetry.Metrics{}); err != nil {
				errCh <- err
			}
		}()

		select {
		case err := <-errCh:
			return err
		case <-time.After(srvtypes.ServerStartTime): // assume server started successfully
		}

		val.api = apiSrv
	}

	if val.AppConfig.GRPC.Enable {
		grpcSrv, err := servergrpc.StartGRPCServer(val.ClientCtx, app, val.AppConfig.GRPC.Address)
		if err != nil {
			return err
		}

		val.grpc = grpcSrv

		if val.AppConfig.GRPCWeb.Enable {
			val.grpcWeb, err = servergrpc.StartGRPCWeb(grpcSrv, *val.AppConfig)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func collectGenFiles(cfg Config, vals []*Validator, outputDir string) error {
	genTime := tmtime.Now()

	for i := 0; i < cfg.NumValidators; i++ {
		tmCfg := vals[i].Ctx.Config

		nodeDir := filepath.Join(outputDir, vals[i].Moniker, "simd")
		gentxsDir := filepath.Join(outputDir, "gentxs")

		tmCfg.Moniker = vals[i].Moniker
		tmCfg.SetRoot(nodeDir)

		initCfg := genutiltypes.NewInitConfig(cfg.ChainID, gentxsDir, vals[i].NodeID, vals[i].PubKey)

		genFile := tmCfg.GenesisFile()
		genDoc, err := types.GenesisDocFromFile(genFile)
		if err != nil {
			return err
		}

		appState, err := genutil.GenAppStateFromConfig(cfg.Codec, cfg.TxConfig,
			tmCfg, initCfg, *genDoc, banktypes.GenesisBalancesIterator{})
		if err != nil {
			return err
		}

		// overwrite each validator's genesis file to have a canonical genesis time
		if err := genutil.ExportGenesisFileWithTime(genFile, cfg.ChainID, nil, appState, genTime); err != nil {
			return err
		}
	}

	return nil
}

func initGenFiles(cfg Config, genAccounts []authtypes.GenesisAccount, genBalances []banktypes.Balance, genFiles []string) error {

	// set the accounts in the genesis state
	var authGenState authtypes.GenesisState
	cfg.Codec.MustUnmarshalJSON(cfg.GenesisState[authtypes.ModuleName], &authGenState)

	accounts, err := authtypes.PackAccounts(genAccounts)
	if err != nil {
		return err
	}

	authGenState.Accounts = append(authGenState.Accounts, accounts...)
	cfg.GenesisState[authtypes.ModuleName] = cfg.Codec.MustMarshalJSON(&authGenState)

	// set the balances in the genesis state
	var bankGenState banktypes.GenesisState
	cfg.Codec.MustUnmarshalJSON(cfg.GenesisState[banktypes.ModuleName], &bankGenState)

	bankGenState.Balances = append(bankGenState.Balances, genBalances...)
	cfg.GenesisState[banktypes.ModuleName] = cfg.Codec.MustMarshalJSON(&bankGenState)

	appGenStateJSON, err := json.MarshalIndent(cfg.GenesisState, "", "  ")
	if err != nil {
		return err
	}

	genDoc := types.GenesisDoc{
		ChainID:    cfg.ChainID,
		AppState:   appGenStateJSON,
		Validators: nil,
	}

	// generate empty genesis files for each validator and save
	for i := 0; i < cfg.NumValidators; i++ {
		if err := genDoc.SaveAs(genFiles[i]); err != nil {
			return err
		}
	}

	return nil
}

func writeFile(name string, dir string, contents []byte) error {
	writePath := filepath.Join(dir)
	file := filepath.Join(writePath, name)

	if err := tmos.EnsureDir(writePath, 0750); err != nil {
		return err
	}

	return os.WriteFile(file, contents, 0600)
}
