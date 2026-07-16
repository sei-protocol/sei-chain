package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
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

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/tracing"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/ethclient"
	ethparams "github.com/ethereum/go-ethereum/params"
	ethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/holiman/uint256"
	"github.com/sei-protocol/sei-chain/admin"
	"github.com/sei-protocol/sei-chain/giga/deps/tasks"
	"github.com/sei-protocol/sei-chain/sei-cosmos/baseapp"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client/grpc/tmservice"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client/rpc"
	"github.com/sei-protocol/sei-chain/sei-cosmos/codec"
	"github.com/sei-protocol/sei-chain/sei-cosmos/codec/types"
	cryptotypes "github.com/sei-protocol/sei-chain/sei-cosmos/crypto/types"
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
	seidb "github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/seilog"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"

	"github.com/gogo/protobuf/proto"
	"github.com/gorilla/mux"
	"github.com/rakyll/statik/fs"
	appante "github.com/sei-protocol/sei-chain/app/ante"
	"github.com/sei-protocol/sei-chain/app/antedecorators"
	"github.com/sei-protocol/sei-chain/app/benchmark"
	"github.com/sei-protocol/sei-chain/app/legacyabci"
	"github.com/sei-protocol/sei-chain/app/migration"
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
	tmos "github.com/sei-protocol/sei-chain/sei-tendermint/libs/os"
	tmutils "github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/types"
	wasmkeeper "github.com/sei-protocol/sei-chain/sei-wasmd/x/wasm/keeper"

	"github.com/sei-protocol/sei-chain/sei-cosmos/storev2/rootmulti"
	"github.com/sei-protocol/sei-chain/utils"
	"github.com/sei-protocol/sei-chain/utils/helpers"
	utilmetrics "github.com/sei-protocol/sei-chain/utils/metrics"
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
	dbm "github.com/tendermint/tm-db"
	"go.opentelemetry.io/otel/attribute"
	otelmetric "go.opentelemetry.io/otel/metric"

	// this line is used by starport scaffolding # stargate/app/moduleImport

	"github.com/sei-protocol/sei-chain/sei-wasmd/x/wasm"
	wasmclient "github.com/sei-protocol/sei-chain/sei-wasmd/x/wasm/client"
	wasmtypes "github.com/sei-protocol/sei-chain/sei-wasmd/x/wasm/types"

	// unnamed import of statik for openapi/swagger UI support
	_ "github.com/sei-protocol/sei-chain/docs/swagger"
	receipt "github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"

	gigastore "github.com/sei-protocol/sei-chain/giga/deps/store"
	gigabankkeeper "github.com/sei-protocol/sei-chain/giga/deps/xbank/keeper"
	gigaevmkeeper "github.com/sei-protocol/sei-chain/giga/deps/xevm/keeper"
	gigaevmstate "github.com/sei-protocol/sei-chain/giga/deps/xevm/state"
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
	logger = seilog.NewLogger("app")

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

	// kvStoreKeyNames is the canonical, in-order list of module KV store
	// names mounted on the SeiDB / memiavl backend. It is the single source
	// of truth consumed by sdk.NewKVStoreKeys in app.New, and is cross-
	// checked against keys.MemIAVLStoreKeys (sei-db/common/keys) by tests
	// in this package. Adding or removing an entry here MUST be matched in
	// sei-db/common/keys/store_keys.go.
	kvStoreKeyNames = []string{
		authtypes.StoreKey, authzkeeper.StoreKey, banktypes.StoreKey, stakingtypes.StoreKey,
		minttypes.StoreKey, distrtypes.StoreKey, slashingtypes.StoreKey,
		govtypes.StoreKey, paramstypes.StoreKey, ibchost.StoreKey, upgradetypes.StoreKey, feegrant.StoreKey,
		evidencetypes.StoreKey, ibctransfertypes.StoreKey, capabilitytypes.StoreKey, oracletypes.StoreKey,
		evmtypes.StoreKey, wasm.StoreKey,
		epochmoduletypes.StoreKey,
		tokenfactorytypes.StoreKey,
		// this line is used by starport scaffolding # stargate/app/storeKey
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

	// NewHeadsNotifierCapacity bounds the in-process eth_newHeads
	// notifier buffer. Capacity 1 pairs with the notifier's
	// overwrite-on-full semantics: if a consumer lags, the latest head
	// always wins and stale heads are dropped. Anything larger only
	// buffers staleness — newHeads subscribers care about the current
	// head, not a backlog.
	NewHeadsNotifierCapacity = 1
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

	encodingConfig       appparams.EncodingConfig
	legacyEncodingConfig appparams.EncodingConfig
	evmRPCConfig         evmrpcconfig.Config
	// blockHeaderNotifier is non-nil only when Autobahn is enabled. It
	// owns the FinalizeBlock→Commit pairing for eth_subscribe("newHeads"):
	// FinalizeBlocker calls Stash with the (hash, header, response)
	// tuple, App.Commit calls PublishStashed after a successful
	// BaseApp.Commit, and FinalizeBlocker entry calls ClearStash to
	// defend against stale tuples from prior failed commits or
	// non-stashing return paths (EthReplay/EthBlockTest).
	blockHeaderNotifier   tmutils.Option[*evmrpc.BlockHeaderNotifier]
	adminConfig           admin.Config
	adminServer           *grpc.Server
	lightInvarianceConfig LightInvarianceConfig

	genesisImportConfig genesistypes.GenesisImportConfig

	stateStore   seidb.StateStore
	rootStore    *rootmulti.Store
	receiptStore receipt.ReceiptStore

	forkInitializer func(sdk.Context)

	httpServerStartSignal     chan struct{}
	wsServerStartSignal       chan struct{}
	httpServerStartSignalSent bool
	wsServerStartSignalSent   bool

	// evmHTTPServer/evmWSServer hold the EVM JSON-RPC HTTP and WebSocket listeners
	// constructed in RegisterLocalServices so an embedding orchestrator (the
	// in-process harness) can Stop() them at teardown. Nil when the respective
	// listener is disabled. Production seid does not read these; its process exit
	// reaps the listeners.
	evmHTTPServer evmrpc.EVMServer
	evmWSServer   evmrpc.EVMServer

	txPrioritizer sdk.TxPrioritizer

	benchmarkManager *benchmark.Manager

	// GigaExecutorEnabled controls whether to use the Giga executor.
	GigaExecutorEnabled bool
	// GigaOCCEnabled controls whether to use OCC with the Giga executor
	GigaOCCEnabled bool
}

type AppOption func(*App)

