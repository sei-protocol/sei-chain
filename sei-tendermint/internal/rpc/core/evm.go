package core

import (
	"github.com/ethereum/go-ethereum/common"
	abevm "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/evm"
)

// EVMEnvironment combines node RPC services with an EVM application.
type EVMEnvironment struct {
	*Environment
	*abevm.Proxy
}

// RememberEvmSender records a verified sender for a locally handled transaction.
func (env *EVMEnvironment) RememberEvmSender(hash common.Hash, sender common.Address) {
	env.evmSigner().Remember(hash, sender)
}

// ForgetEvmSender removes a locally handled transaction from the sender cache.
func (env *EVMEnvironment) ForgetEvmSender(hash common.Hash) {
	env.evmSigner().Forget(hash)
}

func (env *Environment) evmSigner() *abevm.Signer {
	return env.EVMSigner.OrPanic("EVM transaction signer is required")
}
