package core

import (
	"context"
	"fmt"

	"github.com/ethereum/go-ethereum/common"

	abevm "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/evm"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
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

// BlockByNumber returns the finalized EVM block at height, or the latest executed height when omitted.
func (env *GigaEnvironment) BlockByNumber(ctx context.Context, height utils.Option[int64]) (*abevm.EvmBlock, error) {
	var heightPtr *int64
	if h, ok := height.Get(); ok {
		heightPtr = &h
	}
	resolved, err := resolveHeight(env.LastBlockHeight(), heightPtr)
	if err != nil {
		return nil, err
	}
	roadHeight, ok := utils.SafeCast[atypes.RoadHeight](resolved)
	if !ok {
		return nil, fmt.Errorf("invalid height %d", resolved)
	}
	return env.Router.BlockByNumber(ctx, roadHeight)
}

// BlockByHash returns the finalized EVM block with hash, or None when unknown.
func (env *GigaEnvironment) BlockByHash(ctx context.Context, hash common.Hash) (utils.Option[*abevm.EvmBlock], error) {
	return env.Router.BlockByHash(ctx, hash)
}

// InsertTx submits input to the lane of its sender and returns the transaction hash.
func (env *GigaEnvironment) InsertTx(ctx context.Context, input abevm.RawTx) (common.Hash, error) {
	return env.Router.InsertTx(ctx, input)
}

// EvmTransactionCount returns the committed EVM nonce for address.
func (env *GigaEnvironment) EvmTransactionCount(address common.Address) uint64 {
	return env.EvmNonce(address)
}

// EvmPendingNonce returns the next nonce admitted by the lane of the sender.
func (env *GigaEnvironment) EvmPendingNonce(ctx context.Context, address common.Address) (uint64, error) {
	return env.Router.EvmPendingNonce(ctx, address)
}

// EvmBlockNumber returns the height of the most recently committed block.
func (env *GigaEnvironment) EvmBlockNumber() uint64 {
	return utils.Clamp[uint64](env.LastBlockHeight())
}

// GenesisInitialHeight returns the height of the chain's first block.
func (env *GigaEnvironment) GenesisInitialHeight() int64 { return env.GenDoc.InitialHeight }
