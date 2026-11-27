package p2p

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
	"github.com/holiman/uint256"

	abevm "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/evm"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/producer"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
)

// sendProxyError is a lane rejection with a distinct JSON-RPC code for testing error forwarding.
type sendProxyError struct{}

func (sendProxyError) Error() string  { return "lane rejected" }
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
			env := newAutobahnFullnodeEnv(t, utils.TestRng())
			router := env.Router
			hash, err := router.InsertTx(t.Context(), input)
			require.Error(t, err)
			require.Equal(t, common.Hash{}, hash)
		})
	}
}

func TestGigaInsertTxRejectsUnprotectedTransaction(t *testing.T) {
	tx := signEVMStateTestTx(t, ethtypes.NewTx(&ethtypes.LegacyTx{Nonce: 3}), ethtypes.HomesteadSigner{})
	env := newAutobahnFullnodeEnv(t, utils.TestRng())
	router := env.Router
	hash, err := router.InsertTx(t.Context(), utils.OrPanic1(tx.MarshalBinary()))
	require.Error(t, err)
	require.Equal(t, common.Hash{}, hash)
}

func TestGigaInsertTxRejectsForeignChainTransaction(t *testing.T) {
	for _, name := range []string{"legacy chain zero", "typed chain zero", "other chain"} {
		t.Run(name, func(t *testing.T) {
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
			env := newAutobahnFullnodeEnv(t, utils.TestRng())
			router := env.Router
			hash, err := router.InsertTx(t.Context(), utils.OrPanic1(tx.MarshalBinary()))
			require.ErrorIs(t, err, ethtypes.ErrInvalidChainId)
			require.Equal(t, common.Hash{}, hash)
		})
	}
}

func TestGigaInsertTxProxiesToLane(t *testing.T) {
	tx := signedEVMStateTestTx(t, 0)
	raw := utils.OrPanic1(tx.MarshalBinary())
	remoteHash := common.HexToHash("0x1234")
	client, requests := newSendProxy(t, remoteHash, utils.None[error]())
	env := newAutobahnProxyEnv(t, utils.TestRng(), false, client)
	router := env.Router
	got, err := router.InsertTx(t.Context(), raw)
	require.NoError(t, err)
	require.Equal(t, remoteHash, got)
	forwarded, err := utils.Recv(t.Context(), requests)
	require.NoError(t, err)
	require.Equal(t, hexutil.Bytes(raw), forwarded)
}

func TestGigaInsertTxValidatorKeepsTxLocal(t *testing.T) {
	tx := signedEVMStateTestTx(t, 0)
	client, requests := newSendProxy(t, common.HexToHash("0x1234"), utils.None[error]())
	env := newAutobahnProxyEnv(t, utils.TestRng(), true, client)
	hash, err := env.Router.InsertTx(t.Context(), utils.OrPanic1(tx.MarshalBinary()))
	// The validator's own mempool gets the tx and rejects it, because it is not producing yet.
	require.ErrorIs(t, err, producer.ErrNotProducing)
	require.Equal(t, tx.Hash(), hash)
	// InsertTx returned, so a forwarded tx would already be in requests.
	require.Equal(t, 0, len(requests))
}

func TestGigaInsertTxReturnsProxyErrorWithoutLocalFallback(t *testing.T) {
	tx := signedEVMStateTestTx(t, 0)
	client, requests := newSendProxy(t, common.Hash{}, utils.Some[error](sendProxyError{}))
	env := newAutobahnProxyEnv(t, utils.TestRng(), false, client)
	router := env.Router
	hash, err := router.InsertTx(t.Context(), utils.OrPanic1(tx.MarshalBinary()))
	rpcErr, ok := errors.AsType[ethrpc.Error](err)
	require.True(t, ok)
	require.Equal(t, -32012, rpcErr.ErrorCode())
	require.Equal(t, tx.Hash(), hash)
	_, err = utils.Recv(t.Context(), requests)
	require.NoError(t, err)
}

