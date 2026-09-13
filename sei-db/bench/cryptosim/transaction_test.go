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

	// The write the transaction made is in the batch, so it is served without reaching the view.
	_, found := db.Get([]byte("src"))
	require.True(t, found)
}

func TestDefaultCryptoSimConfigDisablesTransactionReadsByDefaultFalse(t *testing.T) {
	t.Parallel()

	cfg := DefaultCryptoSimConfig()
	require.False(t, cfg.DisableTransactionReads)
}
