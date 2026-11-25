package p2p

import (
	"context"
	"errors"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethrpc "github.com/ethereum/go-ethereum/rpc"

	abevm "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/evm"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/producer"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

// gigaEvmRouting selects remote shard owners and local transaction admission.
type gigaEvmRouting interface {
	EvmProxy(common.Address) utils.Option[*ethrpc.Client]
	Mempool() utils.Option[*producer.State]
}

// insertTx submits input to its sender's shard and returns the transaction hash.
func (r *gigaRouterCommon) insertTx(ctx context.Context, input types.Tx, router gigaEvmRouting) (common.Hash, error) {
	tx, err := r.signer.ParseTx(input)
	if err != nil {
		return common.Hash{}, err
	}
	hash := tx.Hash()
	if client, ok := router.EvmProxy(tx.Sender()).Get(); ok {
		err := client.CallContext(ctx, &hash, "eth_sendRawTransaction", hexutil.Bytes(input))
		return hash, err
	}
	mempool, ok := router.Mempool().Get()
	if !ok {
		return hash, errors.New("autobahn fullnode has no local mempool; transactions must be sent to a validator")
	}
	// Local admission reuses the verified sender through the shared signer.
	r.signer.Remember(hash, tx.Sender())
	if err := mempool.InsertTx(ctx, input); err != nil {
		r.signer.Forget(hash)
		return hash, err
	}
	return hash, nil
}

// evmPendingNonce returns the next nonce admitted by the sender's shard.
func (r *gigaRouterCommon) evmPendingNonce(ctx context.Context, address common.Address, router gigaEvmRouting) (uint64, error) {
	if client, ok := router.EvmProxy(address).Get(); ok {
		var nonce hexutil.Uint64
		if err := client.CallContext(ctx, &nonce, "eth_getTransactionCount", address, ethrpc.BlockNumberOrHashWithNumber(ethrpc.PendingBlockNumber)); err != nil {
			return 0, err
		}
		return uint64(nonce), nil
	}
	mp, ok := router.Mempool().Get()
	if !ok {
		return 0, abevm.ErrPendingNonceUnavailable
	}
	return mp.EvmNextPendingNonce(address), nil
}
