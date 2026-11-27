package p2p

import (
	"context"
	"errors"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethrpc "github.com/ethereum/go-ethereum/rpc"

	abevm "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/evm"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
)

// BlockByNumber returns the finalized EVM block at road n.
func (r *GigaRouterCommon) BlockByNumber(ctx context.Context, n atypes.RoadHeight) (*abevm.EvmBlock, error) {
	block, err := r.data.SuperBlock(ctx, n)
	if err != nil {
		// Keep the RPC pruning sentinel for callers that distinguish pruned heights.
		// The active lower bound lives in the block store's prune watermark.
		if errors.Is(err, atypes.ErrPruned) {
			return nil, coretypes.WrapErrHeightNotAvailable(utils.Clamp[int64](n), utils.None[int64]())
		}
		return nil, fmt.Errorf("data.SuperBlock(%v): %w", n, err)
	}
	return r.evmBlock(block), nil
}

// BlockByHash returns the finalized EVM block with hash, or None when unknown.
func (r *GigaRouterCommon) BlockByHash(ctx context.Context, hash common.Hash) (utils.Option[*abevm.EvmBlock], error) {
	block, err := r.data.SuperBlockByHash(atypes.RoadBlockHeaderHash(hash))
	if err != nil {
		return utils.None[*abevm.EvmBlock](), fmt.Errorf("data.SuperBlockByHash: %w", err)
	}
	return block.Map(r.evmBlock), nil
}

func (r *GigaRouterCommon) evmBlock(block *atypes.SuperBlock) *abevm.EvmBlock {
	return &abevm.EvmBlock{
		Hash:       common.Hash(block.Hash),
		ParentHash: common.Hash(block.ParentHash.Or(atypes.RoadBlockHeaderHash{})),
		Height:     block.Height,
		Time:       block.Timestamp,
		Txs:        block.Txs,
		ChainID:    r.cfg.GenDoc.ChainID,
	}
}

// evmProxy returns the connected shard owner's client, or None for local admission.
func (r *GigaRouterCommon) evmProxy(sender common.Address) utils.Option[*ethrpc.Client] {
	self, isValidator := r.validatorKey.Get()
	if isValidator && !r.cfg.EnableEvmProxy {
		return utils.None[*ethrpc.Client]()
	}
	// TODO: this will cause a mess at the epoch boundary (inconsistent anchor => inconsistent proxying).
	// But once we remove proxying from validator nodes it should be fine.
	validator := r.data.Anchor().Load().NextHeightEpoch().Committee().EvmShard(sender)
	if isValidator && self == validator {
		return utils.None[*ethrpc.Client]()
	}
	for proxies := range r.proxies.RLock() {
		client, ok := proxies[validator]
		if ok {
			return utils.Some(client)
		}
	}
	return utils.None[*ethrpc.Client]()
}

// InsertTx submits input to its sender's shard and returns the transaction hash.
func (r *GigaRouterCommon) InsertTx(ctx context.Context, input abevm.RawTx) (common.Hash, error) {
	tx, err := r.signer.ParseTx(input)
	if err != nil {
		return common.Hash{}, err
	}
	hash := tx.Hash()
	if client, ok := r.evmProxy(tx.Sender()).Get(); ok {
		err := client.CallContext(ctx, &hash, "eth_sendRawTransaction", hexutil.Bytes(input))
		return hash, err
	}
	mempool, ok := r.mempool.Get()
	if !ok {
		return hash, errors.New("mempool unavailable")
	}
	if err := mempool.InsertTx(ctx, tx); err != nil {
		return hash, err
	}
	r.signer.Remember(tx.EvmTx())
	return hash, nil
}

// EvmPendingNonce returns the next nonce admitted by the sender's shard.
func (r *GigaRouterCommon) EvmPendingNonce(ctx context.Context, address common.Address) (uint64, error) {
	if client, ok := r.evmProxy(address).Get(); ok {
		var nonce hexutil.Uint64
		if err := client.CallContext(ctx, &nonce, "eth_getTransactionCount", address, ethrpc.BlockNumberOrHashWithNumber(ethrpc.PendingBlockNumber)); err != nil {
			return 0, err
		}
		return uint64(nonce), nil
	}
	mp, ok := r.mempool.Get()
	if !ok {
		return 0, abevm.ErrPendingNonceUnavailable
	}
	return mp.EvmNextPendingNonce(address), nil
}
