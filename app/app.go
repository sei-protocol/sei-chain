package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/tracing"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/ethclient"
	ethparams "github.com/ethereum/go-ethereum/params"
	ethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/holiman/uint256"
	"github.com/sei-protocol/sei-chain/giga/deps/tasks"
	"github.com/sei-protocol/sei-chain/sei-cosmos/baseapp"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client/grpc/tmservice"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client/rpc"
	"github.com/sei-protocol/sei-chain/sei-cosmos/codec"
	"github.com/sei-protocol/sei-chain/sei-cosmos/codec/types"
	cryptotypes "github.com/sei-protocol/sei-chain/sei-cosmos/crypto/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/server"
	"github.com/sei-protocol/sei-chain/sei-cosmos/server/api"
	"github.com/sei-protocol/sei-chain/sei-cosmos/server/config"
	servertypes "github.com/sei-protocol/sei-chain/sei-cosmos/server/types"
	storetypes "github.com/sei-protocol/sei-chain/sei-cosmos/store/types"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	sdkerrors "github.com/sei-protocol/sei-chain/sei-cosmos/types/errors"
	genesistypes "github.com/sei-protocol/sei-chain/sei-cosmos/types/genesis"
	"github.com/sei-protocol/sei-chain/sei-cosmos/types/module"
	"github.com/sei-protocol/sei-chain/sei-cosmos/types/occ"
	"github.com/sei-protocol/sei-chain/sei-cosmos/version"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/auth"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/ante"
	authrest "github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/client/rest"
	authkeeper "github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/keeper"
	authsims "github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/simulation"
	authtx "github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/tx"
	authtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/vesting"
	vestingtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/vesting/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/authz"
	authzkeeper "github.com/sei-protocol/sei-chain/sei-cosmos/x/authz/keeper"
	authzmodule "github.com/sei-protocol/sei-chain/sei-cosmos/x/authz/module"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/bank"
	bankkeeper "github.com/sei-protocol/sei-chain/sei-cosmos/x/bank/keeper"
	banktypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/bank/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/capability"
	capabilitykeeper "github.com/sei-protocol/sei-chain/sei-cosmos/x/capability/keeper"
	capabilitytypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/capability/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/crisis"
	crisiskeeper "github.com/sei-protocol/sei-chain/sei-cosmos/x/crisis/keeper"
	crisistypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/crisis/types"
	distr "github.com/sei-protocol/sei-chain/sei-cosmos/x/distribution"
	distrclient "github.com/sei-protocol/sei-chain/sei-cosmos/x/distribution/client"
	distrkeeper "github.com/sei-protocol/sei-chain/sei-cosmos/x/distribution/keeper"
	distrtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/distribution/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/evidence"
	evidencekeeper "github.com/sei-protocol/sei-chain/sei-cosmos/x/evidence/keeper"
	evidencetypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/evidence/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/feegrant"
	feegrantkeeper "github.com/sei-protocol/sei-chain/sei-cosmos/x/feegrant/keeper"
	feegrantmodule "github.com/sei-protocol/sei-chain/sei-cosmos/x/feegrant/module"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/genutil"
	genutiltypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/genutil/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/gov"
	govclient "github.com/sei-protocol/sei-chain/sei-cosmos/x/gov/client"
	govkeeper "github.com/sei-protocol/sei-chain/sei-cosmos/x/gov/keeper"
	govtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/gov/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/params"
	paramsclient "github.com/sei-protocol/sei-chain/sei-cosmos/x/params/client"
	paramskeeper "github.com/sei-protocol/sei-chain/sei-cosmos/x/params/keeper"
	paramstypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/params/types"
	paramproposal "github.com/sei-protocol/sei-chain/sei-cosmos/x/params/types/proposal"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/slashing"
	slashingkeeper "github.com/sei-protocol/sei-chain/sei-cosmos/x/slashing/keeper"
	slashingtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/slashing/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/staking"
	stakingkeeper "github.com/sei-protocol/sei-chain/sei-cosmos/x/staking/keeper"
	stakingtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/staking/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/upgrade"
	upgradeclient "github.com/sei-protocol/sei-chain/sei-cosmos/x/upgrade/client"
	upgradekeeper "github.com/sei-protocol/sei-chain/sei-cosmos/x/upgrade/keeper"
	upgradetypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/upgrade/types"

	"github.com/gogo/protobuf/proto"
	"github.com/gorilla/mux"
	"github.com/rakyll/statik/fs"
	"github.com/sei-protocol/sei-chain/app/antedecorators"
	"github.com/sei-protocol/sei-chain/app/benchmark"
	"github.com/sei-protocol/sei-chain/app/legacyabci"
	appparams "github.com/sei-protocol/sei-chain/app/params"
	"github.com/sei-protocol/sei-chain/app/upgrades"
	v0upgrade "github.com/sei-protocol/sei-chain/app/upgrades/v0"
	"github.com/sei-protocol/sei-chain/evmrpc"
	evmrpcconfig "github.com/sei-protocol/sei-chain/evmrpc/config"
	gigaexecutor "github.com/sei-protocol/sei-chain/giga/executor"
	gigaconfig "github.com/sei-protocol/sei-chain/giga/executor/config"
	gigalib "github.com/sei-protocol/sei-chain/giga/executor/lib"
	gigaprecompiles "github.com/sei-protocol/sei-chain/giga/executor/precompiles"
	gigautils "github.com/sei-protocol/sei-chain/giga/executor/utils"
	"github.com/sei-protocol/sei-chain/precompiles"
	putils "github.com/sei-protocol/sei-chain/precompiles/utils"
	ssconfig "github.com/sei-protocol/sei-chain/sei-db/config"
	seidb "github.com/sei-protocol/sei-chain/sei-db/state_db/ss/types"
	"github.com/sei-protocol/sei-chain/sei-ibc-go/modules/apps/transfer"
	ibctransferkeeper "github.com/sei-protocol/sei-chain/sei-ibc-go/modules/apps/transfer/keeper"
	ibctransfertypes "github.com/sei-protocol/sei-chain/sei-ibc-go/modules/apps/transfer/types"
	ibc "github.com/sei-protocol/sei-chain/sei-ibc-go/modules/core"
	ibcclient "github.com/sei-protocol/sei-chain/sei-ibc-go/modules/core/02-client"
	ibcclientclient "github.com/sei-protocol/sei-chain/sei-ibc-go/modules/core/02-client/client"
	ibcclienttypes "github.com/sei-protocol/sei-chain/sei-ibc-go/modules/core/02-client/types"
	ibcporttypes "github.com/sei-protocol/sei-chain/sei-ibc-go/modules/core/05-port/types"
	ibchost "github.com/sei-protocol/sei-chain/sei-ibc-go/modules/core/24-host"
	ibckeeper "github.com/sei-protocol/sei-chain/sei-ibc-go/modules/core/keeper"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	tmcfg "github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/log"
	tmos "github.com/sei-protocol/sei-chain/sei-tendermint/libs/os"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/types"
	wasmkeeper "github.com/sei-protocol/sei-chain/sei-wasmd/x/wasm/keeper"
	"github.com/sei-protocol/sei-chain/utils"
	"github.com/sei-protocol/sei-chain/utils/helpers"
	"github.com/sei-protocol/sei-chain/utils/metrics"
	"github.com/sei-protocol/sei-chain/wasmbinding"
	epochmodule "github.com/sei-protocol/sei-chain/x/epoch"
	epochmodulekeeper "github.com/sei-protocol/sei-chain/x/epoch/keeper"
	epochmoduletypes "github.com/sei-protocol/sei-chain/x/epoch/types"
	"github.com/sei-protocol/sei-chain/x/evm"
	evmante "github.com/sei-protocol/sei-chain/x/evm/ante"
	"github.com/sei-protocol/sei-chain/x/evm/blocktest"
	evmconfig "github.com/sei-protocol/sei-chain/x/evm/config"
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
	"github.com/spf13/cast"
	dbm "github.com/tendermint/tm-db"
	"go.opentelemetry.io/otel/attribute"

	// this line is used by starport scaffolding # stargate/app/moduleImport

	"github.com/sei-protocol/sei-chain/sei-wasmd/x/wasm"
	wasmclient "github.com/sei-protocol/sei-chain/sei-wasmd/x/wasm/client"
	wasmtypes "github.com/sei-protocol/sei-chain/sei-wasmd/x/wasm/types"

	// unnamed import of statik for openapi/swagger UI support
	_ "github.com/sei-protocol/sei-chain/docs/swagger"
	receipt "github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"

	gigabankkeeper "github.com/sei-protocol/sei-chain/giga/deps/xbank/keeper"
	gigaevmkeeper "github.com/sei-protocol/sei-chain/giga/deps/xevm/keeper"
	gigaevmstate "github.com/sei-protocol/sei-chain/giga/deps/xevm/state"
	"github.com/sei-protocol/sei-chain/giga/deps/xevm/types/ethtx"
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
		mintclient.UpdateMinterHandler,
		// this line is used by starport scaffolding # stargate/app/govProposalHandler
	)

	return govProposalHandlers
}

