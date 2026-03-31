package p2p

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"time"

	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/consensus"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/producer"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/giga"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/rpc"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/tcp"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/version"
)

type GigaNodeAddr struct {
	Key      NodePublicKey
	HostPort tcp.HostPort
}

func (a GigaNodeAddr) String() string {
	return fmt.Sprintf("%v@%v", a.Key, a.HostPort)
}

type GigaRouterConfig struct {
	ValidatorAddrs map[atypes.PublicKey]GigaNodeAddr
	Consensus      *consensus.Config
	Producer       *producer.Config
	App            abci.Application
	GenDoc         *types.GenesisDoc
}

type GigaRouter struct {
	cfg       *GigaRouterConfig
	committee *atypes.Committee
	key       NodeSecretKey
	data      *data.State
	producer  *producer.State
	consensus *consensus.State
	service   *giga.Service
	poolIn    *giga.Pool[NodePublicKey, rpc.Server[giga.API]]
	poolOut   *giga.Pool[NodePublicKey, rpc.Client[giga.API]]
}

func (r *GigaRouter) PushToMempool(ctx context.Context, tx *pb.Transaction) error {
	return r.producer.PushToMempool(ctx, tx)
}

func NewGigaRouter(cfg *GigaRouterConfig, key NodeSecretKey) (*GigaRouter, error) {
	if cfg.GenDoc.InitialHeight < 1 {
		return nil, fmt.Errorf("GenDoc.InitialHeight = %v, want >=1", cfg.GenDoc.InitialHeight)
	}
	committee, err := atypes.NewRoundRobinElection(
		slices.Collect(maps.Keys(cfg.ValidatorAddrs)),
		atypes.GlobalBlockNumber(cfg.GenDoc.InitialHeight),
	)
	if err != nil {
		return nil, fmt.Errorf("atypes.NewRoundRobinElection(): %w", err)
	}
	// Automated pruning is disabled, because it is controlled by the application.
	dataState := data.NewState(&data.Config{Committee: committee}, utils.None[data.BlockStore]())
	consensusState, err := consensus.NewState(cfg.Consensus, dataState)
	if err != nil {
		return nil, fmt.Errorf("consensus.NewState(): %w", err)
	}
	producerState := producer.NewState(cfg.Producer, consensusState)
	return &GigaRouter{
		cfg:       cfg,
		key:       key,
		data:      dataState,
		consensus: consensusState,
		producer:  producerState,
		service:   giga.NewService(consensusState),
		poolIn:    giga.NewPool[NodePublicKey, rpc.Server[giga.API]](),
		poolOut:   giga.NewPool[NodePublicKey, rpc.Client[giga.API]](),
	}, nil
}

func (r *GigaRouter) runExecute(ctx context.Context) error {
	info, err := r.cfg.App.Info(ctx, &version.RequestInfo)
	if err != nil {
		return fmt.Errorf("App.Info(): %w", err)
	}
	last := atypes.GlobalBlockNumber(info.LastBlockHeight)
	next := last + 1
	appHash := info.LastBlockAppHash
	if last == 0 {
		resp, err := r.cfg.App.InitChain(ctx, r.cfg.GenDoc.ToRequestInitChain())
		if err != nil {
			return fmt.Errorf("App.InitChain(): %w", err)
		}
		next = atypes.GlobalBlockNumber(r.cfg.GenDoc.InitialHeight)
		appHash = resp.AppHash
	} else {
		// NOTE that with the current implementation losing prefix of appHashes on crash is fine:
		// if everyone votes on apphashes of a suffix of finalized blocks, then AppQC will be reached.
		if err := r.data.PushAppHash(last, appHash); err != nil {
			return fmt.Errorf("r.data.PushAppHash(): %w", err)
		}
	}

	for n := next; ; n += 1 {
		b, err := r.data.Block(ctx, n)
		if err != nil {
			return err
		}

		hash := b.Header().Hash()
		var proposerAddress types.Address
		if vals := r.cfg.App.GetValidators(); len(vals) > 0 {
			// Deterministically select a proposer from the app's validator committee.
			// We need it so that app does not emit error logs.
			proposer := slices.MinFunc(vals, func(a,b abci.ValidatorUpdate) int { return a.PubKey.Compare(b.PubKey) })
			key, err := crypto.PubKeyFromProto(proposer.PubKey)
			if err != nil {
				return fmt.Errorf("crypto.PubKeyFromProto(): %w", err)
			}
			proposerAddress = key.Address()
		}
		resp, err := r.cfg.App.FinalizeBlock(ctx, &abci.RequestFinalizeBlock{
			Txs: b.Payload().Txs(),
			// Empty DecidedLastCommit is does not indicate missing votes.

			// WARNING: this is a hash of the autobahn block header.
			// It is used to identify block processed optimistically
			// and is fed as block hash to EVM contracts.
			Hash: hash[:],
			Header: (&types.Header{
				ChainID: r.cfg.GenDoc.ChainID,
				Height:  int64(n),
				Time:    b.Payload().CreatedAt(),
				// WARNING: the reward distribution has corner cases where it forgets the proposer,
				// because reward is distributed with a delay. This is not our problem here though.
				ProposerAddress: proposerAddress,
			}).ToProto(),
		})
		appHash = resp.AppHash
		// TODO: we need the block to be persisted before we vote for apphash.
		if err := r.data.PushAppHash(n, appHash); err != nil {
			return fmt.Errorf("r.data.PushAppHash(%v): %w", n, err)
		}
		commitResp, err := r.cfg.App.Commit(ctx)
		if err != nil {
			return fmt.Errorf("r.cfg.App.Commit(): %w", err)
		}
		r.data.PruneBefore(atypes.GlobalBlockNumber(commitResp.RetainHeight))
	}
}

