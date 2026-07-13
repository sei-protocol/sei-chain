package node

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	_ "net/http/pprof" // nolint: gosec // securely exposed on separate, optional port
	"os"
	"path/filepath"
	"strings"
	"time"

	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/producer"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/blocksync"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/consensus"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/eventbus"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/evidence"
	mempoolreactor "github.com/sei-protocol/sei-chain/sei-tendermint/internal/mempool/reactor"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/conn"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/pex"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"
	sm "github.com/sei-protocol/sei-chain/sei-tendermint/internal/state"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/state/indexer"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/statesync"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/store"
	tmnet "github.com/sei-protocol/sei-chain/sei-tendermint/libs/net"
	tmstrings "github.com/sei-protocol/sei-chain/sei-tendermint/libs/strings"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/privval"
	tmgrpc "github.com/sei-protocol/sei-chain/sei-tendermint/privval/grpc"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/version"
	dbm "github.com/tendermint/tm-db"
	"golang.org/x/time/rate"
)

// ErrGenesisMaxGasInvalid is returned by buildGigaConfig when the genesis
// consensus_params.block.max_gas is missing or non-positive. Producer.MaxGasPerBlock
// must be a positive integer derived from this value; tests assert via errors.Is.
var ErrGenesisMaxGasInvalid = errors.New("genesis consensus_params.block.max_gas must be > 0")

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

func logNodeStartupInfo(state sm.State, pubKey utils.Option[crypto.PubKey], mode string) {
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
		k := pubKey.OrPanic("validator node is missing key")
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
	if state.Validators.Size() != 1 {
		return false
	}
	addr, _, ok := state.Validators.GetByIndex(0)
	return ok && bytes.Equal(k.Address(), addr)
}

func createEvidenceReactor(
	cfg *config.Config,
	dbProvider config.DBProvider,
	store sm.Store,
	blockStore *store.BlockStore,
	router *p2p.Router,
	eventBus *eventbus.EventBus,
) (*evidence.Reactor, *evidence.Pool, closer, error) {
	evidenceDB, err := dbProvider(&config.DBContext{ID: "evidence", Config: cfg})
	if err != nil {
		return nil, nil, func() error { return nil }, fmt.Errorf("unable to initialize evidence db: %w", err)
	}

	evidencePool := evidence.NewPool(evidenceDB, store, blockStore, eventBus)
	evidenceReactor, err := evidence.NewReactor(router, evidencePool)
	if err != nil {
		return nil, nil, evidenceDB.Close, fmt.Errorf("evidence.NewReactor(): %w", err)
	}
	return evidenceReactor, evidencePool, evidenceDB.Close, nil
}

func loadAutobahnFileConfig(path string) (*config.AutobahnFileConfig, error) {
	data, err := os.ReadFile(path) //nolint:gosec // G304: path is from operator-controlled config
	if err != nil {
		return nil, err
	}
	var fc config.AutobahnFileConfig
	if err := json.Unmarshal(data, &fc); err != nil {
		return nil, err
	}
	if err := fc.Validate(); err != nil {
		return nil, err
	}
	return &fc, nil
}

// loadAutobahnCommittee reads the autobahn config file and builds the
// committee map (validator pubkey → GigaNodeAddr) used by both router
// modes. Rejects duplicate validator/node keys.
func loadAutobahnCommittee(autobahnConfigFile string) (*config.AutobahnFileConfig, map[atypes.PublicKey]p2p.GigaNodeAddr, error) {
	if autobahnConfigFile == "" {
		return nil, nil, errors.New("autobahn config file path must not be empty")
	}
	fc, err := loadAutobahnFileConfig(autobahnConfigFile)
	if err != nil {
		return nil, nil, fmt.Errorf("loading autobahn config from %q: %w", autobahnConfigFile, err)
	}
	validatorAddrs := map[atypes.PublicKey]p2p.GigaNodeAddr{}
	seenNodeKeys := map[p2p.NodePublicKey]bool{}
	for _, entry := range fc.Validators {
		if _, exists := validatorAddrs[entry.ValidatorKey]; exists {
			return nil, nil, fmt.Errorf("duplicate validator key in autobahn validators: %s", entry.ValidatorKey)
		}
		if seenNodeKeys[entry.NodeKey] {
			return nil, nil, fmt.Errorf("duplicate node key in autobahn validators: %s", entry.NodeKey)
		}
		seenNodeKeys[entry.NodeKey] = true
		validatorAddrs[entry.ValidatorKey] = p2p.GigaNodeAddr{
			Key:      entry.NodeKey,
			HostPort: entry.Address,
			EVMRPC:   entry.EVMRPC.URL,
		}
	}
	return fc, validatorAddrs, nil
}

