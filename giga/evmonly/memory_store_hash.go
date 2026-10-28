package evmonly

import (
	"encoding/binary"
	"fmt"
	"io"

	"github.com/ethereum/go-ethereum/common"
	"golang.org/x/sync/errgroup"
)

// memoryStoreHashChunk is how many changes one goroutine encodes at a time.
const memoryStoreHashChunk = 2048

const (
	memoryStoreBalanceChanged uint8 = 1 << iota
	memoryStoreNonceChanged
	memoryStoreCodeChanged
)

// WriteMemoryStoreHashInput writes to w the bytes an app hash takes over the changeset
// EncodeMemoryStoreChangeSet returns for changes: the changeset name, then every pair's key, delete
// flag and value, each length-prefixed. It encodes in parallel chunks.
func WriteMemoryStoreHashInput(w io.Writer, changes StateChangeSet) error {
	accounts := changes.Accounts
	flags := make([]uint8, len(accounts))
	accountChunks := chunkCount(len(accounts))
	firstInvalid := make([]int, accountChunks)
	var group errgroup.Group
	for c := range accountChunks {
		group.Go(func() error {
			firstInvalid[c] = -1
			for i := c * memoryStoreHashChunk; i < min((c+1)*memoryStoreHashChunk, len(accounts)); i++ {
				change := &accounts[i]
				if firstInvalid[c] < 0 && validateMemoryStoreBalance(change.Account.Balance) != nil {
					firstInvalid[c] = i
				}
				if change.BalanceChanged() {
					flags[i] |= memoryStoreBalanceChanged
				}
				if change.NonceChanged() {
					flags[i] |= memoryStoreNonceChanged
				}
				if change.CodeChanged() {
					flags[i] |= memoryStoreCodeChanged
				}
			}
			return nil
		})
	}
	_ = group.Wait()
	for _, i := range firstInvalid {
		if i >= 0 {
			return fmt.Errorf("memory store account change %d: %w", i, validateMemoryStoreBalance(accounts[i].Account.Balance))
		}
	}

	// Pairs are grouped by kind, as EncodeMemoryStoreChangeSet lays them out: every balance, then
	// every nonce, then every code, then storage.
	storageChunks := chunkCount(len(changes.Storage))
	sections := [3][][]byte{make([][]byte, accountChunks), make([][]byte, accountChunks), make([][]byte, accountChunks)}
	storage := make([][]byte, storageChunks)
	for c := range accountChunks {
		start, end := c*memoryStoreHashChunk, min((c+1)*memoryStoreHashChunk, len(accounts))
		group.Go(func() error {
			var buf []byte
			for i := start; i < end; i++ {
				if flags[i]&memoryStoreBalanceChanged == 0 {
					continue
				}
				var value [common.HashLength]byte
				if balance := accounts[i].Account.Balance; balance != nil {
					balance.FillBytes(value[:])
				}
				buf = appendMemoryStoreAccountPair(buf, memoryStoreBalanceKey, accounts[i].Address, false, value[:])
			}
			sections[0][c] = buf
			return nil
		})
		group.Go(func() error {
			var buf []byte
			for i := start; i < end; i++ {
				if flags[i]&memoryStoreNonceChanged == 0 {
					continue
				}
				buf = appendMemoryStoreAccountPair(buf, memoryStoreNonceKey, accounts[i].Address, false,
					binary.BigEndian.AppendUint64(nil, accounts[i].Account.Nonce))
			}
			sections[1][c] = buf
			return nil
		})
		group.Go(func() error {
			var buf []byte
			for i := start; i < end; i++ {
				if flags[i]&memoryStoreCodeChanged == 0 {
					continue
				}
				code := accounts[i].Account.Code
				buf = appendMemoryStoreAccountPair(buf, memoryStoreCodeKey, accounts[i].Address, len(code) == 0, code)
			}
			sections[2][c] = buf
			return nil
		})
	}
	for c := range storageChunks {
		group.Go(func() error {
			var buf []byte
			for _, change := range changes.Storage[c*memoryStoreHashChunk : min((c+1)*memoryStoreHashChunk, len(changes.Storage))] {
				var key [memoryStoreStorageKeyLen]byte
				key[0] = memoryStoreStorageKeyKind
				copy(key[1:memoryStoreAccountKeyLen], change.Address[:])
				copy(key[memoryStoreAccountKeyLen:], change.Key[:])
				var value []byte
				if !change.Delete {
					value = change.Value[:]
				}
				buf = appendMemoryStoreHashPair(buf, key[:], change.Delete, value)
			}
			storage[c] = buf
			return nil
		})
	}
	_ = group.Wait()

	header := binary.BigEndian.AppendUint64(nil, uint64(len(MemoryStoreChangeSetName)))
	if _, err := w.Write(append(header, MemoryStoreChangeSetName...)); err != nil {
		return err
	}
	for _, section := range append(sections[:], storage) {
		for _, buf := range section {
			if _, err := w.Write(buf); err != nil {
				return err
			}
		}
	}
	return nil
}

func chunkCount(n int) int {
	return (n + memoryStoreHashChunk - 1) / memoryStoreHashChunk
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
