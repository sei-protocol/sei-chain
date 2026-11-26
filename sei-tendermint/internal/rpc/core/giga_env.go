package core

import (
	"context"

	"github.com/ethereum/go-ethereum/common"

	abevm "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/evm"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/execution"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

// GigaEnvironment provides RPC services for Autobahn and the EVM-only application.
type GigaEnvironment struct {
	*abevm.Proxy
	Router *p2p.GigaRouterCommon
	// Executor runs the Autobahn blocks.
	Executor *execution.Executor
	GenDoc   *types.GenesisDoc
}

// BlockByNumber returns a finalized EVM block at height, or the latest servable height when omitted.
// Future heights return None; pruned or unrepresentable heights return a pruning error.
func (env *GigaEnvironment) BlockByNumber(ctx context.Context, height utils.Option[int64]) (utils.Option[*abevm.EvmBlock], error) {
	return env.Router.BlockByNumber(ctx, height)
}

// BlockByHash returns the finalized EVM block with hash, or None when unknown.
func (env *GigaEnvironment) BlockByHash(ctx context.Context, hash common.Hash) (utils.Option[*abevm.EvmBlock], error) {
	return env.Router.BlockByHash(ctx, hash)
}

// InsertTx submits input to its sender's shard and returns the transaction hash.
func (env *GigaEnvironment) InsertTx(ctx context.Context, input abevm.RawTx) (common.Hash, error) {
	return env.Router.InsertTx(ctx, input)
}

// EvmTransactionCount returns the committed EVM nonce for address.
func (env *GigaEnvironment) EvmTransactionCount(address common.Address) uint64 {
	return env.EvmNonce(address)
}

// EvmPendingNonce returns the next nonce admitted by the sender's shard.
func (env *GigaEnvironment) EvmPendingNonce(ctx context.Context, address common.Address) (uint64, error) {
	return env.Router.EvmPendingNonce(ctx, address)
}

// GenesisInitialHeight returns the height of the chain's first block.
func (env *GigaEnvironment) GenesisInitialHeight() int64 { return env.GenDoc.InitialHeight }
