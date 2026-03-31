package p2p

import (
	"context"
	"crypto/sha256"
	"fmt"
	"encoding/json"
	"testing"
	"slices"
	"time"
	"net/netip"

	dbm "github.com/tendermint/tm-db"
	"golang.org/x/time/rate"

	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/consensus"
	apb "github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/producer"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/conn"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/tcp"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto/ed25519"
	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto"
)

type shaHash = [sha256.Size]byte

type testAppState struct {
	init    utils.Option[*abci.RequestInitChain]
	validators []abci.ValidatorUpdate
	blocks  []*abci.RequestFinalizeBlock
	txs map[shaHash] bool
	appHash shaHash
}

func testAppStateJSON(rng utils.Rng) json.RawMessage {
	return utils.OrPanic1(json.Marshal(&abci.ValidatorUpdate{
		PubKey: crypto.PubKeyToProto(ed25519.TestSecretKey(utils.GenBytes(rng,32)).Public()),
		Power: rng.Int63(), 
	}))
}

type testApp struct {
	abci.Application
	state utils.Watch[*testAppState]
}

func newTestApp() *testApp {
	return &testApp{state: utils.NewWatch(&testAppState{
		txs: map[shaHash]bool{},
	})}
}

func (a *testApp) GetValidators() []abci.ValidatorUpdate {
	for state := range a.state.Lock() {
		return slices.Clone(state.validators)
	}
	panic("unreachable")
}

func (a *testApp) Info(_ context.Context, _ *abci.RequestInfo) (*abci.ResponseInfo, error) {
	for state := range a.state.Lock() {
		init,ok := state.init.Get()
		if !ok {
			return &abci.ResponseInfo{},nil
		}
		return &abci.ResponseInfo{
			LastBlockHeight:  init.InitialHeight + int64(len(state.blocks)) -1,
			LastBlockAppHash: slices.Clone(state.appHash[:]),
		}, nil
	}
	panic("unreachable")
}

func (a *testApp) InitChain(_ context.Context, req *abci.RequestInitChain) (*abci.ResponseInitChain, error) {
	for state,ctrl := range a.state.Lock() {
		if state.init.IsPresent() { return nil,fmt.Errorf("chain already initialized") }
		if req.InitialHeight<1 { return nil,fmt.Errorf("InitialHeight = %v, want >=1",req.InitialHeight) }
		var val abci.ValidatorUpdate
		if err := json.Unmarshal(req.AppStateBytes,&val); err!=nil {
			return nil,fmt.Errorf("proto.Unmarshal(): %w",err)
		}
		state.init = utils.Some(req)
		state.appHash = sha256.Sum256(req.AppStateBytes)
		state.validators = utils.Slice(val)
		ctrl.Updated()
		return &abci.ResponseInitChain{
			AppHash:    slices.Clone(state.appHash[:]),
			Validators: slices.Clone(state.validators),
		}, nil
	}
	panic("unreachable")	
}

func (a *testApp) FinalizeBlock(_ context.Context, req *abci.RequestFinalizeBlock) (*abci.ResponseFinalizeBlock, error) {
	for state, ctrl := range a.state.Lock() {
		init,ok := state.init.Get()
		if !ok { return nil,fmt.Errorf("app not initialized") }
		state.blocks = append(state.blocks, req)
		state.appHash = sha256.Sum256(slices.Concat(req.Hash,state.appHash[:])) 
		for _,tx := range req.Txs {
			state.txs[sha256.Sum256(tx)] = true
		}
		logger.Info("FinalizeBlock","n",req.Header.Height-init.InitialHeight)
		ctrl.Updated()
		return &abci.ResponseFinalizeBlock{
			AppHash:   slices.Clone(state.appHash[:]),
			TxResults: slices.Repeat([]*abci.ExecTxResult{{Code: abci.CodeTypeOK}},len(req.Txs)),
		},nil
	}
	panic("unreachable")
}

func (a *testApp) Commit(context.Context) (*abci.ResponseCommit, error) {
	return &abci.ResponseCommit{
		// Don't prune anything.
		RetainHeight: 0,
	}, nil
}

func (a *testApp) WaitForTx(ctx context.Context, tx []byte) error {
	h := sha256.Sum256(tx)
	for state, ctrl := range a.state.Lock() {
		return ctrl.WaitUntil(ctx, func() bool {
			_,ok := state.txs[h]
			return ok
		})
	}
	panic("unreachable")
}

func (a *testApp) Snapshot() testAppState {
	for state := range a.state.Lock() {
		return *state
	}
	panic("unreachable")
}

