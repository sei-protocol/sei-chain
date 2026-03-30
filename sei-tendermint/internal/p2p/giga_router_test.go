package p2p

import (
	"context"
	"crypto/sha256"
	"fmt"
	"testing"
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
)

const (
	gigaRouterTestBlocks         = 20
	gigaRouterTestBlockInterval  = 10 * time.Millisecond
)

type gigaRouterTestAppState struct {
	blocks  [][][]byte
	txs     [][]byte
	appHash []byte
}

type gigaRouterTestAppSnapshot struct {
	blocks  [][][]byte
	txs     [][]byte
	appHash []byte
}

type gigaRouterTestApp struct {
	abci.BaseApplication

	validators []abci.ValidatorUpdate
	state      utils.Watch[*gigaRouterTestAppState]
}

func newGigaRouterTestApp(validators []abci.ValidatorUpdate) *gigaRouterTestApp {
	return &gigaRouterTestApp{
		validators: append([]abci.ValidatorUpdate(nil), validators...),
		state: utils.NewWatch(&gigaRouterTestAppState{
			blocks:  [][][]byte{},
			txs:     [][]byte{},
			appHash: nil,
		}),
	}
}

func (a *gigaRouterTestApp) GetValidators() []abci.ValidatorUpdate {
	return append([]abci.ValidatorUpdate(nil), a.validators...)
}

func (a *gigaRouterTestApp) Info(_ context.Context, _ *abci.RequestInfo) (*abci.ResponseInfo, error) {
	for state := range a.state.Lock() {
		return &abci.ResponseInfo{
			LastBlockHeight:  int64(len(state.blocks)),
			LastBlockAppHash: append([]byte(nil), state.appHash...),
		}, nil
	}
	panic("unreachable")
}

func (a *gigaRouterTestApp) InitChain(context.Context, *abci.RequestInitChain) (*abci.ResponseInitChain, error) {
	return &abci.ResponseInitChain{
		AppHash:    nil,
		Validators: append([]abci.ValidatorUpdate(nil), a.validators...),
	}, nil
}

func (a *gigaRouterTestApp) FinalizeBlock(_ context.Context, req *abci.RequestFinalizeBlock) (*abci.ResponseFinalizeBlock, error) {
	blockTxs := cloneTxList(req.Txs)
	txResults := make([]*abci.ExecTxResult, len(req.Txs))
	for i := range req.Txs {
		txResults[i] = &abci.ExecTxResult{Code: abci.CodeTypeOK}
	}
	for state, ctrl := range a.state.Lock() {
		state.blocks = append(state.blocks, blockTxs)
		state.txs = append(state.txs, blockTxs...)
		state.appHash = gigaRouterTestMerkle(state.txs)
		resp := &abci.ResponseFinalizeBlock{
			AppHash:   append([]byte(nil), state.appHash...),
			TxResults: txResults,
		}
		ctrl.Updated()
		return resp, nil
	}
	panic("unreachable")
}

func (a *gigaRouterTestApp) Commit(context.Context) (*abci.ResponseCommit, error) {
	return &abci.ResponseCommit{}, nil
}

func (a *gigaRouterTestApp) WaitForHeight(ctx context.Context, height int) error {
	for state, ctrl := range a.state.Lock() {
		return ctrl.WaitUntil(ctx, func() bool { return len(state.blocks) >= height })
	}
	panic("unreachable")
}

func (a *gigaRouterTestApp) Snapshot() gigaRouterTestAppSnapshot {
	for state := range a.state.Lock() {
		return gigaRouterTestAppSnapshot{
			blocks:  cloneBlocks(state.blocks),
			txs:     cloneTxList(state.txs),
			appHash: append([]byte(nil), state.appHash...),
		}
	}
	panic("unreachable")
}

type gigaRouterTestNode struct {
	nodeKey      NodeSecretKey
	validatorKey atypes.SecretKey
	endpoint     Endpoint
	router      *Router
	app         *gigaRouterTestApp
}

func TestGigaRouter_FinalizesSame20Blocks(t *testing.T) {
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
		InitialHeight: 1234,
	}
	require.NoError(t, genDoc.ValidateAndComplete())

	err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		for _, node := range nodes {
			app := newGigaRouterTestApp(validatorUpdates)
			nodeInfo := makeInfo(node.privKey)
			nodeInfo.ListenAddr = node.endpoint.String()

			router, err := NewRouter(
				NopMetrics(),
				node.privKey,
				func() *types.NodeInfo { return &nodeInfo },
				dbm.NewMemDB(),
				&RouterOptions{
					SelfAddress:              utils.Some(node.endpoint.NodeAddress(node.privKey.Public().NodeID())),
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
						App:    app,
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
		require.Len(t, got.blocks, gigaRouterTestBlocks)
		require.Len(t, got.txs, gigaRouterTestBlocks)
		require.Equal(t, want.blocks, got.blocks)
		require.Equal(t, want.txs, got.txs)
		require.Equal(t, want.appHash, got.appHash)
	}
}

func gigaRouterTestMerkle(txs [][]byte) []byte {
	switch len(txs) {
	case 0:
		return nil
	case 1:
		sum := sha256.Sum256(txs[0])
		return sum[:]
	default:
		last := sha256.Sum256(txs[len(txs)-1])
		earlier := gigaRouterTestMerkle(txs[:len(txs)-1])
		input := make([]byte, 0, len(last)+len(earlier))
		input = append(input, last[:]...)
		input = append(input, earlier...)
		sum := sha256.Sum256(input)
		return sum[:]
	}
}

func cloneTxList(txs [][]byte) [][]byte {
	out := make([][]byte, len(txs))
	for i, tx := range txs {
		out[i] = append([]byte(nil), tx...)
	}
	return out
}

func cloneBlocks(blocks [][][]byte) [][][]byte {
	out := make([][][]byte, len(blocks))
	for i, block := range blocks {
		out[i] = cloneTxList(block)
	}
	return out
}
