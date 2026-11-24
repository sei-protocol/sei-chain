package core

import (
	"context"
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	ethrpc "github.com/ethereum/go-ethereum/rpc"

	abevm "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/evm"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/execution"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

var (
	errUnprotectedTx  = errors.New("only replay-protected (EIP-155) transactions allowed over RPC")
	errInvalidChainID = errors.New("invalid chain-id")
)

// GigaEnvironment provides RPC services for Autobahn and the EVM-only application.
type GigaEnvironment struct {
	*abevm.Proxy
	Router p2p.GigaRouter
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

// InsertTx submits input to its sender's shard and returns the transaction hash.
func (env *GigaEnvironment) InsertTx(ctx context.Context, input types.Tx) (common.Hash, error) {
	tx := new(ethtypes.Transaction)
	if err := tx.UnmarshalBinary(input); err != nil {
		return common.Hash{}, err
	}
	chainID := new(big.Int).SetUint64(env.Proxy.EvmChainID())
	if err := checkChainID(tx, chainID); err != nil {
		return common.Hash{}, err
	}
	hash := tx.Hash()
	if client, ok := env.shardProxy(tx, chainID).Get(); ok {
		err := client.CallContext(ctx, &hash, "eth_sendRawTransaction", hexutil.Bytes(input))
		return hash, err
	}
	if err := env.insertLocalTx(ctx, input); err != nil {
		env.Router.ForgetEvmSender(hash)
		return hash, err
	}
	return hash, nil
}

func (env *GigaEnvironment) insertLocalTx(ctx context.Context, input types.Tx) error {
	mempool, ok := env.Router.Mempool().Get()
	if !ok {
		return errors.New("autobahn fullnode has no local mempool; transactions must be sent to a validator")
	}
	_, err := mempool.InsertTx(ctx, input)
	return err
}

// shardProxy returns the shard owner's client, remembering verified senders handled locally.
func (env *GigaEnvironment) shardProxy(tx *ethtypes.Transaction, chainID *big.Int) utils.Option[*ethrpc.Client] {
	// Sender recovery is skipped entirely when the router has no proxies,
	// since CheckTx recovers it anyway.
	if !env.Router.EvmProxyEnabled() {
		return utils.None[*ethrpc.Client]()
	}
	sender, err := ethtypes.Sender(ethtypes.LatestSignerForChainID(chainID), tx)
	if err != nil {
		return utils.None[*ethrpc.Client]()
	}
	proxy := env.Router.EvmProxy(sender)
	if !proxy.IsPresent() {
		// Local admission reuses the verified sender through the router's cache.
		env.Router.RememberEvmSender(tx.Hash(), sender)
	}
	return proxy
}

// checkChainID rejects tx unless it is replay-protected and signed for chainID.
func checkChainID(tx *ethtypes.Transaction, chainID *big.Int) error {
	if !tx.Protected() {
		return errUnprotectedTx
	}
	if tx.ChainId().Sign() == 0 || tx.ChainId().Cmp(chainID) != 0 {
		return errInvalidChainID
	}
	return nil
}

// EvmTransactionCount returns the committed EVM nonce for address.
func (env *GigaEnvironment) EvmTransactionCount(address common.Address) uint64 {
	return env.Proxy.EvmNonce(address)
}

// EvmPendingNonce returns the next nonce admitted by the sender's shard.
func (env *GigaEnvironment) EvmPendingNonce(ctx context.Context, address common.Address) (uint64, error) {
	if client, ok := env.Router.EvmProxy(address).Get(); ok {
		var nonce hexutil.Uint64
		if err := client.CallContext(ctx, &nonce, "eth_getTransactionCount", address, ethrpc.BlockNumberOrHashWithNumber(ethrpc.PendingBlockNumber)); err != nil {
			return 0, err
		}
		return uint64(nonce), nil
	}
	mp, ok := env.Router.Mempool().Get()
	if !ok {
		return 0, abevm.ErrPendingNonceUnavailable
	}
	return mp.EvmNextPendingNonce(address), nil
}

// EvmBlockNumber returns the height of the most recently committed block.
func (env *GigaEnvironment) EvmBlockNumber() uint64 {
	return utils.Clamp[uint64](env.Proxy.LastBlockHeight())
}

// GenesisInitialHeight returns the height of the chain's first block.
func (env *GigaEnvironment) GenesisInitialHeight() int64 { return env.GenDoc.InitialHeight }
