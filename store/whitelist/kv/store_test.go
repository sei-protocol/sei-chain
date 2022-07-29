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
	require.NotPanics(t, func() { whitelistedStore.Set([]byte("foofoo"), []byte("val")) })
}

func TestNotWhitelistedSet(t *testing.T) {
	whitelistedStore := kv.NewStore(store.NewTestKVStore(), TestWhiteList)
	require.Panics(t, func() { whitelistedStore.Set([]byte("bar"), []byte("val")) })
	require.Panics(t, func() { whitelistedStore.Set([]byte("barfoo"), []byte("val")) })
}

func TestWhitelistedDelete(t *testing.T) {
	whitelistedStore := kv.NewStore(store.NewTestKVStore(), TestWhiteList)
	require.NotPanics(t, func() { whitelistedStore.Delete([]byte("foo")) })
	require.NotPanics(t, func() { whitelistedStore.Set([]byte("foofoo"), []byte("val")) })
}

func TestNotWhitelistedDelete(t *testing.T) {
	whitelistedStore := kv.NewStore(store.NewTestKVStore(), TestWhiteList)
	require.Panics(t, func() { whitelistedStore.Delete([]byte("bar")) })
	require.Panics(t, func() { whitelistedStore.Set([]byte("barfoo"), []byte("val")) })
}

// Read should never panic
func TestWhitelistedHas(t *testing.T) {
	whitelistedStore := kv.NewStore(store.NewTestKVStore(), TestWhiteList)
	require.NotPanics(t, func() { whitelistedStore.Has([]byte("foo")) })
	require.NotPanics(t, func() { whitelistedStore.Set([]byte("foofoo"), []byte("val")) })
}

func TestNotWhitelistedHas(t *testing.T) {
	whitelistedStore := kv.NewStore(store.NewTestKVStore(), TestWhiteList)
	require.NotPanics(t, func() { whitelistedStore.Has([]byte("bar")) })
	require.Panics(t, func() { whitelistedStore.Set([]byte("barfoo"), []byte("val")) })
}

func TestWhitelistedGet(t *testing.T) {
	whitelistedStore := kv.NewStore(store.NewTestKVStore(), TestWhiteList)
	require.NotPanics(t, func() { whitelistedStore.Get([]byte("foo")) })
	require.NotPanics(t, func() { whitelistedStore.Set([]byte("foofoo"), []byte("val")) })
}

func TestNotWhitelistedGet(t *testing.T) {
	whitelistedStore := kv.NewStore(store.NewTestKVStore(), TestWhiteList)
	require.NotPanics(t, func() { whitelistedStore.Get([]byte("bar")) })
	require.Panics(t, func() { whitelistedStore.Set([]byte("barfoo"), []byte("val")) })
}