var (
	// DefaultNodeHome default home directories for the application daemon
	DefaultNodeHome string

	// upgradePanicRe matches upgrade panic messages using Cosmovisor-compatible regex
	// Matches multiple upgrade-related panic patterns:
	// 1. UPGRADE "name" NEEDED at height: 123 (or height123)
	// 2. Wrong app version X, upgrade handler is missing for name upgrade plan
	// 3. BINARY UPDATED BEFORE TRIGGER! UPGRADE "name"
	upgradePanicRe = regexp.MustCompile(`^(UPGRADE "[^"]+" NEEDED at height:?\s*\d+|Wrong app version \d+, upgrade handler is missing for .+ upgrade plan|BINARY UPDATED BEFORE TRIGGER! UPGRADE "[^"]+")`)

	// ModuleBasics defines the module BasicManager is in charge of setting up basic,
	// non-dependant module elements, such as codec registration
	// and genesis verification.
	ModuleBasics = module.NewBasicManager(
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
	// EnableOCC allows tests to override default OCC enablement behavior
	EnableOCC       = true
	EmptyAppOptions []AppOption
)

var (
	_ servertypes.Application = (*App)(nil)
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
// gigaBlockCache holds block-constant values that are identical for all txs in a block.
// Populated once before block execution, read-only during parallel execution, cleared after.
type gigaBlockCache struct {
	chainID     *big.Int
	blockCtx    vm.BlockContext
	chainConfig *ethparams.ChainConfig
	baseFee     *big.Int
}

func newGigaBlockCache(ctx sdk.Context, keeper *gigaevmkeeper.Keeper) (*gigaBlockCache, error) {
	chainID := keeper.ChainID(ctx)
	gp := keeper.GetGasPool()
	blockCtx, err := keeper.GetVMBlockContext(ctx, gp)
	if err != nil {
		return nil, err
	}
	sstore := keeper.GetParams(ctx).SeiSstoreSetGasEip2200
	chainConfig := evmtypes.DefaultChainConfig().EthereumConfigWithSstore(chainID, &sstore)
	baseFee := keeper.GetBaseFee(ctx)
	return &gigaBlockCache{
		chainID:     chainID,
		blockCtx:    *blockCtx,
		chainConfig: chainConfig,
		baseFee:     baseFee,
	}, nil
}

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
	AccountKeeper    authkeeper.AccountKeeper
	AuthzKeeper      authzkeeper.Keeper
	BankKeeper       bankkeeper.Keeper
	GigaBankKeeper   *gigabankkeeper.BaseKeeper
	CapabilityKeeper *capabilitykeeper.Keeper
	StakingKeeper    stakingkeeper.Keeper
	SlashingKeeper   slashingkeeper.Keeper
	MintKeeper       mintkeeper.Keeper
	DistrKeeper      distrkeeper.Keeper
	GovKeeper        govkeeper.Keeper
	CrisisKeeper     crisiskeeper.Keeper
	UpgradeKeeper    upgradekeeper.Keeper
	ParamsKeeper     paramskeeper.Keeper
	IBCKeeper        *ibckeeper.Keeper // IBC Keeper must be a pointer in the app, so we can SetRouter on it correctly
	EvidenceKeeper   evidencekeeper.Keeper
	TransferKeeper   ibctransferkeeper.Keeper
	FeeGrantKeeper   feegrantkeeper.Keeper
	WasmKeeper       wasm.Keeper
	OracleKeeper     oraclekeeper.Keeper
	EvmKeeper        evmkeeper.Keeper
	GigaEvmKeeper    gigaevmkeeper.Keeper

	// make scoped keepers public for test purposes
	ScopedIBCKeeper      capabilitykeeper.ScopedKeeper
	ScopedTransferKeeper capabilitykeeper.ScopedKeeper
	ScopedWasmKeeper     capabilitykeeper.ScopedKeeper

	EpochKeeper epochmodulekeeper.Keeper

	TokenFactoryKeeper tokenfactorykeeper.Keeper

	BeginBlockKeepers legacyabci.BeginBlockKeepers
	EndBlockKeepers   legacyabci.EndBlockKeepers
	CheckTxKeepers    legacyabci.CheckTxKeepers
	DeliverTxKeepers  legacyabci.DeliverTxKeepers

	// mm is the module manager
	mm *module.Manager

	// sm is the simulation manager
	sm *module.SimulationManager

	configurator module.Configurator

	optimisticProcessingInfo      OptimisticProcessingInfo
	optimisticProcessingInfoMutex sync.RWMutex

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
	evmRPCConfig          evmrpcconfig.Config
	lightInvarianceConfig LightInvarianceConfig

	genesisImportConfig genesistypes.GenesisImportConfig

	stateStore   seidb.StateStore
	receiptStore receipt.ReceiptStore

	forkInitializer func(sdk.Context)

	httpServerStartSignal     chan struct{}
	wsServerStartSignal       chan struct{}
	httpServerStartSignalSent bool
	wsServerStartSignalSent   bool

	txPrioritizer sdk.TxPrioritizer

	benchmarkManager *benchmark.Manager

	// GigaExecutorEnabled controls whether to use the Giga executor (evmone-based)
	// instead of geth's interpreter for EVM execution. Experimental feature.
	GigaExecutorEnabled bool
	// GigaOCCEnabled controls whether to use OCC with the Giga executor
	GigaOCCEnabled bool
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

	// Bind OTEL metrics provider once at application construction
	if err := metrics.SetupOtelMetricsProvider(); err != nil {
		logger.Error(err.Error())
	}

	keys := sdk.NewKVStoreKeys(
		authtypes.StoreKey, authzkeeper.StoreKey, banktypes.StoreKey, stakingtypes.StoreKey,
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
	gigaBankKeeper := gigabankkeeper.NewBaseKeeperWithDeferredCache(
		appCodec, keys[banktypes.StoreKey], app.AccountKeeper, app.GetSubspace(banktypes.ModuleName), app.ModuleAccountAddrs(), memKeys[banktypes.DeferredCacheStoreKey],
	)
	app.GigaBankKeeper = &gigaBankKeeper
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
	receiptConfig := ssconfig.DefaultReceiptStoreConfig()
	receiptConfig.DBDirectory = receiptStorePath
	receiptConfig.KeepRecent = cast.ToInt(appOpts.Get(server.FlagMinRetainBlocks))
	if app.receiptStore == nil {
		receiptStore, err := receipt.NewReceiptStore(logger, receiptConfig, keys[evmtypes.StoreKey])
		if err != nil {
			panic(fmt.Sprintf("error while creating receipt store: %s", err))
		}
		app.receiptStore = receiptStore
	}
	app.EvmKeeper = *evmkeeper.NewKeeper(keys[evmtypes.StoreKey],
		tkeys[evmtypes.TransientStoreKey], app.GetSubspace(evmtypes.ModuleName), app.receiptStore, app.BankKeeper,
		&app.AccountKeeper, &app.StakingKeeper, app.TransferKeeper,
		wasmkeeper.NewDefaultPermissionKeeper(app.WasmKeeper), &app.WasmKeeper, &app.UpgradeKeeper)
	app.BankKeeper.RegisterRecipientChecker(app.EvmKeeper.CanAddressReceive)

	bApp.SetPreCommitHandler(app.HandlePreCommit)
	bApp.SetCloseHandler(app.HandleClose)

	app.evmRPCConfig, err = evmrpcconfig.ReadConfig(appOpts)
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

	app.GigaEvmKeeper = *gigaevmkeeper.NewKeeper(keys[evmtypes.StoreKey],
		tkeys[evmtypes.TransientStoreKey], app.GetSubspace(evmtypes.ModuleName), app.receiptStore, app.GigaBankKeeper,
		&app.AccountKeeper, &app.StakingKeeper, app.TransferKeeper,
		wasmkeeper.NewDefaultPermissionKeeper(app.WasmKeeper), &app.WasmKeeper, &app.UpgradeKeeper)
	app.GigaEvmKeeper.UseRegularStore = true
	app.GigaBankKeeper.UseRegularStore = true
	app.GigaBankKeeper.RegisterRecipientChecker(app.GigaEvmKeeper.CanAddressReceive)
	// Read Giga Executor config
	gigaExecutorConfig, err := gigaconfig.ReadConfig(appOpts)
	if err != nil {
		panic(fmt.Sprintf("error reading giga executor config due to %s", err))
	}
	app.GigaExecutorEnabled = gigaExecutorConfig.Enabled
	app.GigaOCCEnabled = gigaExecutorConfig.OCCEnabled
	tmtypes.SkipLastResultsHashValidation.Store(gigaExecutorConfig.Enabled)
	if gigaExecutorConfig.Enabled {
		evmoneVM, err := gigalib.InitEvmoneVM()
		if err != nil {
			panic(fmt.Sprintf("failed to load evmone: %s", err))
		}
		app.GigaEvmKeeper.EvmoneVM = evmoneVM
		if gigaExecutorConfig.OCCEnabled {
			logger.Info("benchmark: Giga Executor with OCC is ENABLED - using new EVM execution path with parallel execution")
		} else {
			logger.Info("benchmark: Giga Executor (evmone-based) is ENABLED - using new EVM execution path (sequential)")
		}
	} else {
		logger.Info("benchmark: Giga Executor is DISABLED - using default GETH interpreter")
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

	app.BeginBlockKeepers = legacyabci.BeginBlockKeepers{
		EpochKeeper:      &app.EpochKeeper,
		UpgradeKeeper:    &app.UpgradeKeeper,
		CapabilityKeeper: app.CapabilityKeeper,
		DistrKeeper:      &app.DistrKeeper,
		SlashingKeeper:   &app.SlashingKeeper,
		EvidenceKeeper:   &app.EvidenceKeeper,
		StakingKeeper:    &app.StakingKeeper,
		IBCKeeper:        app.IBCKeeper,
		EvmKeeper:        &app.EvmKeeper,
	}
	app.EndBlockKeepers = legacyabci.EndBlockKeepers{
		CrisisKeeper:  &app.CrisisKeeper,
		GovKeeper:     &app.GovKeeper,
		StakingKeeper: &app.StakingKeeper,
		OracleKeeper:  &app.OracleKeeper,
		EvmKeeper:     &app.EvmKeeper,
	}
	app.CheckTxKeepers = legacyabci.CheckTxKeepers{
		AccountKeeper:  app.AccountKeeper,
		BankKeeper:     app.BankKeeper,
		FeeGrantKeeper: &app.FeeGrantKeeper,
		IBCKeeper:      app.IBCKeeper,
		OracleKeeper:   app.OracleKeeper,
		EvmKeeper:      &app.EvmKeeper,
		ParamsKeeper:   app.ParamsKeeper,
		UpgradeKeeper:  &app.UpgradeKeeper,
	}
	app.DeliverTxKeepers = legacyabci.DeliverTxKeepers{
		AccountKeeper:  app.AccountKeeper,
		BankKeeper:     app.BankKeeper,
		FeeGrantKeeper: &app.FeeGrantKeeper,
		OracleKeeper:   app.OracleKeeper,
		EvmKeeper:      &app.EvmKeeper,
		ParamsKeeper:   app.ParamsKeeper,
		UpgradeKeeper:  &app.UpgradeKeeper,
	}

	app.mm.SetOrderMidBlockers(
		oracletypes.ModuleName,
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

	signModeHandler := encodingConfig.TxConfig.SignModeHandler()

	anteHandler, tracerAnteHandler, err := NewAnteHandler(
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
			IBCKeeper:         app.IBCKeeper,
			TXCounterStoreKey: keys[wasm.StoreKey],
			WasmConfig:        &wasmConfig,
			WasmKeeper:        &app.WasmKeeper,
			OracleKeeper:      &app.OracleKeeper,
			EVMKeeper:         &app.EvmKeeper,
			UpgradeKeeper:     &app.UpgradeKeeper,
			TracingInfo:       app.GetBaseApp().TracingInfo,
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
	app.SetMidBlocker(app.MidBlocker)

	// benchmarkEnabled is enabled via build flag (make install-bench)
	if benchmarkEnabled {
		evmChainID := evmconfig.GetEVMChainID(app.ChainID).Int64()
		app.InitBenchmark(context.Background(), app.ChainID, evmChainID, logger)
		app.SetPrepareProposalHandler(app.PrepareProposalBenchmarkHandler)
	} else {
		app.SetPrepareProposalHandler(app.PrepareProposalHandler)
	}

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

		ctx := app.NewUncachedContext(true, tmproto.Header{})
		if err := app.WasmKeeper.InitializePinnedCodes(ctx); err != nil {
			tmos.Exit(fmt.Sprintf("failed initialize pinned codes %s", err))
		}
		return nil
	}

	if err := loadVersionHandler(); err != nil {
		panic(err)
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

	app.txPrioritizer = NewSeiTxPrioritizer(logger, &app.EvmKeeper, &app.UpgradeKeeper, &app.ParamsKeeper).GetTxPriorityHint
	app.SetTxPrioritizer(app.txPrioritizer)

	return app
}

// HandlePreCommit happens right before the block is committed
func (app *App) HandlePreCommit(ctx sdk.Context) error {
	return app.EvmKeeper.FlushTransientReceipts(ctx)
}

// Close closes all items that needs closing (called by baseapp)
func (app *App) HandleClose() error {
	var errs []error

	// Close receipt store
	if app.receiptStore != nil {
		if err := app.receiptStore.Close(); err != nil {
			app.Logger().Error("failed to close receipt store", "error", err)
			errs = append(errs, fmt.Errorf("failed to close receipt store: %w", err))
		}
	}

	// Note: stateStore (ssStore) is already closed by cms.Close() in BaseApp.Close()
	// No need to close it again here.

	if len(errs) > 0 {
		return fmt.Errorf("errors during close: %v", errs)
	}
	return nil
}

// Add (or remove) keepers when they are introduced / removed in different versions
func (app *App) SetStoreUpgradeHandlers() {
	upgradeInfo, err := app.UpgradeKeeper.ReadUpgradeInfoFromDisk()
	if err != nil {
		panic(err)
	}

	accesscontrolStoreKeyName := "aclaccesscontrol"

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
			Added: []string{accesscontrolStoreKeyName},
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

	if (upgradeInfo.Name == "v6.3.0") && !app.UpgradeKeeper.IsSkipHeight(upgradeInfo.Height) {
		storeUpgrades := storetypes.StoreUpgrades{
			Deleted: []string{accesscontrolStoreKeyName},
		}

		// configure store loader that checks if version == upgradeHeight and applies store upgrades
		app.SetStoreLoader(upgradetypes.UpgradeStoreLoader(upgradeInfo.Height, &storeUpgrades))
	}
}

// AppName returns the name of the App
func (app *App) Name() string { return app.BaseApp.Name() }

// GetBaseApp returns the base app of the application
func (app *App) GetBaseApp() *baseapp.BaseApp { return app.BaseApp }

// GetStateStore returns the state store of the application
func (app *App) GetStateStore() seidb.StateStore { return app.stateStore }

// MidBlocker application updates every mid block
func (app *App) MidBlocker(ctx sdk.Context, height int64) []abci.Event {
	return app.mm.MidBlock(ctx, height)
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

func (app *App) GetOptimisticProcessingInfo() OptimisticProcessingInfo {
	app.optimisticProcessingInfoMutex.RLock()
	defer app.optimisticProcessingInfoMutex.RUnlock()
	return app.optimisticProcessingInfo
}

func (app *App) ClearOptimisticProcessingInfo() {
	app.optimisticProcessingInfoMutex.Lock()
	defer app.optimisticProcessingInfoMutex.Unlock()
	app.optimisticProcessingInfo = OptimisticProcessingInfo{}
}

func (app *App) ProcessProposalHandler(ctx sdk.Context, req *abci.RequestProcessProposal) (resp *abci.ResponseProcessProposal, err error) {
	// Start block processing timing (ends at FinalizeBlock)
	app.StartBenchmarkBlockProcessing()

	// TODO: this check decodes transactions which is redone in subsequent processing. We might be able to optimize performance
	// by recording the decoding results and avoid decoding again later on.

	if !app.checkTotalBlockGas(ctx, req.Txs) {
		metrics.IncrFailedTotalGasWantedCheck(string(req.GetProposerAddress()))
		return &abci.ResponseProcessProposal{
			Status: abci.ResponseProcessProposal_REJECT,
		}, nil
	}

	app.optimisticProcessingInfoMutex.Lock()
	shouldStartOptimisticProcessing := app.optimisticProcessingInfo.Completion == nil
	if shouldStartOptimisticProcessing {
		completionSignal := make(chan struct{}, 1)
		app.optimisticProcessingInfo = OptimisticProcessingInfo{
			Height:     req.Height,
			Hash:       req.Hash,
			Completion: completionSignal,
		}
	}
	app.optimisticProcessingInfoMutex.Unlock()

	if shouldStartOptimisticProcessing {
		plan, found := app.UpgradeKeeper.GetUpgradePlan(ctx)
		if found && plan.ShouldExecute(ctx) {
			app.Logger().Info("Potential upgrade planned; skipping optimistic processing", "height", plan.Height)
			app.optimisticProcessingInfoMutex.Lock()
			app.optimisticProcessingInfo.Aborted = true
			completion := app.optimisticProcessingInfo.Completion
			app.optimisticProcessingInfoMutex.Unlock()
			completion <- struct{}{}
		} else {
			go func() {
				// ProcessBlock has panic recovery and returns error for any processing failures
				// All panics (including GetSigners) are handled in ProcessBlock, not affecting proposal acceptance
				events, txResults, endBlockResp, processErr := app.ProcessBlock(ctx, req.Txs, req, req.ProposedLastCommit, false)

				app.optimisticProcessingInfoMutex.Lock()
				if processErr != nil {
					// ProcessBlock failed (including GetSigners panics), mark as aborted
					app.Logger().Info("ProcessBlock failed in optimistic processing", "error", processErr)
					app.optimisticProcessingInfo.Aborted = true
				} else {
					// ProcessBlock succeeded, store results
					app.optimisticProcessingInfo.Events = events
					app.optimisticProcessingInfo.TxRes = txResults
					app.optimisticProcessingInfo.EndBlockResp = endBlockResp
				}
				completion := app.optimisticProcessingInfo.Completion
				app.optimisticProcessingInfoMutex.Unlock()
				completion <- struct{}{}
			}()
		}
	} else {
		// Optimistic processing already running, check if hash matches
		if !bytes.Equal(app.GetOptimisticProcessingInfo().Hash, req.Hash) {
			app.optimisticProcessingInfoMutex.Lock()
			app.optimisticProcessingInfo.Aborted = true
			app.optimisticProcessingInfoMutex.Unlock()
		}
	}

	resp = &abci.ResponseProcessProposal{
		Status: abci.ResponseProcessProposal_ACCEPT,
	}

	return resp, nil
}

func (app *App) FinalizeBlocker(ctx sdk.Context, req *abci.RequestFinalizeBlock) (*abci.ResponseFinalizeBlock, error) {
	startTime := time.Now()
	defer func() {
		app.ClearOptimisticProcessingInfo()
		duration := time.Since(startTime)
		ctx.Logger().Info(fmt.Sprintf("FinalizeBlock took %dms", duration/time.Millisecond))
		// End block processing timing (started at ProcessProposal)
		app.EndBenchmarkBlockProcessing()
		// Process receipts for benchmark deployment tracking
		app.ProcessBenchmarkReceipts(ctx)
	}()

	// Get all optimistic processing info atomically
	app.optimisticProcessingInfoMutex.RLock()
	completion := app.optimisticProcessingInfo.Completion
	app.optimisticProcessingInfoMutex.RUnlock()

	if completion != nil {
		<-completion

		// Get the final state atomically after completion
		app.optimisticProcessingInfoMutex.RLock()
		aborted := app.optimisticProcessingInfo.Aborted
		finalHash := app.optimisticProcessingInfo.Hash
		events := app.optimisticProcessingInfo.Events
		txRes := app.optimisticProcessingInfo.TxRes
		endBlockResp := app.optimisticProcessingInfo.EndBlockResp
		app.optimisticProcessingInfoMutex.RUnlock()

		if !aborted && bytes.Equal(finalHash, req.Hash) {
			metrics.IncrementOptimisticProcessingCounter(true)
			app.SetProcessProposalStateToCommit()
			if app.EvmKeeper.EthReplayConfig.Enabled || app.EvmKeeper.EthBlockTestConfig.Enabled {
				return &abci.ResponseFinalizeBlock{}, nil
			}
			cms := app.WriteState()
			app.LightInvarianceChecks(cms, app.lightInvarianceConfig)
			appHash := app.GetWorkingHash()
			resp := app.getFinalizeBlockResponse(appHash, events, txRes, endBlockResp)
			return &resp, nil
		}
	}
	metrics.IncrementOptimisticProcessingCounter(false)
	ctx.Logger().Info("optimistic processing ineligible")
	events, txResults, endBlockResp, processErr := app.ProcessBlock(ctx, req.Txs, req, req.DecidedLastCommit, false)
	if processErr != nil {
		ctx.Logger().Error("ProcessBlock failed in FinalizeBlocker", "error", processErr)
		return nil, processErr
	}

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
	deliverTxResp := app.DeliverTx(ctx, abci.RequestDeliverTxV2{
		Tx: tx,
	}, typedTx, sha256.Sum256(tx))

	// Check if transaction is gasless before recording metrics
	// perf optimization: skip gasless check for obviously non-gasless transaction types
	shouldCheckGasless := app.couldBeGaslessTransaction(typedTx)

	var skipMetrics bool
	if shouldCheckGasless {
		// Only do expensive validation for potentially gasless transactions
		isGasless, err := antedecorators.IsTxGasless(typedTx, ctx, app.OracleKeeper, &app.EvmKeeper)
		if err != nil {
			if isExpectedGaslessMetricsError(err) {
				// ErrAggregateVoteExist is expected when checking gasless status after tx processing
				// since oracle votes will now exist in state. We know it was gasless, skip metrics.
				skipMetrics = true
			} else {
				ctx.Logger().Debug("error checking if tx is gasless for metrics", "error", err)
				// If we can't determine if it's gasless, record metrics to maintain existing behavior
			}
		} else if isGasless {
			skipMetrics = true // Skip metrics for confirmed gasless transactions
		}
	}

	if !skipMetrics {
		// Record metrics for non-gasless transactions
		metrics.IncrGasCounter("gas_used", deliverTxResp.GasUsed)
		metrics.IncrGasCounter("gas_wanted", deliverTxResp.GasWanted)
	}

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

func (app *App) ProcessTxsSynchronousV2(ctx sdk.Context, txs [][]byte, typedTxs []sdk.Tx, absoluteTxIndices []int) []*abci.ExecTxResult {
	defer metrics.BlockProcessLatency(time.Now(), metrics.SYNCHRONOUS)

	txResults := make([]*abci.ExecTxResult, 0, len(txs))
	for i, tx := range txs {
		ctx = ctx.WithTxIndex(absoluteTxIndices[i])
		res := app.DeliverTxWithResult(ctx, tx, typedTxs[i])
		txResults = append(txResults, res)
		metrics.IncrTxProcessTypeCounter(metrics.SYNCHRONOUS)
	}
	return txResults
}

func (app *App) ProcessTxsSynchronousGiga(ctx sdk.Context, txs [][]byte, typedTxs []sdk.Tx, absoluteTxIndices []int) []*abci.ExecTxResult {
	defer metrics.BlockProcessLatency(time.Now(), metrics.SYNCHRONOUS)

	ms := ctx.MultiStore().CacheMultiStore()
	defer ms.Write()
	ctx = ctx.WithMultiStore(ms)

	// Cache block-level constants (identical for all txs in this block).
	cache, cacheErr := newGigaBlockCache(ctx, &app.GigaEvmKeeper)
	if cacheErr != nil {
		ctx.Logger().Error("failed to build giga block cache", "error", cacheErr, "height", ctx.BlockHeight())
		return nil
	}

	txResults := make([]*abci.ExecTxResult, len(txs))
	for i, tx := range txs {
		ctx = ctx.WithTxIndex(absoluteTxIndices[i])
		evmMsg := app.GetEVMMsg(typedTxs[i])
		// If not an EVM tx, fall back to v2 processing
		if evmMsg == nil {
			result := app.DeliverTxWithResult(ctx, tx, typedTxs[i])
			txResults[i] = result
			ms.Write()
			continue
		}

		// Execute EVM transaction through giga executor
		result, execErr := app.executeEVMTxWithGigaExecutor(ctx, evmMsg, cache)
		if execErr != nil {
			// Check if this is a fail-fast error (Cosmos precompile interop detected)
			if gigautils.ShouldExecutionAbort(execErr) {
				res := app.DeliverTxWithResult(ctx, tx, typedTxs[i])
				txResults[i] = res
				ms.Write()
				continue
			}
			txResults[i] = &abci.ExecTxResult{
				Code: 1,
				Log:  fmt.Sprintf("[BUG] giga executor error: %v", execErr),
			}
			continue
		}

		txResults[i] = result
		ctx.GigaMultiStore().WriteGiga()
		metrics.IncrTxProcessTypeCounter(metrics.SYNCHRONOUS)
	}

	return txResults
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
	// Giga only supports synchronous execution for now
	if app.GigaExecutorEnabled && app.GigaOCCEnabled {
		return app.ProcessTXsWithOCCGiga(ctx, txs, typedTxs, absoluteTxIndices)
	} else if app.GigaExecutorEnabled {
		return app.ProcessTxsSynchronousGiga(ctx, txs, typedTxs, absoluteTxIndices), ctx
	} else if !ctx.IsOCCEnabled() {
		return app.ProcessTxsSynchronousV2(ctx, txs, typedTxs, absoluteTxIndices), ctx
	}

	return app.ProcessTXsWithOCCV2(ctx, txs, typedTxs, absoluteTxIndices)
}

func (app *App) GetDeliverTxEntry(ctx sdk.Context, txIndex int, absoluateIndex int, bz []byte, tx sdk.Tx) (res *sdk.DeliverTxEntry) {
	res = &sdk.DeliverTxEntry{
		Request:       abci.RequestDeliverTxV2{Tx: bz},
		SdkTx:         tx,
		Checksum:      sha256.Sum256(bz),
		AbsoluteIndex: absoluateIndex,
	}
	return
}

// ProcessTXsWithOCCV2 runs the transactions concurrently via OCC, using the V2 executor
func (app *App) ProcessTXsWithOCCV2(ctx sdk.Context, txs [][]byte, typedTxs []sdk.Tx, absoluteTxIndices []int) ([]*abci.ExecTxResult, sdk.Context) {
	entries := make([]*sdk.DeliverTxEntry, len(txs))
	for txIndex, tx := range txs {
		entries[txIndex] = app.GetDeliverTxEntry(ctx, txIndex, absoluteTxIndices[txIndex], tx, typedTxs[txIndex])
	}

	batchResult := app.DeliverTxBatch(ctx, sdk.DeliverTxBatchRequest{TxEntries: entries})

	execResults := make([]*abci.ExecTxResult, 0, len(batchResult.Results))
	for i, r := range batchResult.Results {
		metrics.IncrTxProcessTypeCounter(metrics.OCC_CONCURRENT)

		// Check if transaction is gasless before recording gas metrics
		var recordGasMetrics = true
		if i < len(typedTxs) {
			// perf optimization: skip gasless check for obviously non-gasless transaction types
			shouldCheckGasless := app.couldBeGaslessTransaction(typedTxs[i])
			if shouldCheckGasless {
				// Only do expensive validation for potentially gasless transactions
				isGasless, err := antedecorators.IsTxGasless(typedTxs[i], ctx, app.OracleKeeper, &app.EvmKeeper)
				if err != nil {
					if isExpectedGaslessMetricsError(err) {
						// ErrAggregateVoteExist is expected when checking gasless status after tx processing
						// since oracle votes will now exist in state. We know it was gasless, skip metrics.
						recordGasMetrics = false
					} else {
						ctx.Logger().Debug("error checking if tx is gasless for OCC metrics", "error", err, "txIndex", i)
						// If we can't determine if it's gasless, record metrics to maintain existing behavior
					}
				} else if isGasless {
					recordGasMetrics = false
				}
			}
		}

		if recordGasMetrics {
			metrics.IncrGasCounter("gas_used", r.Response.GasUsed)
			metrics.IncrGasCounter("gas_wanted", r.Response.GasWanted)
		}

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

// ProcessTXsWithOCCGiga runs the transactions concurrently via OCC, using the Giga executor
func (app *App) ProcessTXsWithOCCGiga(ctx sdk.Context, txs [][]byte, typedTxs []sdk.Tx, absoluteTxIndices []int) ([]*abci.ExecTxResult, sdk.Context) {
	evmEntries := make([]*sdk.DeliverTxEntry, 0, len(txs))
	v2Entries := make([]*sdk.DeliverTxEntry, 0, len(txs))
	for txIndex, tx := range txs {
		if app.GetEVMMsg(typedTxs[txIndex]) != nil {
			evmEntries = append(evmEntries, app.GetDeliverTxEntry(ctx, txIndex, absoluteTxIndices[txIndex], tx, typedTxs[txIndex]))
		} else {
			v2Entries = append(v2Entries, app.GetDeliverTxEntry(ctx, txIndex, absoluteTxIndices[txIndex], tx, typedTxs[txIndex]))
		}
	}

	// Run EVM txs against a cache so we can discard all changes on fallback.
	evmCtx, evmCache := app.CacheContext(ctx)

	// Cache block-level constants (identical for all txs in this block).
	// Must use evmCtx (not ctx) because giga KV stores are registered in CacheContext.
	cache, cacheErr := newGigaBlockCache(evmCtx, &app.GigaEvmKeeper)
	if cacheErr != nil {
		ctx.Logger().Error("failed to build giga block cache", "error", cacheErr, "height", ctx.BlockHeight())
		return nil, ctx
	}

	// Create OCC scheduler with giga executor deliverTx capturing the cache.
	evmScheduler := tasks.NewScheduler(
		app.ConcurrencyWorkers(),
		app.TracingInfo,
		app.makeGigaDeliverTx(cache),
	)

	evmBatchResult, evmSchedErr := evmScheduler.ProcessAll(evmCtx, evmEntries)
	if evmSchedErr != nil {
		// TODO: DeliverTxBatch panics in this case
		// TODO: detect if it was interop, and use v2 if so
		ctx.Logger().Error("benchmark OCC scheduler error (EVM txs)", "error", evmSchedErr, "height", ctx.BlockHeight(), "txCount", len(evmEntries))
		return nil, ctx
	}

	fallbackToV2 := false
	for _, r := range evmBatchResult {
		if r.Code == gigautils.GigaAbortCode && r.Codespace == gigautils.GigaAbortCodespace {
			fallbackToV2 = true
			break
		}
	}

	if fallbackToV2 {
		metrics.IncrGigaFallbackToV2Counter()
		// Discard all EVM changes by skipping cache writes, then re-run all txs via DeliverTx.
		evmBatchResult = nil
		v2Entries = make([]*sdk.DeliverTxEntry, len(txs))
		for txIndex, tx := range txs {
			v2Entries[txIndex] = app.GetDeliverTxEntry(ctx, txIndex, absoluteTxIndices[txIndex], tx, typedTxs[txIndex])
		}
	} else {
		// Commit EVM cache to main store before processing non-EVM txs.
		evmCache.Write()
		evmCtx.GigaMultiStore().WriteGiga()
	}

	v2Scheduler := tasks.NewScheduler(
		app.ConcurrencyWorkers(),
		app.TracingInfo,
		app.DeliverTx,
	)
	v2BatchResult, v2SchedErr := v2Scheduler.ProcessAll(ctx, v2Entries)
	if v2SchedErr != nil {
		ctx.Logger().Error("benchmark OCC scheduler error", "error", v2SchedErr, "height", ctx.BlockHeight(), "txCount", len(v2Entries))
		return nil, ctx
	}

	execResults := make([]*abci.ExecTxResult, 0, len(evmBatchResult)+len(v2BatchResult))
	for _, r := range evmBatchResult {
		execResults = append(execResults, &abci.ExecTxResult{
			Code:      r.Code,
			Data:      r.Data,
			Log:       r.Log,
			Info:      r.Info,
			GasWanted: r.GasWanted,
			GasUsed:   r.GasUsed,
			Events:    r.Events,
			Codespace: r.Codespace,
			EvmTxInfo: r.EvmTxInfo,
		})
	}
	for _, r := range v2BatchResult {
		execResults = append(execResults, &abci.ExecTxResult{
			Code:      r.Code,
			Data:      r.Data,
			Log:       r.Log,
			Info:      r.Info,
			GasWanted: r.GasWanted,
			GasUsed:   r.GasUsed,
			Events:    r.Events,
			Codespace: r.Codespace,
			EvmTxInfo: r.EvmTxInfo,
		})
	}

	return execResults, ctx
}

func (app *App) ProcessBlock(ctx sdk.Context, txs [][]byte, req BlockProcessRequest, lastCommit abci.CommitInfo, simulate bool) (events []abci.Event, txResults []*abci.ExecTxResult, endBlockResp abci.ResponseEndBlock, err error) {
	defer func() {
		if r := recover(); r != nil {
			panicMsg := fmt.Sprintf("%v", r)

			// Re-panic for upgrade-related panics to allow proper upgrade mechanism
			if upgradePanicRe.MatchString(panicMsg) {
				ctx.Logger().Error("upgrade panic detected, panicking to trigger upgrade", "panic", r)
				panic(r) // Re-panic to trigger upgrade mechanism
			}
			stack := string(debug.Stack())
			ctx.Logger().Error("panic recovered in ProcessBlock", "panic", r, "stack", stack)
			err = fmt.Errorf("ProcessBlock panic: %v", r)
			events = nil
			txResults = nil
			endBlockResp = abci.ResponseEndBlock{}
		}
	}()

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

	blockSpanCtx, blockSpan := app.GetBaseApp().TracingInfo.Start("Block")
	defer blockSpan.End()
	blockSpan.SetAttributes(attribute.Int64("height", req.GetHeight()))
	ctx = ctx.WithTraceSpanContext(blockSpanCtx)

	beginBlockResp := app.BeginBlock(ctx, req.GetHeight(), lastCommit.Votes, req.GetByzantineValidators(), true)
	events = append(events, beginBlockResp.Events...)

	evmTxs := make([]*evmtypes.MsgEVMTransaction, len(txs)) // nil for non-EVM txs
	txResults = make([]*abci.ExecTxResult, len(txs))
	typedTxs := app.DecodeTransactionsConcurrently(ctx, txs)

	prioritizedTxs, otherTxs, prioritizedTypedTxs, otherTypedTxs, prioritizedIndices, otherIndices := app.PartitionPrioritizedTxs(ctx, txs, typedTxs)

	// run the prioritized txs
	prioritizedResults, ctx := app.ExecuteTxsConcurrently(ctx, prioritizedTxs, prioritizedTypedTxs, prioritizedIndices)
	for relativePrioritizedIndex, originalIndex := range prioritizedIndices {
		txResults[originalIndex] = prioritizedResults[relativePrioritizedIndex]
		evmTxs[originalIndex] = app.GetEVMMsg(prioritizedTypedTxs[relativePrioritizedIndex])
	}

	// Flush giga stores so WriteDeferredBalances (which uses the standard BankKeeper)
	// can see balance changes made by the giga executor via GigaBankKeeper.
	if app.GigaExecutorEnabled {
		ctx.GigaMultiStore().WriteGiga()
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

	// Flush giga stores after second round (same reason as above)
	if app.GigaExecutorEnabled {
		ctx.GigaMultiStore().WriteGiga()
	}

	app.EvmKeeper.SetTxResults(txResults)
	app.EvmKeeper.SetMsgs(evmTxs)

	// Finalize all Bank Module Transfers here so that events are included
	lazyWriteEvents := app.BankKeeper.WriteDeferredBalances(ctx)
	events = append(events, lazyWriteEvents...)

	// Sum up total used per block only for evm transactions
	var evmTotalGasUsed int64
	for _, txResult := range txResults {
		if txResult.EvmTxInfo != nil {
			evmTotalGasUsed += txResult.GasUsed
		}
	}

	endBlockResp = app.EndBlock(ctx, req.GetHeight(), evmTotalGasUsed)

	events = append(events, endBlockResp.Events...)
	return events, txResults, endBlockResp, nil
}

// executeEVMTxWithGigaExecutor executes a single EVM transaction using the giga executor.
// The sender address is recovered directly from the transaction signature - no Cosmos SDK ante handlers needed.
func (app *App) executeEVMTxWithGigaExecutor(ctx sdk.Context, msg *evmtypes.MsgEVMTransaction, cache *gigaBlockCache) (*abci.ExecTxResult, error) {
	// Get the Ethereum transaction from the message
	ethTx, txData := msg.AsTransaction()
	if ethTx == nil || txData == nil {
		return nil, fmt.Errorf("failed to convert to eth transaction")
	}

	chainID := cache.chainID

	// Recover sender using the same logic as preprocess.go (version-based signer selection)
	sender, seiAddr, pubkey, recoverErr := evmante.RecoverSenderFromEthTx(ctx, ethTx, chainID)
	if recoverErr != nil {
		return &abci.ExecTxResult{
			Code: 1,
			Log:  fmt.Sprintf("failed to recover sender from signature: %v", recoverErr),
		}, nil
	}

	_, isAssociated := app.GigaEvmKeeper.GetEVMAddress(ctx, seiAddr)

	// ============================================================================
	// Fee validation (mirrors V2's ante handler checks in evm_checktx.go)
	// NOTE: In V2, failed transactions still increment nonce and charge gas.
	// We track validation errors here but don't return early - we still need to
	// create stateDB, increment nonce, and finalize state to match V2 behavior.
	// ============================================================================
	baseFee := app.GigaEvmKeeper.GetBaseFee(ctx)
	if baseFee == nil {
		baseFee = new(big.Int) // default to 0 when base fee is unset
	}

	// Track validation errors - we'll skip execution but still finalize state
	var validationErr *abci.ExecTxResult

	// 1. Fee cap < base fee check (INSUFFICIENT_MAX_FEE_PER_GAS)
	// V2: evm_checktx.go line 284-286
	if txData.GetGasFeeCap().Cmp(baseFee) < 0 {
		validationErr = &abci.ExecTxResult{
			Code: sdkerrors.ErrInsufficientFee.ABCICode(),
			Log:  "max fee per gas less than block base fee",
		}
	}

	// 2. Tip > fee cap check (PRIORITY_GREATER_THAN_MAX_FEE_PER_GAS)
	// This is checked in txData.Validate() for DynamicFeeTx, but we also check here
	// to ensure consistent rejection before execution.
	if validationErr == nil && txData.GetGasTipCap().Cmp(txData.GetGasFeeCap()) > 0 {
		validationErr = &abci.ExecTxResult{
			Code: 1,
			Log:  "max priority fee per gas higher than max fee per gas",
		}
	}

	// 3. Gas limit * gas price overflow check (GASLIMIT_PRICE_PRODUCT_OVERFLOW)
	// V2: Uses IsValidInt256(tx.Fee()) in dynamic_fee_tx.go Validate()
	// Fee = GasFeeCap * GasLimit, must fit in 256 bits
	if validationErr == nil && !ethtx.IsValidInt256(txData.Fee()) {
		validationErr = &abci.ExecTxResult{
			Code: 1,
			Log:  "fee out of bound",
		}
	}

	// 4. TX gas limit > block gas limit check (GAS_ALLOWANCE_EXCEEDED)
	// V2: x/evm/ante/basic.go lines 63-68
	if validationErr == nil {
		if cp := ctx.ConsensusParams(); cp != nil && cp.Block != nil {
			if cp.Block.MaxGas > 0 && ethTx.Gas() > uint64(cp.Block.MaxGas) { //nolint:gosec
				validationErr = &abci.ExecTxResult{
					Code: sdkerrors.ErrOutOfGas.ABCICode(),
					Log:  fmt.Sprintf("tx gas limit %d exceeds block max gas %d", ethTx.Gas(), cp.Block.MaxGas),
				}
			}
		}
	}

	// 5. Insufficient balance check for gas * price + value (INSUFFICIENT_FUNDS_FOR_TRANSFER)
	if validationErr == nil {
		// BuyGas checks balance against GasLimit * GasFeeCap + Value (see go-ethereum/core/state_transition.go:264-291)
		balanceCheck := new(big.Int).Mul(new(big.Int).SetUint64(ethTx.Gas()), ethTx.GasFeeCap())
		balanceCheck.Add(balanceCheck, ethTx.Value())

		senderBalance := app.GigaEvmKeeper.GetBalance(ctx, seiAddr)

		// For unassociated addresses, V2's PreprocessDecorator migrates the cast address balance
		// BEFORE the fee check (in a CacheMultiStore). We need to include the cast address balance
		// in our check to match V2's behavior, even though we defer the actual migration.
		if !isAssociated {
			// Cast address is the EVM address bytes interpreted as a Sei address
			castAddr := sdk.AccAddress(sender[:])
			castBalance := app.GigaEvmKeeper.GetBalance(ctx, castAddr)
			senderBalance = new(big.Int).Add(senderBalance, castBalance)
		}

		if senderBalance.Cmp(balanceCheck) < 0 {
			validationErr = &abci.ExecTxResult{
				Code: sdkerrors.ErrInsufficientFunds.ABCICode(),
				Log:  fmt.Sprintf("insufficient funds for gas * price + value: address %s have %v want %v: insufficient funds", sender.Hex(), senderBalance, balanceCheck),
			}
		}
	}

	// Prepare context for EVM transaction (set infinite gas meter like original flow)
	ctx = ctx.WithGasMeter(sdk.NewInfiniteGasMeterWithMultiplier(ctx))

	// If validation failed, increment nonce via keeper (matching V2's DeliverTxCallback behavior
	// in x/evm/ante/basic.go). V2 does NOT create stateDB or handle surplus for early failures.
	if validationErr != nil {
		// Match V2 error handling: bump nonce directly via keeper (not stateDB)
		currentNonce := app.GigaEvmKeeper.GetNonce(ctx, sender)
		app.GigaEvmKeeper.SetNonce(ctx, sender, currentNonce+1)

		// V2 reports intrinsic gas as gasUsed even on validation failure (for metrics),
		// but no actual balance is deducted
		intrinsicGas, _ := core.IntrinsicGas(ethTx.Data(), ethTx.AccessList(), ethTx.SetCodeAuthorizations(), ethTx.To() == nil, true, true, true)
		validationErr.GasUsed = int64(intrinsicGas)  //nolint:gosec
		validationErr.GasWanted = int64(ethTx.Gas()) //nolint:gosec
		return validationErr, nil
	}

	if !isAssociated {
		// Set address mapping
		app.GigaEvmKeeper.SetAddressMapping(ctx, seiAddr, sender)
		// Set pubkey on account if not already set
		if acc := app.AccountKeeper.GetAccount(ctx, seiAddr); acc != nil && acc.GetPubKey() == nil {
			if err := acc.SetPubKey(pubkey); err != nil {
				return &abci.ExecTxResult{
					Code: 1,
					Log:  fmt.Sprintf("failed to set pubkey: %v", err),
				}, nil
			}
			app.AccountKeeper.SetAccount(ctx, acc)
		}
		// Migrate balance from cast address
		associateHelper := helpers.NewAssociationHelper(&app.GigaEvmKeeper, app.GigaBankKeeper, &app.AccountKeeper)
		if err := associateHelper.MigrateBalance(ctx, sender, seiAddr, false); err != nil {
			return &abci.ExecTxResult{
				Code: 1,
				Log:  fmt.Sprintf("failed to migrate balance: %v", err),
			}, nil
		}
	}

	// Create state DB for this transaction (only for valid transactions)
	stateDB := gigaevmstate.NewDBImpl(ctx, &app.GigaEvmKeeper, false)
	defer stateDB.Cleanup()

	// Pre-charge gas fee (like V2's ante handler), then execute with feeAlreadyCharged=true.
	// V2 charges fees in the ante handler, then runs the EVM with feeAlreadyCharged=true
	// which skips buyGas/refundGas/coinbase. Without this, GasUsed differs between Giga
	// and V2, causing LastResultsHash → AppHash divergence.
	effectiveGasPrice := new(big.Int).Add(new(big.Int).Set(ethTx.GasTipCap()), baseFee)
	if effectiveGasPrice.Cmp(ethTx.GasFeeCap()) > 0 {
		effectiveGasPrice.Set(ethTx.GasFeeCap())
	}
	gasFee := new(big.Int).Mul(new(big.Int).SetUint64(ethTx.Gas()), effectiveGasPrice)
	stateDB.SubBalance(sender, uint256.MustFromBig(gasFee), tracing.BalanceDecreaseGasBuy)

	// Get gas pool (mutated per tx, cannot be cached)
	gp := app.GigaEvmKeeper.GetGasPool()

	// Use cached block-level constants
	blockCtx := cache.blockCtx
	cfg := cache.chainConfig

	// Create Giga executor VM
	gigaExecutor := gigaexecutor.NewGethExecutor(blockCtx, stateDB, cfg, vm.Config{}, gigaprecompiles.AllCustomPrecompilesFailFast)

	// Execute with feeAlreadyCharged=true — matching V2's msg_server behavior
	execResult, execErr := gigaExecutor.ExecuteTransactionFeeCharged(ethTx, sender, cache.baseFee, &gp)
	if execErr != nil {
		// Match V2 error handling: bump nonce, commit fee deduction, track surplus
		stateDB.SetNonce(sender, stateDB.GetNonce(sender)+1, tracing.NonceChangeEoACall)
		surplus, ferr := stateDB.Finalize()
		if ferr != nil {
			ctx.Logger().Error("giga: failed to finalize stateDB on consensus error",
				"txHash", ethTx.Hash().Hex(),
				"error", ferr,
			)
		}
		bloom := ethtypes.Bloom{}
		app.EvmKeeper.AppendToEvmTxDeferredInfo(ctx, bloom, ethTx.Hash(), surplus)

		return &abci.ExecTxResult{
			Code:      1,
			GasWanted: int64(ethTx.Gas()), //nolint:gosec
			Log:       fmt.Sprintf("giga executor apply message error: %v", execErr),
		}, nil
	}

	// Check if the execution hit a fail-fast precompile (Cosmos interop detected)
	// Return the error to the caller so it can handle accordingly (e.g., fallback to standard execution)
	if execResult.Err != nil && gigautils.ShouldExecutionAbort(execResult.Err) {
		return nil, execResult.Err
	}

	// Finalize state changes — captures surplus (fee deduction + execution balance changes)
	surplus, ferr := stateDB.Finalize()
	if ferr != nil {
		return &abci.ExecTxResult{
			Code: 1,
			Log:  fmt.Sprintf("failed to finalize state: %v", ferr),
		}, nil
	}

	// Write receipt
	vmError := ""
	if execResult.Err != nil {
		vmError = execResult.Err.Error()
	}

	// Create core.Message from ethTx for WriteReceipt
	// WriteReceipt needs msg for GasPrice, To, From, Data, Nonce fields
	evmMsg := &core.Message{
		Nonce:     ethTx.Nonce(),
		GasLimit:  ethTx.Gas(),
		GasPrice:  ethTx.GasPrice(),
		GasFeeCap: ethTx.GasFeeCap(),
		GasTipCap: ethTx.GasTipCap(),
		To:        ethTx.To(),
		Value:     ethTx.Value(),
		Data:      ethTx.Data(),
		From:      sender,
	}
	receipt, rerr := app.GigaEvmKeeper.WriteReceipt(ctx, stateDB, evmMsg, uint32(ethTx.Type()), ethTx.Hash(), execResult.UsedGas, vmError)
	if rerr != nil {
		return &abci.ExecTxResult{
			Code: 1,
			Log:  fmt.Sprintf("failed to write receipt: %v", rerr),
		}, nil
	}

	// Append deferred info for EndBlock processing
	bloom := ethtypes.Bloom{}
	bloom.SetBytes(receipt.LogsBloom)
	app.EvmKeeper.AppendToEvmTxDeferredInfo(ctx, bloom, ethTx.Hash(), surplus)

	// Determine result code based on VM error
	code := uint32(0)
	if execResult.Err != nil {
		code = sdkerrors.ErrEVMVMError.ABCICode()
	}

	// GasWanted should be set to the transaction's gas limit to match standard executor behavior.
	// This is critical for LastResultsHash computation which uses Code, Data, GasWanted, and GasUsed.
	gasWanted := int64(ethTx.Gas())      //nolint:gosec // G115: safe, Gas() won't exceed int64 max
	gasUsed := int64(execResult.UsedGas) //nolint:gosec // G115: safe, UsedGas won't exceed int64 max

	// Build Data field to match standard executor format.
	// Standard path wraps MsgEVMTransactionResponse in TxMsgData.
	// This is critical for LastResultsHash to match.
	evmResponse := &evmtypes.MsgEVMTransactionResponse{
		GasUsed:    execResult.UsedGas,
		VmError:    vmError,
		ReturnData: execResult.ReturnData,
		Hash:       ethTx.Hash().Hex(),
		Logs:       evmtypes.NewLogsFromEth(stateDB.GetAllLogs()),
	}
	evmResponseBytes, marshalErr := evmResponse.Marshal()
	if marshalErr != nil {
		return &abci.ExecTxResult{
			Code: 1,
			Log:  fmt.Sprintf("failed to marshal evm response: %v", marshalErr),
		}, nil
	}

	// Wrap in TxMsgData like the standard path does
	txMsgData := &sdk.TxMsgData{
		Data: []*sdk.MsgData{
			{
				MsgType: sdk.MsgTypeURL(msg),
				Data:    evmResponseBytes,
			},
		},
	}
	txMsgDataBytes, txMarshalErr := proto.Marshal(txMsgData)
	if txMarshalErr != nil {
		return &abci.ExecTxResult{
			Code: 1,
			Log:  fmt.Sprintf("failed to marshal tx msg data: %v", txMarshalErr),
		}, nil
	}

	return &abci.ExecTxResult{
		Code:      code,
		Data:      txMsgDataBytes,
		GasWanted: gasWanted,
		GasUsed:   gasUsed,
		Log:       vmError,
		EvmTxInfo: &abci.EvmTxInfo{
			TxHash:  ethTx.Hash().Hex(),
			VmError: vmError,
			Nonce:   ethTx.Nonce(),
		},
	}, nil
}

// gigaDeliverTx is the OCC-compatible deliverTx function for the giga executor.
// makeGigaDeliverTx returns an OCC-compatible deliverTx callback that captures the given
// block cache, avoiding mutable state on App for cache lifecycle management.
func (app *App) makeGigaDeliverTx(cache *gigaBlockCache) func(sdk.Context, abci.RequestDeliverTxV2, sdk.Tx, [32]byte) abci.ResponseDeliverTx {
	return func(ctx sdk.Context, req abci.RequestDeliverTxV2, tx sdk.Tx, checksum [32]byte) abci.ResponseDeliverTx {
		defer func() {
			if r := recover(); r != nil {
				// OCC abort panics are expected - the scheduler uses them to detect conflicts
				// and reschedule transactions. Don't log these as errors.
				if _, isOCCAbort := r.(occ.Abort); !isOCCAbort {
					ctx.Logger().Error("benchmark panic in gigaDeliverTx", "panic", r, "stack", string(debug.Stack()))
				}
			}
		}()

		evmMsg := app.GetEVMMsg(tx)
		if evmMsg == nil {
			return abci.ResponseDeliverTx{Code: 1, Log: "not an EVM transaction"}
		}

		result, err := app.executeEVMTxWithGigaExecutor(ctx, evmMsg, cache)
		if err != nil {
			// Check if this is a fail-fast error (Cosmos precompile interop detected)
			if gigautils.ShouldExecutionAbort(err) {
				// Return a sentinel response so the caller can fall back to v2.
				return abci.ResponseDeliverTx{
					Code:      gigautils.GigaAbortCode,
					Codespace: gigautils.GigaAbortCodespace,
					Info:      gigautils.GigaAbortInfo,
					Log:       "giga executor abort: fall back to v2",
				}
			}

			return abci.ResponseDeliverTx{Code: 1, Log: fmt.Sprintf("giga executor error: %v", err)}
		}

		return abci.ResponseDeliverTx{
			Code:      result.Code,
			Data:      result.Data,
			Log:       result.Log,
			Info:      result.Info,
			GasWanted: result.GasWanted,
			GasUsed:   result.GasUsed,
			Events:    result.Events,
			Codespace: result.Codespace,
			EvmTxInfo: result.EvmTxInfo,
		}
	}
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
				if isEVM, _ := evmante.IsEVMMessage(typedTx); isEVM && !app.GigaExecutorEnabled {
					msg := evmtypes.MustGetEVMTransactionMessage(typedTx)
					if err := evmante.Preprocess(ctx, msg, app.EvmKeeper.ChainID(ctx), app.EvmKeeper.EthBlockTestConfig.Enabled); err != nil {
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
	authtx.RegisterTxService(app.GRPCQueryRouter(), clientCtx, app.Simulate, app.interfaceRegistry)
}

func (app *App) RPCContextProvider(i int64) sdk.Context {
	if i == evmrpc.LatestCtxHeight {
		return app.GetCheckCtx().WithIsEVM(true).WithIsTracing(true).WithIsCheckTx(false).WithClosestUpgradeName(LatestUpgrade)
	}
	ctx, err := app.CreateQueryContext(i, false)
	if err != nil {
		panic(err)
	}
	closestUpgrade, upgradeHeight := app.UpgradeKeeper.GetClosestUpgrade(app.GetCheckCtx(), i)
	if closestUpgrade == "" && upgradeHeight == 0 {
		closestUpgrade = LatestUpgrade
	}
	ctx = ctx.WithClosestUpgradeName(closestUpgrade)
	return ctx.WithIsEVM(true).WithIsTracing(true).WithIsCheckTx(false)
}

// RegisterTendermintService implements the Application.RegisterTendermintService method.
func (app *App) RegisterTendermintService(clientCtx client.Context) {
	tmservice.RegisterTendermintService(app.GRPCQueryRouter(), clientCtx, app.interfaceRegistry)
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
		evmHTTPServer, err := evmrpc.NewEVMHTTPServer(app.Logger(), app.evmRPCConfig, clientCtx.Client, &app.EvmKeeper, app.BeginBlockKeepers, app.BaseApp, app.TracerAnteHandler, app.RPCContextProvider, txConfigProvider, DefaultNodeHome, app.GetStateStore(), nil)
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
		evmWSServer, err := evmrpc.NewEVMWebSocketServer(app.Logger(), app.evmRPCConfig, clientCtx.Client, &app.EvmKeeper, app.BeginBlockKeepers, app.BaseApp, app.TracerAnteHandler, app.RPCContextProvider, txConfigProvider, DefaultNodeHome, app.GetStateStore())
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
func (app *App) checkTotalBlockGas(ctx sdk.Context, txs [][]byte) (result bool) {
	defer func() {
		if r := recover(); r != nil {
			ctx.Logger().Error("panic recovered in checkTotalBlockGas", "panic", r)
			result = false // Reject proposal if panic occurs
		}
	}()

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
			if strings.Contains(err.Error(), "panic in IsTxGasless") {
				// This is a unexpected panic, reject the entire proposal
				ctx.Logger().Error("malicious transaction detected in gasless check", "error", err)
				return false
			}
			// Other business logic errors (like duplicate votes) - continue processing but tx is not gasless
			ctx.Logger().Info("transaction failed gasless check but not malicious", "error", err)
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

		if int64(gasWanted) < 0 || int64(totalGas) > math.MaxInt64-int64(gasWanted) { // nolint:gosec
			return false
		}

		if gasWanted > 0 {
			nonzeroTxsCnt++
		}

		totalGasWanted += gasWanted

		// If the gas estimate is set and at least 21k (the minimum gas needed for an EVM tx)
		// and less than or equal to the tx gas limit, use the gas estimate. Otherwise, use gasWanted.
		useEstimate := false
		if decodedTx.GetGasEstimate() >= MinGasEVMTx {
			if decodedTx.GetGasEstimate() <= gasWanted {
				useEstimate = true
			}
		}
		if useEstimate {
			totalGas += decodedTx.GetGasEstimate()
		} else {
			totalGas += gasWanted
		}

		if totalGasWanted > uint64(ctx.ConsensusParams().Block.MaxGasWanted) { //nolint:gosec
			return false
		}

		if totalGas > uint64(ctx.ConsensusParams().Block.MaxGas) { //nolint:gosec
			return false
		}
	}

	return true
}

func isExpectedGaslessMetricsError(err error) bool {
	if err == nil {
		return false
	}

	if errors.Is(err, oracletypes.ErrAggregateVoteExist) {
		return true
	}

	// Some wrapped error chains can lose sentinel identity while preserving
	// the canonical oracle error text.
	return strings.Contains(err.Error(), oracletypes.ErrAggregateVoteExist.Error())
}

// couldBeGaslessTransaction performs a fast heuristic check to identify potentially
// gasless transactions, avoiding expensive keeper queries for performance.
//
// Returns true if the transaction COULD be gasless (needs expensive check).
// Returns false only if DEFINITELY not gasless.
// False negatives are unacceptable as they cause incorrect gas metrics.
func (app *App) couldBeGaslessTransaction(tx sdk.Tx) bool {
	if tx == nil {
		return false
	}

	msgs := tx.GetMsgs()
	if len(msgs) == 0 {
		// Empty transactions are definitely not gasless
		return false
	}

	// Check if ANY message could potentially be gasless
	for _, msg := range msgs {
		switch msg.(type) {
		case *evmtypes.MsgAssociate:
			// Associate txs can be gasless, so we need to check
			return true
		case *oracletypes.MsgAggregateExchangeRateVote:
			// Oracle vote txs can be gasless, so we need to check
			return true
		}
	}

	// If none of the messages are known gasless types, it's definitely not gasless
	return false
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
