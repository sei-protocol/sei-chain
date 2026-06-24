//go:build inprocess

package app

import "github.com/sei-protocol/sei-chain/evmrpc"

// This file holds the harness-only accessors for App's EVM serve plumbing. They
// are gated behind the `inprocess` build tag so production App's public surface
// does not widen — only the in-process harness (which builds with that tag) sees
// them. The backing fields and the reportEVMServeErr/recoverEVMServe helpers stay
// in untagged app.go because the production serve goroutines use them.

// SetEVMServeErr registers the channel that EVM listener Start() (listener-start)
// failures are sent to, replacing the default fail-loud panic. An in-process host
// that runs multiple apps in one process calls this before the first block so one
// node's listener-start failure is a reportable error rather than a process-wide
// panic. The channel should be buffered (>= 2: one HTTP + one WS listener).
func (app *App) SetEVMServeErr(ch chan<- error) { app.evmServeErr = ch }

// EVMHTTPServer returns the EVM JSON-RPC HTTP listener constructed in
// RegisterLocalServices, or nil if HTTP serving is disabled. An embedding
// orchestrator calls Stop() on it at teardown.
func (app *App) EVMHTTPServer() evmrpc.EVMServer { return app.evmHTTPServer }

// EVMWebSocketServer returns the EVM JSON-RPC WebSocket listener, or nil if WS
// serving is disabled.
func (app *App) EVMWebSocketServer() evmrpc.EVMServer { return app.evmWSServer }