type testNodeCfg struct {
	validatorKey atypes.SecretKey
	nodeKey NodeSecretKey
	addr netip.AddrPort
}

func (c *testNodeCfg) GigaNodeAddr() GigaNodeAddr {
	return GigaNodeAddr {
		Key: c.nodeKey.Public(),
		HostPort: tcp.HostPort{Hostname: c.addr.Addr().String(), Port: c.addr.Port() },
	}
}

func TestGigaRouter_FinalizeBlocks(t *testing.T) {
	const maxTxsPerBlock = 20
	const blocksPerLane = 5

	ctx := t.Context()
	rng := utils.TestRng()
	_,keys := atypes.GenCommittee(rng,4)
	var cfgs []*testNodeCfg
	for _,key := range keys {
		cfgs = append(cfgs, &testNodeCfg {
			validatorKey: key,
			nodeKey: makeKey(rng),
			addr: tcp.TestReserveAddr(),
		})
	}
	addrs := map[atypes.PublicKey]GigaNodeAddr{}
	for _,cfg := range cfgs {
		addrs[cfg.validatorKey.Public()] = cfg.GigaNodeAddr()
	}
	genDoc := &types.GenesisDoc{
		ChainID:       "giga-router-test",
		InitialHeight: rng.Int63n(100000)+1,
		AppState:      testAppStateJSON(rng),
	}
	require.NoError(t, genDoc.ValidateAndComplete())

	err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		var apps []*testApp
		var allTxs [][]byte
		for i, cfg := range cfgs {
			nodeInfo := makeInfo(cfg.nodeKey)
			nodeInfo.ListenAddr = cfg.addr.String() 
			nodeInfo.Network = genDoc.ChainID
			e := Endpoint{AddrPort:cfg.addr}
			app := newTestApp()
			router, err := NewRouter(
				NopMetrics(),
				cfg.nodeKey,
				func() *types.NodeInfo { return &nodeInfo },
				dbm.NewMemDB(),
				&RouterOptions{
					SelfAddress:              utils.Some(e.NodeAddress(cfg.nodeKey.Public().NodeID())),
					Endpoint:                 e,
					Connection:               conn.DefaultMConnConfig(),
					IncomingConnectionWindow: utils.Some(time.Duration(0)),
					MaxAcceptRate:            utils.Some(rate.Inf),
					MaxDialRate:              utils.Some(rate.Limit(30)),
					Giga: utils.Some(&GigaRouterConfig{
						ValidatorAddrs: addrs,
						Consensus: &consensus.Config{
							Key:                cfg.validatorKey,
							ViewTimeout:        func(atypes.View) time.Duration { return time.Hour },
							PersistentStateDir: utils.None[string](),
						},
						Producer: &producer.Config{
							MaxGasPerBlock:   21_000,
							MaxTxsPerBlock:   maxTxsPerBlock,
							MaxTxsPerSecond:  utils.None[uint64](),
							MempoolSize:      100,
							BlockInterval:    time.Second,
							AllowEmptyBlocks: false,
						},
						App: app,
						GenDoc: genDoc,
					}),
				},
			)
			if err!=nil {
				return fmt.Errorf("NewRouter(): %w",err)
			}
			// TODO: startup might be slow here, because of dial backoff.
			s.SpawnBgNamed(fmt.Sprintf("router[%v]",i), func() error { return router.Run(ctx) })
			apps = append(apps,app)
			var txs [][]byte
			for range maxTxsPerBlock*blocksPerLane {
				tx := utils.GenBytes(rng, 1024)
				txs = append(txs,tx)
				allTxs = append(allTxs,tx)
			}
			s.SpawnNamed(fmt.Sprintf("producer[%v]",i),func() error {
				giga, ok := router.giga.Get()
				if !ok { panic("giga router not set up") }
				for _,payload := range txs {
					tx := &apb.Transaction{
						Payload: payload, 
						GasUsed: 21_000,
					}
					if err := giga.PushToMempool(ctx, tx); err != nil {
						return fmt.Errorf("PushToMempool(): %w", err)
					}
				}
				return nil
			})
		}
		// Each node should finalize all txs locally.
		for _,app := range apps {
			for _,tx := range allTxs {
				if err := app.WaitForTx(ctx,tx); err!=nil {
					return fmt.Errorf("WaitForTx(): %w",err)
				}
			}
		}
		// Nodes should agree on the final state.
		want := apps[0].Snapshot()
		for _, app := range apps {
			if err:=utils.TestDiff(want, app.Snapshot()); err!=nil {
				return fmt.Errorf("state mismatch: %w",err)
			}
		}
		return nil
	})
	require.NoError(t, err)
	
}
