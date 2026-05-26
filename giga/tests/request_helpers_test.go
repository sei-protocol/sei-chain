package giga_test

import (
	"time"

	"github.com/sei-protocol/sei-chain/app"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
)

func finalizeBlockToBlockProcessRequest(req *abci.RequestFinalizeBlock) *app.BlockProcessRequest {
	var height int64
	var blockTime time.Time
	var hash []byte
	var byz []abci.Misbehavior

	if req != nil {
		hash = req.Hash
		byz = req.ByzantineValidators
		if req.Header != nil {
			height = req.Header.Height
			blockTime = req.Header.Time
		}
	}

	return &app.BlockProcessRequest{
		Hash:                hash,
		ByzantineValidators: byz,
		Height:              height,
		Time:                blockTime,
	}
}
