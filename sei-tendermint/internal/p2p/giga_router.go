package p2p

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/giga/apphash/builder"
	abevm "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/evm"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/producer"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/tcp"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

type GigaNodeAddr struct {
	Key      NodePublicKey
	HostPort tcp.HostPort
}

func (a GigaNodeAddr) String() string {
	return fmt.Sprintf("%v@%v", a.Key, a.HostPort)
}

// GigaRouterCommonConfig is the slice of giga config shared by both
// validator and fullnode constructors.
type GigaRouterCommonConfig struct {
	DialInterval     time.Duration
	HandshakeTimeout utils.Option[time.Duration]
	ValidatorAddrs   map[atypes.PublicKey]GigaNodeAddr
	GenDoc           *types.GenesisDoc
	// PersistentStateDir is the absolute on-disk root for durable state
	// (BlockDB, epoch snapshots, and the validator's consensus persister in
	// sibling subdirs). Required and must already exist.
	PersistentStateDir string
	// App is the application the blocks run on. The router reads its committed
	// height and validator set, and NewGigaValidatorRouter passes it to the
	// producer's mempool.
	App *abevm.Proxy
	// AppHashBuilder is the source of every Autobahn application hash. The
	// storage manager owns it. The router reads each executed block's app hash
	// from it.
	AppHashBuilder builder.AppHashBuilder
	// MaxInboundFullnodePeers caps inbound block-sync from non-committee
	// peers outside PersistentPeerIDs. 0 rejects every such peer; a positive
	// value caps at n, up to maxInboundFullnodePeers.
	MaxInboundFullnodePeers int
	// PersistentPeerIDs are node IDs from p2p.persistent-peers and
	// p2p.blocksync-peers. They do not consume MaxInboundFullnodePeers.
	PersistentPeerIDs map[types.NodeID]struct{}

	// Whether validator should proxy txs which do not belong to the local node.
	EnableEvmProxy bool
	// AllowLocalAddrs permits loopback and link-local EVM RPC URLs in tests.
	AllowLocalAddrs bool
}

// GigaValidatorConfig configures a committee-member GigaRouter.
type GigaValidatorConfig struct {
	GigaRouterCommonConfig
	ValidatorKey    atypes.SecretKey
	EVMRPC          url.URL
	ViewTimeout     func(atypes.View) time.Duration
	ProposalTimeout time.Duration
	Producer        *producer.Config
}

// GigaRouter serves Autobahn peers, blocks, and shard-routed EVM requests.
// Mempool returns Some only on validators. RunInboundConn is served by both
// validators and fullnodes — non-committee peers get the block-sync subset only.
// A fullnode accepts committee peers but has no consensus state to serve them.
type GigaRouter interface {
	Run(ctx context.Context) error
	RunInboundConn(ctx context.Context, hConn *handshakedConn) error
	BlockByNumber(ctx context.Context, n atypes.RoadHeight) (*abevm.EvmBlock, error)
	BlockByHash(ctx context.Context, hash common.Hash) (utils.Option[*abevm.EvmBlock], error)
	InsertTx(ctx context.Context, input types.Tx) (common.Hash, error)
	EvmPendingNonce(ctx context.Context, address common.Address) (uint64, error)
	Mempool() utils.Option[*producer.State]
	fillInboundHandshake(spec handshakeSpec) handshakeSpec
}
