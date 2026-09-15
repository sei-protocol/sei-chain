package rpc

import (
	"context"
	"math/big"

	"github.com/ethereum/go-ethereum/common/hexutil"
)

type infoAPI struct {
	backend Backend
}

// BlockNumber returns the height of the most recently committed block.
func (api *infoAPI) BlockNumber(_ context.Context) hexutil.Uint64 {
	return hexutil.Uint64(api.backend.EvmBlockNumber())
}

// ChainId returns the EVM chain ID this Autobahn shard is configured for.
//
//nolint:revive // matches the go-ethereum RPC method name eth_chainId.
func (api *infoAPI) ChainId(_ context.Context) *hexutil.Big {
	return (*hexutil.Big)(new(big.Int).SetUint64(api.backend.EvmChainID()))
}
