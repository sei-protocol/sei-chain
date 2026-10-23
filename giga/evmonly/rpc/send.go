package rpc

import (
	"context"
	"errors"
	"fmt"
	"sync"

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
	// senders remembers the sender recovered for each transaction routed here, so a retry of the
	// same transaction is routed without recovering its signature again.
	senders senderCache
}

// senderCacheCap bounds the senders a senderCache remembers per generation.
const senderCacheCap = 1 << 18

// senderCache maps transaction hashes to their recovered senders, keeping two generations so the
// most recent entries survive a rollover. The zero value is ready to use.
type senderCache struct {
	mu           sync.Mutex
	fresh, stale map[common.Hash]common.Address
}

func (c *senderCache) get(hash common.Hash) (common.Address, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if sender, ok := c.fresh[hash]; ok {
		return sender, true
	}
	sender, ok := c.stale[hash]
	return sender, ok
}

func (c *senderCache) put(hash common.Hash, sender common.Address) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.fresh == nil || len(c.fresh) >= senderCacheCap {
		c.stale, c.fresh = c.fresh, make(map[common.Hash]common.Address, senderCacheCap)
	}
	c.fresh[hash] = sender
}

// SendRawTransaction submits a signed raw Ethereum transaction to Autobahn and
// returns its Ethereum transaction hash.
func (api *sendAPI) SendRawTransaction(ctx context.Context, input hexutil.Bytes) (common.Hash, error) {
	tx := new(ethtypes.Transaction)
	if err := tx.UnmarshalBinary(input); err != nil {
		return common.Hash{}, err
	}
	hash := tx.Hash()

	if client, ok := api.shardProxy(tx, hash).Get(); ok {
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
func (api *sendAPI) shardProxy(tx *ethtypes.Transaction, hash common.Hash) utils.Option[*ethrpc.Client] {
	if !api.backend.EvmProxyEnabled() {
		return utils.None[*ethrpc.Client]()
	}
	sender, ok := api.senders.get(hash)
	if !ok {
		var err error
		sender, err = ethtypes.Sender(ethtypes.LatestSignerForChainID(tx.ChainId()), tx)
		if err != nil {
			return utils.None[*ethrpc.Client]()
		}
		api.senders.put(hash, sender)
	}
	return api.backend.EvmProxy(sender)
}
