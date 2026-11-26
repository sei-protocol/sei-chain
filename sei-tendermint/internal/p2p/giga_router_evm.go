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
)

// BlockByNumber returns a finalized EVM block at height, or the latest servable height when omitted.
// Future heights return None; pruned or unrepresentable heights return ErrPruned.
func (r *GigaRouterCommon) BlockByNumber(ctx context.Context, height utils.Option[int64]) (utils.Option[*abevm.EvmBlock], error) {
	latest := r.cfg.App.EvmBlockNumber()
	resolved := height.Or(latest)
	if resolved > latest {
		return utils.None[*abevm.EvmBlock](), nil
	}
	n, ok := utils.SafeCast[atypes.RoadHeight](resolved)
	if !ok {
		return utils.None[*abevm.EvmBlock](), abevm.ErrPruned
	}
	block, err := r.data.RoadBlock(ctx, n)
	if err != nil {
		// The active lower bound lives in the block store's prune watermark.
		if errors.Is(err, atypes.ErrPruned) {
			return utils.None[*abevm.EvmBlock](), abevm.ErrPruned
		}
		return utils.None[*abevm.EvmBlock](), fmt.Errorf("data.RoadBlock(%v): %w", n, err)
	}
	return r.evmBlock(block)
}

// BlockByHash returns the finalized EVM block with hash, or None when unknown.
func (r *GigaRouterCommon) BlockByHash(ctx context.Context, hash common.Hash) (utils.Option[*abevm.EvmBlock], error) {
	block, err := r.data.RoadBlockByHash(atypes.RoadBlockHeaderHash(hash))
	if err != nil {
		return utils.None[*abevm.EvmBlock](), fmt.Errorf("data.RoadBlockByHash: %w", err)
	}
	b, ok := block.Get()
	if !ok {
		return utils.None[*abevm.EvmBlock](), nil
	}
	return r.evmBlock(b)
}

// evmBlock returns block with its receipt-backed transactions in execution order.
func (r *GigaRouterCommon) evmBlock(block *atypes.RoadBlock) (utils.Option[*abevm.EvmBlock], error) {
	header := block.Header()
	height, ok := utils.SafeCast[int64](header.Height())
	if !ok {
		return utils.None[*abevm.EvmBlock](), fmt.Errorf("invalid block height %d", header.Height())
	}
	found, err := r.cfg.App.BlockTxsHashes(height)
	if err != nil {
		return utils.None[*abevm.EvmBlock](), err
	}
	hashes, ok := found.Get()
	if !ok {
		return utils.None[*abevm.EvmBlock](), nil
	}
	byHash := make(map[common.Hash][]byte, len(hashes))
	for _, lane := range block.LaneBlocks() {
		for _, raw := range lane.Payload().Txs() {
			byHash[abevm.RawTx(raw).Hash()] = raw
		}
	}
	txs := make([][]byte, len(hashes))
	for i, hash := range hashes {
		raw, ok := byHash[hash]
		if !ok {
			return utils.None[*abevm.EvmBlock](), fmt.Errorf("receipt transaction %s is missing from road block %d", hash, header.Height())
		}
		txs[i] = raw
	}
	return utils.Some(&abevm.EvmBlock{
		Hash:    common.Hash(header.Hash()),
		Height:  header.Height(),
		Time:    header.Timestamp(),
		Txs:     txs,
		ChainID: r.cfg.GenDoc.ChainID,
	}), nil
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
