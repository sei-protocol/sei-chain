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

func (r *readTrackingWrapper) LoadLatest() error {
	return nil
}

func (r *readTrackingWrapper) Importer(_ int64) (scTypes.Importer, error) {
	return nil, nil
}

func (r *readTrackingWrapper) AwaitBlockHash(int64) error {
	return nil
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

	// Execute performs no writes at all: a transaction's writes are recorded by the block builder when
	// the block is generated, so there is nothing left for this to do but read. This used to assert the
	// opposite — that Execute had written the source account — and that coverage moved to
	// TestBlockCarriesItsWritesToTheDB along with the behaviour.
	require.Empty(t, db.pendingWrites)
}

// TestBlockCarriesItsWritesToTheDB covers the handoff the finalize path depends on: writes accumulate
// in the Database, the builder harvests them into a block, and the block yields the changeset with
// nothing left to convert on the commit thread.
func TestBlockCarriesItsWritesToTheDB(t *testing.T) {
	t.Parallel()

	cfg := DefaultCryptoSimConfig()
	db := NewDatabase(cfg, &readTrackingWrapper{}, nil, 0)

	require.NoError(t, db.Put([]byte("src"), []byte("src-balance")))
	require.NoError(t, db.Put([]byte("dst"), []byte("dst-balance")))

	// A key written twice in one block collapses to its last write, which is what keeps the changeset
	// the size of the key set rather than the write count.
	require.NoError(t, db.Put([]byte("src"), []byte("src-balance-again")))

	harvested := db.HarvestWrites()
	require.Len(t, harvested, 2)
	require.Empty(t, db.pendingWrites, "harvest must leave a fresh map behind")

	blk := NewBlock(cfg, nil, 0, cfg.TransactionsPerBlock)
	blk.SetWrites(harvested)

	require.Len(t, blk.Changeset(), 2)
	require.Equal(t, len(blk.Changeset())+3, cap(blk.Changeset()),
		"the changeset reserves room for the counter keys FinalizeBlock appends")

	var values [][]byte
	for _, pair := range blk.Changeset() {
		values = append(values, pair.Value)
	}
	require.Contains(t, values, []byte("src-balance-again"), "the last write for a key is the one kept")
	require.NotContains(t, values, []byte("src-balance"))
}

// TestDatabaseReadsAlwaysReachTheDB pins the property the benchmark's fidelity depends on: no read is
// ever served from memory, not even one whose key this block writes.
//
// The regression it guards against is real and shipped once: Get consulted the block's pending writes
// first, and because a transaction reads the same keys it writes, that excluded most of a block's
// reads from the measurement entirely.
func TestDatabaseReadsAlwaysReachTheDB(t *testing.T) {
	t.Parallel()

	cfg := DefaultCryptoSimConfig()
	wrapper := &readTrackingWrapper{}
	db := NewDatabase(cfg, wrapper, nil, 0)

	require.NoError(t, db.Put([]byte("written"), []byte("value")))

	blk := NewBlock(cfg, nil, 0, cfg.TransactionsPerBlock)
	blk.SetWrites(db.HarvestWrites())
	db.SetCurrentBlock(blk)

	// The key this block writes, read while that block is current: still goes to the DB.
	_, _, err := db.Get([]byte("written"))
	require.NoError(t, err)
	require.Equal(t, 1, wrapper.readCalls)

	// And a key nothing wrote.
	_, _, err = db.Get([]byte("absent"))
	require.NoError(t, err)
	require.Equal(t, 2, wrapper.readCalls)
}

func TestDefaultCryptoSimConfigDisablesTransactionReadsByDefaultFalse(t *testing.T) {
	t.Parallel()

	cfg := DefaultCryptoSimConfig()
	require.False(t, cfg.DisableTransactionReads)
	require.Equal(t, wrappers.FlatKV, cfg.Backend)
}
