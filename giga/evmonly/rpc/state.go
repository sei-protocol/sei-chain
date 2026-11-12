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
	"github.com/holiman/uint256"

	tmbytes "github.com/sei-protocol/sei-chain/sei-tendermint/libs/bytes"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
)

var (
	errBlockNotSpecified   = errors.New("neither block number nor hash specified")
	errBlockNotFound       = errors.New("block not found")
	errBlockAboveHead      = errors.New("block is above the latest block")
	errBlockTagUnsupported = errors.New("block tag is not supported by EVM-only RPC")
	errStorageKeyInvalid   = errors.New("hex string invalid")
	errStorageKeyTooLong   = errors.New("hex string too long, want at most 32 bytes")
)

type stateAPI struct {
	backend Backend
}

// GetBalance returns the address balance in the committed EVM state after block.
func (api *stateAPI) GetBalance(ctx context.Context, address common.Address, block ethrpc.BlockNumberOrHash) (*hexutil.Big, error) {
	height, latest, err := stateHeight(ctx, api.backend, block)
	if err != nil {
		return nil, err
	}
	var balance uint256.Int
	if latest {
		balance = api.backend.EvmBalance(address)
	} else if balance, err = api.backend.EvmBalanceAt(address, height); err != nil {
		return nil, err
	}
	return (*hexutil.Big)(balance.ToBig()), nil
}

// GetCode returns the contract code at address in the committed EVM state after
// block. An account without code yields empty bytes, encoded as "0x".
func (api *stateAPI) GetCode(ctx context.Context, address common.Address, block ethrpc.BlockNumberOrHash) (hexutil.Bytes, error) {
	height, latest, err := stateHeight(ctx, api.backend, block)
	if err != nil {
		return nil, err
	}
	if latest {
		return api.backend.EvmCode(address)
	}
	return api.backend.EvmCodeAt(address, height)
}

// stateHeight returns the block a state query reads: the latest committed state for a latest tag,
// or else the block a number or hash names, with earliest as block 0 and requireCanonical ignored.
func stateHeight(ctx context.Context, backend Backend, block ethrpc.BlockNumberOrHash) (height int64, latest bool, err error) {
	if hash, ok := block.Hash(); ok {
		if height, err = blockHeightByHash(ctx, backend, hash); err != nil {
			return 0, false, err
		}
	} else {
		number, ok := block.Number()
		switch {
		case !ok:
			return 0, false, errBlockNotSpecified
		case isLatestTag(number):
			return 0, true, nil
		case number == ethrpc.EarliestBlockNumber:
			return 0, false, nil
		case number < 0:
			return 0, false, fmt.Errorf("%w: %d", errBlockTagUnsupported, number)
		}
		height = number.Int64()
	}
	head := backend.EvmBlockNumber()
	if uint64(height) > head { //nolint:gosec // non-negative
		return 0, false, fmt.Errorf("%w: block %d, latest %d", errBlockAboveHead, height, head)
	}
	return height, false, nil
}

// blockHeightByHash returns the height of the stored block with hash.
func blockHeightByHash(ctx context.Context, backend Backend, hash common.Hash) (int64, error) {
	block, err := backend.BlockByHash(ctx, &coretypes.RequestBlockByHash{Hash: tmbytes.HexBytes(hash.Bytes())})
	if err != nil {
		return 0, err
	}
	if block == nil || block.Block == nil {
		return 0, fmt.Errorf("%w: %s", errBlockNotFound, hash)
	}
	return block.Block.Height, nil
}

// isLatestTag reports whether number is a tag answered from the latest committed state.
func isLatestTag(number ethrpc.BlockNumber) bool {
	switch number {
	case ethrpc.LatestBlockNumber, ethrpc.SafeBlockNumber, ethrpc.FinalizedBlockNumber, ethrpc.PendingBlockNumber:
		return true
	}
	return false
}

// GetStorageAt returns the 32-byte value at hexKey in address's storage in the
// committed EVM state after block. An unset slot yields 32 zero bytes.
func (api *stateAPI) GetStorageAt(ctx context.Context, address common.Address, hexKey string, block ethrpc.BlockNumberOrHash) (hexutil.Bytes, error) {
	height, latest, err := stateHeight(ctx, api.backend, block)
	if err != nil {
		return nil, err
	}
	key, err := decodeStorageKey(hexKey)
	if err != nil {
		return nil, fmt.Errorf("unable to decode storage key: %w", err)
	}
	var value common.Hash
	if latest {
		value, err = api.backend.EvmStorage(address, key)
	} else {
		value, err = api.backend.EvmStorageAt(address, key, height)
	}
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
