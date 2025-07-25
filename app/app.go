package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	wasmkeeper "github.com/CosmWasm/wasmd/x/wasm/keeper"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/grpc/tmservice"
	"github.com/cosmos/cosmos-sdk/client/rpc"
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/codec/types"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	"github.com/cosmos/cosmos-sdk/server"
	"github.com/cosmos/cosmos-sdk/server/api"
	"github.com/cosmos/cosmos-sdk/server/config"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	"github.com/cosmos/cosmos-sdk/simapp"
	storetypes "github.com/cosmos/cosmos-sdk/store/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkacltypes "github.com/cosmos/cosmos-sdk/types/accesscontrol"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	genesistypes "github.com/cosmos/cosmos-sdk/types/genesis"
	"github.com/cosmos/cosmos-sdk/types/module"
	"github.com/cosmos/cosmos-sdk/version"
	aclmodule "github.com/cosmos/cosmos-sdk/x/accesscontrol"
	aclclient "github.com/cosmos/cosmos-sdk/x/accesscontrol/client"
	aclconstants "github.com/cosmos/cosmos-sdk/x/accesscontrol/constants"
	aclkeeper "github.com/cosmos/cosmos-sdk/x/accesscontrol/keeper"
	acltypes "github.com/cosmos/cosmos-sdk/x/accesscontrol/types"
	"github.com/cosmos/cosmos-sdk/x/auth"
	"github.com/cosmos/cosmos-sdk/x/auth/ante"
	authrest "github.com/cosmos/cosmos-sdk/x/auth/client/rest"
	authkeeper "github.com/cosmos/cosmos-sdk/x/auth/keeper"
	authsims "github.com/cosmos/cosmos-sdk/x/auth/simulation"
	authtx "github.com/cosmos/cosmos-sdk/x/auth/tx"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/cosmos/cosmos-sdk/x/auth/vesting"
	vestingtypes "github.com/cosmos/cosmos-sdk/x/auth/vesting/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
	authzkeeper "github.com/cosmos/cosmos-sdk/x/authz/keeper"
	authzmodule "github.com/cosmos/cosmos-sdk/x/authz/module"
	"github.com/cosmos/cosmos-sdk/x/bank"
	bankkeeper "github.com/cosmos/cosmos-sdk/x/bank/keeper"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/cosmos/cosmos-sdk/x/capability"
	capabilitykeeper "github.com/cosmos/cosmos-sdk/x/capability/keeper"
	capabilitytypes "github.com/cosmos/cosmos-sdk/x/capability/types"
	"github.com/cosmos/cosmos-sdk/x/crisis"
	crisiskeeper "github.com/cosmos/cosmos-sdk/x/crisis/keeper"
	crisistypes "github.com/cosmos/cosmos-sdk/x/crisis/types"
	distr "github.com/cosmos/cosmos-sdk/x/distribution"
	distrclient "github.com/cosmos/cosmos-sdk/x/distribution/client"
	distrkeeper "github.com/cosmos/cosmos-sdk/x/distribution/keeper"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	"github.com/cosmos/cosmos-sdk/x/evidence"
	evidencekeeper "github.com/cosmos/cosmos-sdk/x/evidence/keeper"
	evidencetypes "github.com/cosmos/cosmos-sdk/x/evidence/types"
	"github.com/cosmos/cosmos-sdk/x/feegrant"
	feegrantkeeper "github.com/cosmos/cosmos-sdk/x/feegrant/keeper"
	feegrantmodule "github.com/cosmos/cosmos-sdk/x/feegrant/module"
	"github.com/cosmos/cosmos-sdk/x/genutil"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"
	"github.com/cosmos/cosmos-sdk/x/gov"
	govclient "github.com/cosmos/cosmos-sdk/x/gov/client"
	govkeeper "github.com/cosmos/cosmos-sdk/x/gov/keeper"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	"github.com/cosmos/cosmos-sdk/x/params"
	paramsclient "github.com/cosmos/cosmos-sdk/x/params/client"
	paramskeeper "github.com/cosmos/cosmos-sdk/x/params/keeper"
	paramstypes "github.com/cosmos/cosmos-sdk/x/params/types"
	paramproposal "github.com/cosmos/cosmos-sdk/x/params/types/proposal"
	"github.com/cosmos/cosmos-sdk/x/slashing"
	slashingkeeper "github.com/cosmos/cosmos-sdk/x/slashing/keeper"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	"github.com/cosmos/cosmos-sdk/x/staking"
	stakingkeeper "github.com/cosmos/cosmos-sdk/x/staking/keeper"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/cosmos/cosmos-sdk/x/upgrade"
	upgradeclient "github.com/cosmos/cosmos-sdk/x/upgrade/client"
	upgradekeeper "github.com/cosmos/cosmos-sdk/x/upgrade/keeper"
	upgradetypes "github.com/cosmos/cosmos-sdk/x/upgrade/types"
	"github.com/cosmos/ibc-go/v3/modules/apps/transfer"
	ibctransferkeeper "github.com/cosmos/ibc-go/v3/modules/apps/transfer/keeper"
	ibctransfertypes "github.com/cosmos/ibc-go/v3/modules/apps/transfer/types"
	ibc "github.com/cosmos/ibc-go/v3/modules/core"
	ibcclient "github.com/cosmos/ibc-go/v3/modules/core/02-client"
	ibcclientclient "github.com/cosmos/ibc-go/v3/modules/core/02-client/client"
	ibcclienttypes "github.com/cosmos/ibc-go/v3/modules/core/02-client/types"
	ibcporttypes "github.com/cosmos/ibc-go/v3/modules/core/05-port/types"
	ibchost "github.com/cosmos/ibc-go/v3/modules/core/24-host"
	ibckeeper "github.com/cosmos/ibc-go/v3/modules/core/keeper"
	"github.com/ethereum/go-ethereum/ethclient"
	ethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/gorilla/mux"
	"github.com/rakyll/statik/fs"
	"github.com/sei-protocol/sei-chain/aclmapping"
	aclutils "github.com/sei-protocol/sei-chain/aclmapping/utils"
	"github.com/sei-protocol/sei-chain/app/antedecorators"
	appparams "github.com/sei-protocol/sei-chain/app/params"
	"github.com/sei-protocol/sei-chain/app/upgrades"
	v0upgrade "github.com/sei-protocol/sei-chain/app/upgrades/v0"
	"github.com/sei-protocol/sei-chain/evmrpc"
	"github.com/sei-protocol/sei-chain/precompiles"
	putils "github.com/sei-protocol/sei-chain/precompiles/utils"
	"github.com/sei-protocol/sei-chain/utils"
	"github.com/sei-protocol/sei-chain/utils/metrics"
	"github.com/sei-protocol/sei-chain/wasmbinding"
	epochmodule "github.com/sei-protocol/sei-chain/x/epoch"
	epochmodulekeeper "github.com/sei-protocol/sei-chain/x/epoch/keeper"
	epochmoduletypes "github.com/sei-protocol/sei-chain/x/epoch/types"
	"github.com/sei-protocol/sei-chain/x/evm"
	evmante "github.com/sei-protocol/sei-chain/x/evm/ante"
	"github.com/sei-protocol/sei-chain/x/evm/blocktest"
	evmkeeper "github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/querier"
	"github.com/sei-protocol/sei-chain/x/evm/replay"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/sei-protocol/sei-chain/x/mint"
	mintclient "github.com/sei-protocol/sei-chain/x/mint/client/cli"
	mintkeeper "github.com/sei-protocol/sei-chain/x/mint/keeper"
	minttypes "github.com/sei-protocol/sei-chain/x/mint/types"
	oraclemodule "github.com/sei-protocol/sei-chain/x/oracle"
	oraclekeeper "github.com/sei-protocol/sei-chain/x/oracle/keeper"
	oracletypes "github.com/sei-protocol/sei-chain/x/oracle/types"
	tokenfactorymodule "github.com/sei-protocol/sei-chain/x/tokenfactory"
	tokenfactorykeeper "github.com/sei-protocol/sei-chain/x/tokenfactory/keeper"
	tokenfactorytypes "github.com/sei-protocol/sei-chain/x/tokenfactory/types"
	"github.com/sei-protocol/sei-db/ss"
	seidb "github.com/sei-protocol/sei-db/ss/types"
	"github.com/spf13/cast"
	abci "github.com/tendermint/tendermint/abci/types"
	tmcfg "github.com/tendermint/tendermint/config"
	"github.com/tendermint/tendermint/libs/log"
	tmos "github.com/tendermint/tendermint/libs/os"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	dbm "github.com/tendermint/tm-db"

	// this line is used by starport scaffolding # stargate/app/moduleImport

	"github.com/CosmWasm/wasmd/x/wasm"
	wasmclient "github.com/CosmWasm/wasmd/x/wasm/client"
	wasmtypes "github.com/CosmWasm/wasmd/x/wasm/types"

	// unnamed import of statik for openapi/swagger UI support
	_ "github.com/sei-protocol/sei-chain/docs/swagger"
	ssconfig "github.com/sei-protocol/sei-db/config"
)

// this line is used by starport scaffolding # stargate/wasm/app/enabledProposals
func getGovProposalHandlers() []govclient.ProposalHandler {
	govProposalHandlers := append(wasmclient.ProposalHandlers, //nolint:gocritic // ignore: appending to a slice is OK
		paramsclient.ProposalHandler,
		distrclient.ProposalHandler,
		upgradeclient.ProposalHandler,
		upgradeclient.CancelProposalHandler,
		ibcclientclient.UpdateClientProposalHandler,
		ibcclientclient.UpgradeProposalHandler,
		aclclient.ResourceDependencyProposalHandler,
		mintclient.UpdateMinterHandler,
		// this line is used by starport scaffolding # stargate/app/govProposalHandler
	)

	return govProposalHandlers
}

var (
	// DefaultNodeHome default home directories for the application daemon
	DefaultNodeHome string

	// ModuleBasics defines the module BasicManager is in charge of setting up basic,
	// non-dependant module elements, such as codec registration
	// and genesis verification.
	ModuleBasics = module.NewBasicManager(
		aclmodule.AppModuleBasic{},
		auth.AppModuleBasic{},
		authzmodule.AppModuleBasic{},
		genutil.AppModuleBasic{},
		bank.AppModuleBasic{},
		capability.AppModuleBasic{},
		staking.AppModuleBasic{},
		mint.AppModuleBasic{},
		distr.AppModuleBasic{},
		gov.NewAppModuleBasic(getGovProposalHandlers()...),
		params.AppModuleBasic{},
		crisis.AppModuleBasic{},
		slashing.AppModuleBasic{},
		feegrantmodule.AppModuleBasic{},
		ibc.AppModuleBasic{},
		upgrade.AppModuleBasic{},
		evidence.AppModuleBasic{},
		transfer.AppModuleBasic{},
		vesting.AppModuleBasic{},
		oraclemodule.AppModuleBasic{},
		evm.AppModuleBasic{},
		wasm.AppModuleBasic{},
		epochmodule.AppModuleBasic{},
		tokenfactorymodule.AppModuleBasic{},
		// this line is used by starport scaffolding # stargate/app/moduleBasic
	)

	// module account permissions
	maccPerms = map[string][]string{
		acltypes.ModuleName:            nil,
		authtypes.FeeCollectorName:     nil,
		distrtypes.ModuleName:          nil,
		minttypes.ModuleName:           {authtypes.Minter},
		stakingtypes.BondedPoolName:    {authtypes.Burner, authtypes.Staking},
		stakingtypes.NotBondedPoolName: {authtypes.Burner, authtypes.Staking},
		govtypes.ModuleName:            {authtypes.Burner},
		ibctransfertypes.ModuleName:    {authtypes.Minter, authtypes.Burner},
		oracletypes.ModuleName:         nil,
		wasm.ModuleName:                {authtypes.Burner},
		evmtypes.ModuleName:            {authtypes.Minter, authtypes.Burner},
		tokenfactorytypes.ModuleName:   {authtypes.Minter, authtypes.Burner},
		// this line is used by starport scaffolding # stargate/app/maccPerms
	}

	allowedReceivingModAcc = map[string]bool{
		oracletypes.ModuleName: true,
	}

	// WasmProposalsEnabled enables all x/wasm proposals when it's value is "true"
	// and EnableSpecificWasmProposals is empty. Otherwise, all x/wasm proposals
	// are disabled.
	// Used as a flag to turn it on and off
	WasmProposalsEnabled = "true"

	// EnableSpecificWasmProposals, if set, must be comma-separated list of values
	// that are all a subset of "EnableAllProposals", which takes precedence over
	// WasmProposalsEnabled.
	//
	// See: https://github.com/CosmWasm/wasmd/blob/02a54d33ff2c064f3539ae12d75d027d9c665f05/x/wasm/internal/types/proposal.go#L28-L34
	EnableSpecificWasmProposals = ""

	// EmptyWasmOpts defines a type alias for a list of wasm options.
	EmptyWasmOpts []wasm.Option

	// Boolean to only emit seid version and git commit metric once per chain initialization
	EmittedSeidVersionMetric = false
	// EmptyAclmOpts defines a type alias for a list of wasm options.
	EmptyACLOpts []aclkeeper.Option
	// EnableOCC allows tests to override default OCC enablement behavior
	EnableOCC       = true
	EmptyAppOptions []AppOption
)

