package kv_test

import (
	"testing"

	"github.com/sei-protocol/sei-chain/store"
	"github.com/sei-protocol/sei-chain/store/whitelist/kv"
	"github.com/stretchr/testify/require"
)

var TestWhiteList = []string{"foo"}

// Write should only panic if key is not whitelisted
func TestWhitelistedSet(t *testing.T) {
	whitelistedStore := kv.NewStore(store.NewTestKVStore(), TestWhiteList)
	require.NotPanics(t, func() { whitelistedStore.Set([]byte("foo"), []byte("val")) })
}

func TestNotWhitelistedSet(t *testing.T) {
	whitelistedStore := kv.NewStore(store.NewTestKVStore(), TestWhiteList)
	require.Panics(t, func() { whitelistedStore.Set([]byte("bar"), []byte("val")) })
}

func TestWhitelistedDelete(t *testing.T) {
	whitelistedStore := kv.NewStore(store.NewTestKVStore(), TestWhiteList)
	require.NotPanics(t, func() { whitelistedStore.Delete([]byte("foo")) })
}

func TestNotWhitelistedDelete(t *testing.T) {
	whitelistedStore := kv.NewStore(store.NewTestKVStore(), TestWhiteList)
	require.Panics(t, func() { whitelistedStore.Delete([]byte("bar")) })
}

// Read should never panic
func TestWhitelistedHas(t *testing.T) {
	whitelistedStore := kv.NewStore(store.NewTestKVStore(), TestWhiteList)
	require.NotPanics(t, func() { whitelistedStore.Has([]byte("foo")) })
}

func TestNotWhitelistedHas(t *testing.T) {
	whitelistedStore := kv.NewStore(store.NewTestKVStore(), TestWhiteList)
	require.NotPanics(t, func() { whitelistedStore.Has([]byte("bar")) })
}

func TestWhitelistedGet(t *testing.T) {
	whitelistedStore := kv.NewStore(store.NewTestKVStore(), TestWhiteList)
	require.NotPanics(t, func() { whitelistedStore.Get([]byte("foo")) })
}

func TestNotWhitelistedGet(t *testing.T) {
	whitelistedStore := kv.NewStore(store.NewTestKVStore(), TestWhiteList)
	require.NotPanics(t, func() { whitelistedStore.Get([]byte("bar")) })
}
