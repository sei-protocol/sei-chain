package util

import (
	"encoding/json"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
)

func GetJSON[T any](store precompiles.Store, key []byte) (T, bool, error) {
	var out T
	bz, ok := store.Get(key)
	if !ok {
		return out, false, nil
	}
	if err := json.Unmarshal(bz, &out); err != nil {
		return out, false, err
	}
	return out, true, nil
}

func SetJSON[T any](store precompiles.Store, key []byte, value T) error {
	bz, err := json.Marshal(value)
	if err != nil {
		return err
	}
	store.Set(key, bz)
	return nil
}
