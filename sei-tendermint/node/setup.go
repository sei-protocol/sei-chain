package node

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/blocksync"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/consensus"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/eventbus"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/evidence"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/mempool"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/conn"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/pex"
	sm "github.com/sei-protocol/sei-chain/sei-tendermint/internal/state"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/state/indexer"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/statesync"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/store"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/log"
	tmnet "github.com/sei-protocol/sei-chain/sei-tendermint/libs/net"
	tmstrings "github.com/sei-protocol/sei-chain/sei-tendermint/libs/strings"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/privval"
	tmgrpc "github.com/sei-protocol/sei-chain/sei-tendermint/privval/grpc"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/version"
	dbm "github.com/tendermint/tm-db"
	"golang.org/x/time/rate"

	_ "net/http/pprof" // nolint: gosec // securely exposed on separate, optional port
)

type closer func() error

func makeCloser(cs []closer) closer {
	return func() error {
		errs := make([]string, 0, len(cs))
		for _, cl := range cs {
			if err := cl(); err != nil {
				errs = append(errs, err.Error())
			}
		}
		if len(errs) >= 0 {
			return errors.New(strings.Join(errs, "; "))
		}
		return nil
	}
}

func convertCancelCloser(cancel context.CancelFunc) closer {
	return func() error { cancel(); return nil }
}

func combineCloseError(err error, cl closer) error {
	if err == nil {
		return cl()
	}

	clerr := cl()
	if clerr == nil {
		return err
	}

	return fmt.Errorf("error=%q closerError=%q", err.Error(), clerr.Error())
}

func initDBs(
	cfg *config.Config,
	dbProvider config.DBProvider,
) (*store.BlockStore, dbm.DB, closer, error) {

	blockStoreDB, err := dbProvider(&config.DBContext{ID: "blockstore", Config: cfg})
	if err != nil {
		return nil, nil, func() error { return nil }, fmt.Errorf("unable to initialize blockstore: %w", err)
	}
	closers := []closer{}
	blockStore := store.NewBlockStore(blockStoreDB)
	closers = append(closers, blockStoreDB.Close)

	stateDB, err := dbProvider(&config.DBContext{ID: "state", Config: cfg})
	if err != nil {
		return nil, nil, makeCloser(closers), fmt.Errorf("unable to initialize statestore: %w", err)
	}

	closers = append(closers, stateDB.Close)

	return blockStore, stateDB, makeCloser(closers), nil
}

func logNodeStartupInfo(state sm.State, pubKey utils.Option[crypto.PubKey], logger log.Logger, mode string) {
	// Log the version info.
	logger.Info("Version info",
		"tmVersion", version.TMVersion,
		"block", version.BlockProtocol,
		"p2p", version.P2PProtocol,
		"mode", mode,
	)

	// If the state and software differ in block version, at least log it.
	if state.Version.Consensus.Block != version.BlockProtocol {
		logger.Info("Software and state have different block protocols",
			"software", version.BlockProtocol,
			"state", state.Version.Consensus.Block,
		)
	}

	switch mode {
	case config.ModeFull:
		logger.Info("This node is a fullnode")
	case config.ModeValidator:
		k := pubKey.OrPanic()
		addr := k.Address()
		// Log whether this node is a validator or an observer
		if state.Validators.HasAddress(addr) {
			logger.Info("This node is a validator",
				"addr", addr,
				"pubKey", k.Bytes(),
			)
		} else {
			logger.Info("This node is a validator (NOT in the active validator set)",
				"addr", addr,
				"pubKey", k.Bytes(),
			)
		}
	}
}

func onlyValidatorIsUs(state sm.State, pubKey utils.Option[crypto.PubKey]) bool {
	k, ok := pubKey.Get()
	if !ok {
		return false
	}
	if state.Validators.Size() > 1 {
		return false
	}
	addr, _ := state.Validators.GetByIndex(0)
	return bytes.Equal(k.Address(), addr)
}

