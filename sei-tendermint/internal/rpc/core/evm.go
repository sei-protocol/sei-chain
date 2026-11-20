package core

import (
	abevm "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/evm"
)

// EVMEnvironment combines node RPC services with an EVM application.
type EVMEnvironment struct {
	*Environment
	*abevm.Proxy
}

func (env *Environment) evmSigner() *abevm.Signer {
	return env.EVMSigner.OrPanic("EVM transaction signer is required")
}
