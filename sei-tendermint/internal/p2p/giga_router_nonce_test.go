package p2p

import (
	"context"
	"errors"
	"fmt"
	"testing"

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
	requests chan nonceProxyRequest
	nonce    uint64
	err      utils.Option[error]
}

func (s *nonceProxyService) GetTransactionCount(ctx context.Context, address common.Address, block ethrpc.BlockNumberOrHash) (hexutil.Uint64, error) {
	if err := utils.Send(ctx, s.requests, nonceProxyRequest{address, block}); err != nil {
		return 0, err
	}
	if err, ok := s.err.Get(); ok {
		return 0, err
	}
	return hexutil.Uint64(s.nonce), nil
}

func newNonceProxy(t *testing.T, nonce uint64, callErr utils.Option[error]) (*ethrpc.Client, <-chan nonceProxyRequest) {
	t.Helper()
	service := &nonceProxyService{requests: make(chan nonceProxyRequest, 1), nonce: nonce, err: callErr}
	return newShardProxy(t, service), service.requests
}

func TestGigaPendingNonceForwardsToShardOwner(t *testing.T) {
	for _, validator := range []bool{false, true} {
		t.Run(fmt.Sprintf("validator=%t", validator), func(t *testing.T) {
			address := common.Address{1}
			client, requests := newNonceProxy(t, 9, utils.None[error]())
			env := newAutobahnEnv(t, utils.TestRng(), validator)
			router := trackSendRouter(env, utils.Some(client))
			nonce, err := router.EvmPendingNonce(t.Context(), address)
			require.NoError(t, err)
			require.Equal(t, uint64(9), nonce)
			request, err := utils.Recv(t.Context(), requests)
			require.NoError(t, err)
			require.Equal(t, address, request.address)
			number, ok := request.block.Number()
			require.True(t, ok)
			require.Equal(t, ethrpc.PendingBlockNumber, number)
			require.Equal(t, 1, router.proxyCalls)
			require.Zero(t, router.mempoolCalls)
		})
	}
}

func TestGigaPendingNonceReturnsProxyErrorWithoutLocalFallback(t *testing.T) {
	client, requests := newNonceProxy(t, 0, utils.Some[error](sendProxyError{}))
	env := newAutobahnBroadcastEnv(t, utils.TestRng())
	router := trackSendRouter(env, utils.Some(client))
	nonce, err := router.EvmPendingNonce(t.Context(), common.Address{1})
	rpcErr, ok := errors.AsType[ethrpc.Error](err)
	require.True(t, ok)
	require.Equal(t, -32012, rpcErr.ErrorCode())
	require.Zero(t, nonce)
	_, err = utils.Recv(t.Context(), requests)
	require.NoError(t, err)
	require.Zero(t, router.mempoolCalls)
}

func TestGigaPendingNoncePropagatesCanceledRequest(t *testing.T) {
	client, _ := newNonceProxy(t, 9, utils.None[error]())
	env := newAutobahnBroadcastEnv(t, utils.TestRng())
	router := trackSendRouter(env, utils.Some(client))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := router.EvmPendingNonce(ctx, common.Address{1})
	require.ErrorIs(t, err, context.Canceled)
	require.Zero(t, router.mempoolCalls)
}

func TestGigaPendingNonceIncludesLocalAdmissions(t *testing.T) {
	app := &senderCacheApp{}
	env := newAutobahnEnvWithApp(t, utils.TestRng(), true, abevm.NewProxy(app))
	mempool := env.Router.Mempool().OrPanic("validator mempool")
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
