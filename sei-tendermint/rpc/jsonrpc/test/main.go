package main

import (
	"context"
	"fmt"
	stdlog "log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	rpcserver "github.com/sei-protocol/sei-chain/sei-tendermint/rpc/jsonrpc/server"
	"github.com/sei-protocol/seilog"
)

var logger = seilog.NewLogger("tendermint", "rpc", "jsonrpc", "test")

var routes = map[string]*rpcserver.RPCFunc{
	"hello_world": rpcserver.NewRPCFunc(HelloWorld),
}

func HelloWorld(ctx context.Context, name string, num int) (Result, error) {
	return Result{fmt.Sprintf("hi %s %d", name, num)}, nil
}

type Result struct {
	Result string
}

func main() {
	mux := http.NewServeMux()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	rpcserver.RegisterRPCFuncs(mux, routes)
	config := rpcserver.DefaultConfig()
	listener, err := rpcserver.Listen("tcp://127.0.0.1:8008", config.MaxOpenConnections)
	if err != nil {
		stdlog.Fatalf("rpc listening: %v", err)
	}

	if err = rpcserver.Serve(ctx, listener, mux, config); err != nil {
		logger.Error("rpc serve", "err", err)
	}
}
