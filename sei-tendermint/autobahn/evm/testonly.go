package evm

import (
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
)

// TestEvmTx returns a decoded transaction with the given sender for tests.
func TestEvmTx(tx *ethtypes.Transaction, sender common.Address) EvmTx {
	return EvmTx{tx: tx, sender: sender}
}

// TestEvmTxFromRaw returns a decoded transaction with the supplied sender for cache tests.
func TestEvmTxFromRaw(raw RawTx, sender common.Address) (EvmTx, error) {
	tx := new(ethtypes.Transaction)
	if err := tx.UnmarshalBinary(raw); err != nil {
		return EvmTx{}, err
	}
	return TestEvmTx(tx, sender), nil
}

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
