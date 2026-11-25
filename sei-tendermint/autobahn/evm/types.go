package evm

import (
	"errors"
	"time"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
)

// RawTx is an encoded Ethereum transaction.
type RawTx []byte

// Hash returns the transaction hash of the encoded transaction.
func (tx RawTx) Hash() common.Hash {
	return crypto.Keccak256Hash(tx)
}

// ErrPendingNonceUnavailable indicates that neither a connected shard owner nor
// a local mempool can serve the pending nonce.
var ErrPendingNonceUnavailable = errors.New("pending nonce unavailable: no local mempool and the sender's lane is not connected")

// ResponseInfo describes the application's committed state.
type ResponseInfo struct {
	LastBlockHeight  int64
	LastBlockAppHash []byte
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

// EvmBlock contains finalized block data used by EVM RPCs.
type EvmBlock struct {
	Hash    common.Hash
	Height  types.RoadHeight
	Time    time.Time
	Txs     [][]byte
	ChainID string
}
