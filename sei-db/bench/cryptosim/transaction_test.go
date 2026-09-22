package cryptosim

import (
	"testing"

	"github.com/stretchr/testify/require"

	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/lthash"
)

// readTrackingView counts the reads a transaction issues. It embeds StateView without implementing
// it, so any method the transaction is not expected to call panics on the nil interface rather than
// answering with a zero value.
type readTrackingView struct {
	gigatypes.StateView

	// Reads served, in call order.
	readCalls int
}

func (v *readTrackingView) GetBlockHeight() int64 { return 0 }

func (v *readTrackingView) Get(_ string, _ []byte) ([]byte, bool) {
	v.readCalls++
	return nil, false
}

func (v *readTrackingView) Close() {}

// readTrackingStateDB serves every view from one readTrackingView, so a test can count the reads
// made through it.
type readTrackingStateDB struct {
	gigatypes.StateDB

	view *readTrackingView
}

func (s *readTrackingStateDB) OpenView() gigatypes.StateView { return s.view }

func (s *readTrackingStateDB) RegisterHashListener(_ gigatypes.HashListener) (lthash.BlockHash, error) {
	return lthash.BlockHash{}, nil
}

func TestTransactionExecuteSkipsReadsWhenDisabled(t *testing.T) {
	t.Parallel()

	cfg := DefaultCryptoSimConfig()
	cfg.DisableTransactionReads = true

	stateDB := &readTrackingStateDB{view: &readTrackingView{}}
	db, err := NewDatabase(cfg, stateDB, nil, nil)
	require.NoError(t, err)

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

	require.NoError(t, txn.Execute(db, []byte("fee"), nil))
	require.Zero(t, stateDB.view.readCalls)

	// Execute performs no writes at all: a transaction's writes are recorded by the block builder when
	// the block is generated, so there is nothing left for this to do but read.
	require.Empty(t, db.pendingWrites)
}

// TestBlockCarriesItsWritesToTheDB covers the handoff the finalize path depends on: writes accumulate
// in the Database, the builder harvests them into a block, and the block yields the changeset with
// nothing left to convert on the commit thread.
func TestBlockCarriesItsWritesToTheDB(t *testing.T) {
	t.Parallel()

	cfg := DefaultCryptoSimConfig()
	db, err := NewDatabase(cfg, &readTrackingStateDB{view: &readTrackingView{}}, nil, nil)
	require.NoError(t, err)

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
	require.Equal(t, len(blk.Changeset())+counterKeysPerBlock, cap(blk.Changeset()),
		"the changeset reserves room for the counter keys FinalizeBlock appends")

	values := make([][]byte, 0, len(blk.Changeset()))
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
// first, and because a transaction reads the same keys it writes, that excluded most of a block's reads
// from the measurement entirely.
func TestDatabaseReadsAlwaysReachTheDB(t *testing.T) {
	t.Parallel()

	cfg := DefaultCryptoSimConfig()
	stateDB := &readTrackingStateDB{view: &readTrackingView{}}
	db, err := NewDatabase(cfg, stateDB, nil, nil)
	require.NoError(t, err)

	require.NoError(t, db.Put([]byte("written"), []byte("value")))

	value, found := db.Get([]byte("written"))
	require.Nil(t, value)
	require.False(t, found, "the view serves no reads, so a read that reached it cannot have found a value")
	require.Equal(t, 1, stateDB.view.readCalls, "the read must have reached the view")
}

func TestDefaultCryptoSimConfigDisablesTransactionReadsByDefaultFalse(t *testing.T) {
	t.Parallel()

	cfg := DefaultCryptoSimConfig()
	require.False(t, cfg.DisableTransactionReads)
}
