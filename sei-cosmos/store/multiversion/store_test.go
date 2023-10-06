package multiversion_test

import (
	"testing"

	"github.com/cosmos/cosmos-sdk/store/multiversion"
	"github.com/stretchr/testify/require"
)

func TestMultiVersionStore(t *testing.T) {
	store := multiversion.NewMultiVersionStore()

	// Test Set and GetLatest
	store.Set(1, 1, []byte("key1"), []byte("value1"))
	store.Set(2, 1, []byte("key1"), []byte("value2"))
	store.Set(3, 1, []byte("key2"), []byte("value3"))
	require.Equal(t, []byte("value2"), store.GetLatest([]byte("key1")).Value())
	require.Equal(t, []byte("value3"), store.GetLatest([]byte("key2")).Value())

	// Test SetEstimate
	store.SetEstimate(4, 1, []byte("key1"))
	require.True(t, store.GetLatest([]byte("key1")).IsEstimate())

	// Test Delete
	store.Delete(5, 1, []byte("key1"))
	require.True(t, store.GetLatest([]byte("key1")).IsDeleted())

	// Test GetLatestBeforeIndex
	store.Set(6, 1, []byte("key1"), []byte("value4"))
	require.True(t, store.GetLatestBeforeIndex(5, []byte("key1")).IsEstimate())
	require.Equal(t, []byte("value4"), store.GetLatestBeforeIndex(7, []byte("key1")).Value())

	// Test Has
	require.True(t, store.Has(2, []byte("key1")))
	require.False(t, store.Has(0, []byte("key1")))
	require.False(t, store.Has(5, []byte("key4")))
}

func TestMultiVersionStoreHasLaterValue(t *testing.T) {
	store := multiversion.NewMultiVersionStore()

	store.Set(5, 1, []byte("key1"), []byte("value2"))

	require.Nil(t, store.GetLatestBeforeIndex(4, []byte("key1")))
	require.Equal(t, []byte("value2"), store.GetLatestBeforeIndex(6, []byte("key1")).Value())
}

func TestMultiVersionStoreKeyDNE(t *testing.T) {
	store := multiversion.NewMultiVersionStore()

	require.Nil(t, store.GetLatest([]byte("key1")))
	require.Nil(t, store.GetLatestBeforeIndex(0, []byte("key1")))
	require.False(t, store.Has(0, []byte("key1")))
}
