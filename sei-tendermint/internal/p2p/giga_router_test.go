package p2p

import (
	"context"
	"crypto/sha256"
	"fmt"
	"encoding/json"
	"testing"
	"slices"
	"time"

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

type testAppState struct {
	init    utils.Option[*abci.RequestInitChain]
	validators []abci.ValidatorUpdate
	blocks  []*abci.RequestFinalizeBlock
	appHash [sha256.Size]byte
}

func (s *testAppState) NextHeight() (int64,bool) {
	if n:=len(s.blocks); n>0 {
		return s.blocks[n-1].Header.Height+1,true
	}
	if init,ok := s.init.Get(); ok {
		return init.InitialHeight,true
	}
	return 0,false
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
	return &testApp{state: utils.NewWatch(&testAppState{})}
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
		if !ok { return nil,fmt.Errorf("chain not initialized") }
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
		state.blocks = append(state.blocks, req)
		state.appHash = sha256.Sum256(slices.Concat(req.Hash,state.appHash[:])) 
		ctrl.Updated()
		return &abci.ResponseFinalizeBlock{
			AppHash:   slices.Clone(state.appHash[:]),
			TxResults: slices.Repeat([]*abci.ExecTxResult{{Code: abci.CodeTypeOK}},len(req.Txs)),
		},nil
	}
	panic("unreachable")
}

func (a *testApp) Commit(context.Context) (*abci.ResponseCommit, error) {
	return &abci.ResponseCommit{}, nil
}

func (a *testApp) WaitForHeight(ctx context.Context, height int64) error {
	for state, ctrl := range a.state.Lock() {
		return ctrl.WaitUntil(ctx, func() bool {
			n,ok := state.NextHeight()
			return ok && n>height
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

type gigaRouterTestNode struct {
	nodeKey      NodeSecretKey
	validatorKey atypes.SecretKey
	endpoint     Endpoint
	router      *Router
	app         *testApp
}

func TestGigaRouter_FinalizesSame20Blocks(t *testing.T) {
	const (
		gigaRouterTestBlocks         = 20
		gigaRouterTestBlockInterval  = 10 * time.Millisecond
	)

	ctx := t.Context()
	rng := utils.TestRng()
	_,keys := atypes.GenCommittee(rng,4)
	nodeKeys := map[atypes.PublicKey]NodeSecretKey{}
	addrs := map[atypes.PublicKey]GigaNodeAddr{}
	for _,key := range keys {
		pk := key.Public()
		nodeKey := makeKey(rng)
		addr := tcp.TestReserveAddr()
		nodeKeys[pk] = nodeKey 
		addrs[pk] = GigaNodeAddr{
			Key: nodeKey.Public(),
			HostPort: tcp.HostPort{Hostname:addr.Addr().String(),Port:addr.Port()},
		}
	}
	genDoc := &types.GenesisDoc{
		ChainID:       "giga-router-test",
		InitialHeight: rng.Int63n(100000),
		AppState: testAppStateJSON(rng),
	}
	require.NoError(t, genDoc.ValidateAndComplete())

	err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		for _, key := range keys {
			nodeKey := nodeKeys[key.Public()]
			nodeInfo := makeInfo(nodeKey)
			nodeInfo.ListenAddr = addrs[key.Public()].HostPort.String()
			router, err := NewRouter(
				NopMetrics(),
				nodeKey,
				func() *types.NodeInfo { return &nodeInfo },
				dbm.NewMemDB(),
				&RouterOptions{
					SelfAddress:              utils.Some(node.endpoint.NodeAddress(nodeKey.Public().NodeID())),
					Endpoint:                 node.endpoint,
					Connection:               conn.DefaultMConnConfig(),
					IncomingConnectionWindow: utils.Some(time.Duration(0)),
					MaxAcceptRate:            utils.Some(rate.Inf),
					MaxDialRate:              utils.Some(rate.Limit(30)),
					Giga: utils.Some(&GigaRouterConfig{
						ValidatorAddrs: addrs,
						Consensus: &consensus.Config{
							Key:                node.validatorSK,
							ViewTimeout:        func(atypes.View) time.Duration { return time.Hour },
							PersistentStateDir: utils.None[string](),
						},
						Producer: &producer.Config{
							MaxGasPerBlock:   21_000,
							MaxTxsPerBlock:   20,
							MaxTxsPerSecond:  utils.None[uint64](),
							MempoolSize:      100,
							BlockInterval:    0,
							AllowEmptyBlocks: false,
						},
						App: newTestApp(),
						GenDoc: genDoc,
					}),
				},
			)
			if err!=nil {
				return fmt.Errorf("NewRouter(): %w",err)
			}
			// TODO: startup might be slow here, because of dial backoff.
			s.SpawnBgNamed("router", func() error { return router.Run(ctx) })
			
			rng := rng.Split()
			s.Spawn(func() error {
				giga, ok := router.giga.Get()
				if !ok {
					return fmt.Errorf("router %d giga disabled", node.index)
				}
				for seq := range gigaRouterTestBlocks / gigaRouterTestValidatorCount {
					tx := &apb.Transaction{
						Hash:    utils.GenString(rng, 32), 
						Payload: utils.GenBytes(rng, 1024),
						GasUsed: 21_000,
					}
					if err := giga.PushToMempool(ctx, tx); err != nil {
						return fmt.Errorf("PushToMempool(node=%d, seq=%d): %w", node.index, seq, err)
					}
				}
				return nil
			})
		}
		for _, node := range nodes {
			if err := node.app.WaitForHeight(ctx, gigaRouterTestBlocks); err != nil {
				return fmt.Errorf("WaitForHeight(node=%d): %w", node.index, err)
			}
		}
		return nil
	})
	require.NoError(t, err)

	want := nodes[0].app.Snapshot()
	require.Len(t, want.blocks, gigaRouterTestBlocks)
	require.Len(t, want.txs, gigaRouterTestBlocks)

	for _, node := range nodes {
		got := node.app.Snapshot()
		require.NoError(t, utils.TestDiff(want, got))
	}
}
