package evm

import (
	"sync/atomic"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// TestEvmTx parses a signed test transaction with the signer for its chain.
func TestEvmTx(tx *ethtypes.Transaction) EvmTx {
	signer := NewSigner(ethtypes.LatestSignerForChainID(tx.ChainId()), 0, 1)
	return utils.OrPanic1(signer.ParseTx(utils.OrPanic1(tx.MarshalBinary()))).EvmTx()
}

// TestCountingSigner counts sender recovery calls made to an Ethereum signer.
type TestCountingSigner struct {
	ethtypes.Signer
	Calls atomic.Uint64
}

func (s *TestCountingSigner) Sender(tx *ethtypes.Transaction) (common.Address, error) {
	s.Calls.Add(1)
	return s.Signer.Sender(tx)
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
