package core

import "github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"

// EVMEnvironment combines node RPC services with an EVM application.
type EVMEnvironment struct {
	*Environment
	*proxy.EVMProxy
}