func (r *GigaRouter) Run(ctx context.Context) error {
	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		// Spawn outbound connections dialing.
		for _, addr := range r.cfg.ValidatorAddrs {
			s.Spawn(func() error {
				for {
					err := r.dialAndRunConn(ctx, addr.Key, addr.HostPort)
					logger.Info("giga connection failed", "addr", addr, "err", err)
					if err := utils.Sleep(ctx, 10*time.Second); err != nil {
						return err
					}
				}
			})
		}
		s.SpawnNamed("data", func() error { return r.data.Run(ctx) })
		s.SpawnNamed("consensus", func() error { return r.consensus.Run(ctx) })
		s.SpawnNamed("producer", func() error { return r.producer.Run(ctx) })
		s.SpawnNamed("execute", func() error { return r.runExecute(ctx) })
		return nil
	})
}

func (r *GigaRouter) dialAndRunConn(ctx context.Context, key NodePublicKey, hp tcp.HostPort) error {
	addrs, err := hp.Resolve(ctx)
	if err != nil {
		return fmt.Errorf("%v.Resolve(): %w", hp, err)
	}
	if len(addrs) == 0 {
		return fmt.Errorf("%v.Resolve() = []", hp)
	}
	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		tcpConn, err := tcp.Dial(ctx, addrs[0])
		if err != nil {
			return fmt.Errorf("tcp.Dial(%v): %w", addrs[0], err)
		}
		s.SpawnBg(func() error { return tcpConn.Run(ctx) })
		// TODO: handshake needs a timeout.
		hConn, err := handshake(ctx, tcpConn, r.key, handshakeSpec{SeiGigaConnection: true})
		if err != nil {
			return fmt.Errorf("handshake(): %w", err)
		}
		if !hConn.msg.SeiGigaConnection {
			return fmt.Errorf("not a sei giga connection")
		}
		if got := hConn.msg.NodeAuth.Key(); got != key {
			return fmt.Errorf("peer key = %v, want %v", got, key)
		}
		client := rpc.NewClient[giga.API]()
		return r.poolOut.InsertAndRun(ctx, key, client, func(ctx context.Context) error {
			return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
				s.Spawn(func() error { return client.Run(ctx, hConn.conn) })
				return r.service.RunClient(ctx, client)
			})
		})
	})
}

func (r *GigaRouter) RunInboundConn(ctx context.Context, hConn *handshakedConn) error {
	if !hConn.msg.SeiGigaConnection {
		return fmt.Errorf("not a SeiGiga connection")
	}
	// Filter unwanded connections.
	key := hConn.msg.NodeAuth.Key()
	ok := false
	for _, addr := range r.cfg.ValidatorAddrs {
		if addr.Key == key {
			ok = true
			break
		}
	}
	if !ok {
		return fmt.Errorf("peer not whitelisted")
	}
	server := rpc.NewServer[giga.API]()
	return r.poolIn.InsertAndRun(ctx, key, server, func(ctx context.Context) error {
		return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
			s.Spawn(func() error { return server.Run(ctx, hConn.conn) })
			return r.service.RunServer(ctx, server)
		})
	})
}