var (
	_ servertypes.Application = (*App)(nil)
	_ simapp.App              = (*App)(nil)
)

const (
	MinGasEVMTx = 21000
)

func init() {
	userHomeDir, err := os.UserHomeDir()
	if err != nil {
		panic(err)
	}

	DefaultNodeHome = filepath.Join(userHomeDir, "."+AppName)
}

// GetWasmEnabledProposals parses the WasmProposalsEnabled and
// EnableSpecificWasmProposals values to produce a list of enabled proposals to
// pass into the application.
func GetWasmEnabledProposals() []wasm.ProposalType {
	if EnableSpecificWasmProposals == "" {
		if WasmProposalsEnabled == "true" {
			return wasm.EnableAllProposals
		}

		return wasm.DisableAllProposals
	}

	chunks := strings.Split(EnableSpecificWasmProposals, ",")

	proposals, err := wasm.ConvertToProposals(chunks)
	if err != nil {
		panic(err)
	}

	return proposals
}

// App extends an ABCI application, but with most of its parameters exported.
// They are exported for convenience in creating helper functions, as object
// capabilities aren't needed for testing.
type App struct {
	*baseapp.BaseApp

	cdc               *codec.LegacyAmino
	appCodec          codec.Codec
	interfaceRegistry types.InterfaceRegistry

	invCheckPeriod uint

	// keys to access the substores
	keys    map[string]*sdk.KVStoreKey
	tkeys   map[string]*sdk.TransientStoreKey
	memKeys map[string]*sdk.MemoryStoreKey

	// keepers
	AccessControlKeeper aclkeeper.Keeper
	AccountKeeper       authkeeper.AccountKeeper
	AuthzKeeper         authzkeeper.Keeper
	BankKeeper          bankkeeper.Keeper
	CapabilityKeeper    *capabilitykeeper.Keeper
	StakingKeeper       stakingkeeper.Keeper
	SlashingKeeper      slashingkeeper.Keeper
	MintKeeper          mintkeeper.Keeper
	DistrKeeper         distrkeeper.Keeper
	GovKeeper           govkeeper.Keeper
	CrisisKeeper        crisiskeeper.Keeper
	UpgradeKeeper       upgradekeeper.Keeper
	ParamsKeeper        paramskeeper.Keeper
	IBCKeeper           *ibckeeper.Keeper // IBC Keeper must be a pointer in the app, so we can SetRouter on it correctly
	EvidenceKeeper      evidencekeeper.Keeper
	TransferKeeper      ibctransferkeeper.Keeper
	FeeGrantKeeper      feegrantkeeper.Keeper
	WasmKeeper          wasm.Keeper
	OracleKeeper        oraclekeeper.Keeper
	EvmKeeper           evmkeeper.Keeper

	// make scoped keepers public for test purposes
	ScopedIBCKeeper      capabilitykeeper.ScopedKeeper
	ScopedTransferKeeper capabilitykeeper.ScopedKeeper
	ScopedWasmKeeper     capabilitykeeper.ScopedKeeper

	EpochKeeper epochmodulekeeper.Keeper

	TokenFactoryKeeper tokenfactorykeeper.Keeper

	// mm is the module manager
	mm *module.Manager

	// sm is the simulation manager
	sm *module.SimulationManager

	configurator module.Configurator

	optimisticProcessingInfo *OptimisticProcessingInfo

	// batchVerifier *ante.SR25519BatchVerifier
	txDecoder         sdk.TxDecoder
	AnteHandler       sdk.AnteHandler
	TracerAnteHandler sdk.AnteHandler

	versionInfo version.Info

	// Stores mapping counter name to counter value
	metricCounter *map[string]float32

	mounter func()

	HardForkManager *upgrades.HardForkManager

	encodingConfig        appparams.EncodingConfig
	legacyEncodingConfig  appparams.EncodingConfig
	evmRPCConfig          evmrpc.Config
	lightInvarianceConfig LightInvarianceConfig

	genesisImportConfig genesistypes.GenesisImportConfig

	stateStore   seidb.StateStore
	receiptStore seidb.StateStore

	forkInitializer func(sdk.Context)

	httpServerStartSignal     chan struct{}
	wsServerStartSignal       chan struct{}
	httpServerStartSignalSent bool
	wsServerStartSignalSent   bool
}

type AppOption func(*App)

