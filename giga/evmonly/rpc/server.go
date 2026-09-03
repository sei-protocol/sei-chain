// Package rpc serves the minimal JSON-RPC surface for the EVM-only executor.
package rpc

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	ethrpc "github.com/ethereum/go-ethereum/rpc"

	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/types"
	"github.com/sei-protocol/seilog"
)

const (
	listenAddress = "0.0.0.0:8545"
	shutdownWait  = 5 * time.Second
)

var logger = seilog.NewLogger("giga", "evmonly", "rpc")

// Backend submits transactions locally or returns the RPC client for their
// Autobahn shard owner.
type Backend interface {
	BroadcastTx(context.Context, *coretypes.RequestBroadcastTx) (*coretypes.ResultBroadcastTx, error)
	EvmProxyEnabled() bool
	EvmProxy(common.Address) utils.Option[*ethrpc.Client]
}

type sendAPI struct {
	backend Backend
}

// SendRawTransaction submits a signed raw Ethereum transaction to Autobahn and
// returns its Ethereum transaction hash.
func (api *sendAPI) SendRawTransaction(ctx context.Context, input hexutil.Bytes) (common.Hash, error) {
	tx := new(ethtypes.Transaction)
	if err := tx.UnmarshalBinary(input); err != nil {
		return common.Hash{}, err
	}
	hash := tx.Hash()

	if api.backend.EvmProxyEnabled() {
		if sender, err := ethtypes.Sender(ethtypes.LatestSignerForChainID(tx.ChainId()), tx); err == nil {
			if client, ok := api.backend.EvmProxy(sender).Get(); ok {
				if err := client.CallContext(ctx, &hash, "eth_sendRawTransaction", input); err != nil {
					return hash, err
				}
				return hash, nil
			}
		}
	}

	result, err := api.backend.BroadcastTx(ctx, &coretypes.RequestBroadcastTx{
		Tx: append(tmtypes.Tx(nil), input...),
	})
	if err != nil {
		return hash, err
	}
	if result == nil {
		return hash, errors.New("missing broadcast response")
	}
	if result.Code != abci.CodeTypeOK {
		message := result.Log
		if message == "" {
			message = fmt.Sprintf("transaction rejected with code %d", result.Code)
		}
		return hash, errors.New(message)
	}
	return hash, nil
}

// Server serves the EVM-only JSON-RPC API on port 8545.
type Server struct {
	listener net.Listener
	http     *http.Server
	rpc      *ethrpc.Server
}

// Start binds the EVM-only JSON-RPC listener and returns its server.
func Start(backend Backend) (*Server, error) {
	rpcServer, err := newHandler(backend)
	if err != nil {
		return nil, err
	}
	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", listenAddress)
	if err != nil {
		rpcServer.Stop()
		return nil, fmt.Errorf("listen for EVM-only RPC on %s: %w", listenAddress, err)
	}
	return &Server{
		listener: listener,
		http: &http.Server{
			Handler:           rpcServer,
			ReadHeaderTimeout: 5 * time.Second,
		},
		rpc: rpcServer,
	}, nil
}

func newHandler(backend Backend) (*ethrpc.Server, error) {
	rpcServer := ethrpc.NewServer()
	if err := rpcServer.RegisterName("eth", &sendAPI{backend: backend}); err != nil {
		return nil, fmt.Errorf("register EVM-only RPC: %w", err)
	}
	return rpcServer, nil
}

// Serve handles requests until the server stops or ctx is canceled.
func (s *Server) Serve(ctx context.Context) error {
	logger.Info("Starting Autobahn EVM-only RPC server", "laddr", s.listener.Addr())
	err := s.http.Serve(s.listener)
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
	_ = s.listener.Close()
}