// buildValidatorGigaConfig assembles a GigaValidatorConfig. Errors if
// self isn't in the committee or the node key doesn't match.
func buildValidatorGigaConfig(
	autobahnConfigFile string,
	nodeKey types.NodeKey,
	validatorKey atypes.SecretKey,
	app *proxy.Proxy,
	genDoc *types.GenesisDoc,
) (*p2p.GigaValidatorConfig, error) {
	fc, validatorAddrs, err := loadAutobahnCommittee(autobahnConfigFile)
	if err != nil {
		return nil, err
	}

	// Verify self is in the validator set.
	selfAddr, ok := validatorAddrs[validatorKey.Public()]
	if !ok {
		return nil, fmt.Errorf("node's own validator key not found in autobahn validators; the node must be a committee member")
	}
	selfNodePub := p2p.NodeSecretKey(nodeKey).Public()
	if selfAddr.Key != selfNodePub {
		return nil, fmt.Errorf("node key mismatch for own validator entry: config has %s, but node key is %s", selfAddr.Key, selfNodePub)
	}
	if _, err := genesisMaxGas(genDoc); err != nil {
		return nil, err
	}
	return &p2p.GigaValidatorConfig{
		GigaRouterCommonConfig: p2p.GigaRouterCommonConfig{
			DialInterval:            time.Duration(fc.DialInterval),
			ValidatorAddrs:          validatorAddrs,
			PersistentStateDir:      fc.PersistentStateDir,
			App:                     app,
			GenDoc:                  genDoc,
			MaxInboundFullnodePeers: resolveMaxInboundFullnodePeers(fc.MaxInboundFullnodePeers),
		},
		ValidatorKey: validatorKey,
		ViewTimeout: func(atypes.View) time.Duration {
			return time.Duration(fc.ViewTimeout)
		},
		Producer: &producer.Config{
			MaxGasWantedPerBlock:    genDoc.ConsensusParams.Block.MaxGasWantedUint64(),
			MaxGasEstimatedPerBlock: genDoc.ConsensusParams.Block.MaxGasUint64(),
			MaxTxsPerBlock:          fc.MaxTxsPerBlock,
			MaxTxsPerSecond:         fc.MaxTxsPerSecond,
			AllowEmptyBlocks:        fc.AllowEmptyBlocks,
			BlockInterval:           time.Duration(fc.BlockInterval),
		},
	}, nil
}

