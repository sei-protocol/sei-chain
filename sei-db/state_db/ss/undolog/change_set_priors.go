package undolog

import (
	"encoding/binary"
	"fmt"
	"iter"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/pebbledb/undo"
	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
)

// storageKeyLen is the length of the longest logical key, a storage slot's: type | address | slot.
const storageKeyLen = 1 + keys.AddressLen + common.HashLength

// priorsOf returns changes as the prior values the undo log records, nil when changes is nil.
func priorsOf(changes *gigatypes.StateChangeSet) undo.PriorValues {
	if changes == nil {
		return nil
	}
	return changeSetPriors{changes: changes}
}

// changeSetPriors reads a block's change set as the value each key it changed held before the block.
// A zero value reads as none, since the live state store keeps no row for one, except for code, which
// is none only when empty. A storage clear yields nothing: it only clears accounts created in the same
// transaction, whose storage was empty.
type changeSetPriors struct {
	changes *gigatypes.StateChangeSet
}

// Len returns how many pairs All yields: one per key the block changed, and two per code change,
// which changes the code hash too.
func (p changeSetPriors) Len() int {
	count := len(p.changes.Storage)
	for i := range p.changes.Accounts {
		change := &p.changes.Accounts[i]
		if change.BalanceChanged() {
			count++
		}
		if change.NonceChanged() {
			count++
		}
		if change.CodeChanged() {
			count += 2
		}
	}
	return count
}

func (p changeSetPriors) All() iter.Seq2[[]byte, []byte] {
	return func(yield func(key, prior []byte) bool) {
		// Reused for every pair: each is valid only until the next is requested.
		var key [storageKeyLen]byte
		var balance [common.HashLength]byte
		var nonce [8]byte
		for i := range p.changes.Accounts {
			change := &p.changes.Accounts[i]
			addr, prior := change.Address[:], &change.Prior
			if change.BalanceChanged() {
				clear(balance[:])
				if prior.Balance != nil {
					prior.Balance.FillBytes(balance[:])
				}
				if !yield(putKey(key[:], keys.EVMKeyBalance, addr), nonZero(balance[:])) {
					return
				}
			}
			if change.NonceChanged() {
				binary.BigEndian.PutUint64(nonce[:], prior.Nonce)
				if !yield(putKey(key[:], keys.EVMKeyNonce, addr), nonZero(nonce[:])) {
					return
				}
			}
			if change.CodeChanged() {
				// Code is absent only when empty: zero bytes are valid code.
				var code, codeHash []byte
				if len(prior.Code) != 0 {
					code, codeHash = prior.Code, crypto.Keccak256(prior.Code)
				}
				if !yield(putKey(key[:], keys.EVMKeyCodeHash, addr), codeHash) {
					return
				}
				if !yield(putKey(key[:], keys.EVMKeyCode, addr), code) {
					return
				}
			}
		}
		for i := range p.changes.Storage {
			change := &p.changes.Storage[i]
			if !yield(putKey(key[:], keys.EVMKeyStorage, change.Address[:], change.Key[:]), nonZero(change.PriorValue[:])) {
				return
			}
		}
	}
}

// putKey writes the logical key of kind for parts into the front of buf and returns it.
func putKey(buf []byte, kind keys.EVMKeyKind, parts ...[]byte) []byte {
	n := 1
	for _, part := range parts {
		n += len(part)
	}
	key := buf[:n]
	if !keys.PutEVMKey(key, kind, parts...) {
		panic(fmt.Sprintf("undolog: no logical key for EVM key kind %d", kind))
	}
	return key
}

// nonZero returns value, or nil when it is all zeros.
func nonZero(value []byte) []byte {
	for _, b := range value {
		if b != 0 {
			return value
		}
	}
	return nil
}
