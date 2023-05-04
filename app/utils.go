package app

import (
	"time"

	abci "github.com/tendermint/tendermint/abci/types"
)

type OptimisticProcessingInfo struct {
	Height     int64
	Hash       []byte
	Aborted    bool
	Completion chan struct{}
	// result fields
	Events       []abci.Event
	TxRes        []*abci.ExecTxResult
	EndBlockResp abci.ResponseEndBlock
}

type BlockProcessRequest interface {
	GetHash() []byte
	GetTxs() [][]byte
	GetByzantineValidators() []abci.Misbehavior
	GetHeight() int64
	GetTime() time.Time
}
