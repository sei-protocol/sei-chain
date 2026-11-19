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

	"github.com/sei-protocol/sei-chain/evmrpc"
	receiptpkg "github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"
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
	store   receiptpkg.ReceiptStore
}

// GetBalance returns the address balance in the committed EVM state after block.
func (api *stateAPI) GetBalance(ctx context.Context, address common.Address, block ethrpc.BlockNumberOrHash) (*hexutil.Big, error) {
	height, latest, err := stateHeight(ctx, api.backend, api.store, block)
	if err != nil {
		return nil, err
	}
	balance, err := readState(ctx, func() (uint256.Int, error) {
		if latest {
			return api.backend.EvmBalance(address, nil), nil
		}
		return api.backend.EvmBalanceAt(address, height)
	})
	if err != nil {
		return nil, err
	}
	return (*hexutil.Big)(balance.ToBig()), nil
}

// GetCode returns the contract code at address in the committed EVM state after
// block. An account without code yields empty bytes, encoded as "0x".
func (api *stateAPI) GetCode(ctx context.Context, address common.Address, block ethrpc.BlockNumberOrHash) (hexutil.Bytes, error) {
	height, latest, err := stateHeight(ctx, api.backend, api.store, block)
	if err != nil {
		return nil, err
	}
	return readState(ctx, func() (hexutil.Bytes, error) {
		if latest {
			return api.backend.EvmCode(address)
		}
		return api.backend.EvmCodeAt(address, height)
	})
}

// readState returns read's result, or ctx's error when ctx has ended before read starts or by the
// time it returns.
func readState[T any](ctx context.Context, read func() (T, error)) (T, error) {
	var zero T
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	value, err := read()
	if err != nil {
		return zero, err
	}
	// Backend point reads take no ctx and run to completion, so a read that outlived the request
	// deadline is reported as that deadline rather than as a late success.
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return value, nil
}

// stateHeight returns the block a state query reads: the latest committed state for a head tag,
// or else the block a number or hash names, with earliest and 0x0 naming the earliest block store
// retains and requireCanonical ignored.
func stateHeight(ctx context.Context, backend Backend, store receiptpkg.ReceiptStore, block ethrpc.BlockNumberOrHash) (height int64, latest bool, err error) {
	if hash, ok := block.Hash(); ok {
		if height, err = blockHeightByHash(ctx, backend, hash); err != nil {
			return 0, false, err
		}
	} else {
		number, ok := block.Number()
		if !ok {
			return 0, false, errBlockNotSpecified
		}
		kind, err := classifyBlockNumber(number)
		if err != nil {
			return 0, false, err
		}
		switch kind {
		case headBlock:
			return 0, true, nil
		case earliestBlock:
			height = earliestRetainedHeight(backend, store)
		case heightBlock:
			height = number.Int64()
		}
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

// GetStorageAt returns the 32-byte value at hexKey in address's storage in the
// committed EVM state after block. An unset slot yields 32 zero bytes.
func (api *stateAPI) GetStorageAt(ctx context.Context, address common.Address, hexKey string, block ethrpc.BlockNumberOrHash) (hexutil.Bytes, error) {
	height, latest, err := stateHeight(ctx, api.backend, api.store, block)
	if err != nil {
		return nil, err
	}
	key, err := decodeStorageKey(hexKey)
	if err != nil {
		return nil, fmt.Errorf("unable to decode storage key: %w", err)
	}
	value, err := readState(ctx, func() (common.Hash, error) {
		if latest {
			return api.backend.EvmStorage(address, key)
		}
		return api.backend.EvmStorageAt(address, key, height)
	})
	if err != nil {
		return nil, err
	}
	return value[:], nil
}

// GetProof implements eth_getProof, matching v2: Sei serves no state proofs.
func (*stateAPI) GetProof(context.Context, common.Address, []string, ethrpc.BlockNumberOrHash) (any, error) {
	return nil, &evmrpc.ErrEVMNotSupported{Msg: "eth_getProof is not supported yet; please reach out to the Sei Labs if you need this endpoint"}
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