// New returns a reference to an initialized blockchain app
func New(
	logger log.Logger,
	db dbm.DB,
	traceStore io.Writer,
	_ bool,
	skipUpgradeHeights map[int64]bool,
	homePath string,
	invCheckPeriod uint,
	enableCustomEVMPrecompiles bool,
	tmConfig *tmcfg.Config,
	encodingConfig appparams.EncodingConfig,
	enabledProposals []wasm.ProposalType,
	appOpts servertypes.AppOptions,
	wasmOpts []wasm.Option,
	aclOpts []aclkeeper.Option,
	appOptions []AppOption,
	baseAppOptions ...func(*baseapp.BaseApp),
) *App {
	appCodec := encodingConfig.Marshaler
	cdc := encodingConfig.Amino
	interfaceRegistry := encodingConfig.InterfaceRegistry

	bAppOptions, stateStore := SetupSeiDB(logger, homePath, appOpts, baseAppOptions)

	bApp := baseapp.NewBaseApp(AppName, logger, db, encodingConfig.TxConfig.TxDecoder(), tmConfig, appOpts, bAppOptions...)
	bApp.SetCommitMultiStoreTracer(traceStore)
	bApp.SetVersion(version.Version)
	bApp.SetInterfaceRegistry(interfaceRegistry)

	keys := sdk.NewKVStoreKeys(
		acltypes.StoreKey, authtypes.StoreKey, authzkeeper.StoreKey, banktypes.StoreKey, stakingtypes.StoreKey,
		minttypes.StoreKey, distrtypes.StoreKey, slashingtypes.StoreKey,
		govtypes.StoreKey, paramstypes.StoreKey, ibchost.StoreKey, upgradetypes.StoreKey, feegrant.StoreKey,
		evidencetypes.StoreKey, ibctransfertypes.StoreKey, capabilitytypes.StoreKey, oracletypes.StoreKey,
		evmtypes.StoreKey, wasm.StoreKey,
		epochmoduletypes.StoreKey,
		tokenfactorytypes.StoreKey,
		// this line is used by starport scaffolding # stargate/app/storeKey
	)
	tkeys := sdk.NewTransientStoreKeys(paramstypes.TStoreKey, evmtypes.TransientStoreKey)
	memKeys := sdk.NewMemoryStoreKeys(capabilitytypes.MemStoreKey, banktypes.DeferredCacheStoreKey, oracletypes.MemStoreKey)

	app := &App{
		BaseApp:               bApp,
		cdc:                   cdc,
		appCodec:              appCodec,
		interfaceRegistry:     interfaceRegistry,
		invCheckPeriod:        invCheckPeriod,
		keys:                  keys,
		tkeys:                 tkeys,
		memKeys:               memKeys,
		txDecoder:             encodingConfig.TxConfig.TxDecoder(),
		versionInfo:           version.NewInfo(),
		metricCounter:         &map[string]float32{},
		encodingConfig:        encodingConfig,
		legacyEncodingConfig:  MakeLegacyEncodingConfig(),
		stateStore:            stateStore,
		httpServerStartSignal: make(chan struct{}, 1),
		wsServerStartSignal:   make(chan struct{}, 1),
	}

	for _, option := range appOptions {
		option(app)
	}

	app.ParamsKeeper = initParamsKeeper(appCodec, cdc, keys[paramstypes.StoreKey], tkeys[paramstypes.TStoreKey])

	// set the BaseApp's parameter store
	bApp.SetParamStore(app.ParamsKeeper.Subspace(baseapp.Paramspace).WithKeyTable(paramskeeper.ConsensusParamsKeyTable()))

	// add capability keeper and ScopeToModule for ibc module
	app.CapabilityKeeper = capabilitykeeper.NewKeeper(appCodec, keys[capabilitytypes.StoreKey], memKeys[capabilitytypes.MemStoreKey])

	// grant capabilities for the ibc and ibc-transfer modules
	scopedIBCKeeper := app.CapabilityKeeper.ScopeToModule(ibchost.ModuleName)
	scopedTransferKeeper := app.CapabilityKeeper.ScopeToModule(ibctransfertypes.ModuleName)
	scopedWasmKeeper := app.CapabilityKeeper.ScopeToModule(wasm.ModuleName)
	// this line is used by starport scaffolding # stargate/app/scopedKeeper

	// add keepers
	app.AccountKeeper = authkeeper.NewAccountKeeper(
		appCodec, keys[authtypes.StoreKey], app.GetSubspace(authtypes.ModuleName), authtypes.ProtoBaseAccount, maccPerms,
	)
	app.BankKeeper = bankkeeper.NewBaseKeeperWithDeferredCache(
		appCodec, keys[banktypes.StoreKey], app.AccountKeeper, app.GetSubspace(banktypes.ModuleName), app.ModuleAccountAddrs(), memKeys[banktypes.DeferredCacheStoreKey],
	)
	stakingKeeper := stakingkeeper.NewKeeper(
		appCodec, keys[stakingtypes.StoreKey], app.AccountKeeper, app.BankKeeper, app.GetSubspace(stakingtypes.ModuleName),
	)
	app.AuthzKeeper = authzkeeper.NewKeeper(keys[authzkeeper.StoreKey], appCodec, app.MsgServiceRouter())
	app.MintKeeper = mintkeeper.NewKeeper(
		appCodec, keys[minttypes.StoreKey], app.GetSubspace(minttypes.ModuleName), &stakingKeeper,
		app.AccountKeeper, app.BankKeeper, app.EpochKeeper, authtypes.FeeCollectorName,
	)
	app.DistrKeeper = distrkeeper.NewKeeper(
		appCodec, keys[distrtypes.StoreKey], app.GetSubspace(distrtypes.ModuleName), app.AccountKeeper, app.BankKeeper,
		&stakingKeeper, authtypes.FeeCollectorName, app.ModuleAccountAddrs(),
	)
	app.SlashingKeeper = slashingkeeper.NewKeeper(
		appCodec, keys[slashingtypes.StoreKey], &stakingKeeper, app.GetSubspace(slashingtypes.ModuleName),
	)
	app.CrisisKeeper = crisiskeeper.NewKeeper(
		app.GetSubspace(crisistypes.ModuleName), invCheckPeriod, app.BankKeeper, authtypes.FeeCollectorName,
	)

	app.FeeGrantKeeper = feegrantkeeper.NewKeeper(appCodec, keys[feegrant.StoreKey], app.AccountKeeper)
	app.UpgradeKeeper = upgradekeeper.NewKeeper(skipUpgradeHeights, keys[upgradetypes.StoreKey], appCodec, homePath, app.BaseApp)

	// register the staking hooks
	// NOTE: stakingKeeper above is passed by reference, so that it will contain these hooks
	app.StakingKeeper = *stakingKeeper.SetHooks(
		stakingtypes.NewMultiStakingHooks(app.DistrKeeper.Hooks(), app.SlashingKeeper.Hooks()),
	)

	// ... other modules keepers

	// Create IBC Keeper
	app.IBCKeeper = ibckeeper.NewKeeper(
		appCodec, keys[ibchost.StoreKey], app.GetSubspace(ibchost.ModuleName), app.StakingKeeper, app.UpgradeKeeper, scopedIBCKeeper,
	)

	// Create Transfer Keepers
	app.TransferKeeper = ibctransferkeeper.NewKeeperWithAddressHandler(
		appCodec,
		keys[ibctransfertypes.StoreKey],
		app.GetSubspace(ibctransfertypes.ModuleName),
		app.IBCKeeper.ChannelKeeper,
		app.IBCKeeper.ChannelKeeper,
		&app.IBCKeeper.PortKeeper,
		app.AccountKeeper,
		app.BankKeeper,
		scopedTransferKeeper,
		evmkeeper.NewEvmAddressHandler(&app.EvmKeeper),
	)
	transferModule := transfer.NewAppModule(app.TransferKeeper)
	transferIBCModule := transfer.NewIBCModule(app.TransferKeeper)

	// Create evidence Keeper for to register the IBC light client misbehaviour evidence route
	evidenceKeeper := evidencekeeper.NewKeeper(
		appCodec, keys[evidencetypes.StoreKey], &app.StakingKeeper, app.SlashingKeeper,
	)
	// If evidence needs to be handled for the app, set routes in router here and seal
	app.EvidenceKeeper = *evidenceKeeper

	app.OracleKeeper = oraclekeeper.NewKeeper(
		appCodec, keys[oracletypes.StoreKey], memKeys[oracletypes.MemStoreKey], app.GetSubspace(oracletypes.ModuleName),
		app.AccountKeeper, app.BankKeeper, app.DistrKeeper, &stakingKeeper, distrtypes.ModuleName,
	)

	wasmDir := filepath.Join(homePath, "wasm")
	wasmConfig, err := wasm.ReadWasmConfig(appOpts)
	if err != nil {
		panic(fmt.Sprintf("error while reading wasm config: %s", err))
	}

	app.EpochKeeper = *epochmodulekeeper.NewKeeper(
		appCodec,
		keys[epochmoduletypes.StoreKey],
		keys[epochmoduletypes.MemStoreKey],
		app.GetSubspace(epochmoduletypes.ModuleName),
	).SetHooks(epochmoduletypes.NewMultiEpochHooks(
		app.MintKeeper.Hooks()))

	app.TokenFactoryKeeper = tokenfactorykeeper.NewKeeper(
		appCodec,
		app.keys[tokenfactorytypes.StoreKey],
		app.GetSubspace(tokenfactorytypes.ModuleName),
		app.AccountKeeper,
		app.BankKeeper.(bankkeeper.BaseKeeper).WithMintCoinsRestriction(tokenfactorytypes.NewTokenFactoryDenomMintCoinsRestriction()),
		app.DistrKeeper,
	)

	// The last arguments can contain custom message handlers, and custom query handlers,
	// if we want to allow any custom callbacks
	supportedFeatures := "iterator,staking,stargate,sei"
	wasmOpts = append(
		wasmbinding.RegisterCustomPlugins(
			&app.OracleKeeper,
			&app.EpochKeeper,
			&app.TokenFactoryKeeper,
			&app.AccountKeeper,
			app.MsgServiceRouter(),
			app.IBCKeeper.ChannelKeeper,
			scopedWasmKeeper,
			app.BankKeeper,
			appCodec,
			app.TransferKeeper,
			app.AccessControlKeeper,
			&app.EvmKeeper,
			app.StakingKeeper,
		),
		wasmOpts...,
	)
	app.WasmKeeper = wasm.NewKeeper(
		appCodec,
		keys[wasm.StoreKey],
		app.ParamsKeeper,
		app.GetSubspace(wasm.ModuleName),
		app.AccountKeeper,
		app.BankKeeper,
		app.StakingKeeper,
		app.DistrKeeper,
		app.IBCKeeper.ChannelKeeper,
		&app.IBCKeeper.PortKeeper,
		scopedWasmKeeper,
		app.UpgradeKeeper,
		app.TransferKeeper,
		app.MsgServiceRouter(),
		app.GRPCQueryRouter(),
		wasmDir,
		wasmConfig,
		supportedFeatures,
		wasmOpts...,
	)

	receiptStorePath := filepath.Join(homePath, "data", "receipt.db")
	ssConfig := ssconfig.DefaultStateStoreConfig()
	ssConfig.DedicatedChangelog = true
	ssConfig.KeepRecent = cast.ToInt(appOpts.Get(server.FlagMinRetainBlocks))
	ssConfig.DBDirectory = receiptStorePath
	ssConfig.KeepLastVersion = false
	if app.receiptStore == nil {
		app.receiptStore, err = ss.NewStateStore(logger, receiptStorePath, ssConfig)
		if err != nil {
			panic(fmt.Sprintf("error while creating receipt store: %s", err))
		}
	}
	app.EvmKeeper = *evmkeeper.NewKeeper(keys[evmtypes.StoreKey],
		tkeys[evmtypes.TransientStoreKey], app.GetSubspace(evmtypes.ModuleName), app.receiptStore, app.BankKeeper,
		&app.AccountKeeper, &app.StakingKeeper, app.TransferKeeper,
		wasmkeeper.NewDefaultPermissionKeeper(app.WasmKeeper), &app.WasmKeeper, &app.UpgradeKeeper)
	app.BankKeeper.RegisterRecipientChecker(app.EvmKeeper.CanAddressReceive)

	bApp.SetPreCommitHandler(app.HandlePreCommit)
	bApp.SetCloseHandler(app.HandleClose)

	app.evmRPCConfig, err = evmrpc.ReadConfig(appOpts)
	if err != nil {
		panic(fmt.Sprintf("error reading EVM config due to %s", err))
	}
	evmQueryConfig, err := querier.ReadConfig(appOpts)
	if err != nil {
		panic(fmt.Sprintf("error reading evm query config due to %s", err))
	}
	app.EvmKeeper.QueryConfig = &evmQueryConfig
	ethReplayConfig, err := replay.ReadConfig(appOpts)
	if err != nil {
		panic(fmt.Sprintf("error reading eth replay config due to %s", err))
	}
	app.EvmKeeper.EthReplayConfig = ethReplayConfig
	ethBlockTestConfig, err := blocktest.ReadConfig(appOpts)
	if err != nil {
		panic(fmt.Sprintf("error reading eth block test config due to %s", err))
	}
	app.EvmKeeper.EthBlockTestConfig = ethBlockTestConfig
	if ethReplayConfig.Enabled {
		rpcclient, err := ethrpc.Dial(ethReplayConfig.EthRPC)
		if err != nil {
			panic(fmt.Sprintf("error dialing %s due to %s", ethReplayConfig.EthRPC, err))
		}
		app.EvmKeeper.EthClient = ethclient.NewClient(rpcclient)
	}
	lightInvarianceConfig, err := ReadLightInvarianceConfig(appOpts)
	if err != nil {
		panic(fmt.Sprintf("error reading light invariance config due to %s", err))
	}
	app.lightInvarianceConfig = lightInvarianceConfig

	genesisImportConfig, err := ReadGenesisImportConfig(appOpts)
	if err != nil {
		panic(fmt.Sprintf("error reading genesis import config due to %s", err))
	}
	app.genesisImportConfig = genesisImportConfig

	customDependencyGenerators := aclmapping.NewCustomDependencyGenerator()
	aclOpts = append(aclOpts, aclkeeper.WithResourceTypeToStoreKeyMap(aclutils.ResourceTypeToStoreKeyMap))
	aclOpts = append(aclOpts, aclkeeper.WithDependencyGeneratorMappings(customDependencyGenerators.GetCustomDependencyGenerators(app.EvmKeeper)))
	app.AccessControlKeeper = aclkeeper.NewKeeper(
		appCodec,
		app.keys[acltypes.StoreKey],
		app.GetSubspace(acltypes.ModuleName),
		app.AccountKeeper,
		app.StakingKeeper,
		aclOpts...,
	)

	epochModule := epochmodule.NewAppModule(appCodec, app.EpochKeeper, app.AccountKeeper, app.BankKeeper)

	// register the proposal types
	govRouter := govtypes.NewRouter()
	govRouter.AddRoute(govtypes.RouterKey, govtypes.ProposalHandler).
		AddRoute(paramproposal.RouterKey, params.NewParamChangeProposalHandler(app.ParamsKeeper)).
		AddRoute(distrtypes.RouterKey, distr.NewCommunityPoolSpendProposalHandler(app.DistrKeeper)).
		AddRoute(upgradetypes.RouterKey, upgrade.NewSoftwareUpgradeProposalHandler(app.UpgradeKeeper)).
		AddRoute(ibcclienttypes.RouterKey, ibcclient.NewClientProposalHandler(app.IBCKeeper.ClientKeeper)).
		AddRoute(minttypes.RouterKey, mint.NewProposalHandler(app.MintKeeper)).
		AddRoute(tokenfactorytypes.RouterKey, tokenfactorymodule.NewProposalHandler(app.TokenFactoryKeeper)).
		AddRoute(acltypes.ModuleName, aclmodule.NewProposalHandler(app.AccessControlKeeper)).
		AddRoute(evmtypes.RouterKey, evm.NewProposalHandler(app.EvmKeeper))
	if len(enabledProposals) != 0 {
		govRouter.AddRoute(wasm.RouterKey, wasm.NewWasmProposalHandler(app.WasmKeeper, enabledProposals))
	}

	app.GovKeeper = govkeeper.NewKeeper(
		appCodec, keys[govtypes.StoreKey], app.GetSubspace(govtypes.ModuleName), app.AccountKeeper, app.BankKeeper,
		&stakingKeeper, app.ParamsKeeper, govRouter,
	)

	// this line is used by starport scaffolding # stargate/app/keeperDefinition

	// Create static IBC router, add transfer route, then set and seal it
	ibcRouter := ibcporttypes.NewRouter()
	ibcRouter.AddRoute(ibctransfertypes.ModuleName, transferIBCModule)
	ibcRouter.AddRoute(wasm.ModuleName, wasm.NewIBCHandler(app.WasmKeeper, app.IBCKeeper.ChannelKeeper))
	// this line is used by starport scaffolding # ibc/app/router
	app.IBCKeeper.SetRouter(ibcRouter)

	if enableCustomEVMPrecompiles {
		customPrecompiles := precompiles.GetCustomPrecompiles(LatestUpgrade, app.GetPrecompileKeepers())
		app.EvmKeeper.SetCustomPrecompiles(customPrecompiles, LatestUpgrade)
	}

	/****  Module Options ****/

	// NOTE: we may consider parsing `appOpts` inside module constructors. For the moment
	// we prefer to be more strict in what arguments the modules expect.
	skipGenesisInvariants := cast.ToBool(appOpts.Get(crisis.FlagSkipGenesisInvariants))

	// NOTE: Any module instantiated in the module manager that is later modified
	// must be passed by reference here.

	app.mm = module.NewManager(
		genutil.NewAppModule(
			app.AccountKeeper, app.StakingKeeper, app.BaseApp.DeliverTx,
			encodingConfig.TxConfig,
		),
		aclmodule.NewAppModule(appCodec, app.AccessControlKeeper),
		auth.NewAppModule(appCodec, app.AccountKeeper, nil),
		vesting.NewAppModule(app.AccountKeeper, app.BankKeeper),
		bank.NewAppModule(appCodec, app.BankKeeper, app.AccountKeeper),
		capability.NewAppModule(appCodec, *app.CapabilityKeeper),
		feegrantmodule.NewAppModule(appCodec, app.AccountKeeper, app.BankKeeper, app.FeeGrantKeeper, app.interfaceRegistry),
		crisis.NewAppModule(&app.CrisisKeeper, skipGenesisInvariants),
		gov.NewAppModule(appCodec, app.GovKeeper, app.AccountKeeper, app.BankKeeper),
		mint.NewAppModule(appCodec, app.MintKeeper, app.AccountKeeper),
		slashing.NewAppModule(appCodec, app.SlashingKeeper, app.AccountKeeper, app.BankKeeper, app.StakingKeeper),
		distr.NewAppModule(appCodec, app.DistrKeeper, app.AccountKeeper, app.BankKeeper, app.StakingKeeper),
		staking.NewAppModule(appCodec, app.StakingKeeper, app.AccountKeeper, app.BankKeeper),
		upgrade.NewAppModule(app.UpgradeKeeper),
		evidence.NewAppModule(app.EvidenceKeeper),
		ibc.NewAppModule(app.IBCKeeper),
		params.NewAppModule(app.ParamsKeeper),
		oraclemodule.NewAppModule(appCodec, app.OracleKeeper, app.AccountKeeper, app.BankKeeper),
		wasm.NewAppModule(appCodec, &app.WasmKeeper, app.StakingKeeper, app.AccountKeeper, app.BankKeeper),
		evm.NewAppModule(appCodec, &app.EvmKeeper),
		transferModule,
		epochModule,
		tokenfactorymodule.NewAppModule(app.TokenFactoryKeeper, app.AccountKeeper, app.BankKeeper),
		authzmodule.NewAppModule(appCodec, app.AuthzKeeper, app.AccountKeeper, app.BankKeeper, app.interfaceRegistry),
		// this line is used by starport scaffolding # stargate/app/appModule
	)

	// During begin block slashing happens after distr.BeginBlocker so that
	// there is nothing left over in the validator fee pool, so as to keep the
	// CanWithdrawInvariant invariant.
	// NOTE: staking module is required if HistoricalEntries param > 0
	app.mm.SetOrderBeginBlockers(
		epochmoduletypes.ModuleName,
		upgradetypes.ModuleName,
		capabilitytypes.ModuleName,
		minttypes.ModuleName,
		distrtypes.ModuleName,
		slashingtypes.ModuleName,
		evidencetypes.ModuleName,
		stakingtypes.ModuleName,
		authtypes.ModuleName,
		banktypes.ModuleName,
		govtypes.ModuleName,
		crisistypes.ModuleName,
		genutiltypes.ModuleName,
		authz.ModuleName,
		feegrant.ModuleName,
		paramstypes.ModuleName,
		vestingtypes.ModuleName,
		ibchost.ModuleName,
		ibctransfertypes.ModuleName,
		oracletypes.ModuleName,
		evmtypes.ModuleName,
		wasm.ModuleName,
		tokenfactorytypes.ModuleName,
		acltypes.ModuleName,
	)

	app.mm.SetOrderMidBlockers(
		oracletypes.ModuleName,
	)

	app.mm.SetOrderEndBlockers(
		crisistypes.ModuleName,
		govtypes.ModuleName,
		stakingtypes.ModuleName,
		capabilitytypes.ModuleName,
		authtypes.ModuleName,
		banktypes.ModuleName,
		distrtypes.ModuleName,
		slashingtypes.ModuleName,
		minttypes.ModuleName,
		genutiltypes.ModuleName,
		evidencetypes.ModuleName,
		authz.ModuleName,
		feegrant.ModuleName,
		paramstypes.ModuleName,
		upgradetypes.ModuleName,
		vestingtypes.ModuleName,
		ibchost.ModuleName,
		ibctransfertypes.ModuleName,
		oracletypes.ModuleName,
		epochmoduletypes.ModuleName,
		evmtypes.ModuleName,
		wasm.ModuleName,
		tokenfactorytypes.ModuleName,
		acltypes.ModuleName,
	)

	// NOTE: The genutils module must occur after staking so that pools are
	// properly initialized with tokens from genesis accounts.
	// NOTE: Capability module must occur first so that it can initialize any capabilities
	// so that other modules that want to create or claim capabilities afterwards in InitChain
	// can do so safely.
	app.mm.SetOrderInitGenesis(
		upgradetypes.ModuleName,
		paramstypes.ModuleName,
		capabilitytypes.ModuleName,
		authtypes.ModuleName,
		banktypes.ModuleName,
		distrtypes.ModuleName,
		stakingtypes.ModuleName,
		slashingtypes.ModuleName,
		govtypes.ModuleName,
		minttypes.ModuleName,
		vestingtypes.ModuleName,
		crisistypes.ModuleName,
		ibchost.ModuleName,
		genutiltypes.ModuleName,
		evidencetypes.ModuleName,
		ibctransfertypes.ModuleName,
		authz.ModuleName,
		feegrant.ModuleName,
		oracletypes.ModuleName,
		tokenfactorytypes.ModuleName,
		epochmoduletypes.ModuleName,
		wasm.ModuleName,
		evmtypes.ModuleName,
		acltypes.ModuleName,
		// this line is used by starport scaffolding # stargate/app/initGenesis
	)

	app.mm.RegisterInvariants(&app.CrisisKeeper)
	app.mm.RegisterRoutes(app.Router(), app.QueryRouter(), encodingConfig.Amino)
	app.configurator = module.NewConfigurator(app.appCodec, app.MsgServiceRouter(), app.GRPCQueryRouter())
	app.mm.RegisterServices(app.configurator)

	// create the simulation manager and define the order of the modules for deterministic simulations
	app.sm = module.NewSimulationManager(
		auth.NewAppModule(appCodec, app.AccountKeeper, authsims.RandomGenesisAccounts),
		bank.NewAppModule(appCodec, app.BankKeeper, app.AccountKeeper),
		capability.NewAppModule(appCodec, *app.CapabilityKeeper),
		feegrantmodule.NewAppModule(appCodec, app.AccountKeeper, app.BankKeeper, app.FeeGrantKeeper, app.interfaceRegistry),
		gov.NewAppModule(appCodec, app.GovKeeper, app.AccountKeeper, app.BankKeeper),
		mint.NewAppModule(appCodec, app.MintKeeper, app.AccountKeeper),
		staking.NewAppModule(appCodec, app.StakingKeeper, app.AccountKeeper, app.BankKeeper),
		distr.NewAppModule(appCodec, app.DistrKeeper, app.AccountKeeper, app.BankKeeper, app.StakingKeeper),
		slashing.NewAppModule(appCodec, app.SlashingKeeper, app.AccountKeeper, app.BankKeeper, app.StakingKeeper),
		params.NewAppModule(app.ParamsKeeper),
		evidence.NewAppModule(app.EvidenceKeeper),
		oraclemodule.NewAppModule(appCodec, app.OracleKeeper, app.AccountKeeper, app.BankKeeper),
		wasm.NewAppModule(appCodec, &app.WasmKeeper, app.StakingKeeper, app.AccountKeeper, app.BankKeeper),
		ibc.NewAppModule(app.IBCKeeper),
		transferModule,
		epochModule,
		tokenfactorymodule.NewAppModule(app.TokenFactoryKeeper, app.AccountKeeper, app.BankKeeper),
		// this line is used by starport scaffolding # stargate/app/appModule
	)
	app.sm.RegisterStoreDecoders()

	app.RegisterUpgradeHandlers()
	app.SetStoreUpgradeHandlers()

	// initialize stores
	app.mounter = func() {
		app.MountKVStores(keys)
		app.MountTransientStores(tkeys)
		app.MountMemoryStores(memKeys)
	}
	app.mounter()

	// initialize BaseApp
	app.SetInitChainer(app.InitChainer)
	app.SetBeginBlocker(app.BeginBlocker)

	signModeHandler := encodingConfig.TxConfig.SignModeHandler()
	// app.batchVerifier = ante.NewSR25519BatchVerifier(app.AccountKeeper, signModeHandler)

	anteHandler, tracerAnteHandler, anteDepGenerator, err := NewAnteHandlerAndDepGenerator(
		HandlerOptions{
			HandlerOptions: ante.HandlerOptions{
				AccountKeeper:   app.AccountKeeper,
				BankKeeper:      app.BankKeeper,
				FeegrantKeeper:  app.FeeGrantKeeper,
				ParamsKeeper:    app.ParamsKeeper,
				SignModeHandler: signModeHandler,
				SigGasConsumer:  ante.DefaultSigVerificationGasConsumer,
				// BatchVerifier:   app.batchVerifier,
			},
			IBCKeeper:           app.IBCKeeper,
			TXCounterStoreKey:   keys[wasm.StoreKey],
			WasmConfig:          &wasmConfig,
			WasmKeeper:          &app.WasmKeeper,
			OracleKeeper:        &app.OracleKeeper,
			EVMKeeper:           &app.EvmKeeper,
			UpgradeKeeper:       &app.UpgradeKeeper,
			TracingInfo:         app.GetBaseApp().TracingInfo,
			AccessControlKeeper: &app.AccessControlKeeper,
			LatestCtxGetter: func() sdk.Context {
				return app.GetCheckCtx()
			},
		},
	)
	if err != nil {
		panic(err)
	}
	app.AnteHandler = anteHandler
	app.TracerAnteHandler = tracerAnteHandler

	app.SetAnteHandler(anteHandler)
	app.SetAnteDepGenerator(anteDepGenerator)
	app.SetMidBlocker(app.MidBlocker)
	app.SetEndBlocker(app.EndBlocker)
	app.SetPrepareProposalHandler(app.PrepareProposalHandler)
	app.SetProcessProposalHandler(app.ProcessProposalHandler)
	app.SetFinalizeBlocker(app.FinalizeBlocker)
	app.SetInplaceTestnetInitializer(app.inplacetestnetInitializer)

	// Register snapshot extensions to enable state-sync for wasm.
	if manager := app.SnapshotManager(); manager != nil {
		err := manager.RegisterExtensions(
			wasmkeeper.NewWasmSnapshotter(app.CommitMultiStore(), &app.WasmKeeper),
		)
		if err != nil {
			panic(fmt.Errorf("failed to register snapshot extension: %s", err))
		}
	}

	loadVersionHandler := func() error {
		if err := app.LoadLatestVersion(); err != nil {
			tmos.Exit(err.Error())
		}

		ctx := app.BaseApp.NewUncachedContext(true, tmproto.Header{})
		if err := app.WasmKeeper.InitializePinnedCodes(ctx); err != nil {
			tmos.Exit(fmt.Sprintf("failed initialize pinned codes %s", err))
		}
		return nil
	}

	if app.LastCommitID().Version > 0 || app.TmConfig == nil || !app.TmConfig.DBSync.Enable {
		if err := loadVersionHandler(); err != nil {
			panic(err)
		}
	} else {
		app.SetLoadVersionHandler(loadVersionHandler)
	}

	app.ScopedIBCKeeper = scopedIBCKeeper
	app.ScopedTransferKeeper = scopedTransferKeeper
	app.ScopedWasmKeeper = scopedWasmKeeper

	// Create hard fork manager and register all hard fork upgrade handlers. Note,
	// when creating the manager, BaseApp must already be instantiated.
	//
	// example: app.HardForkManager.RegisterHandler(myHandler)
	app.HardForkManager = upgrades.NewHardForkManager(app.ChainID)
	app.HardForkManager.RegisterHandler(v0upgrade.NewHardForkUpgradeHandler(100_000, upgrades.ChainIDSeiHardForkTest, app.WasmKeeper))

	app.RegisterDeliverTxHook(app.AddCosmosEventsToEVMReceiptIfApplicable)

	return app
}

