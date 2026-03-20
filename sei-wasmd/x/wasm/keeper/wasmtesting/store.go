package wasmtesting

import (
	storetypes "github.com/sei-protocol/sei-chain/sei-cosmos/store/types"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
)

// MockCommitMultiStore mock with a CacheMultiStore to capture commits
type MockCommitMultiStore struct {
	sdk.CommitMultiStore
	Committed []bool
}

func (m *MockCommitMultiStore) CacheMultiStore() storetypes.CacheMultiStore {
	m.Committed = append(m.Committed, false)
	return &mockCMS{m, &m.Committed[len(m.Committed)-1]}
}

type mockCMS struct {
	sdk.CommitMultiStore
	committed *bool
}

func (m *mockCMS) Close() {
}

func (m *mockCMS) Write() {
	*m.committed = true
}
