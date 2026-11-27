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

// evmProxy returns the EVM RPC client of the lane that this node assigns to
// sender, skipping lanes it has no EVM RPC client for. It returns None on a
// validator, and on a fullnode that has a client for no lane of the committee.
func (r *GigaRouterCommon) evmProxy(sender common.Address) utils.Option[*ethrpc.Client] {
	lanes, ok := r.lanes.Get()
	if !ok {
		return utils.None[*ethrpc.Client]()
	}
	assigner := lanes.For(r.data.Anchor().Load().NextHeightEpoch().Committee())
	for proxies := range r.proxies.RLock() {
		reachable := func(v atypes.PublicKey) bool { _, ok := proxies[v]; return ok }
		if v, ok := assigner.Lane(sender, reachable).Get(); ok {
			return utils.Some(proxies[v])
		}
	}
	return utils.None[*ethrpc.Client]()
}

// InsertTx submits input to the lane of its sender and returns the transaction hash.
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

// EvmPendingNonce returns the next nonce admitted by the lane of the sender.
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