// New returns a reference to an initialized blockchain app
func New(
	db dbm.DB,
	traceStore io.Writer,
	_ bool,
	skipUpgradeHeights map[int64]bool,
	homePath string,
	_ uint,
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

	bAppOptions, stateStore := SetupSeiDB(homePath, appOpts, baseAppOptions)

	bApp := baseapp.NewBaseApp(AppName, db, encodingConfig.TxConfig.TxDecoder(), tmConfig, appOpts, bAppOptions...)
	bApp.SetCommitMultiStoreTracer(traceStore)
	bApp.SetVersion(version.Version)
	bApp.SetInterfaceRegistry(interfaceRegistry)

	// Bind OTEL metrics provider once at application construction
	if err := utilmetrics.SetupOtelMetricsProvider(bApp.ChainID); err != nil {
		logger.Error(err.Error())
	}

	keys := sdk.NewKVStoreKeys(kvStoreKeyNames...)
	tkeys := sdk.NewTransientStoreKeys(paramstypes.TStoreKey, evmtypes.TransientStoreKey)
	memKeys := sdk.NewMemoryStoreKeys(capabilitytypes.MemStoreKey, banktypes.DeferredCacheStoreKey, oracletypes.MemStoreKey)

	app := &App{
		BaseApp:               bApp,
		cdc:                   cdc,
		appCodec:              appCodec,
		interfaceRegistry:     interfaceRegistry,
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

	// The storev2 rootmulti store is the only supported commit multistore; its
	// composite SC backend drives the in-flight memiavl->flatkv migration that
	// BeginBlock paces via the migration gov param. Fail fast if the legacy
	// root multistore is somehow in use.
	rootStore, ok := app.CommitMultiStore().(*rootmulti.Store)
	if !ok {
		panic(fmt.Sprintf("unsupported commit multistore %T: expected *rootmulti.Store", app.CommitMultiStore()))
	}
	app.rootStore = rootStore

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

	receiptConfig, err := readReceiptStoreConfig(homePath, appOpts)
	if err != nil {
		panic(fmt.Sprintf("error reading receipt store config: %s", err))
	}
	if app.receiptStore == nil {
		receiptStore, err := receipt.NewReceiptStore(receiptConfig, keys[evmtypes.StoreKey])
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
	// TODO: remove the mode gate and always construct the notifier once
	// non-Autobahn is also producer-wired to feed it (i.e. a listener
	// invocation in sei-tendermint/internal/state/execution.go next to
	// PublishEventNewBlockHeader). That switch is blocked on parity work:
	// the legacy event-bus path encodes a real Tendermint Header (real
	// parentHash/receiptsRoot/transactionsRoot, pre-execution stateRoot)
	// while encodeCommittedBlock zeroes those fields and uses
	// post-execution stateRoot. We need to either verify both encoders
	// produce identical headers for the same block under legacy, or
	// reconcile the encoder so swapping the consumer is a no-op for
	// non-Autobahn subscribers. Until that's verified, keep this gate
	// so non-Autobahn newHeads semantics are unchanged by this PR.
	if tmConfig != nil && tmConfig.AutobahnConfigFile != "" {
		app.blockHeaderNotifier = tmutils.Some(evmrpc.NewBlockHeaderNotifier(NewHeadsNotifierCapacity))
	}
	if app.evmRPCConfig.TraceBakeEnabled {
		traceDB, dbErr := evmkeeper.NewTraceDB(homePath)
		if dbErr != nil {
			panic(fmt.Sprintf("failed to open trace db: %s", dbErr))
		}
		app.EvmKeeper.SetTraceDB(traceDB)

		if app.evmRPCConfig.TraceBakeUseSnapshot {
			if rs, ok := app.CommitMultiStore().(*rootmulti.Store); ok {
				app.EvmKeeper.SetTraceSnapshotStore(evmkeeper.NewTraceSnapshotStore(app.evmRPCConfig.TraceBakeSnapshotWindow))
				app.EvmKeeper.SetTraceSnapshotCapture(rs.SnapshotSCStore)
			} else {
				logger.Info("trace_bake_use_snapshot set but commit multistore is not storev2 rootmulti; falling back to SS-pebble")
			}
		}
	}
	app.adminConfig, err = admin.ReadConfig(appOpts)
	if err != nil {
		panic(fmt.Sprintf("error reading admin config due to %s", err))
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
		// evmone is loaded best-effort
		if evmoneVM, err := gigalib.InitEvmoneVM(); err == nil {
			app.GigaEvmKeeper.EvmoneVM = evmoneVM
		} else {
			logger.Debug("failed to load evmone VM", "error", err)
		}
		// evm_giga_mixed_tests.sh matches these ENABLED/DISABLED strings to guard node roles; keep them in sync.
		if gigaExecutorConfig.OCCEnabled {
			logger.Info("benchmark: Giga Executor with OCC is ENABLED - using new EVM execution path with parallel execution")
		} else {
			logger.Info("benchmark: Giga Executor is ENABLED - using new EVM execution path (sequential)")
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
		app.InitBenchmark(context.Background(), app.ChainID, evmChainID)
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

	app.txPrioritizer = NewSeiTxPrioritizer(&app.EvmKeeper, &app.UpgradeKeeper, &app.ParamsKeeper).GetTxPriorityHint
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

	// Close trace db so its WAL is flushed; baker writes use NoSync.
	if tc := app.EvmKeeper.TraceDB(); tc != nil {
		if err := tc.Close(); err != nil {
			logger.Error("failed to close trace db", "err", err)
			errs = append(errs, fmt.Errorf("failed to close trace db: %w", err))
		}
	}
	if ts := app.EvmKeeper.TraceSnapshotStore(); ts != nil {
		ts.Close()
	}

	// Close receipt store
	if app.receiptStore != nil {
		if err := app.receiptStore.Close(); err != nil {
			logger.Error("failed to close receipt store", "err", err)
			errs = append(errs, fmt.Errorf("failed to close receipt store: %w", err))
		}
	}

	// Stop admin gRPC server
	if app.adminServer != nil {
		app.adminServer.GracefulStop()
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

	typedTxs, err := app.DecodeTxBytesConcurrently(ctx.Context(), req.Txs)
	if err != nil {
		utilmetrics.IncrFailedTotalGasWantedCheck(string(req.Header.ProposerAddress)) // TODO(PLT-327): remove once app_failed_total_gas_wanted_check_total verified
		appMetrics.failedGasWantedCheck.Add(ctx.Context(), 1,
			otelmetric.WithAttributes(attribute.String("proposer", hex.EncodeToString(req.Header.ProposerAddress))))
		return &abci.ResponseProcessProposal{
			Status: abci.ResponseProcessProposal_REJECT,
		}, nil
	}

	// Use the clean context for gas validation only. We cannot reassign
	// ctx because ProcessBlock writes to ctx's store downstream.
	checkCtx := app.GetProcessProposalCleanContext()

	// Invariant: at this point nil entries in typedTxs are proto decode failures only.
	// EVM preprocessing runs later inside ProcessBlock; do not reorder that before this check.
	if !app.checkTotalBlockGas(checkCtx, typedTxs) {
		utilmetrics.IncrFailedTotalGasWantedCheck(string(req.Header.ProposerAddress)) // TODO(PLT-327): remove once app_failed_total_gas_wanted_check_total verified
		appMetrics.failedGasWantedCheck.Add(ctx.Context(), 1,
			otelmetric.WithAttributes(attribute.String("proposer", hex.EncodeToString(req.Header.ProposerAddress))))
		return &abci.ResponseProcessProposal{
			Status: abci.ResponseProcessProposal_REJECT,
		}, nil
	}

	app.optimisticProcessingInfoMutex.Lock()
	shouldStartOptimisticProcessing := app.optimisticProcessingInfo.Completion == nil
	if shouldStartOptimisticProcessing {
		completionSignal := make(chan struct{}, 1)
		app.optimisticProcessingInfo = OptimisticProcessingInfo{
			Height:     req.Header.Height,
			Hash:       req.Hash,
			Completion: completionSignal,
		}
	}
	app.optimisticProcessingInfoMutex.Unlock()

	if shouldStartOptimisticProcessing {
		plan, found := app.UpgradeKeeper.GetUpgradePlan(ctx)
		if found && plan.ShouldExecute(ctx) {
			logger.Info("Potential upgrade planned; skipping optimistic processing", "height", plan.Height)
			app.optimisticProcessingInfoMutex.Lock()
			app.optimisticProcessingInfo.Aborted = true
			completion := app.optimisticProcessingInfo.Completion
			app.optimisticProcessingInfoMutex.Unlock()
			completion <- struct{}{}
		} else {
			go func() {
				// ProcessBlock has panic recovery and returns error for any processing failures
				// All panics (including GetSigners) are handled in ProcessBlock, not affecting proposal acceptance
				bpreq := &BlockProcessRequest{
					Hash:                req.Hash,
					ByzantineValidators: req.ByzantineValidators,
					Height:              req.Header.Height,
					Time:                req.Header.Time,
				}
				events, txResults, endBlockResp, processErr := app.ProcessBlock(ctx, req.Txs, bpreq, req.ProposedLastCommit, false, typedTxs)

				app.optimisticProcessingInfoMutex.Lock()
				if processErr != nil {
					// ProcessBlock failed (including GetSigners panics), mark as aborted
					logger.Info("ProcessBlock failed in optimistic processing", "err", processErr)
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
	// Drop any leftover stash so only the current FinalizeBlock can be
	// published by the next Commit. Defends against return paths that
	// don't Stash (EthReplay/EthBlockTest) and prior Commit failures.
	headNotifier, hasHeadNotifier := app.blockHeaderNotifier.Get()
	if hasHeadNotifier {
		headNotifier.ClearStash()
	}
	startTime := time.Now()
	defer func() {
		app.ClearOptimisticProcessingInfo()
		duration := time.Since(startTime)
		logger.Info("FinalizeBlock complete", "took-ms", duration/time.Millisecond)
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
			utilmetrics.IncrementOptimisticProcessingCounter(true) // TODO(PLT-327): remove once app_optimistic_processing_total verified
			appMetrics.optimisticProcessing.Add(ctx.Context(), 1,
				otelmetric.WithAttributes(attribute.Bool("enabled", true)))
			app.SetProcessProposalStateToCommit()
			if app.EvmKeeper.EthReplayConfig.Enabled || app.EvmKeeper.EthBlockTestConfig.Enabled {
				return &abci.ResponseFinalizeBlock{}, nil
			}
			consensusParamUpdates := app.GetConsensusParamsForStateToCommit()
			cms := app.WriteState()
			app.LightInvarianceChecks(ctx.Context(), cms, app.lightInvarianceConfig)
			appHash := app.GetWorkingHash()
			resp := app.getFinalizeBlockResponse(appHash, events, txRes, endBlockResp, consensusParamUpdates)
			if hasHeadNotifier {
				headNotifier.Stash(req, &resp)
			}
			return &resp, nil
		}
	}
	utilmetrics.IncrementOptimisticProcessingCounter(false) // TODO(PLT-327): remove once app_optimistic_processing_total verified
	appMetrics.optimisticProcessing.Add(ctx.Context(), 1,
		otelmetric.WithAttributes(attribute.Bool("enabled", false)))
	logger.Info("optimistic processing ineligible")
	bpreq := &BlockProcessRequest{
		Hash:                req.Hash,
		ByzantineValidators: req.ByzantineValidators,
		Height:              req.Header.Height,
		Time:                req.Header.Time,
	}
	events, txResults, endBlockResp, processErr := app.ProcessBlock(ctx, req.Txs, bpreq, req.DecidedLastCommit, false, nil)
	if processErr != nil {
		logger.Error("ProcessBlock failed in FinalizeBlocker", "err", processErr)
		return nil, processErr
	}

	app.SetDeliverStateToCommit()
	if app.EvmKeeper.EthReplayConfig.Enabled || app.EvmKeeper.EthBlockTestConfig.Enabled {
		return &abci.ResponseFinalizeBlock{}, nil
	}
	consensusParamUpdates := app.GetConsensusParamsForStateToCommit()
	cms := app.WriteState()
	app.LightInvarianceChecks(ctx.Context(), cms, app.lightInvarianceConfig)
	appHash := app.GetWorkingHash()
	resp := app.getFinalizeBlockResponse(appHash, events, txResults, endBlockResp, consensusParamUpdates)
	if hasHeadNotifier {
		headNotifier.Stash(req, &resp)
	}
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
				logger.Debug("error checking if tx is gasless for metrics", "err", err)
				// If we can't determine if it's gasless, record metrics to maintain existing behavior
			}
		} else if isGasless {
			skipMetrics = true // Skip metrics for confirmed gasless transactions
		}
	}

	if !skipMetrics {
		// Record metrics for non-gasless transactions
		utilmetrics.IncrGasCounter("gas_used", deliverTxResp.GasUsed)     // TODO(PLT-327): remove once app_tx_gas_total verified
		utilmetrics.IncrGasCounter("gas_wanted", deliverTxResp.GasWanted) // TODO(PLT-327): remove once app_tx_gas_total verified
		appMetrics.txGas.Add(ctx.Context(), deliverTxResp.GasUsed,
			otelmetric.WithAttributes(attribute.String("type", "gas_used")))
		appMetrics.txGas.Add(ctx.Context(), deliverTxResp.GasWanted,
			otelmetric.WithAttributes(attribute.String("type", "gas_wanted")))
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

func (app *App) ProcessTxsSynchronousV2(ctx sdk.Context, txs [][]byte, typedTxs []sdk.Tx) []*abci.ExecTxResult {
	blockProcessStart := time.Now()
	defer func() {
		utilmetrics.BlockProcessLatency(blockProcessStart, utilmetrics.Synchronous) // TODO(PLT-327): remove once app_block_process_duration_seconds verified
		appMetrics.blockProcessDuration.Record(ctx.Context(), time.Since(blockProcessStart).Seconds(),
			otelmetric.WithAttributes(attribute.String("type", utilmetrics.Synchronous)))
	}()

	txResults := make([]*abci.ExecTxResult, 0, len(txs))
	for i, tx := range txs {
		ctx = ctx.WithTxIndex(i)
		res := app.DeliverTxWithResult(ctx, tx, typedTxs[i])
		txResults = append(txResults, res)
		utilmetrics.IncrTxProcessTypeCounter(utilmetrics.Synchronous) // TODO(PLT-327): remove once app_tx_process_type_total verified
		appMetrics.txProcessType.Add(ctx.Context(), 1,
			otelmetric.WithAttributes(attribute.String("type", utilmetrics.Synchronous)))
	}
	return txResults
}

func (app *App) ProcessTxsSynchronousGiga(ctx sdk.Context, txs [][]byte, typedTxs []sdk.Tx) []*abci.ExecTxResult {
	blockProcessGigaStart := time.Now()
	defer func() {
		utilmetrics.BlockProcessLatency(blockProcessGigaStart, utilmetrics.Synchronous) // TODO(PLT-327): remove once app_block_process_duration_seconds verified
		appMetrics.blockProcessDuration.Record(ctx.Context(), time.Since(blockProcessGigaStart).Seconds(),
			otelmetric.WithAttributes(attribute.String("type", utilmetrics.SynchronousGiga)))
	}()

	ms := ctx.MultiStore().CacheMultiStore()
	defer ms.Write()
	ctx = ctx.WithMultiStore(ms)

	// Cache block-level constants (identical for all txs in this block).
	cache, cacheErr := newGigaBlockCache(ctx, &app.GigaEvmKeeper)
	if cacheErr != nil {
		logger.Error("failed to build giga block cache", "error", cacheErr, "height", ctx.BlockHeight())
		return nil
	}

	txResults := make([]*abci.ExecTxResult, len(txs))
	for i, tx := range txs {
		ctx = ctx.WithTxIndex(i)
		evmMsg := app.GetEVMMsg(typedTxs[i])
		// If not an EVM tx, fall back to v2 processing
		if evmMsg == nil {
			result := app.DeliverTxWithResult(ctx, tx, typedTxs[i])
			txResults[i] = result
			ms.Write()
			continue
		}

		// Execute EVM transaction through giga executor with panic recovery.
		// Matches V2's recover behavior in legacyabci/deliver_tx.go.
		var result *abci.ExecTxResult
		var execErr error
		// fallbackToV2: store-iteration panic; re-run this tx via v2 to match v2.
		var fallbackToV2 bool
		// IIFE (immediately-invoked function) to scope defer/recover to this tx only,
		// allowing the loop to continue processing subsequent transactions after a panic.
		func() {
			defer func() {
				if r := recover(); r != nil {
					// Handle panics by type (matches V2's recovery middleware in baseapp/recovery.go)
					if oogErr, isOOG := r.(sdk.ErrorOutOfGas); isOOG {
						result = &abci.ExecTxResult{
							Codespace: sdkerrors.RootCodespace,
							Code:      sdkerrors.ErrOutOfGas.ABCICode(),
							Log:       fmt.Sprintf("out of gas in location: %v", oogErr.Descriptor),
						}
						return
					}
					// Store-iteration panic: giga can't handle this tx; fall back to v2 (mirrors makeGigaDeliverTx).
					if err, ok := r.(error); ok && errors.Is(err, gigastore.ErrIteratorUnsupported) {
						fallbackToV2 = true
						return
					}
					// For other panics (e.g., nil deref from malformed protobuf), log and return ErrPanic
					logger.Error("panic in giga synchronous executor", "panic", r, "stack", string(debug.Stack()))
					result = &abci.ExecTxResult{
						Codespace: sdkerrors.UndefinedCodespace,
						Code:      sdkerrors.ErrPanic.ABCICode(),
						Log:       fmt.Sprintf("panic recovered: %v", r),
					}
				}
			}()

			// Validate Cosmos SDK envelope (memo, timeoutHeight, signerInfos, etc.)
			// This prevents consensus divergence if a malicious proposer includes invalid envelope fields.
			if err := appante.EvmStatelessChecks(ctx, typedTxs[i], cache.chainID); err != nil {
				codespace, code, log := sdkerrors.ABCIInfo(err, false)
				result = &abci.ExecTxResult{
					Codespace: codespace,
					Code:      code,
					Log:       log,
				}
				return
			}

			result, execErr = app.executeEVMTxWithGigaExecutor(ctx, evmMsg, cache)
		}()

		// Store-iteration panic: re-run via v2 so the result matches v2 exactly.
		if fallbackToV2 {
			utilmetrics.IncrGigaFallbackToV2Counter() // TODO(PLT-327): remove once app_giga_fallback_to_v2_total verified
			appMetrics.gigaFallback.Add(ctx.Context(), 1,
				otelmetric.WithAttributes(
					attribute.String("reason", gigaFallbackStoreIterator),
					attribute.String("scope", "tx")))
			res := app.DeliverTxWithResult(ctx, tx, typedTxs[i])
			txResults[i] = res
			ms.Write()
			continue
		}

		if execErr != nil {
			// Abort errors (validation failure, balance migration, self-destruct,
			// cosmos-precompile interop) re-run this tx via v2.
			if gigautils.ShouldExecutionAbort(execErr) {
				utilmetrics.IncrGigaFallbackToV2Counter() // TODO(PLT-327): remove once app_giga_fallback_to_v2_total verified
				appMetrics.gigaFallback.Add(ctx.Context(), 1,
					otelmetric.WithAttributes(
						attribute.String("reason", gigaFallbackReason(execErr)),
						attribute.String("scope", "tx")))
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
		utilmetrics.IncrTxProcessTypeCounter(utilmetrics.Synchronous) // TODO(PLT-327): remove once app_tx_process_type_total verified
		appMetrics.txProcessType.Add(ctx.Context(), 1,
			otelmetric.WithAttributes(attribute.String("type", utilmetrics.SynchronousGiga)))
	}

	return txResults
}

func (app *App) shouldProcessSingleRecipientEVMTransfersSynchronously(typedTxs []sdk.Tx) bool {
	const minSingleRecipientEVMTransfers = 64

	if len(typedTxs) < minSingleRecipientEVMTransfers {
		return false
	}

	var recipient common.Address
	for i, tx := range typedTxs {
		msg := app.GetEVMMsg(tx)
		if msg == nil {
			return false
		}
		etx, _ := msg.AsTransaction()
		if etx == nil || etx.To() == nil || len(etx.Data()) != 0 || etx.Value().Sign() <= 0 {
			return false
		}
		if i == 0 {
			recipient = *etx.To()
			continue
		}
		if *etx.To() != recipient {
			return false
		}
	}

	return true
}

// cacheContext returns a new context based off of the provided context with
// a branched multi-store.
func (app *App) CacheContext(ctx sdk.Context) (sdk.Context, sdk.CacheMultiStore) {
	ms := ctx.MultiStore()
	msCache := ms.CacheMultiStore()
	return ctx.WithMultiStore(msCache), msCache
}

// ExecuteTxsConcurrently calls the appropriate function for processing transacitons
func (app *App) ExecuteTxsConcurrently(ctx sdk.Context, txs [][]byte, typedTxs []sdk.Tx) ([]*abci.ExecTxResult, sdk.Context) {
	processSynchronously := app.shouldProcessSingleRecipientEVMTransfersSynchronously(typedTxs)
	if app.GigaExecutorEnabled && app.GigaOCCEnabled && !processSynchronously {
		return app.ProcessTXsWithOCCGiga(ctx, txs, typedTxs)
	} else if app.GigaExecutorEnabled {
		return app.ProcessTxsSynchronousGiga(ctx, txs, typedTxs), ctx
	} else if !ctx.IsOCCEnabled() {
		return app.ProcessTxsSynchronousV2(ctx, txs, typedTxs), ctx
	}
	if processSynchronously {
		return app.ProcessTxsSynchronousV2(ctx, txs, typedTxs), ctx
	}

	return app.ProcessTXsWithOCCV2(ctx, txs, typedTxs)
}

func (app *App) GetDeliverTxEntry(ctx sdk.Context, txIndex int, bz []byte, tx sdk.Tx) (res *sdk.DeliverTxEntry) {
	res = &sdk.DeliverTxEntry{
		Request:       abci.RequestDeliverTxV2{Tx: bz},
		SdkTx:         tx,
		Checksum:      sha256.Sum256(bz),
		AbsoluteIndex: txIndex,
	}
	return
}

// ProcessTXsWithOCCV2 runs the transactions concurrently via OCC, using the V2 executor
func (app *App) ProcessTXsWithOCCV2(ctx sdk.Context, txs [][]byte, typedTxs []sdk.Tx) ([]*abci.ExecTxResult, sdk.Context) {
	blockProcessStart := time.Now()
	defer func() {
		appMetrics.blockProcessDuration.Record(ctx.Context(), time.Since(blockProcessStart).Seconds(),
			otelmetric.WithAttributes(attribute.String("type", utilmetrics.OccConcurrent)))
	}()

	entries := make([]*sdk.DeliverTxEntry, len(txs))
	for txIndex, tx := range txs {
		entries[txIndex] = app.GetDeliverTxEntry(ctx, txIndex, tx, typedTxs[txIndex])
	}

	batchResult := app.DeliverTxBatch(ctx, sdk.DeliverTxBatchRequest{TxEntries: entries})

	execResults := make([]*abci.ExecTxResult, 0, len(batchResult.Results))
	for i, r := range batchResult.Results {
		utilmetrics.IncrTxProcessTypeCounter(utilmetrics.OccConcurrent) // TODO(PLT-327): remove once app_tx_process_type_total verified
		appMetrics.txProcessType.Add(ctx.Context(), 1,
			otelmetric.WithAttributes(attribute.String("type", utilmetrics.OccConcurrent)))

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
						logger.Debug("error checking if tx is gasless for OCC metrics", "error", err, "txIndex", i)
						// If we can't determine if it's gasless, record metrics to maintain existing behavior
					}
				} else if isGasless {
					recordGasMetrics = false
				}
			}
		}

		if recordGasMetrics {
			utilmetrics.IncrGasCounter("gas_used", r.Response.GasUsed)     // TODO(PLT-327): remove once app_tx_gas_total verified
			utilmetrics.IncrGasCounter("gas_wanted", r.Response.GasWanted) // TODO(PLT-327): remove once app_tx_gas_total verified
			appMetrics.txGas.Add(ctx.Context(), r.Response.GasUsed,
				otelmetric.WithAttributes(attribute.String("type", "gas_used")))
			appMetrics.txGas.Add(ctx.Context(), r.Response.GasWanted,
				otelmetric.WithAttributes(attribute.String("type", "gas_wanted")))
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
func (app *App) ProcessTXsWithOCCGiga(ctx sdk.Context, txs [][]byte, typedTxs []sdk.Tx) ([]*abci.ExecTxResult, sdk.Context) {
	blockProcessStart := time.Now()
	delegatedToV2 := false
	defer func() {
		if !delegatedToV2 {
			appMetrics.blockProcessDuration.Record(ctx.Context(), time.Since(blockProcessStart).Seconds(),
				otelmetric.WithAttributes(attribute.String("type", utilmetrics.OccGiga)))
		}
	}()

	evmEntries := make([]*sdk.DeliverTxEntry, 0, len(txs))
	v2Entries := make([]*sdk.DeliverTxEntry, 0, len(txs))
	firstCosmosSeen := false
	for txIndex, tx := range txs {
		if app.GetEVMMsg(typedTxs[txIndex]) != nil {
			if firstCosmosSeen {
				logger.Error("Giga OCC cannot execute block due to tx ordering, falling back to V2")
				// Oops! This isn't "all EVM txs, then all Cosmos txs" - we need to fallback to V2.
				delegatedToV2 = true
				return app.ProcessTXsWithOCCV2(ctx, txs, typedTxs)
			}

			evmEntries = append(evmEntries, app.GetDeliverTxEntry(ctx, txIndex, tx, typedTxs[txIndex]))
		} else {
			if !firstCosmosSeen {
				firstCosmosSeen = true
			}
			v2Entries = append(v2Entries, app.GetDeliverTxEntry(ctx, txIndex, tx, typedTxs[txIndex]))
		}
	}

	var evmBatchResult []abci.ResponseDeliverTx
	fallbackToV2 := false

	if len(evmEntries) > 0 {
		// Run EVM txs against a cache so we can discard all changes on fallback.
		evmCtx, evmCache := app.CacheContext(ctx)

		// Cache block-level constants (identical for all txs in this block).
		// Must use evmCtx (not ctx) because giga KV stores are registered in CacheContext.
		cache, cacheErr := newGigaBlockCache(evmCtx, &app.GigaEvmKeeper)
		if cacheErr != nil {
			logger.Error("failed to build giga block cache", "error", cacheErr, "height", ctx.BlockHeight())
			return nil, ctx
		}

		// Create OCC scheduler with giga executor deliverTx capturing the cache.
		evmScheduler := tasks.NewScheduler(
			app.ConcurrencyWorkers(),
			app.TracingInfo,
			app.makeGigaDeliverTx(cache),
		)

		var evmSchedErr error
		evmBatchResult, evmSchedErr = evmScheduler.ProcessAll(evmCtx, evmEntries)
		if evmSchedErr != nil {
			logger.Error("benchmark OCC scheduler error (EVM txs)", "error", evmSchedErr, "height", ctx.BlockHeight(), "txCount", len(evmEntries))
			return nil, ctx
		}

		fallbackReason := gigaFallbackOther
		for _, r := range evmBatchResult {
			if r.Code == gigautils.GigaAbortCode && r.Codespace == gigautils.GigaAbortCodespace {
				fallbackToV2 = true
				if r.Info != "" {
					fallbackReason = r.Info
				}
				break
			}
		}

		if fallbackToV2 {
			utilmetrics.IncrGigaFallbackToV2Counter() // TODO(PLT-327): remove once app_giga_fallback_to_v2_total verified
			appMetrics.gigaFallback.Add(ctx.Context(), 1,
				otelmetric.WithAttributes(
					attribute.String("reason", fallbackReason),
					attribute.String("scope", "batch")))
			// Discard all EVM changes by skipping cache writes, then re-run all txs via DeliverTx.
			evmBatchResult = nil
			v2Entries = make([]*sdk.DeliverTxEntry, len(txs))
			for txIndex, tx := range txs {
				v2Entries[txIndex] = app.GetDeliverTxEntry(ctx, txIndex, tx, typedTxs[txIndex])
			}
		} else {
			// Commit EVM cache to main store before processing non-EVM txs.
			evmCache.Write()
			evmCtx.GigaMultiStore().WriteGiga()
		}
	}

	var v2BatchResult []abci.ResponseDeliverTx

	if len(v2Entries) > 0 {
		v2Scheduler := tasks.NewScheduler(
			app.ConcurrencyWorkers(),
			app.TracingInfo,
			app.DeliverTx,
		)
		var v2SchedErr error
		v2BatchResult, v2SchedErr = v2Scheduler.ProcessAll(ctx, v2Entries)
		if v2SchedErr != nil {
			logger.Error("benchmark OCC scheduler error", "error", v2SchedErr, "height", ctx.BlockHeight(), "txCount", len(v2Entries))
			return nil, ctx
		}
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

// ProcessBlock executes block transactions. If preDecoded is non-nil and len(preDecoded)==len(txs),
// those decoded transactions are reused (bytes are not decoded again); EVM preprocessing still runs
// on the block context.
func (app *App) ProcessBlock(ctx sdk.Context, txs [][]byte, req *BlockProcessRequest, lastCommit abci.CommitInfo, simulate bool, preDecoded []sdk.Tx) (events []abci.Event, txResults []*abci.ExecTxResult, endBlockResp abci.ResponseEndBlock, err error) {
	defer func() {
		if r := recover(); r != nil {
			panicMsg := fmt.Sprintf("%v", r)

			// Re-panic for upgrade-related panics to allow proper upgrade mechanism
			if upgradePanicRe.MatchString(panicMsg) {
				logger.Error("upgrade panic detected, panicking to trigger upgrade", "panic", r)
				panic(r) // Re-panic to trigger upgrade mechanism
			}
			stack := string(debug.Stack())
			logger.Error("panic recovered in ProcessBlock", "panic", r, "stack", stack)
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
	blockSpan.SetAttributes(attribute.Int64("height", req.Height))
	ctx = ctx.WithTraceSpanContext(blockSpanCtx)

	beginBlockResp := app.BeginBlock(ctx, req.Height, lastCommit.Votes, req.ByzantineValidators, true)
	events = append(events, beginBlockResp.Events...)

	evmTxs := make([]*evmtypes.MsgEVMTransaction, len(txs)) // nil for non-EVM txs
	var typedTxs []sdk.Tx
	if len(preDecoded) == len(txs) {
		typedTxs = preDecoded
		app.FinalizeDecodedTransactionsConcurrently(ctx, typedTxs)
	} else {
		typedTxs = app.DecodeTransactionsConcurrently(ctx, txs)
	}

	for i := range txs {
		evmTxs[i] = app.GetEVMMsg(typedTxs[i])
	}

	// Execute all transactions
	txResults, ctx = app.ExecuteTxsConcurrently(ctx, txs, typedTxs)

	midBlockEvents := app.MidBlock(ctx, req.Height)
	events = append(events, midBlockEvents...)

	// Flush giga stores so WriteDeferredBalances (which uses the standard BankKeeper)
	// can see balance changes made by the giga executor via GigaBankKeeper.
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

	endBlockResp = app.EndBlock(ctx, req.Height, evmTotalGasUsed)

	events = append(events, endBlockResp.Events...)
	return events, txResults, endBlockResp, nil
}

// Fallback-cause labels for the app_giga_fallback_to_v2 metric.
const (
	gigaFallbackValidationFailed  = "validation_failed"
	gigaFallbackBalanceMigration  = "balance_migration"
	gigaFallbackSelfDestruct      = "self_destruct"
	gigaFallbackInvalidPrecompile = "invalid_precompile"
	gigaFallbackStoreIterator     = "store_iterator"
	gigaFallbackOther             = "other"
)

// gigaFallbackReason labels the app_giga_fallback_to_v2 metric with which
// abort sentinel routed a tx to v2, so operators can tell a validation-failure
// wave from balance-migration churn.
func gigaFallbackReason(err error) string {
	switch err.(type) {
	case *gigautils.ValidationFailedAbortError:
		return gigaFallbackValidationFailed
	case *gigaprecompiles.BalanceMigrationAbortError:
		return gigaFallbackBalanceMigration
	case *gigaprecompiles.SelfDestructAbortError:
		return gigaFallbackSelfDestruct
	case *gigaprecompiles.InvalidPrecompileCallError:
		return gigaFallbackInvalidPrecompile
	default:
		return gigaFallbackOther
	}
}

// executeEVMTxWithGigaExecutor executes a single EVM transaction using the giga executor.
// The sender address is recovered directly from the transaction signature - no Cosmos SDK ante handlers needed.
func (app *App) executeEVMTxWithGigaExecutor(ctx sdk.Context, msg *evmtypes.MsgEVMTransaction, cache *gigaBlockCache) (*abci.ExecTxResult, error) {
	// Get the Ethereum transaction from the message
	ethTx, _ := msg.AsTransaction()
	if ethTx == nil {
		return nil, fmt.Errorf("failed to convert to eth transaction")
	}

	chainID := cache.chainID

	// Recover sender using the same logic as preprocess.go (version-based signer selection)
	sender, seiAddr, _, recoverErr := evmante.RecoverSenderFromEthTx(ctx, ethTx, chainID)
	if recoverErr != nil {
		return &abci.ExecTxResult{
			Code: 1,
			Log:  fmt.Sprintf("failed to recover sender from signature: %v", recoverErr),
		}, nil
	}

	_, isAssociated := app.GigaEvmKeeper.GetEVMAddress(ctx, seiAddr)

	// Run validation checks (fee/nonce/balance - stateless checks done earlier)
	validation := app.validateGigaEVMTx(ctx, ethTx, sender, seiAddr, isAssociated)

	// Prepare context for EVM transaction (set infinite gas meter like original flow)
	ctx = ctx.WithGasMeter(sdk.NewInfiniteGasMeterWithMultiplier(ctx))

	if validation.err != nil {
		// v2 rejects fee/nonce/balance failures in its ante chain, whose
		// receipt gas fields giga cannot reconstruct; a giga-side receipt here
		// diverges LastResultsHash on mixed fleets (CON-368). Fall back to v2,
		// which also owns the nonce bump.
		return nil, gigautils.ErrValidationFailed
	}

	if !isAssociated {
		// Unassociated addresses require balance migration (iterating all balances),
		// which giga's cachekv doesn't support. Fall back to v2 for this tx.
		return nil, gigaprecompiles.ErrBalanceMigrationRequired
	}

	// EIP-7702 authorization authorities must be associated with their true (pubkey-derived)
	// Sei address before SetCode installs delegation code, otherwise SetCode creates a mutable
	// direct-cast mapping that a later associatePubKey can remap (orphaning staking/distribution
	// state). That pre-association is done in the V2 ante handler and requires balance migration,
	// which giga's cachekv cannot perform, so defer such transactions to V2.
	if app.setCodeTxRequiresAuthorityAssociation(ctx, ethTx) {
		return nil, gigaprecompiles.ErrBalanceMigrationRequired
	}

	// Create state DB for this transaction (only for valid transactions)
	stateDB := gigaevmstate.NewDBImpl(ctx, &app.GigaEvmKeeper, false)
	defer stateDB.Cleanup()

	// Pre-charge gas fee (like V2's ante handler), then execute with feeAlreadyCharged=true.
	// V2 charges fees in the ante handler, then runs the EVM with feeAlreadyCharged=true
	// which skips buyGas/refundGas/coinbase. Without this, GasUsed differs between Giga
	// and V2, causing LastResultsHash → AppHash divergence.
	effectiveGasPrice := new(big.Int).Add(new(big.Int).Set(ethTx.GasTipCap()), validation.baseFee)
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

	// Self-destruct requires iterating the store, unsupported by giga. Fallback to v2.
	if stateDB.AnySelfDestructed() {
		return nil, gigaprecompiles.ErrSelfDestructUnsupported
	}

	if execErr != nil {
		// Match V2 error handling: bump nonce, commit fee deduction, track surplus
		stateDB.SetNonce(sender, stateDB.GetNonce(sender)+1, tracing.NonceChangeEoACall)
		surplus, ferr := stateDB.Finalize()
		if ferr != nil {
			// stateDB.Finalize is not expected to fail in practice. If it
			// does, the nonce bump above may not have been persisted, so per
			// the receipt-iff-nonce-bumped invariant we cannot claim the tx
			// happened: skip the receipt + deferred-info writes and return.
			logger.Error("giga: failed to finalize stateDB on consensus error",
				"tx-hash", ethTx.Hash(),
				"error", ferr,
			)
			return &abci.ExecTxResult{
				Code:      1,
				GasWanted: int64(ethTx.Gas()), //nolint:gosec
				Log:       fmt.Sprintf("giga: failed to finalize stateDB on consensus error: %v", ferr),
			}, nil
		}

		// Receipt-iff-nonce-bumped invariant: this tx bumped the sender's
		// nonce on the line above, so it must produce a receipt. State-
		// transition errors land here when Execute() bails before any
		// opcode ran (notably EIP-7623's floor-data-gas check, which
		// happens inside go-ethereum's Execute() rather than the Sei
		// antehandler). Without an explicit WriteReceipt the receipt
		// store stays empty for this tx hash — Giga's
		// AppendToEvmTxDeferredInfo call below doesn't propagate the
		// error, so EndBlock's synthetic-receipt path skips it — and
		// eth_getTransactionReceipt returns null forever, hanging any
		// client that polls for it.
		evmMsg := &core.Message{
			Nonce:     ethTx.Nonce(),
			GasLimit:  ethTx.Gas(),
			GasPrice:  effectiveGasPrice, // EIP-1559 effective gas price (not GasFeeCap)
			GasFeeCap: ethTx.GasFeeCap(),
			GasTipCap: ethTx.GasTipCap(),
			To:        ethTx.To(),
			Value:     ethTx.Value(),
			Data:      ethTx.Data(),
			From:      sender,
		}
		if _, rerr := app.GigaEvmKeeper.WriteReceipt(ctx, stateDB, evmMsg, uint32(ethTx.Type()), ethTx.Hash(), ethTx.Gas(), execErr.Error()); rerr != nil {
			logger.Error("giga: failed to write failed-tx receipt",
				"tx-hash", ethTx.Hash(),
				"error", rerr,
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

	// Create core.Message from ethTx for WriteReceipt.
	// GasPrice must be the EIP-1559 effective gas price (min(baseFee+tip,
	// maxFee)) — that's what the chain actually charges (see line 1866)
	// and what the receipt's EffectiveGasPrice field needs to report.
	// ethTx.GasPrice() returns GasFeeCap for dynamic-fee txs, which puts
	// the wrong value on the receipt and breaks EIP-1559 clients.
	evmMsg := &core.Message{
		Nonce:     ethTx.Nonce(),
		GasLimit:  ethTx.Gas(),
		GasPrice:  effectiveGasPrice,
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

// setCodeTxRequiresAuthorityAssociation reports whether an EIP-7702 transaction has any
// authorization authority that the EVM will apply and that is not yet associated with its
// true (pubkey-derived) Sei address. Such an authority must be associated (with balance
// migration) before SetCode runs, which the V2 ante handler does but the giga executor
// cannot; callers use this to defer the transaction to V2. Authorizations the EVM would
// skip (wrong chain id, bad nonce, authority has code) are ignored: no mapping is created
// for them. It returns false for non-SetCode transactions (which carry no authorizations).
func (app *App) setCodeTxRequiresAuthorityAssociation(ctx sdk.Context, ethTx *ethtypes.Transaction) bool {
	for _, auth := range ethTx.SetCodeAuthorizations() {
		if _, _, _, ok := helpers.AuthorityToPreAssociate(ctx, &app.GigaEvmKeeper, auth); ok {
			return true
		}
	}
	return false
}

// gigaDeliverTx is the OCC-compatible deliverTx function for the giga executor.
// makeGigaDeliverTx returns an OCC-compatible deliverTx callback that captures the given
// block cache, avoiding mutable state on App for cache lifecycle management.
func (app *App) makeGigaDeliverTx(cache *gigaBlockCache) func(sdk.Context, abci.RequestDeliverTxV2, sdk.Tx, [32]byte) abci.ResponseDeliverTx {
	return func(ctx sdk.Context, req abci.RequestDeliverTxV2, tx sdk.Tx, checksum [32]byte) (resp abci.ResponseDeliverTx) {
		defer func() {
			if r := recover(); r != nil {
				// Handle panics as V2 does (matches baseapp/recovery.go middleware chain)
				if abort, isOCCAbort := r.(occ.Abort); isOCCAbort {
					resp = abci.ResponseDeliverTx{
						Codespace: sdkerrors.RootCodespace,
						Code:      sdkerrors.ErrOCCAbort.ABCICode(),
						Log:       fmt.Sprintf("occ abort occurred with dependent index %d and error: %v", abort.DependentTxIdx, abort.Err),
					}
					return
				}
				if oogErr, isOOG := r.(sdk.ErrorOutOfGas); isOOG {
					resp = abci.ResponseDeliverTx{
						Codespace: sdkerrors.RootCodespace,
						Code:      sdkerrors.ErrOutOfGas.ABCICode(),
						Log:       fmt.Sprintf("out of gas in location: %v", oogErr.Descriptor),
					}
					return
				}
				// Any store-iteration panic means giga can't handle this tx: fall back to v2.
				if err, ok := r.(error); ok && errors.Is(err, gigastore.ErrIteratorUnsupported) {
					resp = abci.ResponseDeliverTx{
						Code:      gigautils.GigaAbortCode,
						Codespace: gigautils.GigaAbortCodespace,
						Info:      gigaFallbackStoreIterator,
						Log:       "giga executor abort: store iteration unsupported, fall back to v2",
					}
					return
				}
				// For other panics (e.g., nil deref from malformed protobuf), log and return ErrPanic
				logger.Error("panic in gigaDeliverTx", "panic", r, "stack", string(debug.Stack()))
				resp = abci.ResponseDeliverTx{
					Codespace: sdkerrors.UndefinedCodespace,
					Code:      sdkerrors.ErrPanic.ABCICode(),
					Log:       fmt.Sprintf("recovered: %v\nstack:\n%v", r, string(debug.Stack())),
				}
			}
		}()

		evmMsg := app.GetEVMMsg(tx)
		if evmMsg == nil {
			return abci.ResponseDeliverTx{Code: 1, Log: "not an EVM transaction"}
		}

		// Validate Cosmos SDK envelope (memo, timeoutHeight, signerInfos, etc.)
		// This prevents consensus divergence if a malicious proposer includes invalid envelope fields.
		if err := appante.EvmStatelessChecks(ctx, tx, cache.chainID); err != nil {
			codespace, code, log := sdkerrors.ABCIInfo(err, false)
			return abci.ResponseDeliverTx{
				Codespace: codespace,
				Code:      code,
				Log:       log,
			}
		}

		result, err := app.executeEVMTxWithGigaExecutor(ctx, evmMsg, cache)
		if err != nil {
			// Check if this is a fail-fast error (Cosmos precompile interop detected)
			if gigautils.ShouldExecutionAbort(err) {
				// Return a sentinel response so the caller can fall back to v2.
				return abci.ResponseDeliverTx{
					Code:      gigautils.GigaAbortCode,
					Codespace: gigautils.GigaAbortCodespace,
					Info:      gigaFallbackReason(err),
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

func (app *App) DecodeTxBytesConcurrently(ctx context.Context, txs [][]byte) ([]sdk.Tx, error) {
	typedTxs := make([]sdk.Tx, len(txs))
	eg, ctx := errgroup.WithContext(ctx)
	eg.SetLimit(app.ConcurrencyWorkers())
	for i, tx := range txs {
		i, tx := i, tx // not needed on Go 1.22+
		eg.Go(func() (err error) {
			defer func() {
				if r := recover(); r != nil {
					logger.Error("encountered panic during transaction decoding", "err", r)
					err = fmt.Errorf("panic decoding tx at index %d: %v", i, r)
				}
			}()
			// Bail early if another goroutine already failed.
			if err := ctx.Err(); err != nil {
				return err
			}
			typedTx, decodeErr := app.txDecoder(tx)
			if decodeErr != nil {
				logger.Error("error decoding transaction at index", "index", i, "error", decodeErr)
				return decodeErr
			}
			typedTxs[i] = typedTx
			return nil
		})
	}
	if err := eg.Wait(); err != nil {
		return nil, err
	}
	return typedTxs, nil
}

// finalizeDecodedEVMSlot runs EVM Preprocess for a decoded tx at idx. typedTx must be non-nil EVM.
// Panics and preprocess errors clear typedTxs[idx].
func (app *App) finalizeDecodedEVMSlot(ctx sdk.Context, idx int, typedTx sdk.Tx, typedTxs []sdk.Tx) {
	defer func() {
		if err := recover(); err != nil {
			logger.Error("encountered panic during transaction preprocessing", "err", err)
			typedTxs[idx] = nil
		}
	}()
	msg := evmtypes.MustGetEVMTransactionMessage(typedTx)
	if err := evmante.Preprocess(ctx, msg, app.EvmKeeper.ChainID(ctx), app.EvmKeeper.EthBlockTestConfig.Enabled); err != nil {
		logger.Error("error preprocessing EVM tx", "err", err)
		typedTxs[idx] = nil
	}
}

// FinalizeDecodedTransactionsConcurrently runs EVM preprocessing on typedTxs in place.
// Non-EVM entries are unchanged; failed preprocessing clears the slot (nil).
func (app *App) FinalizeDecodedTransactionsConcurrently(ctx sdk.Context, typedTxs []sdk.Tx) {
	if app.GigaExecutorEnabled {
		return
	}
	wg := sync.WaitGroup{}
	for i, typedTx := range typedTxs {
		if typedTx == nil {
			continue
		}
		isEVM, _ := evmante.IsEVMMessage(typedTx)
		if !isEVM {
			continue
		}
		wg.Add(1)
		go func(idx int, tx sdk.Tx) {
			defer wg.Done()
			app.finalizeDecodedEVMSlot(ctx, idx, tx, typedTxs)
		}(i, typedTx)
	}
	wg.Wait()
}

// DecodeTransactionsConcurrently decodes each tx and runs EVM preprocessing in one goroutine per index.
// Failed decodes, decode panics, and failed preprocessing clear the slot (nil), matching prior behavior.
func (app *App) DecodeTransactionsConcurrently(ctx sdk.Context, txs [][]byte) []sdk.Tx {
	typedTxs := make([]sdk.Tx, len(txs))
	giga := app.GigaExecutorEnabled
	wg := sync.WaitGroup{}
	for i, tx := range txs {
		wg.Add(1)
		go func(idx int, encodedTx []byte) {
			defer wg.Done()
			var typedTx sdk.Tx
			func() {
				defer func() {
					if err := recover(); err != nil {
						logger.Error("encountered panic during transaction decoding", "err", err)
						typedTx = nil
					}
				}()
				var err error
				typedTx, err = app.txDecoder(encodedTx)
				if err != nil {
					logger.Error("error decoding transaction at index", "index", idx, "error", err)
					typedTx = nil
				}
			}()
			typedTxs[idx] = typedTx
			if giga || typedTx == nil {
				return
			}
			isEVM, _ := evmante.IsEVMMessage(typedTx)
			if !isEVM {
				return
			}
			app.finalizeDecodedEVMSlot(ctx, idx, typedTx, typedTxs)
		}(i, tx)
	}
	wg.Wait()
	return typedTxs
}

func (app *App) getFinalizeBlockResponse(
	appHash []byte,
	events []abci.Event,
	txResults []*abci.ExecTxResult,
	endBlockResp abci.ResponseEndBlock,
	consensusParamUpdates *tmproto.ConsensusParams,
) abci.ResponseFinalizeBlock {
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
		ConsensusParamUpdates: cloneConsensusParams(consensusParamUpdates),
		AppHash:               appHash,
	}
}

func cloneConsensusParams(params *tmproto.ConsensusParams) *tmproto.ConsensusParams {
	if params == nil {
		return nil
	}

	cp := &tmproto.ConsensusParams{}
	if params.Block != nil {
		cp.Block = &tmproto.BlockParams{
			MaxBytes:      params.Block.MaxBytes,
			MaxGas:        params.Block.MaxGas,
			MinTxsInBlock: params.Block.MinTxsInBlock,
			MaxGasWanted:  params.Block.MaxGasWanted,
		}
	}
	if params.Evidence != nil {
		cp.Evidence = &tmproto.EvidenceParams{
			MaxAgeNumBlocks: params.Evidence.MaxAgeNumBlocks,
			MaxAgeDuration:  params.Evidence.MaxAgeDuration,
			MaxBytes:        params.Evidence.MaxBytes,
		}
	}
	if params.Validator != nil {
		cp.Validator = &tmproto.ValidatorParams{
			PubKeyTypes: append([]string(nil), params.Validator.PubKeyTypes...),
		}
	}
	if params.Version != nil {
		cp.Version = &tmproto.VersionParams{
			AppVersion: params.Version.AppVersion,
		}
	}
	if params.Synchrony != nil {
		cp.Synchrony = &tmproto.SynchronyParams{
			Precision:    cloneDuration(params.Synchrony.Precision),
			MessageDelay: cloneDuration(params.Synchrony.MessageDelay),
		}
	}
	if params.Timeout != nil {
		cp.Timeout = &tmproto.TimeoutParams{
			Propose:             cloneDuration(params.Timeout.Propose),
			ProposeDelta:        cloneDuration(params.Timeout.ProposeDelta),
			Vote:                cloneDuration(params.Timeout.Vote),
			VoteDelta:           cloneDuration(params.Timeout.VoteDelta),
			Commit:              cloneDuration(params.Timeout.Commit),
			BypassCommitTimeout: params.Timeout.BypassCommitTimeout,
		}
	}
	if params.Abci != nil {
		cp.Abci = &tmproto.ABCIParams{
			VoteExtensionsEnableHeight: params.Abci.VoteExtensionsEnableHeight,
			RecheckTx:                  params.Abci.RecheckTx,
		}
	}

	return cp
}

func cloneDuration(duration *time.Duration) *time.Duration {
	if duration == nil {
		return nil
	}
	cloned := *duration
	return &cloned
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

func (app *App) GetValidators() []abci.ValidatorUpdate {
	// AUTOBAHN: After InitChain but before the first Commit, the committed
	// store is empty — staking params don't exist, so reading from committed
	// store panics in MaxValidators. Use DeliverContext when available at
	// height 0, since it has the uncommitted staking state from InitChain.
	// CometBFT consensus never hits this because its handshaker commits
	// after InitChain before any block processing begins.
	if app.LastBlockHeight() == 0 {
		if dctx := app.DeliverContext(); dctx != nil {
			return app.StakingKeeper.GetBondedValidators(*dctx)
		}
	}
	ctx := app.NewUncachedContext(false, tmproto.Header{Height: max(app.LastBlockHeight(), 1)})
	return app.StakingKeeper.GetBondedValidators(ctx)
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

func (app *App) RPCContextProvider(i int64) sdk.Context {
	if i == evmrpc.LatestCtxHeight {
		ctx := app.GetCheckCtx()
		// Populate ConsensusParams on the RPC ctx. Neither GetCheckCtx nor
		// CreateQueryContext sets it (only the tx-execution path does, via
		// getContextForTx), so without this, ctx.ConsensusParams() returns
		// nil from any RPC handler. evmrpc/block.go's gasLimit and
		// evmrpc/info.go's gas-used-ratio rely on it.
		ctx = ctx.WithConsensusParams(app.GetConsensusParams(ctx))
		return ctx.WithIsEVM(true).WithTraceMode(true).WithIsCheckTx(false).WithClosestUpgradeName(LatestUpgrade)
	}
	ctx, err := app.CreateQueryContext(i, false)
	if err != nil {
		panic(err)
	}
	ctx = ctx.WithConsensusParams(app.GetConsensusParams(ctx))
	closestUpgrade, upgradeHeight := app.UpgradeKeeper.GetClosestUpgrade(app.GetCheckCtx(), i)
	if closestUpgrade == "" && upgradeHeight == 0 {
		closestUpgrade = LatestUpgrade
	}
	ctx = ctx.WithClosestUpgradeName(closestUpgrade)
	return ctx.WithIsEVM(true).WithTraceMode(true).WithIsCheckTx(false)
}

// SnapshotAwareRPCContextProvider builds SDK contexts from in-memory memiavl
// snapshots; falls back to RPCContextProvider on miss or unsupported backend.
func (app *App) SnapshotAwareRPCContextProvider() evmrpc.TraceContextProvider {
	store := app.EvmKeeper.TraceSnapshotStore()
	if store == nil {
		return evmrpc.TraceContextProvider(func(i int64) (sdk.Context, func()) {
			return app.RPCContextProvider(i), func() {}
		})
	}
	rs, ok := app.CommitMultiStore().(*rootmulti.Store)
	if !ok {
		return evmrpc.TraceContextProvider(func(i int64) (sdk.Context, func()) {
			return app.RPCContextProvider(i), func() {}
		})
	}
	return evmrpc.TraceContextProvider(func(i int64) (sdk.Context, func()) {
		if i <= 0 {
			return app.RPCContextProvider(i), func() {}
		}
		snap, release := store.Lease(i)
		if snap == nil {
			return app.RPCContextProvider(i), func() {}
		}
		cms, err := rs.CacheMultiStoreFromCommitter(snap)
		if err != nil {
			release()
			return app.RPCContextProvider(i), func() {}
		}
		checkCtx := app.GetCheckCtx()
		closestUpgrade, upgradeHeight := app.UpgradeKeeper.GetClosestUpgrade(checkCtx, i)
		if closestUpgrade == "" && upgradeHeight == 0 {
			closestUpgrade = LatestUpgrade
		}
		ctx := sdk.NewContext(cms, checkCtx.BlockHeader(), true).
			WithMinGasPrices(checkCtx.MinGasPrices()).
			WithBlockHeight(i).
			WithClosestUpgradeName(closestUpgrade).
			WithIsEVM(true).WithTraceMode(true).WithIsCheckTx(false)
		ctx = ctx.WithConsensusParams(app.GetConsensusParams(ctx))
		return ctx, release
	})
}

// RegisterTendermintService implements the Application.RegisterLocalServices method.
func (app *App) RegisterLocalServices(node client.LocalClient, txConfig client.TxConfig) {
	authtx.RegisterTxService(app.GRPCQueryRouter(), node, txConfig, app.Simulate, app.interfaceRegistry)
	tmservice.RegisterTendermintService(app.GRPCQueryRouter(), node, app.interfaceRegistry)
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

	rpcCtxProvider := app.RPCContextProvider
	traceCtxProvider := app.SnapshotAwareRPCContextProvider()
	if app.evmRPCConfig.HTTPEnabled {
		evmHTTPServer, err := evmrpc.NewEVMHTTPServer(app.evmRPCConfig, node, &app.EvmKeeper, app.BeginBlockKeepers, app.BaseApp, app.TracerAnteHandler, app.RPCContextProvider, txConfigProvider, DefaultNodeHome, app.GetStateStore(), traceCtxProvider)
		if err != nil {
			panic(err)
		}
		app.evmHTTPServer = evmHTTPServer
		go func() {
			<-app.httpServerStartSignal
			if err := evmHTTPServer.Start(); err != nil {
				panic(err)
			}
		}()
	}

	if app.evmRPCConfig.WSEnabled {
		headNotifier, _ := app.blockHeaderNotifier.Get()
		evmWSServer, err := evmrpc.NewEVMWebSocketServer(app.evmRPCConfig, node, &app.EvmKeeper, app.BeginBlockKeepers, app.BaseApp, app.TracerAnteHandler, rpcCtxProvider, txConfigProvider, DefaultNodeHome, app.GetStateStore(), headNotifier)
		if err != nil {
			panic(err)
		}
		app.evmWSServer = evmWSServer
		go func() {
			<-app.wsServerStartSignal
			if err := evmWSServer.Start(); err != nil {
				panic(err)
			}
		}()
	}

	if app.adminConfig.Enabled {
		srv, err := admin.StartServer(app.adminConfig.Address)
		if err != nil {
			panic(fmt.Sprintf("failed to start admin server: %s", err))
		}
		app.adminServer = srv
	} else {
		logger.Debug("Admin gRPC server is disabled")
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
// or the gas wanted if it's a Cosmos tx. typedTxs must align with proposal order (nil = decode failure).
func (app *App) checkTotalBlockGas(ctx sdk.Context, typedTxs []sdk.Tx) (_result bool) {
	defer func() {
		if r := recover(); r != nil {
			logger.Error("panic recovered in checkTotalBlockGas", "panic", r)
			_result = false
		}
	}()

	var totalGas, totalGasWanted uint64
	for _, decodedTx := range typedTxs {
		if decodedTx == nil {
			return false
		}

		isEVM, evmErr := evmante.IsEVMMessage(decodedTx)

		// MsgEVMTransaction cannot be gasless under IsTxGasless (only oracle vote / MsgAssociate).
		// Skip keeper-backed IsTxGasless for valid single-message EVM txs; still run it when the tx
		// is not EVM or EVM classification failed (e.g. multi-msg with an EVM message).
		skipGaslessCheck := evmErr == nil && isEVM
		if !skipGaslessCheck && app.couldBeGaslessTransaction(decodedTx) {
			isGasless, err := antedecorators.IsTxGasless(decodedTx, ctx, app.OracleKeeper, &app.EvmKeeper)
			if err != nil {
				if strings.Contains(err.Error(), "panic in IsTxGasless") {
					// Unexpected panic: reject the entire proposal.
					logger.Error("malicious transaction detected in gasless check", "err", err)
					return false
				}
				// Business-logic errors (e.g. duplicate votes): keep going, tx is treated as non-gasless.
				logger.Info("transaction failed gasless check but not malicious", "err", err)
			}
			if isGasless {
				continue
			}
		}

		// EVM classification failed (e.g. multi-msg containing an EVM message); such a tx won't be
		// processed and so contributes no gas to the block.
		if evmErr != nil {
			continue
		}

		var gasWanted uint64
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
				// Non-fee tx won't be processed and thus won't consume gas. Skipping.
				continue
			}
			gasWanted = feeTx.GetGas()
		}

		// Overflow guards: gasWanted must fit in int64, and adding it to either accumulator
		// must not wrap uint64.
		if int64(gasWanted) < 0 || //nolint:gosec
			totalGasWanted > math.MaxUint64-gasWanted ||
			totalGas > math.MaxUint64-gasWanted {
			return false
		}

		totalGasWanted += gasWanted

		// Prefer the gas estimate when it's a valid EVM estimate (>= MinGasEVMTx) and not
		// inflated above gasWanted; otherwise charge full gasWanted.
		if est := decodedTx.GetGasEstimate(); est >= MinGasEVMTx && est <= gasWanted {
			totalGas += est
		} else {
			totalGas += gasWanted
		}

		blockParams := ctx.ConsensusParams().Block
		maxGasWanted := uint64(blockParams.MaxGasWanted) //nolint:gosec
		maxGas := uint64(blockParams.MaxGas)             //nolint:gosec
		if totalGasWanted > maxGasWanted || totalGas > maxGas {
			return false
		}
	}

	appMetrics.blockGasWanted.Record(ctx.Context(), int64(totalGasWanted)) //nolint:gosec
	if cp := ctx.ConsensusParams(); cp != nil && cp.Block != nil && cp.Block.MaxGasWanted > 0 {
		appMetrics.blockGasWantedRatio.Record(ctx.Context(), float64(totalGasWanted)/float64(cp.Block.MaxGasWanted))
	}
	return true
}

// isExpectedGaslessMetricsError reports whether err is the well-known oracle
// duplicate-vote error that we deliberately tolerate when collecting
// gasless-tx metrics. errors.Is handles properly-wrapped chains; the substring
// fallback covers chains that lost sentinel identity via %s/%v wrapping.
func isExpectedGaslessMetricsError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, oracletypes.ErrAggregateVoteExist) {
		return true
	}
	return strings.Contains(err.Error(), oracletypes.ErrAggregateVoteExist.Error())
}

// couldBeGaslessTransaction is a fast heuristic that returns true when tx
// might be gasless and a full IsTxGasless keeper check is therefore worth
// running. It MUST be a conservative over-approximation: returning false for
// a tx that is actually gasless would cause its gas to be counted against
// the block limit, producing incorrect gas accounting (and in the worst case
// rejecting an otherwise-valid block).
func (app *App) couldBeGaslessTransaction(tx sdk.Tx) bool {
	if tx == nil {
		return false
	}
	for _, msg := range tx.GetMsgs() {
		switch msg.(type) {
		case *evmtypes.MsgAssociate, *oracletypes.MsgAggregateExchangeRateVote:
			return true
		}
	}
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
	paramsKeeper.Subspace(ibctransfertypes.ModuleName)
	paramsKeeper.Subspace(ibchost.ModuleName)
	paramsKeeper.Subspace(oracletypes.ModuleName)
	paramsKeeper.Subspace(wasm.ModuleName)
	paramsKeeper.Subspace(evmtypes.ModuleName)
	paramsKeeper.Subspace(epochmoduletypes.ModuleName)
	paramsKeeper.Subspace(tokenfactorytypes.ModuleName)
	paramsKeeper.Subspace(migration.SubspaceName).WithKeyTable(migration.ParamKeyTable())
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

// gigaValidationResult holds the result of EVM transaction validation.
type gigaValidationResult struct {
	err     *abci.ExecTxResult // nil if validation passed
	baseFee *big.Int           // the base fee used for validation
}

// validateGigaEVMTx validates an EVM tx for fee, nonce, and stateless checks.
// Note: Cosmos envelope checks (chain ID, intrinsic gas, etc.) are done earlier via EvmStatelessChecks.
//
// This function handles checks from V2's EVMFeeCheckDecorator + go-ethereum's StatelessChecks:
//  1. Fee cap checks
//  2. Nonce validity (including overflow guard)
//  3. Sender EOA check (unless delegated via EIP-7702)
//  4. Gas fee/tip cap bit length checks
//  5. Tip <= fee cap check
//  6. Set-code tx validation
//  7. Balance check
func (app *App) validateGigaEVMTx(
	ctx sdk.Context,
	ethTx *ethtypes.Transaction,
	sender common.Address,
	seiAddr sdk.AccAddress,
	isAssociated bool,
) gigaValidationResult {
	baseFee := app.GigaEvmKeeper.GetBaseFee(ctx)
	if baseFee == nil {
		baseFee = new(big.Int)
	}

	currentNonce := app.GigaEvmKeeper.GetNonce(ctx, sender)
	txNonce := ethTx.Nonce()

	// Fee cap below base fee
	if ethTx.GasFeeCap().Cmp(baseFee) < 0 {
		return gigaValidationResult{
			err: &abci.ExecTxResult{
				Code: sdkerrors.ErrInsufficientFee.ABCICode(),
				Log:  "max fee per gas less than block base fee",
			},
			baseFee: baseFee,
		}
	}

	// Fee cap below minimum fee
	minimumFee := app.GigaEvmKeeper.GetMinimumFeePerGas(ctx).TruncateInt().BigInt()
	if ethTx.GasFeeCap().Cmp(minimumFee) < 0 {
		return gigaValidationResult{
			err: &abci.ExecTxResult{
				Code: sdkerrors.ErrInsufficientFee.ABCICode(),
				Log:  "max fee per gas less than minimum fee",
			},
			baseFee: baseFee,
		}
	}

	// ========================================================================
	// go-ethereum StatelessChecks (matches V2's EVMFeeCheckDecorator call to st.StatelessChecks())
	// ========================================================================

	// Nonce checks (too high, too low, overflow guard)
	if txNonce > currentNonce {
		return gigaValidationResult{
			err: &abci.ExecTxResult{
				Code: sdkerrors.ErrWrongSequence.ABCICode(),
				Log:  fmt.Sprintf("nonce too high: address %s, tx: %d state: %d", sender.Hex(), txNonce, currentNonce),
			},
			baseFee: baseFee,
		}
	}
	if txNonce < currentNonce {
		return gigaValidationResult{
			err: &abci.ExecTxResult{
				Code: sdkerrors.ErrWrongSequence.ABCICode(),
				Log:  fmt.Sprintf("nonce too low: address %s, tx: %d state: %d", sender.Hex(), txNonce, currentNonce),
			},
			baseFee: baseFee,
		}
	}
	// Nonce overflow guard (currentNonce + 1 would overflow)
	if currentNonce+1 < currentNonce {
		return gigaValidationResult{
			err: &abci.ExecTxResult{
				Code: sdkerrors.ErrWrongSequence.ABCICode(),
				Log:  fmt.Sprintf("nonce max: address %s, nonce: %d", sender.Hex(), currentNonce),
			},
			baseFee: baseFee,
		}
	}

	// Sender must be EOA unless delegated (EIP-7702)
	senderCode := app.GigaEvmKeeper.GetCode(ctx, sender)
	if len(senderCode) > 0 {
		_, isDelegated := ethtypes.ParseDelegation(senderCode)
		if !isDelegated {
			return gigaValidationResult{
				err: &abci.ExecTxResult{
					Code: sdkerrors.ErrWrongSequence.ABCICode(),
					Log:  fmt.Sprintf("sender not an eoa: address %s, len(code): %d", sender.Hex(), len(senderCode)),
				},
				baseFee: baseFee,
			}
		}
	}

	// GasFeeCap bit length must be <= 256
	if l := ethTx.GasFeeCap().BitLen(); l > 256 {
		return gigaValidationResult{
			err: &abci.ExecTxResult{
				Code: sdkerrors.ErrWrongSequence.ABCICode(),
				Log:  fmt.Sprintf("max fee per gas higher than 2^256-1: address %s, maxFeePerGas bit length: %d", sender.Hex(), l),
			},
			baseFee: baseFee,
		}
	}

	// GasTipCap bit length must be <= 256
	if l := ethTx.GasTipCap().BitLen(); l > 256 {
		return gigaValidationResult{
			err: &abci.ExecTxResult{
				Code: sdkerrors.ErrWrongSequence.ABCICode(),
				Log:  fmt.Sprintf("max priority fee per gas higher than 2^256-1: address %s, maxPriorityFeePerGas bit length: %d", sender.Hex(), l),
			},
			baseFee: baseFee,
		}
	}

	// GasTipCap must be <= GasFeeCap
	if ethTx.GasTipCap().Cmp(ethTx.GasFeeCap()) > 0 {
		return gigaValidationResult{
			err: &abci.ExecTxResult{
				Code: sdkerrors.ErrWrongSequence.ABCICode(),
				Log:  fmt.Sprintf("max priority fee per gas higher than max fee per gas: address %s, maxPriorityFeePerGas: %s, maxFeePerGas: %s", sender.Hex(), ethTx.GasTipCap(), ethTx.GasFeeCap()),
			},
			baseFee: baseFee,
		}
	}

	// Set-code tx (EIP-7702) validation
	if ethTx.Type() == ethtypes.SetCodeTxType {
		// Set-code tx must not be contract creation
		if ethTx.To() == nil {
			return gigaValidationResult{
				err: &abci.ExecTxResult{
					Code: sdkerrors.ErrWrongSequence.ABCICode(),
					Log:  fmt.Sprintf("set-code transaction must not be a create transaction: sender %s", sender.Hex()),
				},
				baseFee: baseFee,
			}
		}
		// Set-code tx auth list must be non-empty
		if len(ethTx.SetCodeAuthorizations()) == 0 {
			return gigaValidationResult{
				err: &abci.ExecTxResult{
					Code: sdkerrors.ErrWrongSequence.ABCICode(),
					Log:  fmt.Sprintf("set-code transaction with empty auth list: sender %s", sender.Hex()),
				},
				baseFee: baseFee,
			}
		}
	}

	// ========================================================================
	// Balance check (matches V2's st.BuyGas())
	// ========================================================================

	// Insufficient balance for gas + value
	balanceCheck := new(big.Int).Mul(new(big.Int).SetUint64(ethTx.Gas()), ethTx.GasFeeCap())
	balanceCheck.Add(balanceCheck, ethTx.Value())

	senderBalance := app.GigaEvmKeeper.GetBalance(ctx, seiAddr)

	// Include cast address balance for unassociated addresses (matches V2 PreprocessDecorator)
	if !isAssociated {
		castAddr := sdk.AccAddress(sender[:])
		castBalance := app.GigaEvmKeeper.GetBalance(ctx, castAddr)
		senderBalance = new(big.Int).Add(senderBalance, castBalance)
	}

	if senderBalance.Cmp(balanceCheck) < 0 {
		return gigaValidationResult{
			err: &abci.ExecTxResult{
				Code: sdkerrors.ErrInsufficientFunds.ABCICode(),
				Log:  fmt.Sprintf("insufficient funds for gas * price + value: address %s have %v want %v: insufficient funds", sender.Hex(), senderBalance, balanceCheck),
			},
			baseFee: baseFee,
		}
	}

	// All checks passed
	return gigaValidationResult{
		err:     nil,
		baseFee: baseFee,
	}
}

func init() {
	// override max wasm size to 2MB
	wasmtypes.MaxWasmSize = 2 * 1024 * 1024
}
