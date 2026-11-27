package evm

import (
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
)

// ResponseInfo describes the application's committed state.
type ResponseInfo struct {
	Data             string
	Version          string
	AppVersion       uint64
	LastBlockHeight  int64
	LastBlockAppHash []byte
	MinimumGasPrices string
}

// CommitteeWeights maps Autobahn validator keys to voting weights.
type CommitteeWeights = map[types.PublicKey]uint64

// InitConfig contains the initial block height and fixed block gas limit.
type InitConfig struct {
	InitialHeight types.RoadHeight
	GasLimit      uint64
}

// FinalizeBlockReq contains a block to execute.
type FinalizeBlockReq struct {
	Height types.RoadHeight
	Time   time.Time
	Hash   common.Hash
	Txs    []EvmTx
}

// FinalizeBlockResp contains the gas consumed by a finalized block.
type FinalizeBlockResp struct{ GasUsed uint64 }

// AdmittedTx contains app-computed metadata for an accepted EVM transaction.
type AdmittedTx struct {
	GasEstimated uint64
}

// BlockRef contains the execution context of a historical EVM block.
type BlockRef struct {
	Height     types.RoadHeight
	Time       uint64
	ParentHash common.Hash
	// BaseFee is the base fee the block ran at.
	BaseFee *big.Int
}

type tx = ethtypes.Transaction

// EvmTx is a decoded replay-protected Ethereum transaction with a verified sender.
type EvmTx struct {
	*tx
	sender common.Address
}

func (tx EvmTx) Sender() common.Address { return tx.sender }

// Transaction returns the underlying Ethereum transaction for read-only use.
func (tx EvmTx) Transaction() *ethtypes.Transaction { return tx.tx }
