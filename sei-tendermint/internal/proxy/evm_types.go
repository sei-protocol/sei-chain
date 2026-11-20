package proxy

import (
	"time"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
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
	InitialHeight types.RoadIndex
	GasLimit      uint64
}

// FinalizeBlockReq contains a block and its consensus-owned epoch data.
type FinalizeBlockReq struct {
	Height     types.RoadIndex
	Time       time.Time
	Hash       common.Hash
	Txs        []EvmTx
	EpochState utils.Option[EpochState]
}

// FinalizeBlockResp contains the gas consumed by a finalized block.
type FinalizeBlockResp struct{ GasUsed uint64 }

// AdmittedTx contains app-computed metadata for an accepted EVM transaction.
type AdmittedTx struct {
	GasEstimated uint64
}

// EpochState is consensus-owned epoch data committed unchanged with a block.
// Both values are written as given; an empty value is committed as an empty row.
type EpochState struct {
	EpochRegistry []byte
	NextEpoch     []byte
}

// BlockRef contains the execution context of a historical EVM block.
type BlockRef struct {
	Height     types.RoadIndex
	Time       uint64
	ParentHash common.Hash
}

type tx = ethtypes.Transaction

// EvmTx is a decoded replay-protected Ethereum transaction with verified signature and sender.
type EvmTx struct {
	*tx
	sender common.Address
}

func (tx EvmTx) Sender() common.Address { return tx.sender }

// Transaction returns the underlying Ethereum transaction for read-only use.
func (tx EvmTx) Transaction() *ethtypes.Transaction { return tx.tx }