func createMempoolReactor(
	logger log.Logger,
	cfg *config.Config,
	appClient abci.Application,
	store sm.Store,
	memplMetrics *mempool.Metrics,
	router *p2p.Router,
) (*mempool.Reactor, mempool.Mempool, error) {
	logger = logger.With("module", "mempool")

	mp := mempool.NewTxMempool(
		logger,
		cfg.Mempool,
		appClient,
		router,
		mempool.WithMetrics(memplMetrics),
		mempool.WithPreCheck(sm.TxPreCheckFromStore(store)),
		mempool.WithPostCheck(sm.TxPostCheckFromStore(store)),
	)

	reactor, err := mempool.NewReactor(
		logger,
		cfg.Mempool,
		mp,
		router,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("mempool.NewReactor(): %w", err)
	}

	if cfg.Consensus.WaitForTxs() {
		mp.EnableTxsAvailable()
	}

	return reactor, mp, nil
}

func createEvidenceReactor(
	logger log.Logger,
	cfg *config.Config,
	dbProvider config.DBProvider,
	store sm.Store,
	blockStore *store.BlockStore,
	router *p2p.Router,
	metrics *evidence.Metrics,
	eventBus *eventbus.EventBus,
) (*evidence.Reactor, *evidence.Pool, closer, error) {
	evidenceDB, err := dbProvider(&config.DBContext{ID: "evidence", Config: cfg})
	if err != nil {
		return nil, nil, func() error { return nil }, fmt.Errorf("unable to initialize evidence db: %w", err)
	}

	logger = logger.With("module", "evidence")

	evidencePool := evidence.NewPool(logger, evidenceDB, store, blockStore, metrics, eventBus)
	evidenceReactor, err := evidence.NewReactor(logger, router, evidencePool)
	if err != nil {
		return nil, nil, evidenceDB.Close, fmt.Errorf("evidence.NewReactor(): %w", err)
	}
	return evidenceReactor, evidencePool, evidenceDB.Close, nil
}

