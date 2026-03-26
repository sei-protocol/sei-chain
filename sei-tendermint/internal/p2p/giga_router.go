package p2p

import (
	"context"
	"fmt"
	"time"

	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/consensus"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/giga"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/rpc"
	"github.com/sei-protocol/sei-chain/sei-tendermint/version"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/tcp"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

type GigaNodeAddr struct {
	Key NodePublicKey
	HostPort tcp.HostPort
}

func (a GigaNodeAddr) String() string {
	return fmt.Sprintf("%v@%v",a.Key,a.HostPort)
}

type GigaRouterConfig struct {
	Committee     *atypes.Committee
	Consensus     *consensus.Config
	App           abci.Application
	ValidatorAddrs map[atypes.PublicKey]GigaNodeAddr
}

type GigaRouter struct {
	cfg     *GigaRouterConfig
	key     NodeSecretKey
	data     *data.State
	consensus *consensus.State
	service *giga.Service
	poolIn  *giga.Pool[NodePublicKey, rpc.Server[giga.API]]
	poolOut *giga.Pool[NodePublicKey, rpc.Client[giga.API]]
}

func NewGigaRouter(cfg *GigaRouterConfig, key NodeSecretKey) (*GigaRouter,error) {
	// Automated pruning is disabled, because it is controlled by the application.
	dataState := data.NewState(&data.Config{Committee:cfg.Committee}, utils.None[data.BlockStore]())
	consensusState,err := consensus.NewState(cfg.Consensus, dataState)	
	if err!=nil {
		return nil,fmt.Errorf("consensus.NewState(): %w",err)
	}
	return &GigaRouter{
		cfg:     cfg,
		key:     key,
		data: dataState,
		consensus: consensusState,
		service: giga.NewService(consensusState),
		poolIn:  giga.NewPool[NodePublicKey, rpc.Server[giga.API]](),
		poolOut: giga.NewPool[NodePublicKey, rpc.Client[giga.API]](),
	},nil
}

func (r *GigaRouter) Run(ctx context.Context) error {
	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		// Spawn outbound connections dialing.
		for _,addr := range r.cfg.ValidatorAddrs {
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
		
		info,err := r.cfg.App.Info(ctx, &version.RequestInfo)
		if err!=nil { return fmt.Errorf("App.Info(): %w",err) }
		last := atypes.GlobalBlockNumber(info.LastBlockHeight)
		// TODO: in case next=1, we actually need to initialize the chain.
		
		// NOTE that with the current implementation losing prefix of appHashes on crash is fine:
		// if everyone votes on apphashes of a suffix of finalized blocks, then AppQC will be reached.
		appHash := info.LastBlockAppHash
		if err := r.data.PushAppHash(last,appHash); err!=nil {
			return fmt.Errorf("r.data.PushAppHash(): %w",err)
		}
		for n:=last+1;; n += 1 {
			b,err := r.data.Block(ctx,n)
			hash := b.Header().Hash()
			if err!=nil { return err }
			resp,err := r.cfg.App.FinalizeBlock(ctx, &abci.RequestFinalizeBlock {
				Txs: b.Payload().Txs(),
				DecidedLastCommit: abci.CommitInfo{}, 
				ByzantineValidators: nil, 
				// WARNING: this is a hash of the autobahn block header.
				// AFAICT app does not verify the hash wrt the header.
				Hash: hash[:],
				Header: (&types.Header{
					Version: version.Consensus{}, // TODO
					ChainID: "", // TODO 
					Height: int64(n),  
					Time: b.Payload().CreatedAt(),
					LastBlockID: types.BlockID{
						Hash: []byte{}, // TODO
						PartSetHeader: types.PartSetHeader{}, // TODO
					},

					// hashes of block data
					LastCommitHash: []byte{}, // TODO
					DataHash:       []byte{}, // TODO

					// hashes from the app output from the prev block
					ValidatorsHash: []byte{},      // validators for the current block
					NextValidatorsHash: []byte{}, // validators for the next block
					ConsensusHash: []byte{}, // consensus params for current block
					AppHash: appHash,            
					// root hash of all results from the txs from the previous block
					// see `deterministicResponseDeliverTx` to understand which parts of a tx is hashed into here
					LastResultsHash: []byte{}, // TODO: not sure how to mock it. 
					EvidenceHash: []byte{}, // TODO: hash of empty evidence
					ProposerAddress: types.Address{},  // TODO: let's just hardcode a single fake validator.
				}).ToProto(),
			})
			appHash = resp.AppHash		
			if err := r.data.PushAppHash(n, appHash); err!=nil {
				return fmt.Errorf("r.data.PushAppHash(%v): %w",n,err)
			}
			commitResp,err := r.cfg.App.Commit(ctx)
			if err!=nil {
				return fmt.Errorf("r.cfg.App.Commit(): %w",err)
			}
			// TODO: prune blocks
			r.data.PruneBefore(atypes.GlobalBlockNumber(commitResp.RetainHeight))
		}
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
	for _,addr := range r.cfg.ValidatorAddrs {
		if addr.Key==key {
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
