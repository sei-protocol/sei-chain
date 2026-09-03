package giga

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/statewal"
)

var _ StateDB = (*stateDB)(nil)

// stateDB fans a committed block out to the state WAL and the live state DB, and serves current-block
// reads from the live state DB.
type stateDB struct {
	// The state WAL a committed block is written to.
	wal statewal.StateWAL

	// The live state DB, which both receives writes and serves current-block reads.
	liveStateDB LiveStateStore
}

// NewStateDB returns a StateDB backed by wal and liveStateDB.
//
// liveStateDB must have been constructed with no WAL of its own, since this StateDB writes wal on its
// behalf; one holding a WAL would record every block twice.
//
// The caller retains ownership of wal and liveStateDB: a StateDB has no Close.
func NewStateDB(wal statewal.StateWAL, liveStateDB LiveStateStore) StateDB {
	return &stateDB{
		wal:         wal,
		liveStateDB: liveStateDB,
	}
}

func (s *stateDB) CommitStateChanges(blockNum int64, changeset []*proto.NamedChangeSet) error {
	if blockNum < 0 {
		// The WAL numbers blocks with a uint64, so a negative height converts to a block far in the
		// future that the WAL has no way to recognize as a mistake.
		return fmt.Errorf("commit block %d: block number must not be negative", blockNum)
	}

	// No need to flush WAL, since this WAL isn't used for crash recoverability safety (that's the BlockDB's job).
	if err := s.wal.Write(uint64(blockNum), changeset); err != nil {
		return fmt.Errorf("write block %d to state WAL: %w", blockNum, err)
	}
	if err := s.wal.SignalEndOfBlock(); err != nil {
		return fmt.Errorf("end block %d in state WAL: %w", blockNum, err)
	}

	if err := s.liveStateDB.CommitStateChanges(blockNum, changeset); err != nil {
		return fmt.Errorf("commit block %d to live state DB: %w", blockNum, err)
	}

	return nil
}

func (s *stateDB) OpenView() StateView {
	return s.liveStateDB.OpenView()
}

// OpenViewAt panics. Serving a past height requires the historical state DB, which is not wired into
// StateDB.
func (s *stateDB) OpenViewAt(blockNum int64) (StateView, bool) {
	panic(fmt.Sprintf(
		"giga: OpenViewAt(%d) is not implemented: the historical state DB is not wired in", blockNum))
}
