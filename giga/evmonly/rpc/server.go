// Package rpc serves the minimal JSON-RPC surface for the EVM-only executor.
package rpc

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"time"

	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/params"
	ethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/holiman/uint256"
	"golang.org/x/net/netutil"

	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
	"github.com/sei-protocol/seilog"
)

const (
	listenAddress   = "0.0.0.0:8545"
	wsListenAddress = "0.0.0.0:8546"
	shutdownWait    = 5 * time.Second
	// maxWSConns bounds concurrently open WebSocket connections; each one is
	// hijacked and held with its own goroutine until the peer disconnects.
	maxWSConns = 2000
)

// wsAllowedOrigins accepts WebSocket upgrades from any Origin header. WebSocket
// is exempt from the browser same-origin policy, so this is the only origin
// gate on the listener.
var wsAllowedOrigins = []string{"*"}

var logger = seilog.NewLogger("giga", "evmonly", "rpc")

// Backend submits transactions, reads committed EVM state and finalized
// blocks, publishes new block heights, and returns the RPC client for an
// Autobahn shard owner.
// EvmProxyEnabled reports whether EvmProxy can ever return a client; when it
// is false every transaction is broadcast locally without recovering its sender.
type Backend interface {
	Block(context.Context, *coretypes.RequestBlockInfo) (*coretypes.ResultBlock, error)
	BlockByHash(context.Context, *coretypes.RequestBlockByHash) (*coretypes.ResultBlock, error)
	BroadcastTx(context.Context, *coretypes.RequestBroadcastTx) (*coretypes.ResultBroadcastTx, error)
	EvmBalance(common.Address) uint256.Int
	EvmBaseFee() (*big.Int, error)
	EvmBlockNumber() uint64
	ExecutedBlocks() (utils.AtomicRecv[atypes.ExecutedBlocks], error)
	EvmCall(context.Context, *core.Message) (*core.ExecutionResult, error)
	EvmChainConfig() (*params.ChainConfig, error)
	EvmChainID() uint64
	EvmGasLimit() (uint64, error)
	EvmMinGasPrice() (*big.Int, error)
	EvmProxy(common.Address) utils.Option[*ethrpc.Client]
	EvmProxyEnabled() bool
	EvmTransactionCount(common.Address) uint64
}

// Server serves the EVM-only JSON-RPC API over HTTP on port 8545 and over
// WebSocket on port 8546.
type Server struct {
	listener   net.Listener
	http       *http.Server
	wsListener net.Listener
	ws         *http.Server
	rpc        *ethrpc.Server
}

// Start binds the EVM-only JSON-RPC HTTP and WebSocket listeners and returns
// their server.
func Start(backend Backend, receiptStore receipt.ReceiptStore) (*Server, error) {
	rpcServer, err := newHandler(backend, receiptStore)
	if err != nil {
		return nil, err
	}
	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", listenAddress)
	if err != nil {
		rpcServer.Stop()
		return nil, fmt.Errorf("listen for EVM-only RPC on %s: %w", listenAddress, err)
	}
	wsListener, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", wsListenAddress)
	if err != nil {
		_ = listener.Close()
		rpcServer.Stop()
		return nil, fmt.Errorf("listen for EVM-only WebSocket RPC on %s: %w", wsListenAddress, err)
	}
	return &Server{
		listener: listener,
		http: &http.Server{
			Handler:           rpcServer,
			ReadHeaderTimeout: 5 * time.Second,
		},
		wsListener: netutil.LimitListener(wsListener, maxWSConns),
		ws: &http.Server{
			Handler:           websocketHandler(rpcServer),
			ReadHeaderTimeout: 5 * time.Second,
		},
		rpc: rpcServer,
	}, nil
}

// websocketHandler upgrades incoming connections to WebSocket and serves the
// same JSON-RPC methods as the HTTP listener over them.
func websocketHandler(rpcServer *ethrpc.Server) http.Handler {
	return rpcServer.WebsocketHandler(wsAllowedOrigins)
}

// newHandler registers the eth namespace. receiptStore may be nil on a node that keeps no
// receipts: transactions and blocks are then served without receipt-derived fields, and
// receipt lookups report ErrNoReceiptStore.
func newHandler(backend Backend, receiptStore receipt.ReceiptStore) (*ethrpc.Server, error) {
	rpcServer := ethrpc.NewServer()
	if err := rpcServer.RegisterName("eth", &sendAPI{backend: backend}); err != nil {
		return nil, fmt.Errorf("register EVM-only send RPC: %w", err)
	}
	if err := rpcServer.RegisterName("eth", &txAPI{backend: backend, store: receiptStore}); err != nil {
		return nil, fmt.Errorf("register EVM-only transaction RPC: %w", err)
	}
	if err := rpcServer.RegisterName("eth", &stateAPI{backend: backend}); err != nil {
		return nil, fmt.Errorf("register EVM-only state RPC: %w", err)
	}
	if err := rpcServer.RegisterName("eth", &infoAPI{backend: backend, store: receiptStore}); err != nil {
		return nil, fmt.Errorf("register EVM-only info RPC: %w", err)
	}
	if err := rpcServer.RegisterName("eth", &callAPI{backend: backend}); err != nil {
		return nil, fmt.Errorf("register EVM-only call RPC: %w", err)
	}
	if err := rpcServer.RegisterName("eth", &blockAPI{backend: backend, store: receiptStore}); err != nil {
		return nil, fmt.Errorf("register EVM-only block RPC: %w", err)
	}
	if err := rpcServer.RegisterName("eth", &subscribeAPI{backend: backend, store: receiptStore}); err != nil {
		return nil, fmt.Errorf("register EVM-only subscription RPC: %w", err)
	}
	return rpcServer, nil
}

// Serve handles HTTP and WebSocket requests until either listener stops or
// ctx is canceled.
func (s *Server) Serve(ctx context.Context) error {
	logger.Info("Starting Autobahn EVM-only RPC server", "laddr", s.listener.Addr(), "ws_laddr", s.wsListener.Addr())
	errs := make(chan error, 2)
	go func() { errs <- s.http.Serve(s.listener) }()
	go func() { errs <- s.ws.Serve(s.wsListener) }()
	err := <-errs
	if errors.Is(err, http.ErrServerClosed) && ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}

// Stop closes the EVM-only JSON-RPC listener and active server.
func (s *Server) Stop() {
	s.rpc.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), shutdownWait)
	defer cancel()
	if err := s.http.Shutdown(ctx); err != nil {
		logger.Error("EVM-only RPC graceful shutdown failed", "err", err)
		_ = s.http.Close()
	}
	// Shutdown does not wait for hijacked WebSocket connections; rpc.Stop above
	// has already closed them, so Close only releases the listener.
	_ = s.ws.Close()
	_ = s.listener.Close()
	_ = s.wsListener.Close()
}
