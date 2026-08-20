package util

import (
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
)

func EmitEvent(logs precompiles.LogSink, address common.Address, event abi.Event, indexed common.Address, args ...interface{}) {
	if logs == nil {
		return
	}
	data, err := event.Inputs.NonIndexed().Pack(args...)
	if err != nil {
		return
	}
	logs.AddLog(&ethtypes.Log{
		Address: address,
		Topics: []common.Hash{
			event.ID,
			common.BytesToHash(indexed.Bytes()),
		},
		Data: data,
	})
}