// HandlePreCommit happens right before the block is committed
func (app *App) HandlePreCommit(ctx sdk.Context) error {
	if app.evmRPCConfig.FlushReceiptSync {
		return app.EvmKeeper.FlushTransientReceiptsSync(ctx)
	} else {
		return app.EvmKeeper.FlushTransientReceiptsAsync(ctx)
	}

}

// Close closes all items that needs closing (called by baseapp)
func (app *App) HandleClose() error {
	if app.receiptStore != nil {
		return app.receiptStore.Close()
	}
	return nil
}

// Add (or remove) keepers when they are introduced / removed in different versions
func (app *App) SetStoreUpgradeHandlers() {
	upgradeInfo, err := app.UpgradeKeeper.ReadUpgradeInfoFromDisk()
	if err != nil {
		panic(err)
	}

	if upgradeInfo.Name == "1.0.4beta" && !app.UpgradeKeeper.IsSkipHeight(upgradeInfo.Height) {
		storeUpgrades := storetypes.StoreUpgrades{
			Added: []string{oracletypes.StoreKey},
		}

		// configure store loader that checks if version == upgradeHeight and applies store upgrades
		app.SetStoreLoader(upgradetypes.UpgradeStoreLoader(upgradeInfo.Height, &storeUpgrades))
	}

	if upgradeInfo.Name == "1.1.1beta" && !app.UpgradeKeeper.IsSkipHeight(upgradeInfo.Height) {
		storeUpgrades := storetypes.StoreUpgrades{
			Added: []string{tokenfactorytypes.StoreKey},
		}

		// configure store loader that checks if version == upgradeHeight and applies store upgrades
		app.SetStoreLoader(upgradetypes.UpgradeStoreLoader(upgradeInfo.Height, &storeUpgrades))
	}

	if upgradeInfo.Name == "1.2.2beta" && !app.UpgradeKeeper.IsSkipHeight(upgradeInfo.Height) {
		storeUpgrades := storetypes.StoreUpgrades{}

		// configure store loader that checks if version == upgradeHeight and applies store upgrades
		app.SetStoreLoader(upgradetypes.UpgradeStoreLoader(upgradeInfo.Height, &storeUpgrades))
	}

	if upgradeInfo.Name == "2.0.0beta" && !app.UpgradeKeeper.IsSkipHeight(upgradeInfo.Height) {
		storeUpgrades := storetypes.StoreUpgrades{
			Added: []string{acltypes.StoreKey},
		}

		// configure store loader that checks if version == upgradeHeight and applies store upgrades
		app.SetStoreLoader(upgradetypes.UpgradeStoreLoader(upgradeInfo.Height, &storeUpgrades))
	}

	if upgradeInfo.Name == "3.0.6" && !app.UpgradeKeeper.IsSkipHeight(upgradeInfo.Height) {
		storeUpgrades := storetypes.StoreUpgrades{
			Added: []string{authzkeeper.StoreKey},
		}

		// configure store loader that checks if version == upgradeHeight and applies store upgrades
		app.SetStoreLoader(upgradetypes.UpgradeStoreLoader(upgradeInfo.Height, &storeUpgrades))
	}

	if (upgradeInfo.Name == "v5.1.0" || upgradeInfo.Name == "v5.5.2") && !app.UpgradeKeeper.IsSkipHeight(upgradeInfo.Height) {
		storeUpgrades := storetypes.StoreUpgrades{
			Added: []string{evmtypes.StoreKey},
		}

		// configure store loader that checks if version == upgradeHeight and applies store upgrades
		app.SetStoreLoader(upgradetypes.UpgradeStoreLoader(upgradeInfo.Height, &storeUpgrades))
	}

	if (upgradeInfo.Name == "v5.8.0") && !app.UpgradeKeeper.IsSkipHeight(upgradeInfo.Height) {
		dexStoreKeyName := "dex"
		storeUpgrades := storetypes.StoreUpgrades{
			Deleted: []string{dexStoreKeyName},
		}

		// configure store loader that checks if version == upgradeHeight and applies store upgrades
		app.SetStoreLoader(upgradetypes.UpgradeStoreLoader(upgradeInfo.Height, &storeUpgrades))
	}
}

