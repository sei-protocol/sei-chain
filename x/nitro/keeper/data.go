package keeper

import (
	"encoding/binary"
	"errors"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

const (
	HeaderSizeTotalCount = 8
	HeaderSizePerTx      = 8
)

func (k Keeper) SetTransactionData(ctx sdk.Context, slot uint64, data [][]byte) {
	store := k.GetTransactionDataStore(ctx)
	key := make([]byte, 8)
	binary.BigEndian.PutUint64(key, slot)
	store.Set(key, EncodeTransactionData(data))
}

func (k Keeper) GetTransactionData(ctx sdk.Context, slot uint64) ([][]byte, error) {
	store := k.GetTransactionDataStore(ctx)
	key := make([]byte, 8)
	binary.BigEndian.PutUint64(key, slot)
	encoded := store.Get(key)
	if encoded == nil {
		return nil, errors.New("not found")
	}
	return DecodeTransactionData(encoded)
}

// since we don't want to write one entry per transaction but still want to be able
// to read individual transaction data when we implement fraud proof, a custom encoding
// scheme is applied to flatten the transactions into a 1D byte array:

// First 8 bytes: count (C) of transactions
// Next 8 * C bytes: transaction sizes
// The rest: transaction bodies
func EncodeTransactionData(data [][]byte) []byte {
	totalSize := uint64(HeaderSizeTotalCount)
	for _, datum := range data {
		totalSize += HeaderSizePerTx    // space header data
		totalSize += uint64(len(datum)) // tx itself
	}
	res := make([]byte, totalSize)
	binary.BigEndian.PutUint64(res[:HeaderSizeTotalCount], uint64(len(data)))
	headerSizePerTxPointer := HeaderSizeTotalCount
	txPointer := HeaderSizeTotalCount + HeaderSizePerTx*len(data)
	for _, datum := range data {
		size := make([]byte, 8)
		binary.BigEndian.PutUint64(size, uint64(len(datum)))
		copy(res[headerSizePerTxPointer:headerSizePerTxPointer+HeaderSizePerTx], size)
		copy(res[txPointer:txPointer+len(datum)], datum)
		txPointer += len(datum)
		headerSizePerTxPointer += HeaderSizePerTx
	}
	return res
}

func DecodeTransactionData(encoded []byte) ([][]byte, error) {
	if len(encoded) < HeaderSizeTotalCount {
		return nil, fmt.Errorf("encoded transaction data should have a size of at least %d, found %d", HeaderSizeTotalCount, len(encoded))
	}
	totalCount := int(binary.BigEndian.Uint64(encoded[:HeaderSizeTotalCount]))
	minimalSize := HeaderSizeTotalCount + totalCount*HeaderSizePerTx
	if len(encoded) < minimalSize {
		return nil, fmt.Errorf("encoded transaction data with count %d should have a size of at least %d, found %d", totalCount, minimalSize, len(encoded))
	}
	res := [][]byte{}
	headerSizePerTxPointer := HeaderSizeTotalCount
	txPointer := minimalSize
	for i := 0; i < totalCount; i++ {
		txSize := int(binary.BigEndian.Uint64(encoded[headerSizePerTxPointer : headerSizePerTxPointer+HeaderSizePerTx]))
		if len(encoded) < txPointer+txSize {
			return nil, errors.New("encoded transaction data is truncated")
		}
		res = append(res, encoded[txPointer:txPointer+txSize])
		headerSizePerTxPointer += HeaderSizePerTx
		txPointer += txSize
	}
	if txPointer != len(encoded) {
		return nil, errors.New("encoded transaction data has too many bytes")
	}
	return res, nil
}
