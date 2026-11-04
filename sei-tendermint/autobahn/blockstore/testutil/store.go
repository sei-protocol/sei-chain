// Package testutil opens temporary consensus block stores for tests.
package testutil

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/blockstore"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// New opens a temporary LittDB store and closes it when the test finishes.
func New(t testing.TB) *blockstore.Store {
	t.Helper()
	cfg := utils.OrPanic1(blockstore.DefaultConfig(t.TempDir()))
	store := utils.OrPanic1(blockstore.New(cfg))
	t.Cleanup(func() { utils.OrPanic(store.Close()) })
	return store
}
