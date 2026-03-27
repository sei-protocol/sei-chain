package blocksim

import (
	"bytes"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/common/rand"
	"github.com/stretchr/testify/require"
)

const testBufferSize = 1024

func newTestCrand() *rand.CannedRandom {
	return rand.NewCannedRandom(testBufferSize, 42)
}

func TestTransactionRoundTrip(t *testing.T) {
	crand := newTestCrand()
	txn := RandomTransaction(7, crand, 64)

	serialized := txn.Serialize()
	crand.Reset()
	deserialized, err := DeserializeTransaction(crand, serialized)
	require.NoError(t, err)

	require.Equal(t, txn.ID(), deserialized.ID())
	require.True(t, bytes.Equal(txn.Hash(), deserialized.Hash()))
	require.True(t, bytes.Equal(txn.Payload(), deserialized.Payload()))
}

func TestTransactionRoundTripEmptyPayload(t *testing.T) {
	crand := newTestCrand()
	txn := RandomTransaction(1, crand, 0)

	serialized := txn.Serialize()
	crand.Reset()
	deserialized, err := DeserializeTransaction(crand, serialized)
	require.NoError(t, err)

	require.Equal(t, txn.ID(), deserialized.ID())
	require.True(t, bytes.Equal(txn.Hash(), deserialized.Hash()))
	require.Empty(t, deserialized.Payload())
}

func TestTransactionDeserializePayloadIsolation(t *testing.T) {
	crand := newTestCrand()
	txn := RandomTransaction(1, crand, 32)
	serialized := txn.Serialize()

	crand.Reset()
	deserialized, err := DeserializeTransaction(crand, serialized)
	require.NoError(t, err)

	// Mutating the serialized buffer should not affect the deserialized transaction.
	for i := 12; i < len(serialized); i++ {
		serialized[i] = 0xFF
	}
	require.True(t, bytes.Equal(txn.Payload(), deserialized.Payload()))
}

func TestTransactionDeserializeTooShort(t *testing.T) {
	crand := newTestCrand()
	_, err := DeserializeTransaction(crand, make([]byte, 11))
	require.Error(t, err)

	_, err = DeserializeTransaction(crand, make([]byte, 0))
	require.Error(t, err)
}

func TestTransactionDeserializeTruncatedPayload(t *testing.T) {
	crand := newTestCrand()
	txn := RandomTransaction(1, crand, 64)
	serialized := txn.Serialize()

	// Truncate the serialized data to cut off part of the payload.
	crand.Reset()
	_, err := DeserializeTransaction(crand, serialized[:20])
	require.Error(t, err)
}

func TestBlockRoundTrip(t *testing.T) {
	crand := newTestCrand()
	blk := RandomBlock(100, crand, 1, 5, 48, 32)

	serialized := blk.Serialize()
	crand.Reset()
	deserialized, err := DeserializeBlock(crand, serialized)
	require.NoError(t, err)

	require.Equal(t, blk.Height(), deserialized.Height())
	require.True(t, bytes.Equal(blk.Hash(), deserialized.Hash()))
	require.True(t, bytes.Equal(blk.Metadata(), deserialized.Metadata()))
	require.Equal(t, len(blk.Transactions()), len(deserialized.Transactions()))

	for i, txn := range blk.Transactions() {
		dtxn := deserialized.Transactions()[i]
		require.Equal(t, txn.ID(), dtxn.ID())
		require.True(t, bytes.Equal(txn.Hash(), dtxn.Hash()))
		require.True(t, bytes.Equal(txn.Payload(), dtxn.Payload()))
	}
}

func TestBlockRoundTripNoTransactions(t *testing.T) {
	crand := newTestCrand()
	hash := crand.Address(0, int64(5), 32)
	metadata := crand.Bytes(16)

	blk := &block{
		height:       5,
		hash:         hash,
		transactions: []*transaction{},
		metadata:     metadata,
	}

	serialized := blk.Serialize()
	crand.Reset()
	deserialized, err := DeserializeBlock(crand, serialized)
	require.NoError(t, err)

	require.Equal(t, blk.Height(), deserialized.Height())
	require.True(t, bytes.Equal(blk.Metadata(), deserialized.Metadata()))
	require.Empty(t, deserialized.Transactions())
}

func TestBlockRoundTripEmptyMetadata(t *testing.T) {
	crand := newTestCrand()
	blk := RandomBlock(1, crand, 1, 3, 32, 0)

	serialized := blk.Serialize()
	crand.Reset()
	deserialized, err := DeserializeBlock(crand, serialized)
	require.NoError(t, err)

	require.Equal(t, blk.Height(), deserialized.Height())
	require.Empty(t, deserialized.Metadata())
	require.Equal(t, len(blk.Transactions()), len(deserialized.Transactions()))

	for i, txn := range blk.Transactions() {
		dtxn := deserialized.Transactions()[i]
		require.Equal(t, txn.ID(), dtxn.ID())
		require.True(t, bytes.Equal(txn.Payload(), dtxn.Payload()))
	}
}

func TestBlockRoundTripLargeMetadata(t *testing.T) {
	crand := newTestCrand()
	blk := RandomBlock(42, crand, 1, 2, 32, 256)

	serialized := blk.Serialize()
	crand.Reset()
	deserialized, err := DeserializeBlock(crand, serialized)
	require.NoError(t, err)

	require.Equal(t, blk.Height(), deserialized.Height())
	require.True(t, bytes.Equal(blk.Metadata(), deserialized.Metadata()))
	require.Equal(t, len(blk.Transactions()), len(deserialized.Transactions()))
}

func TestBlockDeserializeMetadataIsolation(t *testing.T) {
	crand := newTestCrand()
	blk := RandomBlock(1, crand, 1, 2, 32, 16)
	serialized := blk.Serialize()

	crand.Reset()
	deserialized, err := DeserializeBlock(crand, serialized)
	require.NoError(t, err)

	// Mutating the serialized buffer should not affect deserialized metadata.
	for i := 12; i < 12+16; i++ {
		serialized[i] = 0xFF
	}
	require.True(t, bytes.Equal(blk.Metadata(), deserialized.Metadata()))
}

func TestBlockDeserializeTooShort(t *testing.T) {
	crand := newTestCrand()
	_, err := DeserializeBlock(crand, make([]byte, 15))
	require.Error(t, err)

	_, err = DeserializeBlock(crand, make([]byte, 0))
	require.Error(t, err)
}

func TestBlockDeserializeTruncatedMetadata(t *testing.T) {
	crand := newTestCrand()
	blk := RandomBlock(1, crand, 1, 2, 32, 64)
	serialized := blk.Serialize()

	// Truncate so metadata is incomplete.
	crand.Reset()
	_, err := DeserializeBlock(crand, serialized[:14])
	require.Error(t, err)
}

func TestBlockDeserializeTruncatedTransaction(t *testing.T) {
	crand := newTestCrand()
	blk := RandomBlock(1, crand, 1, 3, 48, 8)
	serialized := blk.Serialize()

	// Keep header + metadata + num txns + part of first transaction.
	truncateAt := 8 + 4 + 8 + 4 + 10
	crand.Reset()
	_, err := DeserializeBlock(crand, serialized[:truncateAt])
	require.Error(t, err)
}