func TestGigaInsertTxFallsBackToLocalMempool(t *testing.T) {
	tx := signedEVMStateTestTx(t, 0)
	env := newAutobahnBroadcastEnv(t, utils.TestRng())
	router := env.Router
	hash, err := router.InsertTx(t.Context(), utils.OrPanic1(tx.MarshalBinary()))
	require.ErrorIs(t, err, producer.ErrNotProducing)
	require.Equal(t, tx.Hash(), hash)
}

func TestGigaInsertTxFailsWithoutLocalMempool(t *testing.T) {
	tx := signedEVMStateTestTx(t, 0)
	env := newAutobahnFullnodeEnv(t, utils.TestRng())
	router := env.Router
	hash, err := router.InsertTx(t.Context(), utils.OrPanic1(tx.MarshalBinary()))
	require.Error(t, err)
	require.Equal(t, tx.Hash(), hash)
}

func TestGigaInsertTxDoesNotProxyInvalidSignature(t *testing.T) {
	tx := ethtypes.NewTx(&ethtypes.LegacyTx{V: big.NewInt(37), R: new(big.Int), S: new(big.Int)})
	client, _ := newSendProxy(t, common.HexToHash("0x1234"), utils.None[error]())
	env := newAutobahnProxyEnv(t, utils.TestRng(), false, client)
	router := env.Router
	hash, err := router.InsertTx(t.Context(), utils.OrPanic1(tx.MarshalBinary()))
	require.Error(t, err)
	require.Equal(t, common.Hash{}, hash)
}

func TestGigaInsertTxCachesOnlyAdmittedSenders(t *testing.T) {
	for _, rejected := range []bool{false, true} {
		t.Run(fmt.Sprintf("rejected=%t", rejected), func(t *testing.T) {
			app := &senderCacheApp{}
			rejection := errors.New("rejected")
			if rejected {
				app.checkErr = utils.Some(rejection)
			}
			counted := &abevm.TestCountingSigner{
				Signer: ethtypes.LatestSignerForChainID(big.NewInt(1)),
			}
			signer := abevm.NewSigner(counted, 1024, 0)
			env := newAutobahnEnvWithSigner(t, utils.TestRng(), true, abevm.NewProxy(app), signer)
			router := env.Router
			mp := env.common.mempool.OrPanic("validator mempool")
			tx := signedEVMStateTestTx(t, 0)
			raw := utils.OrPanic1(tx.MarshalBinary())
			sender := utils.OrPanic1(ethtypes.Sender(ethtypes.LatestSignerForChainID(big.NewInt(1)), tx))
			err := scope.Run(t.Context(), func(ctx context.Context, s scope.Scope) error {
				s.SpawnBg(func() error { return utils.IgnoreCancel(mp.Run(ctx)) })
				if err := mp.WaitForMempool(ctx); err != nil {
					return err
				}
				got, err := router.InsertTx(ctx, raw)
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
			// Routing and admission share a single sender recovery.
			require.Equal(t, uint64(1), counted.Calls.Load())
			parsed, err := signer.ParseTx(raw)
			require.NoError(t, err)
			require.Equal(t, sender, parsed.Sender())
			expectedCalls := uint64(1)
			if rejected {
				expectedCalls = 2
			}
			require.Equal(t, expectedCalls, counted.Calls.Load())
		})
	}
}

func TestGigaInsertTxRejectsUnsupportedTransactionType(t *testing.T) {
	tx := ethtypes.NewTx(&ethtypes.BlobTx{ChainID: uint256.NewInt(1)})
	env := newAutobahnFullnodeEnv(t, utils.TestRng())
	router := env.Router
	hash, err := router.InsertTx(t.Context(), utils.OrPanic1(tx.MarshalBinary()))
	require.ErrorIs(t, err, ethtypes.ErrTxTypeNotSupported)
	require.Equal(t, common.Hash{}, hash)
}
