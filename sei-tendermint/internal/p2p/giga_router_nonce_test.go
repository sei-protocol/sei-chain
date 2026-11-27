package p2p

import (
	"context"
	"errors"
	"fmt"
	"net"
	"testing"
	"testing/synctest"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	ethrpc "github.com/ethereum/go-ethereum/rpc"

	abevm "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/evm"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
)

type nonceProxyRequest struct {
	address common.Address
	block   ethrpc.BlockNumberOrHash
}

type nonceProxyService struct {
	requests     chan nonceProxyRequest
	nonce        uint64
	err          utils.Option[error]
	responseGate utils.Option[<-chan struct{}]
}

func (s *nonceProxyService) GetTransactionCount(ctx context.Context, address common.Address, block ethrpc.BlockNumberOrHash) (hexutil.Uint64, error) {
	if err := utils.Send(ctx, s.requests, nonceProxyRequest{address, block}); err != nil {
		return 0, err
	}
	if gate, ok := s.responseGate.Get(); ok {
		if _, _, err := utils.RecvOrClosed(ctx, gate); err != nil {
			return 0, err
		}
	}
	if err, ok := s.err.Get(); ok {
		return 0, err
	}
	return hexutil.Uint64(s.nonce), nil
}

func runNonceProxy(ctx context.Context, env *gigaEvmTestFixture, address common.Address, service *nonceProxyService, f func(context.Context, scope.Scope) error) error {
	server := ethrpc.NewServer()
	defer server.Stop()
	if err := server.RegisterName("eth", service); err != nil {
		return err
	}
	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		serverConn, clientConn := net.Pipe()
		defer func() { _ = serverConn.Close() }()
		s.SpawnBg(func() error {
			server.ServeCodec(ethrpc.NewCodec(serverConn), 0)
			return nil
		})
		client, err := ethrpc.DialIO(ctx, clientConn, clientConn)
		if err != nil {
			_ = clientConn.Close()
			return err
		}
		defer func() {
			_ = clientConn.Close()
			client.Close()
		}()
		env.setEvmProxy(address, client)
		return f(ctx, s)
	})
}

func TestGigaPendingNonceForwardsToShardOwner(t *testing.T) {
	for _, validator := range []bool{false, true} {
		t.Run(fmt.Sprintf("validator=%t", validator), func(t *testing.T) {
			address := common.Address{1}
			app := abevm.NewProxy(broadcastEVMApp{})
			env := newAutobahnRoutingEnv(t, utils.TestRng(), validator, app, newTestSigner(app.EvmChainID(), 0), utils.Some(address))
			service := &nonceProxyService{requests: make(chan nonceProxyRequest, 1), nonce: 9}
			require.NoError(t, runNonceProxy(t.Context(), env, address, service, func(ctx context.Context, _ scope.Scope) error {
				nonce, err := env.Router.EvmPendingNonce(ctx, address)
				if err != nil {
					return err
				}
				if err := utils.TestDiff(uint64(9), nonce); err != nil {
					return err
				}
				request, err := utils.Recv(ctx, service.requests)
				if err != nil {
					return err
				}
				if err := utils.TestDiff(address, request.address); err != nil {
					return err
				}
				number, ok := request.block.Number()
				if !ok {
					return fmt.Errorf("proxy request has no block number")
				}
				return utils.TestDiff(ethrpc.PendingBlockNumber, number)
			}))
		})
	}
}

func TestGigaPendingNonceReturnsProxyErrorWithoutLocalFallback(t *testing.T) {
	address := common.Address{1}
	app := abevm.NewProxy(broadcastEVMApp{})
	env := newAutobahnRoutingEnv(t, utils.TestRng(), true, app, newTestSigner(app.EvmChainID(), 0), utils.Some(address))
	service := &nonceProxyService{requests: make(chan nonceProxyRequest, 1), err: utils.Some[error](sendProxyError{})}
	require.NoError(t, runNonceProxy(t.Context(), env, address, service, func(ctx context.Context, _ scope.Scope) error {
		nonce, err := env.Router.EvmPendingNonce(ctx, address)
		rpcErr, ok := errors.AsType[ethrpc.Error](err)
		if !ok {
			return fmt.Errorf("EvmPendingNonce() = %v, want RPC error", err)
		}
		if err := utils.TestDiff(-32012, rpcErr.ErrorCode()); err != nil {
			return err
		}
		if err := utils.TestDiff(uint64(0), nonce); err != nil {
			return err
		}
		_, err = utils.Recv(ctx, service.requests)
		return err
	}))
}

func TestGigaPendingNoncePropagatesCanceledRequest(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		address := common.Address{1}
		app := abevm.NewProxy(broadcastEVMApp{})
		env := newAutobahnRoutingEnv(t, utils.TestRng(), true, app, newTestSigner(app.EvmChainID(), 0), utils.Some(address))
		gate := make(chan struct{})
		service := &nonceProxyService{
			requests:     make(chan nonceProxyRequest, 1),
			nonce:        9,
			responseGate: utils.Some((<-chan struct{})(gate)),
		}
		var callErr error
		require.NoError(t, runNonceProxy(t.Context(), env, address, service, func(ctx context.Context, s scope.Scope) error {
			defer close(gate)
			callCtx, cancel := context.WithCancel(ctx)
			defer cancel()
			call := s.Spawn1(func() (struct{}, error) {
				_, callErr = env.Router.EvmPendingNonce(callCtx, address)
				return struct{}{}, nil
			})
			if _, err := utils.Recv(ctx, service.requests); err != nil {
				return err
			}
			synctest.Wait()
			cancel()
			_, err := call.Join(ctx)
			return err
		}))
		require.ErrorIs(t, callErr, context.Canceled)
	})
}

func TestGigaPendingNonceIncludesLocalAdmissions(t *testing.T) {
	app := &senderCacheApp{}
	env := newAutobahnEnvWithApp(t, utils.TestRng(), true, abevm.NewProxy(app))
	mempool := env.Router.mempool.OrPanic("validator mempool")
	tx := signedEVMStateTestTx(t, 0)
	raw := utils.OrPanic1(tx.MarshalBinary())
	sender := utils.OrPanic1(ethtypes.Sender(ethtypes.LatestSignerForChainID(tx.ChainId()), tx))
	require.NoError(t, scope.Run(t.Context(), func(ctx context.Context, s scope.Scope) error {
		s.SpawnBg(func() error { return utils.IgnoreCancel(mempool.Run(ctx)) })
		if err := mempool.WaitForMempool(ctx); err != nil {
			return err
		}
		if _, err := env.Router.InsertTx(ctx, raw); err != nil {
			return err
		}
		nonce, err := env.Router.EvmPendingNonce(ctx, sender)
		if err != nil {
			return err
		}
		return utils.TestDiff(uint64(4), nonce)
	}))
	require.Equal(t, uint64(3), env.Proxy.EvmNonce(sender))
}