// buildGigaRouter picks validator-vs-fullnode by cfg.Mode:
// "validator" runs the validator path, any other mode runs as a fullnode.
// Mode is the operator's explicit role declaration, kept separate from
// committee membership so a newly-joined committee member can finish
// catch-up as a fullnode before the operator flips to mode = "validator".
// A warning is logged if mode and committee membership disagree so an
// operator misconfiguration is visible at startup.
func buildGigaRouter(
	cfg *config.Config,
	nodeKey types.NodeKey,
	validatorKey utils.Option[atypes.SecretKey],
	app *proxy.Proxy,
	genDoc *types.GenesisDoc,
) (p2p.GigaRouter, error) {
	_, validatorAddrs, err := loadAutobahnCommittee(cfg.AutobahnConfigFile)
	if err != nil {
		return nil, err
	}
	if valKey, ok := validatorKey.Get(); ok {
		_, inCommittee := validatorAddrs[valKey.Public()]
		switch {
		case cfg.Mode == config.ModeValidator && !inCommittee:
			logger.Warn("Autobahn: mode is \"validator\" but local validator key is not in the committee", "valKey", valKey.Public())
		case cfg.Mode != config.ModeValidator && inCommittee:
			logger.Warn("Autobahn: local validator key is in the committee but mode is not \"validator\"; starting as fullnode", "mode", cfg.Mode)
		}
	}
	if cfg.Mode == config.ModeValidator {
		valKey, ok := validatorKey.Get()
		if !ok {
			return nil, fmt.Errorf("autobahn: mode = %q requires a local validator key", cfg.Mode)
		}
		// Remote signers aren't supported on the validator path —
		// autobahn signs in-process. Fullnodes don't sign and aren't
		// penalised for having priv-validator.laddr set.
		if cfg.PrivValidator.ListenAddr != "" {
			return nil, fmt.Errorf("autobahn does not support remote validator signers (priv-validator.laddr is set)")
		}
		valCfg, err := buildValidatorGigaConfig(cfg.AutobahnConfigFile, nodeKey, valKey, app, genDoc)
		if err != nil {
			return nil, fmt.Errorf("buildValidatorGigaConfig: %w", err)
		}
		if err := preparePersistentStateDir(cfg.RootDir, &valCfg.GigaRouterCommonConfig); err != nil {
			return nil, err
		}
		// The GigaRouter builds and owns the equivocation guard itself; just pass the operator's
		// enable/disable decision through as plain config.
		valCfg.HashVaultDisabledUnsafe = cfg.HashVaultDisabledUnsafe
		logger.Info("Autobahn: starting as validator", "validators", len(valCfg.ValidatorAddrs))
		return p2p.NewGigaValidatorRouter(valCfg, p2p.NodeSecretKey(nodeKey))
	}
	fnCfg, err := buildFullnodeGigaConfig(cfg.AutobahnConfigFile, app, genDoc)
	if err != nil {
		return nil, fmt.Errorf("buildFullnodeGigaConfig: %w", err)
	}
	if err := preparePersistentStateDir(cfg.RootDir, fnCfg); err != nil {
		return nil, err
	}
	// The GigaRouter builds and owns the equivocation guard itself; just pass the operator's
	// enable/disable decision through as plain config.
	fnCfg.HashVaultDisabledUnsafe = cfg.HashVaultDisabledUnsafe
	logger.Info("Autobahn: starting as fullnode", "mode", cfg.Mode, "validators", len(validatorAddrs))
	return p2p.NewGigaFullnodeRouter(fnCfg, p2p.NodeSecretKey(nodeKey))
}

// preparePersistentStateDir resolves a relative PersistentStateDir against
// the node's --home dir (mirrors config.go's rootify) and creates it if absent.
func preparePersistentStateDir(rootDir string, c *p2p.GigaRouterCommonConfig) error {
	dir, ok := c.PersistentStateDir.Get()
	if !ok {
		return nil
	}
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(rootDir, dir)
		c.PersistentStateDir = utils.Some(dir)
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("creating persistent state dir %q: %w", dir, err)
	}
	return nil
}

// resolveMaxInboundFullnodePeers: None ⇒ default, Some(0) ⇒ reject all,
// Some(n) ⇒ n. The default lives in the config package so giga_router
// doesn't carry an operator-facing knob.
func resolveMaxInboundFullnodePeers(o utils.Option[uint64]) int {
	if v, ok := o.Get(); ok {
		return int(v) //nolint:gosec // bounded by maxInboundFullnodePeers in giga_router_common
	}
	return config.DefaultMaxInboundFullnodePeers
}

