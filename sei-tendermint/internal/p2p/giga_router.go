package p2p

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/ethereum/go-ethereum/common"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/producer"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/tcp"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

type GigaNodeAddr struct {
	Key      NodePublicKey
	HostPort tcp.HostPort
	EVMRPC   *url.URL
}

func (a GigaNodeAddr) String() string {
	return fmt.Sprintf("%v@%v", a.Key, a.HostPort)
}

// BlockDBConfig holds optional overrides applied onto littblock.DefaultConfig
// when opening a durable BlockDB. Absent fields keep littblock defaults.
// Paths are always derived from PersistentStateDir — never set here.
type BlockDBConfig struct {
	Retention utils.Option[time.Duration]
	GCPeriod  utils.Option[time.Duration]
}

// GigaRouterCommonConfig is the slice of giga config shared by both
// validator and fullnode constructors.
type GigaRouterCommonConfig struct {
	DialInterval   time.Duration
	ValidatorAddrs map[atypes.PublicKey]GigaNodeAddr
	GenDoc         *types.GenesisDoc
	// PersistentStateDir is the on-disk root for durable state (BlockDB,
	// hashvault, and the validator's consensus persister in sibling subdirs).
	// If None, persistence is disabled and the node runs fully in-memory.
	PersistentStateDir utils.Option[string]
	// BlockDB optionally overrides littblock defaults when PersistentStateDir
	// is set. Zero value keeps littblock.DefaultConfig unchanged.
	BlockDB BlockDBConfig
	// App is the ABCI proxy executeBlock drives. NewGigaValidatorRouter
	// also passes it to producer.NewState so the producer's internal
	// mempool drives the same proxy.
	App *proxy.Proxy
	// MaxInboundFullnodePeers caps inbound block-sync from non-committee
	// peers. 0 rejects all; positive caps at n, up to maxInboundFullnodePeers.
	MaxInboundFullnodePeers int

	// HashVaultDisabledUnsafe disables the app-hash equivocation guard (HashVault). The guard is
	// on by default (false); the GigaRouter builds and owns it (see runExecute). Setting this to true
	// is an explicit, last-resort operator decision to run WITHOUT equivocation protection.
	HashVaultDisabledUnsafe bool
}

// GigaValidatorConfig configures a committee-member GigaRouter.
type GigaValidatorConfig struct {
	GigaRouterCommonConfig
	ValidatorKey atypes.SecretKey
	ViewTimeout  func(atypes.View) time.Duration
	Producer     *producer.Config
}

// GigaRouter is the read-path / Run / EvmProxy surface. Implemented by
// *gigaValidatorRouter and *gigaFullnodeRouter; Mempool returns Some only
// on validators. RunInboundConn is served by both — non-committee peers
// get the block-sync subset only.
type GigaRouter interface {
	Run(ctx context.Context) error
	RunInboundConn(ctx context.Context, hConn *handshakedConn) error
	LastCommittedBlockNumber() int64
	MaxGasEstimatedPerBlock() uint64
	BlockByNumber(ctx context.Context, n atypes.GlobalBlockNumber) (*coretypes.ResultBlock, error)
	BlockByHash(ctx context.Context, hash atypes.BlockHeaderHash) (*coretypes.ResultBlock, error)
	EvmProxy(sender common.Address) utils.Option[*url.URL]
	Mempool() utils.Option[*producer.State]
}