// AppName returns the name of the App
func (app *App) Name() string { return app.BaseApp.Name() }

// GetBaseApp returns the base app of the application
func (app App) GetBaseApp() *baseapp.BaseApp { return app.BaseApp }

// GetStateStore returns the state store of the application
func (app App) GetStateStore() seidb.StateStore { return app.stateStore }

// BeginBlocker application updates every begin block
func (app *App) BeginBlocker(ctx sdk.Context, req abci.RequestBeginBlock) abci.ResponseBeginBlock {
	metrics.GaugeSeidVersionAndCommit(app.versionInfo.Version, app.versionInfo.GitCommit)
	// check if we've reached a target height, if so, execute any applicable handlers
	if app.forkInitializer != nil {
		app.forkInitializer(ctx)
		app.forkInitializer = nil
	}
	if app.HardForkManager.TargetHeightReached(ctx) {
		app.HardForkManager.ExecuteForTargetHeight(ctx)
	}
	return app.mm.BeginBlock(ctx, req)
}

// MidBlocker application updates every mid block
func (app *App) MidBlocker(ctx sdk.Context, height int64) []abci.Event {
	return app.mm.MidBlock(ctx, height)
}

// EndBlocker application updates every end block
func (app *App) EndBlocker(ctx sdk.Context, req abci.RequestEndBlock) abci.ResponseEndBlock {
	return app.mm.EndBlock(ctx, req)
}

// InitChainer application update at chain initialization
func (app *App) InitChainer(ctx sdk.Context, req abci.RequestInitChain) abci.ResponseInitChain {
	var genesisState GenesisState
	if !app.genesisImportConfig.StreamGenesisImport {
		if err := json.Unmarshal(req.AppStateBytes, &genesisState); err != nil {
			panic(err)
		}
	}
	app.UpgradeKeeper.SetModuleVersionMap(ctx, app.mm.GetVersionMap())
	return app.mm.InitGenesis(ctx, app.appCodec, genesisState, app.genesisImportConfig)
}

func (app *App) PrepareProposalHandler(_ sdk.Context, req *abci.RequestPrepareProposal) (*abci.ResponsePrepareProposal, error) {
	return &abci.ResponsePrepareProposal{
		TxRecords: utils.Map(req.Txs, func(tx []byte) *abci.TxRecord {
			return &abci.TxRecord{Action: abci.TxRecord_UNMODIFIED, Tx: tx}
		}),
	}, nil
}

func (app *App) GetOptimisticProcessingInfo() *OptimisticProcessingInfo {
	return app.optimisticProcessingInfo
}

func (app *App) ClearOptimisticProcessingInfo() {
	app.optimisticProcessingInfo = nil
}

func (app *App) ProcessProposalHandler(ctx sdk.Context, req *abci.RequestProcessProposal) (*abci.ResponseProcessProposal, error) {
	// TODO: this check decodes transactions which is redone in subsequent processing. We might be able to optimize performance
	// by recording the decoding results and avoid decoding again later on.

	if !app.checkTotalBlockGas(ctx, req.Txs) {
		metrics.IncrFailedTotalGasWantedCheck(string(req.GetProposerAddress()))
		return &abci.ResponseProcessProposal{
			Status: abci.ResponseProcessProposal_REJECT,
		}, nil
	}
	if app.optimisticProcessingInfo == nil {
		completionSignal := make(chan struct{}, 1)
		optimisticProcessingInfo := &OptimisticProcessingInfo{
			Height:     req.Height,
			Hash:       req.Hash,
			Completion: completionSignal,
		}
		app.optimisticProcessingInfo = optimisticProcessingInfo

		plan, found := app.UpgradeKeeper.GetUpgradePlan(ctx)
		if found && plan.ShouldExecute(ctx) {
			app.Logger().Info(fmt.Sprintf("Potential upgrade planned for height=%d skipping optimistic processing", plan.Height))
			app.optimisticProcessingInfo.Aborted = true
			app.optimisticProcessingInfo.Completion <- struct{}{}
		} else {
			go func() {
				events, txResults, endBlockResp, _ := app.ProcessBlock(ctx, req.Txs, req, req.ProposedLastCommit, false)
				optimisticProcessingInfo.Events = events
				optimisticProcessingInfo.TxRes = txResults
				optimisticProcessingInfo.EndBlockResp = endBlockResp
				optimisticProcessingInfo.Completion <- struct{}{}
			}()
		}
	} else if !bytes.Equal(app.optimisticProcessingInfo.Hash, req.Hash) {
		app.optimisticProcessingInfo.Aborted = true
	}
	return &abci.ResponseProcessProposal{
		Status: abci.ResponseProcessProposal_ACCEPT,
	}, nil
}

func (app *App) FinalizeBlocker(ctx sdk.Context, req *abci.RequestFinalizeBlock) (*abci.ResponseFinalizeBlock, error) {
	startTime := time.Now()
	defer func() {
		app.ClearOptimisticProcessingInfo()
		duration := time.Since(startTime)
		ctx.Logger().Info(fmt.Sprintf("FinalizeBlock took %dms", duration/time.Millisecond))
	}()
	if app.optimisticProcessingInfo != nil {
		<-app.optimisticProcessingInfo.Completion
		if !app.optimisticProcessingInfo.Aborted && bytes.Equal(app.optimisticProcessingInfo.Hash, req.Hash) {
			metrics.IncrementOptimisticProcessingCounter(true)
			app.SetProcessProposalStateToCommit()
			if app.EvmKeeper.EthReplayConfig.Enabled || app.EvmKeeper.EthBlockTestConfig.Enabled {
				return &abci.ResponseFinalizeBlock{}, nil
			}
			cms := app.WriteState()
			app.LightInvarianceChecks(cms, app.lightInvarianceConfig)
			appHash := app.GetWorkingHash()
			resp := app.getFinalizeBlockResponse(appHash, app.optimisticProcessingInfo.Events, app.optimisticProcessingInfo.TxRes, app.optimisticProcessingInfo.EndBlockResp)
			return &resp, nil
		}
	}
	metrics.IncrementOptimisticProcessingCounter(false)
	ctx.Logger().Info("optimistic processing ineligible")
	events, txResults, endBlockResp, _ := app.ProcessBlock(ctx, req.Txs, req, req.DecidedLastCommit, false)

	app.SetDeliverStateToCommit()
	if app.EvmKeeper.EthReplayConfig.Enabled || app.EvmKeeper.EthBlockTestConfig.Enabled {
		return &abci.ResponseFinalizeBlock{}, nil
	}
	cms := app.WriteState()
	app.LightInvarianceChecks(cms, app.lightInvarianceConfig)
	appHash := app.GetWorkingHash()
	resp := app.getFinalizeBlockResponse(appHash, events, txResults, endBlockResp)
	return &resp, nil
}

func (app *App) DeliverTxWithResult(ctx sdk.Context, tx []byte, typedTx sdk.Tx) *abci.ExecTxResult {
	deliverTxResp := app.DeliverTx(ctx, abci.RequestDeliverTx{
		Tx: tx,
	}, typedTx, sha256.Sum256(tx))

	metrics.IncrGasCounter("gas_used", deliverTxResp.GasUsed)
	metrics.IncrGasCounter("gas_wanted", deliverTxResp.GasWanted)

	return &abci.ExecTxResult{
		Code:      deliverTxResp.Code,
		Data:      deliverTxResp.Data,
		Log:       deliverTxResp.Log,
		Info:      deliverTxResp.Info,
		GasWanted: deliverTxResp.GasWanted,
		GasUsed:   deliverTxResp.GasUsed,
		Events:    deliverTxResp.Events,
		Codespace: deliverTxResp.Codespace,
		EvmTxInfo: deliverTxResp.EvmTxInfo,
	}
}