// genesisMaxGas returns consensus_params.block.max_gas as uint64. Errors
// when missing or <= 0 (CometBFT uses -1 for "unlimited" which neither
// path supports).
func genesisMaxGas(genDoc *types.GenesisDoc) (uint64, error) {
	if genDoc.ConsensusParams == nil || genDoc.ConsensusParams.Block.MaxGas <= 0 {
		return 0, fmt.Errorf("%w (got %v)", ErrGenesisMaxGasInvalid, genDoc.ConsensusParams)
	}
	return uint64(genDoc.ConsensusParams.Block.MaxGas), nil //nolint:gosec // validated > 0 above
}

// buildFullnodeGigaConfig assembles the common config for a fullnode
// GigaRouter. No consensus/producer/mempool — fullnodes pull blocks rather
// than producing them and forward every EVM tx to the shard owner.
func buildFullnodeGigaConfig(
	autobahnConfigFile string,
	app *proxy.Proxy,
	genDoc *types.GenesisDoc,
) (*p2p.GigaRouterCommonConfig, error) {
	fc, validatorAddrs, err := loadAutobahnCommittee(autobahnConfigFile)
	if err != nil {
		return nil, err
	}
	// MaxGasEstimatedPerBlock reads through to genDoc; validate the source
	// so a malformed genesis can't silently expose 0 to clients.
	if _, err := genesisMaxGas(genDoc); err != nil {
		return nil, err
	}
	return &p2p.GigaRouterCommonConfig{
		DialInterval:            time.Duration(fc.DialInterval),
		ValidatorAddrs:          validatorAddrs,
		PersistentStateDir:      fc.PersistentStateDir,
		App:                     app,
		GenDoc:                  genDoc,
		MaxInboundFullnodePeers: resolveMaxInboundFullnodePeers(fc.MaxInboundFullnodePeers),
	}, nil
}

