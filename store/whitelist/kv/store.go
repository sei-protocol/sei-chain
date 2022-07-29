package kv

import (
	"fmt"
	"strings"

	storetypes "github.com/cosmos/cosmos-sdk/store/types"
)

type Store struct {
	storetypes.KVStore

	writeWhitelist map[string]struct{}
}

func NewStore(parent storetypes.KVStore, writeWhitelistKeys []string) storetypes.KVStore {
	writeWhitelist := map[string]struct{}{}
	for _, writeWhitelistKey := range writeWhitelistKeys {
		writeWhitelist[writeWhitelistKey] = struct{}{}
	}
	return &Store{
		KVStore:        parent,
		writeWhitelist: writeWhitelist,
	}
}

func (store *Store) Set(key []byte, value []byte) {
	store.validateKeyForWrite(key)
	store.KVStore.Set(key, value)
}

func (store *Store) Delete(key []byte) {
	store.validateKeyForWrite(key)
	store.KVStore.Delete(key)
}

func (store *Store) validateKeyForWrite(key []byte) {
	keyStr := string(key)
	for whitelist := range store.writeWhitelist {
		if strings.HasPrefix(keyStr, whitelist) {
			return
		}
	}
	// Panic since the Store interface does not return error on Set. Can be
	// intercepted by calling routine through `err := recover()`
	panic(fmt.Sprintf("Setting disallowed key %s in whitelisted KV store", keyStr))
}
