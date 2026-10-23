package rpc

import (
	"context"
	"errors"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	ethrpc "github.com/ethereum/go-ethereum/rpc"

	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

type sendAPI struct {
	backend Backend
}

// SendRawTransaction submits a signed raw Ethereum transaction to Autobahn and
// returns its Ethereum transaction hash.
func (api *sendAPI) SendRawTransaction(ctx context.Context, input hexutil.Bytes) (common.Hash, error) {
	tx := new(ethtypes.Transaction)
	if err := tx.UnmarshalBinary(input); err != nil {
		return common.Hash{}, err
	}
	hash := tx.Hash()

	if client, ok := api.shardProxy(tx).Get(); ok {
		if err := client.CallContext(ctx, &hash, "eth_sendRawTransaction", input); err != nil {
			return hash, err
		}
		return hash, nil
	}

	result, err := api.backend.BroadcastTx(ctx, &coretypes.RequestBroadcastTx{
		Tx: append(tmtypes.Tx(nil), input...),
	})
	if err != nil {
		return hash, err
	}
	if result == nil {
		return hash, errors.New("missing broadcast response")
	}
	if result.Code != abci.CodeTypeOK {
		message := result.Log
		if message == "" {
			message = fmt.Sprintf("transaction rejected with code %d", result.Code)
		}
		return hash, errors.New(message)
	}
	return hash, nil
}

// shardProxy returns the RPC client of the validator owning tx's sender shard,
// or None when the transaction is handled locally. Sender recovery is skipped
// entirely when the backend has no proxies, since CheckTx recovers it anyway.
func (api *sendAPI) shardProxy(tx *ethtypes.Transaction) utils.Option[*ethrpc.Client] {
	if !api.backend.EvmProxyEnabled() {
		return utils.None[*ethrpc.Client]()
	}
	sender, err := ethtypes.Sender(ethtypes.LatestSignerForChainID(tx.ChainId()), tx)
	if err != nil {
		return utils.None[*ethrpc.Client]()
	}
	return api.backend.EvmProxy(sender)
}
