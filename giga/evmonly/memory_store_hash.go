package evmonly

import (
	"encoding/binary"
	"io"
	"sync"

	"github.com/ethereum/go-ethereum/common"
)

// memoryStoreHashChunk is how many changes one goroutine encodes at a time.
const memoryStoreHashChunk = 2048

// WriteMemoryStoreHashInput writes to w the app-hash input for the changeset
// EncodeMemoryStoreChangeSet returns for changes: the length-prefixed changeset name, then every
// pair's length-prefixed key, delete flag and length-prefixed value. It encodes in parallel chunks.
func WriteMemoryStoreHashInput(w io.Writer, changes StateChangeSet) error {
	if err := validateMemoryStoreChangeSet(changes); err != nil {
		return err
	}
	var wg sync.WaitGroup
	// Pairs are grouped by kind, as EncodeMemoryStoreChangeSet lays them out: every balance, then
	// every nonce, then every code, then storage.
	sections := [][][]byte{
		encodeInChunks(&wg, changes.Accounts, appendMemoryStoreBalancePair),
		encodeInChunks(&wg, changes.Accounts, appendMemoryStoreNoncePair),
		encodeInChunks(&wg, changes.Accounts, appendMemoryStoreCodePair),
		encodeInChunks(&wg, changes.Storage, appendMemoryStoreStoragePair),
	}
	wg.Wait()

	header := binary.BigEndian.AppendUint64(nil, uint64(len(MemoryStoreChangeSetName)))
	if _, err := w.Write(append(header, MemoryStoreChangeSetName...)); err != nil {
		return err
	}
	for _, section := range sections {
		for _, chunk := range section {
			if _, err := w.Write(chunk); err != nil {
				return err
			}
		}
	}
	return nil
}

// encodeInChunks encodes items with encode, one goroutine on wg per memoryStoreHashChunk items. The
// returned chunks hold the encoded bytes in item order once wg is done.
func encodeInChunks[T any](wg *sync.WaitGroup, items []T, encode func([]byte, *T) []byte) [][]byte {
	chunks := make([][]byte, (len(items)+memoryStoreHashChunk-1)/memoryStoreHashChunk)
	for c := range chunks {
		chunkItems := items[c*memoryStoreHashChunk : min((c+1)*memoryStoreHashChunk, len(items))]
		wg.Go(func() {
			var buf []byte
			for i := range chunkItems {
				buf = encode(buf, &chunkItems[i])
			}
			chunks[c] = buf
		})
	}
	return chunks
}

func appendMemoryStoreBalancePair(buf []byte, change *AccountChange) []byte {
	if !change.BalanceChanged() {
		return buf
	}
	var value [common.HashLength]byte
	if change.Account.Balance != nil {
		change.Account.Balance.FillBytes(value[:])
	}
	return appendMemoryStoreAccountPair(buf, memoryStoreBalanceKey, change.Address, false, value[:])
}

func appendMemoryStoreNoncePair(buf []byte, change *AccountChange) []byte {
	if !change.NonceChanged() {
		return buf
	}
	var value [8]byte
	binary.BigEndian.PutUint64(value[:], change.Account.Nonce)
	return appendMemoryStoreAccountPair(buf, memoryStoreNonceKey, change.Address, false, value[:])
}

func appendMemoryStoreCodePair(buf []byte, change *AccountChange) []byte {
	if !change.CodeChanged() {
		return buf
	}
	code := change.Account.Code
	return appendMemoryStoreAccountPair(buf, memoryStoreCodeKey, change.Address, len(code) == 0, code)
}

func appendMemoryStoreStoragePair(buf []byte, change *StorageChange) []byte {
	var key [memoryStoreStorageKeyLen]byte
	key[0] = memoryStoreStorageKeyKind
	copy(key[1:memoryStoreAccountKeyLen], change.Address[:])
	copy(key[memoryStoreAccountKeyLen:], change.Key[:])
	var value []byte
	if !change.Delete {
		value = change.Value[:]
	}
	return appendMemoryStoreHashPair(buf, key[:], change.Delete, value)
}

func appendMemoryStoreAccountPair(buf []byte, kind byte, address common.Address, deleteValue bool, value []byte) []byte {
	var key [memoryStoreAccountKeyLen]byte
	key[0] = kind
	copy(key[1:], address[:])
	return appendMemoryStoreHashPair(buf, key[:], deleteValue, value)
}

// appendMemoryStoreHashPair appends a pair's length-prefixed key, its delete flag, and its
// length-prefixed value.
func appendMemoryStoreHashPair(buf []byte, key []byte, deleteValue bool, value []byte) []byte {
	buf = binary.BigEndian.AppendUint64(buf, uint64(len(key)))
	buf = append(buf, key...)
	if deleteValue {
		buf = append(buf, 1)
	} else {
		buf = append(buf, 0)
	}
	buf = binary.BigEndian.AppendUint64(buf, uint64(len(value)))
	return append(buf, value...)
}
