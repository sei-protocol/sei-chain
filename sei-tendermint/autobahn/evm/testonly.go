package evm

import "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"

// epochSnapshotApp serves the current snapshot of a test epoch registry.
type epochSnapshotApp struct {
	Application
	snapshot func() *types.EpochRegistry
}

func (a epochSnapshotApp) EpochRegistry() (*types.EpochRegistry, error) {
	return a.snapshot(), nil
}

// TestEpochApp returns an EVM proxy whose epoch registry is registry's current snapshot.
// Intended for use in tests only.
func TestEpochApp(registry interface{ Snapshot() *types.EpochRegistry }) *Proxy {
	return NewProxy(epochSnapshotApp{snapshot: registry.Snapshot})
}
