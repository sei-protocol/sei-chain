package evmrpc

import (
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/tendermint/tendermint/libs/log"
)

type EVMServer interface {
	Start() error
}

func NewEVMHTTPServer(
	logger log.Logger,
	addr string,
	port int,
	timeouts rpc.HTTPTimeouts,
) (EVMServer, error) {
	httpServer := newHTTPServer(logger, timeouts)
	if err := httpServer.setListenAddr(addr, port); err != nil {
		return nil, err
	}
	apis := []rpc.API{
		{
			Namespace: "echo",
			Service:   NewEchoAPI(),
		},
	}
	if err := httpServer.enableRPC(apis, httpConfig{
		// TODO: add CORS configs and virtual host configs
	}); err != nil {
		return nil, err
	}
	return httpServer, nil
}

func NewEVMWebSocketServer(
	logger log.Logger,
	addr string,
	port int,
	origins []string,
	timeouts rpc.HTTPTimeouts,
) (EVMServer, error) {
	httpServer := newHTTPServer(logger, timeouts)
	if err := httpServer.setListenAddr(addr, port); err != nil {
		return nil, err
	}
	apis := []rpc.API{
		{
			Namespace: "echo",
			Service:   NewEchoAPI(),
		},
	}
	if err := httpServer.enableWS(apis, wsConfig{Origins: origins}); err != nil {
		return nil, err
	}
	return httpServer, nil
}
