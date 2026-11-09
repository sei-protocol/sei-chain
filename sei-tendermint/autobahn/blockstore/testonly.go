package blockstore

import (
	"fmt"
	"testing"

	littdb "github.com/sei-protocol/sei-chain/sei-db/db_engine/litt"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// NewTestStore opens a temporary LittDB store and closes it when the test finishes.
func NewTestStore(t testing.TB) *Store {
	t.Helper()
	cfg := utils.OrPanic1(DefaultConfig(t.TempDir()))
	store := utils.OrPanic1(New(cfg))
	t.Cleanup(func() { utils.OrPanic(store.Close()) })
	return store
}

// ForceGC runs synchronous garbage collection for this store.
func (s *Store) ForceGC() error {
	managed, ok := s.table.(littdb.ManagedTable)
	if !ok {
		return fmt.Errorf("table %q is not a ManagedTable", s.table.Name())
	}
	if err := managed.RunGC(); err != nil {
		return fmt.Errorf("managed.RunGC() [table %q]: %w", s.table.Name(), err)
	}
	return nil
}
