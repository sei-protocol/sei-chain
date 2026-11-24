package core

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"net/http/httptest"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	ethrpc "github.com/ethereum/go-ethereum/rpc"

	abevm "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/evm"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/producer"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
)

type rememberedSender struct {
	hash   common.Hash
	sender common.Address
}

type sendRouter struct {
	p2p.GigaRouter
	proxy        utils.Option[*ethrpc.Client]
	enabled      bool
	enabledCalls int
	proxyCalls   int
	mempoolCalls int
	remembered   []rememberedSender
	forgotten    []common.Hash
}

func trackSendRouter(env *GigaEnvironment, enabled bool, proxy utils.Option[*ethrpc.Client]) *sendRouter {
	router := &sendRouter{GigaRouter: env.Router, proxy: proxy, enabled: enabled}
	env.Router = router
	return router
}

func (r *sendRouter) EvmProxyEnabled() bool { r.enabledCalls++; return r.enabled }
func (r *sendRouter) EvmProxy(common.Address) utils.Option[*ethrpc.Client] {
	r.proxyCalls++
	return r.proxy
}
func (r *sendRouter) Mempool() utils.Option[*producer.State] {
	r.mempoolCalls++
	return r.GigaRouter.Mempool()
}
func (r *sendRouter) RememberEvmSender(hash common.Hash, sender common.Address) {
	r.remembered = append(r.remembered, rememberedSender{hash, sender})
	r.GigaRouter.RememberEvmSender(hash, sender)
}
func (r *sendRouter) ForgetEvmSender(hash common.Hash) {
	r.forgotten = append(r.forgotten, hash)
	r.GigaRouter.ForgetEvmSender(hash)
}

type sendProxyError struct{}

func (sendProxyError) Error() string  { return "shard owner rejected" }
func (sendProxyError) ErrorCode() int { return -32012 }

type sendProxyService struct {
	requests chan hexutil.Bytes
	hash     common.Hash
	err      utils.Option[error]
}

func (s *sendProxyService) SendRawTransaction(ctx context.Context, input hexutil.Bytes) (common.Hash, error) {
	if err := utils.Send(ctx, s.requests, append(hexutil.Bytes(nil), input...)); err != nil {
		return common.Hash{}, err
	}
	if err, ok := s.err.Get(); ok {
		return common.Hash{}, err
	}
	return s.hash, nil
}

func newSendProxy(t *testing.T, hash common.Hash, callErr utils.Option[error]) (*ethrpc.Client, <-chan hexutil.Bytes) {
	t.Helper()
	service := &sendProxyService{requests: make(chan hexutil.Bytes, 1), hash: hash, err: callErr}
	return newShardProxy(t, service), service.requests
}

func newShardProxy[S any](t *testing.T, service S) *ethrpc.Client {
	t.Helper()
	handler := ethrpc.NewServer()
	require.NoError(t, handler.RegisterName("eth", service))
	t.Cleanup(handler.Stop)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := ethrpc.DialHTTP(server.URL)
	require.NoError(t, err)
	t.Cleanup(client.Close)
	return client
}

func TestGigaInsertTxRejectsMalformedInput(t *testing.T) {
	for _, input := range [][]byte{nil, {1, 2}} {
		t.Run(fmt.Sprintf("%x", input), func(t *testing.T) {
			env := newAutobahnFullnodeEnv(t)
			router := trackSendRouter(env, true, utils.None[*ethrpc.Client]())
			hash, err := env.InsertTx(t.Context(), input)
			require.Error(t, err)
			require.Equal(t, common.Hash{}, hash)
			require.Zero(t, router.enabledCalls)
			require.Zero(t, router.mempoolCalls)
		})
	}
}

