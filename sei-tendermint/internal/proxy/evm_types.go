package proxy

import (
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/holiman/uint256"

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

// ValidatorUpdate is an Autobahn validator and its voting power.
type ValidatorUpdate struct {
	PubKey types.PublicKey
	Power  int64
}

// RequestInitChain contains the genesis inputs for an Autobahn application.
type RequestInitChain struct {
	Time          time.Time
	ChainID       string
	InitialHeight int64
	MaxGas        int64
	AppStateBytes []byte
}

// ResponseInitChain contains the initial validator set and application hash.
type ResponseInitChain struct {
	Validators []ValidatorUpdate
	AppHash    []byte
}

// Header is the execution context of an Autobahn block.
type Header struct {
	ChainID         string
	Height          int64
	Time            time.Time
	ProposerAddress []byte
}

// RequestCheckTxV2 is a transaction submitted for admission.
type RequestCheckTxV2 struct{ Tx []byte }

// ResponseCheckTx is the admission result of a transaction.
type ResponseCheckTx struct {
	Code         uint32
	Data         []byte
	Log          string
	GasWanted    int64
	Codespace    string
	Priority     int64
	GasEstimated int64
}

func (r *ResponseCheckTx) IsOK() bool  { return r.Code == 0 }
func (r *ResponseCheckTx) IsErr() bool { return !r.IsOK() }

// ResponseCheckTxV2 contains admission results and EVM sender metadata.
type ResponseCheckTxV2 struct {
	*ResponseCheckTx
	IsEVM              bool
	EVMNonce           uint64
	EVMHash            common.Hash
	EVMSenderAddress   common.Address
	SeiSenderAddress   []byte
	EVMRequiredBalance uint256.Int
}

// RequestFinalizeBlock contains an Autobahn block and its consensus-owned epoch data.
type RequestFinalizeBlock struct {
	Txs        [][]byte
	Hash       []byte
	Header     *Header
	EpochState utils.Option[EpochState]
}

// EpochState is consensus-owned epoch data committed unchanged with a block.
// Both values are written as given; an empty value is committed as an empty row.
type EpochState struct {
	EpochRegistry []byte
	NextEpoch     []byte
}

// ResponseFinalizeBlock contains a block's execution results and application hash.
type ResponseFinalizeBlock struct {
	TxResults []*ExecTxResult
	AppHash   []byte
}

// ExecTxResult describes an executed transaction.
type ExecTxResult struct {
	Code      uint32
	Data      []byte
	Log       string
	Info      string
	GasWanted int64
	GasUsed   int64
	Codespace string
}

// ResponseCommit contains the application's block retention floor.
type ResponseCommit struct{ RetainHeight int64 }

const CodeTypeOK uint32 = 0

func (r *ExecTxResult) IsOK() bool  { return r.Code == CodeTypeOK }
func (r *ExecTxResult) IsErr() bool { return !r.IsOK() }