func (app *App) ProcessBlockSynchronous(ctx sdk.Context, txs [][]byte, typedTxs []sdk.Tx, absoluteTxIndices []int) []*abci.ExecTxResult {
	defer metrics.BlockProcessLatency(time.Now(), metrics.SYNCHRONOUS)

	txResults := []*abci.ExecTxResult{}
	for i, tx := range txs {
		ctx = ctx.WithTxIndex(absoluteTxIndices[i])
		res := app.DeliverTxWithResult(ctx, tx, typedTxs[i])
		txResults = append(txResults, res)
		metrics.IncrTxProcessTypeCounter(metrics.SYNCHRONOUS)
	}
	return txResults
}

// Returns a mapping of the accessOperation to the channels
func GetChannelsFromSignalMapping(signalMapping acltypes.MessageCompletionSignalMapping) sdkacltypes.MessageAccessOpsChannelMapping {
	channelsMapping := make(sdkacltypes.MessageAccessOpsChannelMapping)
	for messageIndex, accessOperationsToSignal := range signalMapping {
		channelsMapping[messageIndex] = make(sdkacltypes.AccessOpsChannelMapping)
		for accessOperation, completionSignals := range accessOperationsToSignal {
			var channels []chan interface{}
			for _, completionSignal := range completionSignals {
				channels = append(channels, completionSignal.Channel)
			}
			channelsMapping[messageIndex][accessOperation] = channels
		}
	}
	return channelsMapping
}

type ChannelResult struct {
	txIndex int
	result  *abci.ExecTxResult
}

// cacheContext returns a new context based off of the provided context with
// a branched multi-store.
func (app *App) CacheContext(ctx sdk.Context) (sdk.Context, sdk.CacheMultiStore) {
	ms := ctx.MultiStore()
	msCache := ms.CacheMultiStore()
	return ctx.WithMultiStore(msCache), msCache
}

// TODO: (occ) this is the roughly analogous to the execution + validation tasks for OCC, but this one performs validation in isolation
// rather than comparing against a multi-version store
// The validation happens immediately after execution all part of DeliverTx (which is a path that goes through sei-cosmos to runTx eventually)
func (app *App) ProcessTxConcurrent(
	ctx sdk.Context,
	txIndex int,
	absoluateTxIndex int,
	txBytes []byte,
	typedTx sdk.Tx,
	wg *sync.WaitGroup,
	resultChan chan<- ChannelResult,
	txCompletionSignalingMap acltypes.MessageCompletionSignalMapping,
	txBlockingSignalsMap acltypes.MessageCompletionSignalMapping,
	txMsgAccessOpMapping acltypes.MsgIndexToAccessOpMapping,
) {
	defer wg.Done()
	// Store the Channels in the Context Object for each transaction
	ctx = ctx.WithTxCompletionChannels(GetChannelsFromSignalMapping(txCompletionSignalingMap))
	ctx = ctx.WithTxBlockingChannels(GetChannelsFromSignalMapping(txBlockingSignalsMap))
	ctx = ctx.WithTxMsgAccessOps(txMsgAccessOpMapping)
	ctx = ctx.WithMsgValidator(
		sdkacltypes.NewMsgValidator(aclutils.StoreKeyToResourceTypePrefixMap),
	)
	ctx = ctx.WithTxIndex(absoluateTxIndex)

	// Deliver the transaction and store the result in the channel
	resultChan <- ChannelResult{txIndex, app.DeliverTxWithResult(ctx, txBytes, typedTx)}
	metrics.IncrTxProcessTypeCounter(metrics.CONCURRENT)
}

type ProcessBlockConcurrentFunction func(
	ctx sdk.Context,
	txs [][]byte,
	typedTxs []sdk.Tx,
	completionSignalingMap map[int]acltypes.MessageCompletionSignalMapping,
	blockingSignalsMap map[int]acltypes.MessageCompletionSignalMapping,
	txMsgAccessOpMapping map[int]acltypes.MsgIndexToAccessOpMapping,
	absoluteTxIndices []int,
) ([]*abci.ExecTxResult, bool)

func (app *App) ProcessBlockConcurrent(
	ctx sdk.Context,
	txs [][]byte,
	typedTxs []sdk.Tx,
	completionSignalingMap map[int]acltypes.MessageCompletionSignalMapping,
	blockingSignalsMap map[int]acltypes.MessageCompletionSignalMapping,
	txMsgAccessOpMapping map[int]acltypes.MsgIndexToAccessOpMapping,
	absoluteTxIndices []int,
) ([]*abci.ExecTxResult, bool) {
	defer metrics.BlockProcessLatency(time.Now(), metrics.CONCURRENT)

	txResults := []*abci.ExecTxResult{}
	// If there's no transactions then return empty results
	if len(txs) == 0 {
		return txResults, true
	}

	var waitGroup sync.WaitGroup
	resultChan := make(chan ChannelResult, len(txs))
	// For each transaction, start goroutine and deliver TX
	for txIndex, txBytes := range txs {
		waitGroup.Add(1)
		go app.ProcessTxConcurrent(
			ctx,
			txIndex,
			absoluteTxIndices[txIndex],
			txBytes,
			typedTxs[txIndex],
			&waitGroup,
			resultChan,
			completionSignalingMap[txIndex],
			blockingSignalsMap[txIndex],
			txMsgAccessOpMapping[txIndex],
		)
	}

	// Do not call waitGroup.Wait() synchronously as it blocks on channel reads
	// until all the messages are read. This closes the channel once
	// results are all read and prevent any further writes.
	go func() {
		waitGroup.Wait()
		close(resultChan)
	}()

	// Gather Results and store it based on txIndex and read results from channel
	// Concurrent results may be in different order than the original txIndex
	txResultsMap := map[int]*abci.ExecTxResult{}
	for result := range resultChan {
		txResultsMap[result.txIndex] = result.result
	}

	// Gather Results and store in array based on txIndex to preserve ordering
	for txIndex := range txs {
		txResults = append(txResults, txResultsMap[txIndex])
	}

	ok := true
	for i, result := range txResults {
		if result.GetCode() == sdkerrors.ErrInvalidConcurrencyExecution.ABCICode() {
			ctx.Logger().Error(fmt.Sprintf("Invalid concurrent execution of deliverTx index=%d", i))
			metrics.IncrFailedConcurrentDeliverTxCounter()
			ok = false
		}
	}

	return txResults, ok
}

func (app *App) ProcessTxs(
	ctx sdk.Context,
	txs [][]byte,
	typedTxs []sdk.Tx,
	dependencyDag *acltypes.Dag,
	processBlockConcurrentFunction ProcessBlockConcurrentFunction,
	absoluteTxIndices []int,
) ([]*abci.ExecTxResult, sdk.Context) {
	// Only run concurrently if no error
	// Branch off the current context and pass a cached context to the concurrent delivered TXs that are shared.
	// runTx will write to this ephermeral CacheMultiStore, after the process block is done, Write() is called on this
	// CacheMultiStore where it writes the data to the parent store (DeliverState) in sorted Key order to maintain
	// deterministic ordering between validators in the case of concurrent deliverTXs
	processBlockCtx, processBlockCache := app.CacheContext(ctx)
	// TODO: (occ) replaced with scheduler sending tasks to workers such as execution and validation
	concurrentResults, ok := processBlockConcurrentFunction(
		processBlockCtx,
		txs,
		typedTxs,
		dependencyDag.CompletionSignalingMap,
		dependencyDag.BlockingSignalsMap,
		dependencyDag.TxMsgAccessOpMapping,
		absoluteTxIndices,
	)
	if ok {
		// Write the results back to the concurrent contexts - if concurrent execution fails,
		// this should not be called and the state is rolled back and retried with synchronous execution
		processBlockCache.Write()
		return concurrentResults, ctx
	}
	// we need to add the wasm dependencies before we process synchronous otherwise it never gets included
	ctx = app.addBadWasmDependenciesToContext(ctx, concurrentResults)
	ctx.Logger().Error("Concurrent Execution failed, retrying with Synchronous")

	txResults := app.ProcessBlockSynchronous(ctx, txs, typedTxs, absoluteTxIndices)
	processBlockCache.Write()
	return txResults, ctx
}

func (app *App) PartitionPrioritizedTxs(_ sdk.Context, txs [][]byte, typedTxs []sdk.Tx) (
	prioritizedTxs, otherTxs [][]byte,
	prioritizedTypedTxs, otherTypedTxs []sdk.Tx,
	prioritizedIndices, otherIndices []int,
) {
	for idx, tx := range txs {
		if typedTxs[idx] == nil {
			otherTxs = append(otherTxs, tx)
			otherTypedTxs = append(otherTypedTxs, nil)
			otherIndices = append(otherIndices, idx)
			continue
		}

		if utils.IsTxPrioritized(typedTxs[idx]) {
			prioritizedTxs = append(prioritizedTxs, tx)
			prioritizedTypedTxs = append(prioritizedTypedTxs, typedTxs[idx])
			prioritizedIndices = append(prioritizedIndices, idx)
		} else {
			otherTxs = append(otherTxs, tx)
			otherTypedTxs = append(otherTypedTxs, typedTxs[idx])
			otherIndices = append(otherIndices, idx)
		}

	}
	return prioritizedTxs, otherTxs, prioritizedTypedTxs, otherTypedTxs, prioritizedIndices, otherIndices
}

// ExecuteTxsConcurrently calls the appropriate function for processing transacitons
func (app *App) ExecuteTxsConcurrently(ctx sdk.Context, txs [][]byte, typedTxs []sdk.Tx, absoluteTxIndices []int) ([]*abci.ExecTxResult, sdk.Context) {
	// TODO after OCC release, remove this check and call ProcessTXsWithOCC directly
	if ctx.IsOCCEnabled() {
		return app.ProcessTXsWithOCC(ctx, txs, typedTxs, absoluteTxIndices)
	}
	results := app.ProcessBlockSynchronous(ctx, txs, typedTxs, absoluteTxIndices)
	return results, ctx
}

func (app *App) GetDeliverTxEntry(ctx sdk.Context, txIndex int, absoluateIndex int, bz []byte, tx sdk.Tx) (res *sdk.DeliverTxEntry) {
	res = &sdk.DeliverTxEntry{
		Request:       abci.RequestDeliverTx{Tx: bz},
		SdkTx:         tx,
		Checksum:      sha256.Sum256(bz),
		AbsoluteIndex: absoluateIndex,
	}
	return
}

// ProcessTXsWithOCC runs the transactions concurrently via OCC
func (app *App) ProcessTXsWithOCC(ctx sdk.Context, txs [][]byte, typedTxs []sdk.Tx, absoluteTxIndices []int) ([]*abci.ExecTxResult, sdk.Context) {
	entries := make([]*sdk.DeliverTxEntry, len(txs))
	for txIndex, tx := range txs {
		entries[txIndex] = app.GetDeliverTxEntry(ctx, txIndex, absoluteTxIndices[txIndex], tx, typedTxs[txIndex])
	}

	batchResult := app.DeliverTxBatch(ctx, sdk.DeliverTxBatchRequest{TxEntries: entries})

	execResults := make([]*abci.ExecTxResult, 0, len(batchResult.Results))
	for _, r := range batchResult.Results {
		metrics.IncrTxProcessTypeCounter(metrics.OCC_CONCURRENT)
		metrics.IncrGasCounter("gas_used", r.Response.GasUsed)
		metrics.IncrGasCounter("gas_wanted", r.Response.GasWanted)
		execResults = append(execResults, &abci.ExecTxResult{
			Code:      r.Response.Code,
			Data:      r.Response.Data,
			Log:       r.Response.Log,
			Info:      r.Response.Info,
			GasWanted: r.Response.GasWanted,
			GasUsed:   r.Response.GasUsed,
			Events:    r.Response.Events,
			Codespace: r.Response.Codespace,
			EvmTxInfo: r.Response.EvmTxInfo,
		})
	}

	return execResults, ctx
}