func TestGigaInsertTxRejectsUnprotectedTransaction(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(fmt.Sprintf("proxies=%t", enabled), func(t *testing.T) {
			tx := signEVMStateTestTx(t, ethtypes.NewTx(&ethtypes.LegacyTx{Nonce: 3}), ethtypes.HomesteadSigner{})
			env := newAutobahnFullnodeEnv(t)
			router := trackSendRouter(env, enabled, utils.None[*ethrpc.Client]())
			hash, err := env.InsertTx(t.Context(), utils.OrPanic1(tx.MarshalBinary()))
			require.ErrorIs(t, err, errUnprotectedTx)
			require.Equal(t, common.Hash{}, hash)
			require.Zero(t, router.enabledCalls)
			require.Zero(t, router.proxyCalls)
			require.Zero(t, router.mempoolCalls)
		})
	}
}

func TestGigaInsertTxRejectsForeignChainTransaction(t *testing.T) {
	for _, name := range []string{"legacy chain zero", "typed chain zero", "other chain"} {
		for _, enabled := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/proxies=%t", name, enabled), func(t *testing.T) {
				var tx *ethtypes.Transaction
				switch name {
				case "legacy chain zero":
					tx = ethtypes.NewTx(&ethtypes.LegacyTx{V: big.NewInt(35), R: big.NewInt(1), S: big.NewInt(1)})
				case "typed chain zero":
					tx = ethtypes.NewTx(&ethtypes.DynamicFeeTx{ChainID: new(big.Int), V: new(big.Int), R: big.NewInt(1), S: big.NewInt(1)})
				default:
					id := big.NewInt(2)
					tx = signEVMStateTestTx(t, ethtypes.NewTx(&ethtypes.DynamicFeeTx{ChainID: id}), ethtypes.LatestSignerForChainID(id))
				}
				env := newAutobahnFullnodeEnv(t)
				router := trackSendRouter(env, enabled, utils.None[*ethrpc.Client]())
				hash, err := env.InsertTx(t.Context(), utils.OrPanic1(tx.MarshalBinary()))
				require.ErrorIs(t, err, errInvalidChainID)
				require.Equal(t, common.Hash{}, hash)
				require.Zero(t, router.enabledCalls)
				require.Zero(t, router.proxyCalls)
				require.Zero(t, router.mempoolCalls)
			})
		}
	}
}

func TestGigaInsertTxProxiesToShardOwner(t *testing.T) {
	for _, validator := range []bool{false, true} {
		t.Run(fmt.Sprintf("validator=%t", validator), func(t *testing.T) {
			tx := signedEVMStateTestTx(t, 0)
			raw := utils.OrPanic1(tx.MarshalBinary())
			remoteHash := common.HexToHash("0x1234")
			client, requests := newSendProxy(t, remoteHash, utils.None[error]())
			env := newAutobahnEnv(t, validator)
			router := trackSendRouter(env, true, utils.Some(client))
			got, err := env.InsertTx(t.Context(), raw)
			require.NoError(t, err)
			require.Equal(t, remoteHash, got)
			forwarded, err := utils.Recv(t.Context(), requests)
			require.NoError(t, err)
			require.Equal(t, hexutil.Bytes(raw), forwarded)
			require.Equal(t, 1, router.proxyCalls)
			require.Zero(t, router.mempoolCalls)
			require.Empty(t, router.remembered)
			require.Empty(t, router.forgotten)
		})
	}
}

func TestGigaInsertTxReturnsProxyErrorWithoutLocalFallback(t *testing.T) {
	tx := signedEVMStateTestTx(t, 0)
	client, requests := newSendProxy(t, common.Hash{}, utils.Some[error](sendProxyError{}))
	env := newAutobahnBroadcastEnv(t)
	router := trackSendRouter(env, true, utils.Some(client))
	hash, err := env.InsertTx(t.Context(), utils.OrPanic1(tx.MarshalBinary()))
	rpcErr, ok := errors.AsType[ethrpc.Error](err)
	require.True(t, ok)
	require.Equal(t, -32012, rpcErr.ErrorCode())
	require.Equal(t, tx.Hash(), hash)
	_, err = utils.Recv(t.Context(), requests)
	require.NoError(t, err)
	require.Zero(t, router.mempoolCalls)
	require.Empty(t, router.remembered)
	require.Empty(t, router.forgotten)
}

