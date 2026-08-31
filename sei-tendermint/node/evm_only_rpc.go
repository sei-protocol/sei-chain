package node

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
	rpccore "github.com/sei-protocol/sei-chain/sei-tendermint/internal/rpc/core"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

const (
	evmOnlyRPCListenAddress = "0.0.0.0:8545"
	evmOnlyRPCShutdownWait  = 5 * time.Second
)

type evmOnlyRPCBackend interface {
	BroadcastTx(context.Context, *coretypes.RequestBroadcastTx) (*coretypes.ResultBroadcastTx, error)
	EvmProxy(common.Address) utils.Option[*ethrpc.Client]
}

type evmOnlySendAPI struct {
	backend evmOnlyRPCBackend
}

// SendRawTransaction submits a signed raw Ethereum transaction to Autobahn and
// returns its Ethereum transaction hash.
func (api *evmOnlySendAPI) SendRawTransaction(ctx context.Context, input hexutil.Bytes) (common.Hash, error) {
	tx := new(ethtypes.Transaction)
	if err := tx.UnmarshalBinary(input); err != nil {
		return common.Hash{}, err
	}
	hash := tx.Hash()

	if sender, err := ethtypes.Sender(ethtypes.LatestSignerForChainID(tx.ChainId()), tx); err == nil {
		if client, ok := api.backend.EvmProxy(sender).Get(); ok {
			if err := client.CallContext(ctx, &hash, "eth_sendRawTransaction", input); err != nil {
				return hash, err
			}
			return hash, nil
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

type evmOnlyRPCServer struct {
	listener net.Listener
	http     *http.Server
	rpc      *ethrpc.Server
}

func startEVMOnlyRPC(backend *rpccore.Environment) (*evmOnlyRPCServer, error) {
	rpcServer, err := newEVMOnlyRPCHandler(backend)
	if err != nil {
		return nil, err
	}
	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", evmOnlyRPCListenAddress)
	if err != nil {
		rpcServer.Stop()
		return nil, fmt.Errorf("listen for EVM-only RPC on %s: %w", evmOnlyRPCListenAddress, err)
	}
	return &evmOnlyRPCServer{
		listener: listener,
		http: &http.Server{
			Handler:           rpcServer,
			ReadHeaderTimeout: 5 * time.Second,
		},
		rpc: rpcServer,
	}, nil
}

func newEVMOnlyRPCHandler(backend evmOnlyRPCBackend) (*ethrpc.Server, error) {
	rpcServer := ethrpc.NewServer()
	if err := rpcServer.RegisterName("eth", &evmOnlySendAPI{backend: backend}); err != nil {
		return nil, fmt.Errorf("register EVM-only RPC: %w", err)
	}
	return rpcServer, nil
}

func (s *evmOnlyRPCServer) Serve(ctx context.Context) error {
	logger.Info("Starting Autobahn EVM-only RPC server", "laddr", s.listener.Addr())
	err := s.http.Serve(s.listener)
	if errors.Is(err, http.ErrServerClosed) && ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}

func (s *evmOnlyRPCServer) Stop() {
	s.rpc.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), evmOnlyRPCShutdownWait)
	defer cancel()
	if err := s.http.Shutdown(ctx); err != nil {
		logger.Error("EVM-only RPC graceful shutdown failed", "err", err)
		_ = s.http.Close()
	}
	_ = s.listener.Close()
}