// BuildDependenciesAndRunTxs deprecated, use ProcessTXsWithOCC instead
// Deprecated: this will be removed after OCC releases
func (app *App) BuildDependenciesAndRunTxs(ctx sdk.Context, txs [][]byte, typedTxs []sdk.Tx, absoluteTxIndices []int) ([]*abci.ExecTxResult, sdk.Context) {
	// dependencyDag, err := app.AccessControlKeeper.BuildDependencyDag(ctx, app.GetAnteDepGenerator(), typedTxs)

	// switch err {
	// case nil:
	// 	txResults, ctx = app.ProcessTxs(ctx, txs, typedTxs, dependencyDag, app.ProcessBlockConcurrent, absoluteTxIndices)
	// case acltypes.ErrGovMsgInBlock:
	// 	ctx.Logger().Info(fmt.Sprintf("Gov msg found while building DAG, processing synchronously: %s", err))
	// 	txResults = app.ProcessBlockSynchronous(ctx, txs, typedTxs, absoluteTxIndices)
	// 	metrics.IncrDagBuildErrorCounter(metrics.GovMsgInBlock)
	// default:
	// 	ctx.Logger().Error(fmt.Sprintf("Error while building DAG, processing synchronously: %s", err))
	// 	txResults = app.ProcessBlockSynchronous(ctx, txs, typedTxs, absoluteTxIndices)
	// 	metrics.IncrDagBuildErrorCounter(metrics.FailedToBuild)
	// }

	return app.ProcessBlockSynchronous(ctx, txs, typedTxs, absoluteTxIndices), ctx
}

func (app *App) ProcessBlock(ctx sdk.Context, txs [][]byte, req BlockProcessRequest, lastCommit abci.CommitInfo, simulate bool) ([]abci.Event, []*abci.ExecTxResult, abci.ResponseEndBlock, error) {
	defer func() {
		if !app.httpServerStartSignalSent {
			app.httpServerStartSignalSent = true
			app.httpServerStartSignal <- struct{}{}
		}
		if !app.wsServerStartSignalSent {
			app.wsServerStartSignalSent = true
			app.wsServerStartSignal <- struct{}{}
		}
	}()
	ctx = ctx.WithIsOCCEnabled(app.OccEnabled())

	events := []abci.Event{}
	beginBlockReq := abci.RequestBeginBlock{
		Hash: req.GetHash(),
		ByzantineValidators: utils.Map(req.GetByzantineValidators(), func(mis abci.Misbehavior) abci.Evidence {
			return abci.Evidence(mis)
		}),
		LastCommitInfo: abci.LastCommitInfo{
			Round: lastCommit.Round,
			Votes: utils.Map(lastCommit.Votes, func(vote abci.VoteInfo) abci.VoteInfo {
				return abci.VoteInfo{
					Validator:       vote.Validator,
					SignedLastBlock: vote.SignedLastBlock,
				}
			}),
		},
		Header: tmproto.Header{
			ChainID:         app.ChainID,
			Height:          req.GetHeight(),
			Time:            req.GetTime(),
			ProposerAddress: ctx.BlockHeader().ProposerAddress,
		},
		Simulate: simulate,
	}
	beginBlockResp := app.BeginBlock(ctx, beginBlockReq)
	events = append(events, beginBlockResp.Events...)

	evmTxs := make([]*evmtypes.MsgEVMTransaction, len(txs)) // nil for non-EVM txs
	txResults := make([]*abci.ExecTxResult, len(txs))
	typedTxs := app.DecodeTransactionsConcurrently(ctx, txs)

	prioritizedTxs, otherTxs, prioritizedTypedTxs, otherTypedTxs, prioritizedIndices, otherIndices := app.PartitionPrioritizedTxs(ctx, txs, typedTxs)

	// run the prioritized txs
	prioritizedResults, ctx := app.ExecuteTxsConcurrently(ctx, prioritizedTxs, prioritizedTypedTxs, prioritizedIndices)
	for relativePrioritizedIndex, originalIndex := range prioritizedIndices {
		txResults[originalIndex] = prioritizedResults[relativePrioritizedIndex]
		evmTxs[originalIndex] = app.GetEVMMsg(prioritizedTypedTxs[relativePrioritizedIndex])
	}

	// Finalize all Bank Module Transfers here so that events are included for prioritiezd txs
	deferredWriteEvents := app.BankKeeper.WriteDeferredBalances(ctx)
	events = append(events, deferredWriteEvents...)

	midBlockEvents := app.MidBlock(ctx, req.GetHeight())
	events = append(events, midBlockEvents...)

	otherResults, ctx := app.ExecuteTxsConcurrently(ctx, otherTxs, otherTypedTxs, otherIndices)
	for relativeOtherIndex, originalIndex := range otherIndices {
		txResults[originalIndex] = otherResults[relativeOtherIndex]
		evmTxs[originalIndex] = app.GetEVMMsg(otherTypedTxs[relativeOtherIndex])
	}
	app.EvmKeeper.SetTxResults(txResults)
	app.EvmKeeper.SetMsgs(evmTxs)

	// Finalize all Bank Module Transfers here so that events are included
	lazyWriteEvents := app.BankKeeper.WriteDeferredBalances(ctx)
	events = append(events, lazyWriteEvents...)

	// Sum up total used per block only for evm transactions
	evmTotalGasUsed := int64(0)
	for _, txResult := range txResults {
		if txResult.EvmTxInfo != nil {
			evmTotalGasUsed += txResult.GasUsed
		}
	}

	endBlockResp := app.EndBlock(ctx, abci.RequestEndBlock{
		Height:       req.GetHeight(),
		BlockGasUsed: evmTotalGasUsed,
	})

	events = append(events, endBlockResp.Events...)
	return events, txResults, endBlockResp, nil
}

func (app *App) GetEVMMsg(tx sdk.Tx) (res *evmtypes.MsgEVMTransaction) {
	defer func() {
		if err := recover(); err != nil {
			res = nil
		}
	}()
	if tx == nil {
		return nil
	} else if emsg := evmtypes.GetEVMTransactionMessage(tx); emsg != nil && !emsg.IsAssociateTx() {
		return emsg
	} else {
		return nil
	}
}

func (app *App) DecodeTransactionsConcurrently(ctx sdk.Context, txs [][]byte) []sdk.Tx {
	typedTxs := make([]sdk.Tx, len(txs))
	wg := sync.WaitGroup{}
	for i, tx := range txs {
		wg.Add(1)
		go func(idx int, encodedTx []byte) {
			defer wg.Done()
			defer func() {
				if err := recover(); err != nil {
					ctx.Logger().Error(fmt.Sprintf("encountered panic during transaction decoding: %s", err))
					typedTxs[idx] = nil
				}
			}()
			typedTx, err := app.txDecoder(encodedTx)
			// get txkey from tx
			if err != nil {
				ctx.Logger().Error(fmt.Sprintf("error decoding transaction at index %d due to %s", idx, err))
				typedTxs[idx] = nil
			} else {
				if isEVM, _ := evmante.IsEVMMessage(typedTx); isEVM {
					msg := evmtypes.MustGetEVMTransactionMessage(typedTx)
					if err := evmante.Preprocess(ctx, msg, app.EvmKeeper.ChainID(ctx)); err != nil {
						ctx.Logger().Error(fmt.Sprintf("error preprocessing EVM tx due to %s", err))
						typedTxs[idx] = nil
						return
					}
				}
				typedTxs[idx] = typedTx
			}
		}(i, tx)
	}
	wg.Wait()
	return typedTxs
}

func (app *App) addBadWasmDependenciesToContext(ctx sdk.Context, txResults []*abci.ExecTxResult) sdk.Context {
	wasmContractsWithIncorrectDependencies := []sdk.AccAddress{}
	for _, txResult := range txResults {
		// we need to iterate in reverse and pick the first one
		if txResult.Codespace == sdkerrors.RootCodespace && txResult.Code == sdkerrors.ErrInvalidConcurrencyExecution.ABCICode() {
			for _, event := range txResult.Events {
				if event.Type == wasmtypes.EventTypeExecute {
					for _, attr := range event.Attributes {
						if string(attr.Key) == wasmtypes.AttributeKeyContractAddr {
							addr, err := sdk.AccAddressFromBech32(string(attr.Value))
							if err == nil {
								wasmContractsWithIncorrectDependencies = append(wasmContractsWithIncorrectDependencies, addr)
							}
						}
					}
				}
			}
		}
	}
	return ctx.WithContext(context.WithValue(ctx.Context(), aclconstants.BadWasmDependencyAddressesKey, wasmContractsWithIncorrectDependencies))
}

func (app *App) getFinalizeBlockResponse(appHash []byte, events []abci.Event, txResults []*abci.ExecTxResult, endBlockResp abci.ResponseEndBlock) abci.ResponseFinalizeBlock {
	if app.EvmKeeper.EthReplayConfig.Enabled || app.EvmKeeper.EthBlockTestConfig.Enabled {
		return abci.ResponseFinalizeBlock{}
	}
	return abci.ResponseFinalizeBlock{
		Events:    events,
		TxResults: txResults,
		ValidatorUpdates: utils.Map(endBlockResp.ValidatorUpdates, func(v abci.ValidatorUpdate) abci.ValidatorUpdate {
			return abci.ValidatorUpdate{
				PubKey: v.PubKey,
				Power:  v.Power,
			}
		}),
		ConsensusParamUpdates: &tmproto.ConsensusParams{
			Block: &tmproto.BlockParams{
				MaxBytes:      endBlockResp.ConsensusParamUpdates.Block.MaxBytes,
				MaxGas:        endBlockResp.ConsensusParamUpdates.Block.MaxGas,
				MinTxsInBlock: endBlockResp.ConsensusParamUpdates.Block.MinTxsInBlock,
				MaxGasWanted:  endBlockResp.ConsensusParamUpdates.Block.MaxGasWanted,
			},
			Evidence: &tmproto.EvidenceParams{
				MaxAgeNumBlocks: endBlockResp.ConsensusParamUpdates.Evidence.MaxAgeNumBlocks,
				MaxAgeDuration:  endBlockResp.ConsensusParamUpdates.Evidence.MaxAgeDuration,
				MaxBytes:        endBlockResp.ConsensusParamUpdates.Evidence.MaxBytes,
			},
			Validator: &tmproto.ValidatorParams{
				PubKeyTypes: endBlockResp.ConsensusParamUpdates.Validator.PubKeyTypes,
			},
			Version: &tmproto.VersionParams{
				AppVersion: endBlockResp.ConsensusParamUpdates.Version.AppVersion,
			},
		},
		AppHash: appHash,
	}
}

// LoadHeight loads a particular height
func (app *App) LoadHeight(height int64) error {
	return app.LoadVersionWithoutInit(height)
}

// ModuleAccountAddrs returns all the app's module account addresses.
func (app *App) ModuleAccountAddrs() map[string]bool {
	modAccAddrs := make(map[string]bool)
	for acc := range maccPerms {
		modAccAddrs[authtypes.NewModuleAddress(acc).String()] = true
	}

	return modAccAddrs
}

// LegacyAmino returns SimApp's amino codec.
//
// NOTE: This is solely to be used for testing purposes as it may be desirable
// for modules to register their own custom testing types.
func (app *App) LegacyAmino() *codec.LegacyAmino {
	return app.cdc
}

