package cryptosim

import (
	"testing"

	"github.com/stretchr/testify/require"

	commonmetrics "github.com/sei-protocol/sei-chain/sei-db/common/metrics"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/bench/wrappers"
	scTypes "github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
)

type readTrackingWrapper struct {
	readCalls int
}

func (r *readTrackingWrapper) ApplyChangeSets(_ *proto.ChangelogEntry) error {
	return nil
}

func (r *readTrackingWrapper) Read(_ []byte) ([]byte, bool, error) {
	r.readCalls++
	return nil, false, nil
}

func (r *readTrackingWrapper) Commit() (int64, error) {
	return 0, nil
}

func (r *readTrackingWrapper) Close() error {
	return nil
}

func (r *readTrackingWrapper) Version() int64 {
	return 0
}

func (r *readTrackingWrapper) LoadVersion(_ int64) error {
	return nil
}

func (r *readTrackingWrapper) Importer(_ int64) (scTypes.Importer, error) {
	return nil, nil
}

func (r *readTrackingWrapper) GetPhaseTimer() *commonmetrics.PhaseTimer {
	return nil
}

func TestTransactionExecuteSkipsReadsWhenDisabled(t *testing.T) {
	t.Parallel()

	cfg := DefaultCryptoSimConfig()
	cfg.DisableTransactionReads = true

	wrapper := &readTrackingWrapper{}
	db := NewDatabase(cfg, wrapper, nil, 0)

	txn := &transaction{
		erc20Contract:     []byte("erc20"),
		srcAccount:        []byte("src"),
		dstAccount:        []byte("dst"),
		srcAccountSlot:    []byte("src-slot"),
		dstAccountSlot:    []byte("dst-slot"),
		newSrcBalance:     []byte("src-balance"),
		newDstBalance:     []byte("dst-balance"),
		newFeeBalance:     []byte("fee-balance"),
		newSrcAccountSlot: []byte("src-slot-value"),
		newDstAccountSlot: []byte("dst-slot-value"),
	}

	err := txn.Execute(db, []byte("fee"), nil)
	require.NoError(t, err)
	require.Zero(t, wrapper.readCalls)

	_, found, err := db.Get([]byte("src"))
	require.NoError(t, err)
	require.True(t, found)
}

func TestDefaultCryptoSimConfigDisablesTransactionReadsByDefaultFalse(t *testing.T) {
	t.Parallel()

	cfg := DefaultCryptoSimConfig()
	require.False(t, cfg.DisableTransactionReads)
	require.Equal(t, wrappers.FlatKV, cfg.Backend)
}