func TestGigaInsertTxSkipsShardLookupWithoutProxies(t *testing.T) {
	tx := signedEVMStateTestTx(t, 0)
	env := newAutobahnBroadcastEnv(t)
	router := trackSendRouter(env, false, utils.None[*ethrpc.Client]())
	hash, err := env.InsertTx(t.Context(), utils.OrPanic1(tx.MarshalBinary()))
	require.ErrorIs(t, err, producer.ErrNotProducing)
	require.Equal(t, tx.Hash(), hash)
	require.Zero(t, router.proxyCalls)
	require.Empty(t, router.remembered)
	require.Equal(t, []common.Hash{tx.Hash()}, router.forgotten)
}

func TestGigaInsertTxClearsSenderWithoutLocalMempool(t *testing.T) {
	tx := signedEVMStateTestTx(t, 0)
	env := newAutobahnFullnodeEnv(t)
	router := trackSendRouter(env, true, utils.None[*ethrpc.Client]())
	hash, err := env.InsertTx(t.Context(), utils.OrPanic1(tx.MarshalBinary()))
	require.Error(t, err)
	require.Equal(t, tx.Hash(), hash)
	require.Equal(t, 1, router.proxyCalls)
	require.Len(t, router.remembered, 1)
	require.Equal(t, []common.Hash{tx.Hash()}, router.forgotten)
}

func TestGigaInsertTxDoesNotProxyInvalidSignature(t *testing.T) {
	tx := ethtypes.NewTx(&ethtypes.LegacyTx{V: big.NewInt(37), R: new(big.Int), S: new(big.Int)})
	client, _ := newSendProxy(t, common.HexToHash("0x1234"), utils.None[error]())
	env := newAutobahnFullnodeEnv(t)
	router := trackSendRouter(env, true, utils.Some(client))
	hash, err := env.InsertTx(t.Context(), utils.OrPanic1(tx.MarshalBinary()))
	require.Error(t, err)
	require.Equal(t, tx.Hash(), hash)
	require.Zero(t, router.proxyCalls)
	require.Equal(t, 1, router.mempoolCalls)
	require.Empty(t, router.remembered)
}

func TestGigaInsertTxCachesOnlyAdmittedSenders(t *testing.T) {
	for _, rejected := range []bool{false, true} {
		t.Run(fmt.Sprintf("rejected=%t", rejected), func(t *testing.T) {
			app := &senderCacheApp{}
			rejection := errors.New("rejected")
			if rejected {
				app.checkErr = utils.Some(rejection)
			}
			env := newAutobahnEnvWithApp(t, true, abevm.NewProxy(app))
			router := trackSendRouter(env, true, utils.None[*ethrpc.Client]())
			mp := router.GigaRouter.Mempool().OrPanic("validator mempool")
			tx := signedEVMStateTestTx(t, 0)
			raw := utils.OrPanic1(tx.MarshalBinary())
			sender := utils.OrPanic1(ethtypes.Sender(ethtypes.LatestSignerForChainID(big.NewInt(1)), tx))
			err := scope.Run(t.Context(), func(ctx context.Context, s scope.Scope) error {
				s.SpawnBg(func() error { return utils.IgnoreCancel(mp.Run(ctx)) })
				if err := mp.WaitForMempool(ctx); err != nil {
					return err
				}
				got, err := env.InsertTx(ctx, raw)
				if got != tx.Hash() {
					return fmt.Errorf("unexpected transaction hash: %v", got)
				}
				if rejected {
					if !errors.Is(err, rejection) {
						return fmt.Errorf("expected admission rejection, got %v", err)
					}
				} else if err != nil {
					return err
				}
				return nil
			})
			require.NoError(t, err)
			require.Equal(t, utils.Some(sender), app.sender)
			require.Equal(t, []rememberedSender{{tx.Hash(), sender}}, router.remembered)
			if rejected {
				require.Equal(t, []common.Hash{tx.Hash()}, router.forgotten)
			} else {
				require.Empty(t, router.forgotten)
			}
		})
	}
}