func createRouter(
	logger log.Logger,
	p2pMetrics *p2p.Metrics,
	nodeInfoProducer func() *types.NodeInfo,
	nodeKey types.NodeKey,
	cfg *config.Config,
	appClient abci.Application,
	dbProvider config.DBProvider,
) (*p2p.Router, closer, error) {
	closer := func() error { return nil }
	ep, err := p2p.ResolveEndpoint(nodeKey.ID.AddressString(cfg.P2P.ListenAddress))
	if err != nil {
		return nil, closer, err
	}
	options := getRouterConfig(cfg, appClient)
	options.Endpoint = ep
	options.MaxOutboundConnections = utils.Some(int(cfg.P2P.MaxOutboundConnections))
	options.MaxIncomingConnectionAttempts = utils.Some(cfg.P2P.MaxIncomingConnectionAttempts)
	options.MaxDialRate = utils.Some(rate.Every(cfg.P2P.DialInterval))
	options.HandshakeTimeout = utils.Some(cfg.P2P.HandshakeTimeout)
	options.DialTimeout = utils.Some(cfg.P2P.DialTimeout)
	options.Connection = conn.DefaultMConnConfig()
	options.Connection.FlushThrottle = cfg.P2P.FlushThrottleTimeout
	options.Connection.SendRate = cfg.P2P.SendRate
	options.Connection.RecvRate = cfg.P2P.RecvRate
	options.Connection.MaxPacketMsgPayloadSize = cfg.P2P.MaxPacketMsgPayloadSize
	if addr := cfg.P2P.ExternalAddress; addr != "" {
		nodeAddr, err := p2p.ParseNodeAddress(nodeKey.ID.AddressString(addr))
		if err != nil {
			return nil, closer, fmt.Errorf("couldn't parse ExternalAddress %q: %w", cfg.P2P.ExternalAddress, err)
		}
		options.SelfAddress = utils.Some(nodeAddr)
	}
	var privatePeerIDs []types.NodeID
	for _, id := range tmstrings.SplitAndTrimEmpty(cfg.P2P.PrivatePeerIDs, ",", " ") {
		privatePeerIDs = append(privatePeerIDs, types.NodeID(id))
	}

	var maxConns int

	switch {
	case cfg.P2P.MaxConnections > 0:
		maxConns = int(cfg.P2P.MaxConnections)
	default:
		maxConns = 64
	}
	options.MaxConcurrentAccepts = utils.Some(maxConns)
	options.MaxConnected = utils.Some(maxConns)
	options.MaxPeers = utils.Some(2 * maxConns)
	options.PrivatePeers = privatePeerIDs

	for _, p := range tmstrings.SplitAndTrimEmpty(cfg.P2P.PersistentPeers, ",", " ") {
		address, err := p2p.ParseNodeAddress(p)
		if err != nil {
			return nil, closer, fmt.Errorf("invalid peer address %q: %w", p, err)
		}
		options.PersistentPeers = append(options.PersistentPeers, address)
	}

	for _, p := range tmstrings.SplitAndTrimEmpty(cfg.P2P.BootstrapPeers, ",", " ") {
		address, err := p2p.ParseNodeAddress(p)
		if err != nil {
			return nil, closer, fmt.Errorf("invalid peer address %q: %w", p, err)
		}
		options.BootstrapPeers = append(options.BootstrapPeers, address)
	}

	for _, p := range tmstrings.SplitAndTrimEmpty(cfg.P2P.BlockSyncPeers, ",", " ") {
		address, err := p2p.ParseNodeAddress(p)
		if err != nil {
			return nil, closer, fmt.Errorf("invalid peer address %q: %w", p, err)
		}
		options.PersistentPeers = append(options.PersistentPeers, address)
		options.BlockSyncPeers = append(options.BlockSyncPeers, address.NodeID)
	}

	for _, p := range tmstrings.SplitAndTrimEmpty(cfg.P2P.UnconditionalPeerIDs, ",", " ") {
		options.UnconditionalPeers = append(options.UnconditionalPeers, types.NodeID(p))
	}
	peerDB, err := dbProvider(&config.DBContext{ID: "peerstore", Config: cfg})
	if err != nil {
		return nil, closer, fmt.Errorf("unable to initialize peer store: %w", err)
	}
	closer = peerDB.Close
	router, err := p2p.NewRouter(
		logger.With("module", "p2p"),
		p2pMetrics,
		p2p.NodeSecretKey(nodeKey.PrivKey),
		nodeInfoProducer,
		peerDB,
		options,
	)
	if err != nil {
		return nil, closer, fmt.Errorf("p2p.NewRouter(): %w", err)
	}
	return router, closer, nil
}

func makeNodeInfo(
	cfg *config.Config,
	nodeKey types.NodeKey,
	eventSinks []indexer.EventSink,
	genDoc *types.GenesisDoc,
	versionInfo version.Consensus,
) (types.NodeInfo, error) {

	txIndexerStatus := "off"

	if indexer.IndexingEnabled(eventSinks) {
		txIndexerStatus = "on"
	}

	nodeInfo := types.NodeInfo{
		ProtocolVersion: types.ProtocolVersion{
			P2P:   version.P2PProtocol, // global
			Block: versionInfo.Block,
			App:   versionInfo.App,
		},
		NodeID:  nodeKey.ID,
		Network: genDoc.ChainID,
		Version: version.TMVersion,
		Channels: []byte{
			byte(blocksync.BlockSyncChannel),
			byte(consensus.StateChannel),
			byte(consensus.DataChannel),
			byte(consensus.VoteChannel),
			byte(consensus.VoteSetBitsChannel),
			byte(mempool.MempoolChannel),
			byte(evidence.EvidenceChannel),
			byte(statesync.SnapshotChannel),
			byte(statesync.ChunkChannel),
			byte(statesync.LightBlockChannel),
			byte(statesync.ParamsChannel),
		},
		Moniker: cfg.Moniker,
		Other: types.NodeInfoOther{
			TxIndex:    txIndexerStatus,
			RPCAddress: cfg.RPC.ListenAddress,
		},
	}

	if cfg.P2P.PexReactor {
		nodeInfo.Channels = append(nodeInfo.Channels, pex.PexChannel)
	}

	nodeInfo.ListenAddr = cfg.P2P.ExternalAddress
	if nodeInfo.ListenAddr == "" {
		nodeInfo.ListenAddr = cfg.P2P.ListenAddress
	}

	return nodeInfo, nodeInfo.Validate()
}