func createRouter(
	nodeInfoProducer func() *types.NodeInfo,
	nodeKey types.NodeKey,
	validatorKey utils.Option[atypes.SecretKey],
	cfg *config.Config,
	app utils.Option[*proxy.Proxy],
	genDoc *types.GenesisDoc,
	dbProvider config.DBProvider,
) (*p2p.Router, closer, error) {
	closer := func() error { return nil }
	ep, err := p2p.ResolveEndpoint(nodeKey.ID().AddressString(cfg.P2P.ListenAddress))
	if err != nil {
		return nil, closer, err
	}
	var privatePeerIDs []types.NodeID
	for _, id := range tmstrings.SplitAndTrimEmpty(cfg.P2P.PrivatePeerIDs, ",", " ") {
		privatePeerIDs = append(privatePeerIDs, types.NodeID(id))
	}

	// MaxConnections defaults to 64
	maxConns := 64
	if cfg.P2P.MaxConnections > 0 {
		maxConns = utils.Clamp[int](cfg.P2P.MaxConnections)
	}
	// MaxOutbound defaults to 20, unless MaxConnections<40,
	// then it defaults to half of the maxConnections.
	maxOutbound := min(20, (maxConns+1)/2)
	if m := cfg.P2P.MaxOutboundConnections; m != nil {
		maxOutbound = min(maxConns, utils.Clamp[int](*m))
	}
	// MaxInbound is simply MaxConnections - MaxOutbound,
	// because now we have totally separate inbound and outbound connection pools.
	// TODO(gprusak): eventually we should migrate configs to specify
	// MaxInbound and MaxOutbound explicitly, rather than doing the computation above.
	maxInbound := maxConns - maxOutbound
	connection := conn.DefaultMConnConfig()
	connection.FlushThrottle = cfg.P2P.FlushThrottleTimeout
	connection.SendRate = cfg.P2P.SendRate
	connection.RecvRate = cfg.P2P.RecvRate
	connection.MaxPacketMsgPayloadSize = cfg.P2P.MaxPacketMsgPayloadSize
	options := &p2p.RouterOptions{
		Endpoint:                      ep,
		MaxIncomingConnectionAttempts: utils.Some(cfg.P2P.MaxIncomingConnectionAttempts),
		MaxDialRate:                   utils.Some(rate.Every(cfg.P2P.DialInterval)),
		HandshakeTimeout:              utils.Some(cfg.P2P.HandshakeTimeout),
		DialTimeout:                   utils.Some(cfg.P2P.DialTimeout),
		PexOnHandshake:                cfg.P2P.PexReactor,
		PrivatePeers:                  privatePeerIDs,
		MaxInbound:                    utils.Some(maxInbound),
		MaxOutbound:                   utils.Some(maxOutbound),
		MaxConcurrentAccepts:          utils.Some(maxInbound),
		Connection:                    connection,
	}
	if addr := cfg.P2P.ExternalAddress; addr != "" {
		nodeAddr, err := p2p.ParseNodeAddress(nodeKey.ID().AddressString(addr))
		if err != nil {
			return nil, closer, fmt.Errorf("couldn't parse ExternalAddress %q: %w", cfg.P2P.ExternalAddress, err)
		}
		options.SelfAddress = utils.Some(nodeAddr)
	}

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
	// Wire up Autobahn if enabled. Role dispatch (validator vs fullnode)
	// happens inside buildGigaRouter based on cfg.Mode.
	if cfg.AutobahnConfigFile != "" {
		logger.Info("Autobahn config enabled", "config_file", cfg.AutobahnConfigFile, "mode", cfg.Mode)
		proxyApp, ok := app.Get()
		if !ok {
			return nil, closer, fmt.Errorf("autobahn requires app")
		}
		giga, err := buildGigaRouter(cfg, nodeKey, validatorKey, proxyApp, genDoc)
		if err != nil {
			return nil, closer, err
		}
		options.Giga = utils.Some(giga)
	}

	peerDB, err := dbProvider(&config.DBContext{ID: "peerstore", Config: cfg})
	if err != nil {
		return nil, closer, fmt.Errorf("unable to initialize peer store: %w", err)
	}
	closer = peerDB.Close
	router, err := p2p.NewRouter(
		p2p.NodeSecretKey(nodeKey),
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
		NodeID:  nodeKey.ID(),
		Network: genDoc.ChainID,
		Version: version.TMVersion,
		Channels: []byte{
			byte(blocksync.BlockSyncChannel),
			byte(consensus.StateChannel),
			byte(consensus.DataChannel),
			byte(consensus.VoteChannel),
			byte(consensus.VoteSetBitsChannel),
			byte(mempoolreactor.MempoolChannel),
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
		NodeID:  nodeKey.ID(),
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
) (types.PrivValidator, error) {

	pve, err := privval.NewSignerListener(listenAddr)
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
) (types.PrivValidator, error) {
	pvsc, err := tmgrpc.DialRemoteSigner(
		ctx,
		cfg.PrivValidator,
		chainID,
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

func createPrivval(ctx context.Context, conf *config.Config, genDoc *types.GenesisDoc, defaultPV *privval.FilePV) (types.PrivValidator, error) {
	if conf.PrivValidator.ListenAddr != "" {
		protocol, _ := tmnet.ProtocolAndAddress(conf.PrivValidator.ListenAddr)
		// FIXME: we should return un-started services and
		// then start them later.
		switch protocol {
		case "grpc":
			privValidator, err := createAndStartPrivValidatorGRPCClient(ctx, conf, genDoc.ChainID)
			if err != nil {
				return nil, fmt.Errorf("error with private validator grpc client: %w", err)
			}
			return privValidator, nil
		default:
			privValidator, err := createAndStartPrivValidatorSocketClient(
				ctx,
				conf.PrivValidator.ListenAddr,
				genDoc.ChainID,
			)
			if err != nil {
				return nil, fmt.Errorf("error with private validator socket client: %w", err)

			}
			return privValidator, nil
		}
	}

	return defaultPV, nil
}
