package evmonly

import "github.com/sei-protocol/sei-chain/sei-db/proto"

type readOnlyTestStore struct {
	*MemoryStore
}

func (*readOnlyTestStore) CommitStateChanges(int64, []*proto.NamedChangeSet) error {
	return nil
}

// withTestState keeps executor unit tests focused on execution behavior while
// production code exposes only giga StateDB configuration.
func withTestState(state StateReader) Option {
	store := &readOnlyTestStore{MemoryStore: NewMemoryStore(state)}
	return WithStore(store, store.EncodeChangeSet)
}
