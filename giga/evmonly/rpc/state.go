package rpc

import (
	"context"
	"errors"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethrpc "github.com/ethereum/go-ethereum/rpc"
)

var errHistoricalStateUnsupported = errors.New("historical state is not supported by EVM-only RPC")

type stateAPI struct {
	backend Backend
}

// GetBalance returns the address balance from the current committed EVM state.
func (api *stateAPI) GetBalance(_ context.Context, address common.Address, block ethrpc.BlockNumberOrHash) (*hexutil.Big, error) {
	if err := requireCurrentState(block); err != nil {
		return nil, err
	}
	balance := api.backend.EvmBalance(address)
	return (*hexutil.Big)(balance.ToBig()), nil
}

// GetCode returns the contract code at address from the current committed EVM
// state. An account without code yields empty bytes, encoded as "0x".
func (api *stateAPI) GetCode(_ context.Context, address common.Address, block ethrpc.BlockNumberOrHash) (hexutil.Bytes, error) {
	if err := requireCurrentState(block); err != nil {
		return nil, err
	}
	code, err := api.backend.EvmCode(address)
	if err != nil {
		return nil, err
	}
	return code, nil
}

func requireCurrentState(block ethrpc.BlockNumberOrHash) error {
	number, ok := block.Number()
	if !ok {
		return errHistoricalStateUnsupported
	}
	switch number {
	case ethrpc.LatestBlockNumber, ethrpc.SafeBlockNumber, ethrpc.FinalizedBlockNumber, ethrpc.PendingBlockNumber:
		return nil
	default:
		return errHistoricalStateUnsupported
	}
}
