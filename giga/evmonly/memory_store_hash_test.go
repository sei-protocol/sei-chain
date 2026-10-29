package evmonly

import (
	"bytes"
	"encoding/binary"
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"
)

// memoryStoreHashInputFromChangeSet is the app-hash byte stream taken over the changeset
// EncodeMemoryStoreChangeSet returns.
func memoryStoreHashInputFromChangeSet(t *testing.T, changes StateChangeSet) []byte {
	t.Helper()
	changesets, err := EncodeMemoryStoreChangeSet(changes)
	require.NoError(t, err)
	var buf []byte
	for _, changeset := range changesets {
		buf = binary.BigEndian.AppendUint64(buf, uint64(len(changeset.Name)))
		buf = append(buf, changeset.Name...)
		for _, pair := range changeset.Changeset.Pairs {
			buf = appendMemoryStoreHashPair(buf, pair.Key, pair.Delete, pair.Value)
		}
	}
	return buf
}

// The app hash is consensus-critical, so the chunked stream has to match the changeset's byte for
// byte, on one chunk and across many.
func TestWriteMemoryStoreHashInputMatchesTheChangeSet(t *testing.T) {
	for _, n := range []int{0, 10, 3*memoryStoreHashChunk + 7} {
		changes := largeFlatKVChangeSet(n)
		var got bytes.Buffer
		require.NoError(t, WriteMemoryStoreHashInput(&got, changes))
		require.Equal(t, memoryStoreHashInputFromChangeSet(t, changes), got.Bytes(), "n=%d", n)
	}
}

func TestWriteMemoryStoreHashInputRejectsTheFirstOversizedBalance(t *testing.T) {
	changes := largeFlatKVChangeSet(3 * memoryStoreHashChunk)
	tooLarge := new(big.Int).Lsh(big.NewInt(1), 256)
	changes.Accounts[2*memoryStoreHashChunk+1].Account.Balance = tooLarge
	changes.Accounts[memoryStoreHashChunk+5].Account.Balance = tooLarge
	err := WriteMemoryStoreHashInput(&bytes.Buffer{}, changes)
	_, want := EncodeMemoryStoreChangeSet(changes)
	require.Error(t, want)
	require.EqualError(t, err, want.Error())
}