func makeSeedNodeInfo(
	cfg *config.Config,
	nodeKey types.NodeKey,
	genDoc *types.GenesisDoc,
	state sm.State,
) (types.NodeInfo, error) {
	nodeInfo := types.NodeInfo{
		ProtocolVersion: types.ProtocolVersion{
			P2P:   version.P2PProtocol, // global
			Block: state.Version.Consensus.Block,
			App:   state.Version.Consensus.App,
		},
		NodeID:  nodeKey.ID,
		Network: genDoc.ChainID,
		Version: version.TMVersion,
		Channels: []byte{
			pex.PexChannel,
		},
		Moniker: cfg.Moniker,
		Other: types.NodeInfoOther{
			TxIndex:    "off",
			RPCAddress: cfg.RPC.ListenAddress,
		},
	}

	nodeInfo.ListenAddr = cfg.P2P.ExternalAddress
	if nodeInfo.ListenAddr == "" {
		nodeInfo.ListenAddr = cfg.P2P.ListenAddress
	}

	return nodeInfo, nodeInfo.Validate()
}

func createAndStartPrivValidatorSocketClient(
	ctx context.Context,
	listenAddr, chainID string,
	logger log.Logger,
) (types.PrivValidator, error) {

	pve, err := privval.NewSignerListener(listenAddr, logger)
	if err != nil {
		return nil, fmt.Errorf("starting validator listener: %w", err)
	}

	pvsc, err := privval.NewSignerClient(ctx, pve, chainID)
	if err != nil {
		return nil, fmt.Errorf("starting validator client: %w", err)
	}

	// try to get a pubkey from private validate first time
	_, err = pvsc.GetPubKey(ctx)
	if err != nil {
		return nil, fmt.Errorf("can't get pubkey: %w", err)
	}

	const (
		timeout = 100 * time.Millisecond
		maxTime = 5 * time.Second
		retries = int(maxTime / timeout)
	)
	pvscWithRetries := privval.NewRetrySignerClient(pvsc, retries, timeout)

	return pvscWithRetries, nil
}

func createAndStartPrivValidatorGRPCClient(
	ctx context.Context,
	cfg *config.Config,
	chainID string,
	logger log.Logger,
) (types.PrivValidator, error) {
	pvsc, err := tmgrpc.DialRemoteSigner(
		ctx,
		cfg.PrivValidator,
		chainID,
		logger,
		cfg.Instrumentation.Prometheus,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to start private validator: %w", err)
	}

	// try to get a pubkey from private validate first time
	_, err = pvsc.GetPubKey(ctx)
	if err != nil {
		return nil, fmt.Errorf("can't get pubkey: %w", err)
	}

	return pvsc, nil
}

func createPrivval(ctx context.Context, logger log.Logger, conf *config.Config, genDoc *types.GenesisDoc, defaultPV *privval.FilePV) (types.PrivValidator, error) {
	if conf.PrivValidator.ListenAddr != "" {
		protocol, _ := tmnet.ProtocolAndAddress(conf.PrivValidator.ListenAddr)
		// FIXME: we should return un-started services and
		// then start them later.
		switch protocol {
		case "grpc":
			privValidator, err := createAndStartPrivValidatorGRPCClient(ctx, conf, genDoc.ChainID, logger)
			if err != nil {
				return nil, fmt.Errorf("error with private validator grpc client: %w", err)
			}
			return privValidator, nil
		default:
			privValidator, err := createAndStartPrivValidatorSocketClient(
				ctx,
				conf.PrivValidator.ListenAddr,
				genDoc.ChainID,
				logger,
			)
			if err != nil {
				return nil, fmt.Errorf("error with private validator socket client: %w", err)

			}
			return privValidator, nil
		}
	}

	return defaultPV, nil
}