// AppCodec returns an app codec.
//
// NOTE: This is solely to be used for testing purposes as it may be desirable
// for modules to register their own custom testing types.
func (app *App) AppCodec() codec.Codec {
	return app.appCodec
}

// InterfaceRegistry returns an InterfaceRegistry
func (app *App) InterfaceRegistry() types.InterfaceRegistry {
	return app.interfaceRegistry
}

// GetKey returns the KVStoreKey for the provided store key.
//
// NOTE: This is solely to be used for testing purposes.
func (app *App) GetKey(storeKey string) *sdk.KVStoreKey {
	return app.keys[storeKey]
}

// GetTKey returns the TransientStoreKey for the provided store key.
//
// NOTE: This is solely to be used for testing purposes.
func (app *App) GetTKey(storeKey string) *sdk.TransientStoreKey {
	return app.tkeys[storeKey]
}

// GetMemKey returns the MemStoreKey for the provided mem key.
//
// NOTE: This is solely used for testing purposes.
func (app *App) GetMemKey(storeKey string) *sdk.MemoryStoreKey {
	return app.memKeys[storeKey]
}

// GetSubspace returns a param subspace for a given module name.
//
// NOTE: This is solely to be used for testing purposes.
func (app *App) GetSubspace(moduleName string) paramstypes.Subspace {
	subspace, _ := app.ParamsKeeper.GetSubspace(moduleName)
	return subspace
}

// RegisterAPIRoutes registers all application module routes with the provided
// API server.
func (app *App) RegisterAPIRoutes(apiSvr *api.Server, apiConfig config.APIConfig) {
	clientCtx := apiSvr.ClientCtx
	rpc.RegisterRoutes(clientCtx, apiSvr.Router)
	// Register legacy tx routes.
	authrest.RegisterTxRoutes(clientCtx, apiSvr.Router)
	// Register new tx routes from grpc-gateway.
	authtx.RegisterGRPCGatewayRoutes(clientCtx, apiSvr.GRPCGatewayRouter)
	// Register new tendermint queries routes from grpc-gateway.
	tmservice.RegisterGRPCGatewayRoutes(clientCtx, apiSvr.GRPCGatewayRouter)

	// Register legacy and grpc-gateway routes for all modules.
	ModuleBasics.RegisterRESTRoutes(clientCtx, apiSvr.Router)
	ModuleBasics.RegisterGRPCGatewayRoutes(clientCtx, apiSvr.GRPCGatewayRouter)

	// register swagger API from root so that other applications can override easily
	if apiConfig.Swagger {
		RegisterSwaggerAPI(apiSvr.Router)
	}

}

// RegisterTxService implements the Application.RegisterTxService method.
func (app *App) RegisterTxService(clientCtx client.Context) {
	authtx.RegisterTxService(app.BaseApp.GRPCQueryRouter(), clientCtx, app.BaseApp.Simulate, app.interfaceRegistry)
}

func (app *App) RPCContextProvider(i int64) sdk.Context {
	if i == evmrpc.LatestCtxHeight {
		return app.GetCheckCtx().WithIsTracing(true).WithIsCheckTx(false)
	}
	ctx, err := app.CreateQueryContext(i, false)
	if err != nil {
		app.Logger().Error(fmt.Sprintf("failed to create query context for EVM; using latest context instead: %v+", err.Error()))
		return app.GetCheckCtx().WithIsTracing(true).WithIsCheckTx(false)
	}
	return ctx.WithIsEVM(true).WithIsTracing(true).WithIsCheckTx(false)
}

// RegisterTendermintService implements the Application.RegisterTendermintService method.
func (app *App) RegisterTendermintService(clientCtx client.Context) {
	tmservice.RegisterTendermintService(app.BaseApp.GRPCQueryRouter(), clientCtx, app.interfaceRegistry)
	txConfigProvider := func(height int64) client.TxConfig {
		if app.ChainID != "pacific-1" {
			return app.encodingConfig.TxConfig
		}
		// use current for post v6.0.6 heights
		if height >= v606UpgradeHeight {
			return app.encodingConfig.TxConfig
		}
		return app.legacyEncodingConfig.TxConfig
	}

	if app.evmRPCConfig.HTTPEnabled {
		evmHTTPServer, err := evmrpc.NewEVMHTTPServer(app.Logger(), app.evmRPCConfig, clientCtx.Client, &app.EvmKeeper, app.BaseApp, app.TracerAnteHandler, app.RPCContextProvider, txConfigProvider, DefaultNodeHome, nil)
		if err != nil {
			panic(err)
		}
		go func() {
			<-app.httpServerStartSignal
			if err := evmHTTPServer.Start(); err != nil {
				panic(err)
			}
		}()
	}

	if app.evmRPCConfig.WSEnabled {
		evmWSServer, err := evmrpc.NewEVMWebSocketServer(app.Logger(), app.evmRPCConfig, clientCtx.Client, &app.EvmKeeper, app.BaseApp, app.TracerAnteHandler, app.RPCContextProvider, txConfigProvider, DefaultNodeHome)
		if err != nil {
			panic(err)
		}
		go func() {
			<-app.wsServerStartSignal
			if err := evmWSServer.Start(); err != nil {
				panic(err)
			}
		}()
	}
}

// RegisterSwaggerAPI registers swagger route with API Server
func RegisterSwaggerAPI(rtr *mux.Router) {
	statikFS, err := fs.NewWithNamespace("swagger")
	if err != nil {
		panic(err)
	}

	staticServer := http.FileServer(statikFS)
	rtr.PathPrefix("/swagger/").Handler(http.StripPrefix("/swagger/", staticServer))
}

// checkTotalBlockGas checks that the block gas limit is not exceeded by our best estimate of
// the total gas by the txs in the block. The gas of a tx is either the gas estimate if it's an EVM tx,
// or the gas wanted if it's a Cosmos tx.
func (app *App) checkTotalBlockGas(ctx sdk.Context, txs [][]byte) bool {
	totalGas, totalGasWanted := uint64(0), uint64(0)
	nonzeroTxsCnt := 0
	for _, tx := range txs {
		decodedTx, err := app.txDecoder(tx)
		if err != nil {
			// such tx will not be processed and thus won't consume gas. Skipping
			continue
		}
		// check gasless first (this has to happen before other checks to avoid panics)
		isGasless, err := antedecorators.IsTxGasless(decodedTx, ctx, app.OracleKeeper, &app.EvmKeeper)
		if err != nil {
			ctx.Logger().Error("error checking if tx is gasless", "error", err)
			continue
		}
		if isGasless {
			continue
		}
		// Check whether it's associate tx
		gasWanted := uint64(0)
		// Check whether it's an EVM or Cosmos tx
		isEVM, err := evmante.IsEVMMessage(decodedTx)
		if err != nil {
			continue
		}
		if isEVM {
			msg := evmtypes.MustGetEVMTransactionMessage(decodedTx)
			if msg.IsAssociateTx() {
				continue
			}
			etx, _ := msg.AsTransaction()
			gasWanted = etx.Gas()
		} else {
			feeTx, ok := decodedTx.(sdk.FeeTx)
			if !ok {
				// such tx will not be processed and thus won't consume gas. Skipping
				continue
			}

			// Check for overflow before adding
			gasWanted = feeTx.GetGas()
		}

		if int64(gasWanted) < 0 || int64(totalGas) > math.MaxInt64-int64(gasWanted) {
			return false
		}

		if gasWanted > 0 {
			nonzeroTxsCnt++
		}

		totalGasWanted += gasWanted

		// If the gas estimate is set and at least 21k (the minimum gas needed for an EVM tx), use the gas estimate.
		// Otherwise, use the gas wanted. Typically the gas estimate is set for EVM txs and not set for Cosmos txs.
		if decodedTx.GetGasEstimate() >= MinGasEVMTx {
			totalGas += decodedTx.GetGasEstimate()
		} else {
			totalGas += gasWanted
		}

		if totalGasWanted > uint64(ctx.ConsensusParams().Block.MaxGasWanted) {
			return false
		}

		if totalGas > uint64(ctx.ConsensusParams().Block.MaxGas) {
			return false
		}
	}
	return true
}

func (app *App) GetTxConfig() client.TxConfig {
	return app.encodingConfig.TxConfig
}

func (app *App) GetLegacyTxConfig() client.TxConfig {
	return app.legacyEncodingConfig.TxConfig
}

// GetMaccPerms returns a copy of the module account permissions
func GetMaccPerms() map[string][]string {
	dupMaccPerms := make(map[string][]string)
	for k, v := range maccPerms {
		dupMaccPerms[k] = v
	}
	return dupMaccPerms
}

// initParamsKeeper init params keeper and its subspaces
func initParamsKeeper(appCodec codec.BinaryCodec, legacyAmino *codec.LegacyAmino, key, tkey sdk.StoreKey) paramskeeper.Keeper {
	paramsKeeper := paramskeeper.NewKeeper(appCodec, legacyAmino, key, tkey)

	paramsKeeper.Subspace(acltypes.ModuleName)
	paramsKeeper.Subspace(authtypes.ModuleName)
	paramsKeeper.Subspace(banktypes.ModuleName)
	paramsKeeper.Subspace(stakingtypes.ModuleName)
	paramsKeeper.Subspace(minttypes.ModuleName)
	paramsKeeper.Subspace(distrtypes.ModuleName)
	paramsKeeper.Subspace(slashingtypes.ModuleName)
	paramsKeeper.Subspace(govtypes.ModuleName).WithKeyTable(govtypes.ParamKeyTable())
	paramsKeeper.Subspace(crisistypes.ModuleName)
	paramsKeeper.Subspace(ibctransfertypes.ModuleName)
	paramsKeeper.Subspace(ibchost.ModuleName)
	paramsKeeper.Subspace(oracletypes.ModuleName)
	paramsKeeper.Subspace(wasm.ModuleName)
	paramsKeeper.Subspace(evmtypes.ModuleName)
	paramsKeeper.Subspace(epochmoduletypes.ModuleName)
	paramsKeeper.Subspace(tokenfactorytypes.ModuleName)
	// this line is used by starport scaffolding # stargate/app/paramSubspace

	return paramsKeeper
}

// SimulationManager implements the SimulationApp interface
func (app *App) SimulationManager() *module.SimulationManager {
	return app.sm
}

func (app *App) BlacklistedAccAddrs() map[string]bool {
	blacklistedAddrs := make(map[string]bool)
	for acc := range maccPerms {
		blacklistedAddrs[authtypes.NewModuleAddress(acc).String()] = !allowedReceivingModAcc[acc]
	}

	return blacklistedAddrs
}

func (app *App) GetPrecompileKeepers() putils.Keepers {
	return NewPrecompileKeepers(app)
}

// test-only
func (app *App) SetTxDecoder(txDecoder sdk.TxDecoder) {
	app.txDecoder = txDecoder
}

func (app *App) inplacetestnetInitializer(pk cryptotypes.PubKey) error {
	app.forkInitializer = func(ctx sdk.Context) {
		val, _ := stakingtypes.NewValidator(
			sdk.ValAddress(pk.Address()), pk, stakingtypes.NewDescription("test", "test", "test", "test", "test"))
		app.StakingKeeper.SetValidator(ctx, val)
		_ = app.StakingKeeper.SetValidatorByConsAddr(ctx, val)
		app.StakingKeeper.SetValidatorByPowerIndex(ctx, val)
		_ = app.SlashingKeeper.AddPubkey(ctx, pk)
		app.SlashingKeeper.SetValidatorSigningInfo(
			ctx,
			sdk.ConsAddress(pk.Address()),
			slashingtypes.NewValidatorSigningInfo(
				sdk.ConsAddress(pk.Address()), 0, 0, time.Unix(0, 0), false, 0,
			),
		)
	}
	return nil
}

func init() {
	// override max wasm size to 2MB
	wasmtypes.MaxWasmSize = 2 * 1024 * 1024
}
