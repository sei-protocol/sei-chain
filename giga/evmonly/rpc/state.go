package rpc

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethrpc "github.com/ethereum/go-ethereum/rpc"
)

var (
	errHistoricalStateUnsupported = errors.New("historical state is not supported by EVM-only RPC")
	errStorageKeyInvalid          = errors.New("hex string invalid")
	errStorageKeyTooLong          = errors.New("hex string too long, want at most 32 bytes")
)

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

// GetStorageAt returns the 32-byte value at hexKey in address's storage from
// the current committed EVM state. An unset slot yields 32 zero bytes.
func (api *stateAPI) GetStorageAt(_ context.Context, address common.Address, hexKey string, block ethrpc.BlockNumberOrHash) (hexutil.Bytes, error) {
	if err := requireCurrentState(block); err != nil {
		return nil, err
	}
	key, err := decodeStorageKey(hexKey)
	if err != nil {
		return nil, fmt.Errorf("unable to decode storage key: %w", err)
	}
	value, err := api.backend.EvmStorage(address, key)
	if err != nil {
		return nil, err
	}
	return value[:], nil
}

// decodeStorageKey decodes a hex storage slot of at most 32 bytes, with or
// without a 0x prefix and odd length allowed, left-padding it to 32 bytes.
func decodeStorageKey(s string) (common.Hash, error) {
	if strings.HasPrefix(s, "0x") || strings.HasPrefix(s, "0X") {
		s = s[2:]
	}
	if len(s)%2 == 1 {
		s = "0" + s
	}
	b, err := hex.DecodeString(s)
	if err != nil {
		return common.Hash{}, errStorageKeyInvalid
	}
	if len(b) > common.HashLength {
		return common.Hash{}, errStorageKeyTooLong
	}
	return common.BytesToHash(b), nil
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
