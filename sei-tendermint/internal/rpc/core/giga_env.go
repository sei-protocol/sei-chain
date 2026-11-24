package core

import (
	"context"
	"errors"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	ethrpc "github.com/ethereum/go-ethereum/rpc"

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
	Router p2p.GigaRouter
	// Executor runs the Autobahn blocks. None when execution is unavailable.
	Executor utils.Option[*execution.Executor]
	GenDoc   *types.GenesisDoc
}

// BlockByNumber returns the finalized EVM block at height, or the latest executed height when omitted.
func (env *GigaEnvironment) BlockByNumber(ctx context.Context, height utils.Option[int64]) (*abevm.EvmBlock, error) {
	var heightPtr *int64
	if h, ok := height.Get(); ok {
		heightPtr = &h
	}
	resolved, err := resolveHeight(env.Proxy.LastBlockHeight(), heightPtr)
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

// BroadcastTx submits tx to the local mempool and returns its admission error, if any.
func (env *GigaEnvironment) BroadcastTx(ctx context.Context, tx types.Tx) error {
	mempool, ok := env.Router.Mempool().Get()
	if !ok {
		return errors.New("autobahn fullnode has no local mempool; transactions must be sent to a validator")
	}
	_, err := mempool.InsertTx(ctx, tx)
	return err
}

// EvmProxy returns the EVM RPC client of the Autobahn validator that owns the
// sender shard, or None if the sender maps to the local validator.
func (env *GigaEnvironment) EvmProxy(sender common.Address) utils.Option[*ethrpc.Client] {
	return env.Router.EvmProxy(sender)
}

// EvmProxyEnabled reports whether EvmProxy can return a client for any sender.
func (env *GigaEnvironment) EvmProxyEnabled() bool {
	return env.Router.EvmProxyEnabled()
}

// RememberEvmSender records a verified sender for local admission through Autobahn.
func (env *GigaEnvironment) RememberEvmSender(hash common.Hash, sender common.Address) {
	env.Router.RememberEvmSender(hash, sender)
}

// ForgetEvmSender removes a transaction's cached sender through Autobahn.
func (env *GigaEnvironment) ForgetEvmSender(hash common.Hash) {
	env.Router.ForgetEvmSender(hash)
}

// EvmTransactionCount returns the committed EVM nonce for address.
func (env *GigaEnvironment) EvmTransactionCount(address common.Address) uint64 {
	return env.Proxy.EvmNonce(address)
}

// EvmPendingNonce returns the next nonce admitted by the local mempool, or None on fullnodes.
func (env *GigaEnvironment) EvmPendingNonce(address common.Address) utils.Option[uint64] {
	mp, ok := env.Router.Mempool().Get()
	if !ok {
		return utils.None[uint64]()
	}
	return utils.Some(mp.EvmNextPendingNonce(address))
}

// EvmBlockNumber returns the height of the most recently committed block.
func (env *GigaEnvironment) EvmBlockNumber() uint64 {
	return utils.Clamp[uint64](env.Proxy.LastBlockHeight())
}

// GenesisInitialHeight returns the height of the chain's first block.
func (env *GigaEnvironment) GenesisInitialHeight() int64 { return env.GenDoc.InitialHeight }
