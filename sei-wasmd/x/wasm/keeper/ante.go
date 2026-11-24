package keeper

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"

	"github.com/sei-protocol/sei-chain/sei-wasmd/x/wasm/types"
)

// CountTXDecorator ante handler to count the tx position in a block.
type CountTXDecorator struct {
	storeKey sdk.StoreKey
}

// NewCountTXDecorator constructor
func NewCountTXDecorator(storeKey sdk.StoreKey) *CountTXDecorator {
	return &CountTXDecorator{storeKey: storeKey}
}

// AnteHandle handler stores a tx counter with current height encoded in the store to let the app handle
// global rollback behavior instead of keeping state in the handler itself.
// The ante handler passes the counter value via sdk.Context upstream. See `types.TXCounter(ctx)` to read the value.
func (a CountTXDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, next sdk.AnteHandler) (sdk.Context, error) {
	store := ctx.KVStore(a.storeKey)
	currentHeight := ctx.BlockHeight()

	var txCounter uint32 // start with 0
	// load counter when exists
	if bz := store.Get(types.TXCounterPrefix); bz != nil {
		lastHeight, val, err := decodeHeightCounter(bz)
		if err != nil {
			return sdk.Context{}, err
		}
		if currentHeight == lastHeight {
			// then use stored counter
			txCounter = val
		} // else use `0` from above to start with
	}
	// store next counter value for current height
	h, err := encodeHeightCounter(currentHeight, txCounter+1)
	if err != nil {
		return sdk.Context{}, err
	}
	store.Set(types.TXCounterPrefix, h)

	return next(types.WithTXCounter(ctx, txCounter), tx)
}

func encodeHeightCounter(height int64, counter uint32) ([]byte, error) {
	if height < 0 {
		return nil, fmt.Errorf("height %d is negative", height)
	}
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, counter)
	// #nosec G115 -- height is checked above to be non-negative
	return append(sdk.Uint64ToBigEndian(uint64(height)), b...), nil
}

func decodeHeightCounter(bz []byte) (int64, uint32, error) {
	left := sdk.BigEndianToUint64(bz[0:8])
	if left > math.MaxInt64 {
		return 0, 0, errors.New("invalid height")
	}
	return int64(left), binary.BigEndian.Uint32(bz[8:]), nil
}
