package wrappers

import (
	"fmt"
	"sync/atomic"

	"github.com/sei-protocol/sei-chain/sei-db/common/metrics"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	scTypes "github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
)

var _ DBWrapper = (*noOpWrapper)(nil)

// noOpWrapper lets the benchmark measure its own overhead without DB read/write cost.
type noOpWrapper struct {
	version atomic.Int64
}

func NewNoOpWrapper() DBWrapper {
	return &noOpWrapper{}
}

func (n *noOpWrapper) ApplyChangeSets(entry *proto.ChangelogEntry) error {
	n.version.Store(entry.Version)
	return nil
}

func (n *noOpWrapper) Read(_ []byte) ([]byte, bool, error) {
	return nil, false, nil
}

func (n *noOpWrapper) Commit() (int64, error) {
	return n.version.Load(), nil
}

func (n *noOpWrapper) Close() error {
	return nil
}

func (n *noOpWrapper) Version() int64 {
	return n.version.Load()
}

// LoadLatest is a no-op: the tracked version already is this store's latest, since only ApplyChangeSets moves it.
func (n *noOpWrapper) LoadLatest() error {
	return nil
}

func (n *noOpWrapper) Importer(_ int64) (scTypes.Importer, error) {
	return nil, fmt.Errorf("import not supported for no-op wrapper")
}

// AwaitBlockHash returns immediately: this backend does not hash blocks in the background.
func (n *noOpWrapper) AwaitBlockHash(int64) error {
	return nil
}

func (n *noOpWrapper) GetPhaseTimer() *metrics.PhaseTimer {
	return nil
}
