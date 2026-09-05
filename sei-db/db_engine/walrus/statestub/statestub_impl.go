package statestub

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	errorutils "github.com/sei-protocol/sei-chain/sei-db/common/errors"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/pebbledb"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
)

// The subdirectory holding the live database.
const databaseDirName = "state"

// The subdirectory checkpoints are written into before they are handed to a caller.
const checkpointsDirName = "checkpoints"

var _ StateStub = (*stateStub)(nil)

// stateStub is a flat pebble store that can checkpoint itself.
type stateStub struct {
	config       *Config
	database     types.KeyValueDB
	checkpointer types.Checkpointable
	checkpoints  string

	// Guards the committed block number, which Get and Checkpoint read while CommitBlock advances it.
	lock        sync.Mutex
	blockNumber uint64
	started     bool
}

// CommitBlock applies a block's changes and makes them durable.
func (s *stateStub) CommitBlock(blockNumber uint64, changeSets []*proto.NamedChangeSet) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	if s.started && blockNumber != s.blockNumber+1 {
		return fmt.Errorf("block %d does not follow block %d", blockNumber, s.blockNumber)
	}

	batch := s.database.NewBatch()
	defer func() { _ = batch.Close() }()

	for _, changeSet := range changeSets {
		if changeSet.Name != s.config.StoreName {
			return fmt.Errorf("block %d carries a changeset named %q, but this store holds %q",
				blockNumber, changeSet.Name, s.config.StoreName)
		}
		for _, pair := range changeSet.Changeset.Pairs {
			var err error
			if pair.Delete {
				err = batch.Delete(pair.Key)
			} else {
				err = batch.Set(pair.Key, pair.Value)
			}
			if err != nil {
				return fmt.Errorf("failed to stage a change from block %d: %w", blockNumber, err)
			}
		}
	}

	if err := batch.Commit(types.WriteOptions{Sync: false}); err != nil {
		return fmt.Errorf("failed to commit block %d: %w", blockNumber, err)
	}
	s.blockNumber = blockNumber
	s.started = true
	return nil
}

// Get returns the value key currently holds.
func (s *stateStub) Get(key []byte) (value []byte, found bool, err error) {
	stored, err := s.database.Get(key)
	if err != nil {
		if errors.Is(err, errorutils.ErrNotFound) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("failed to read the state stub: %w", err)
	}
	return stored, true, nil
}

// BlockNumber returns the last block committed.
func (s *stateStub) BlockNumber() (ok bool, blockNumber uint64) {
	s.lock.Lock()
	defer s.lock.Unlock()
	return s.started, s.blockNumber
}

// Checkpoint writes an image of state as of the last committed block into a fresh directory.
func (s *stateStub) Checkpoint() (directory string, blockNumber uint64, err error) {
	s.lock.Lock()
	blockNumber = s.blockNumber
	started := s.started
	s.lock.Unlock()

	if !started {
		return "", 0, fmt.Errorf("nothing has been committed yet")
	}

	directory = filepath.Join(s.checkpoints, fmt.Sprintf("checkpoint-%020d", blockNumber))
	if err := os.RemoveAll(directory); err != nil {
		return "", 0, fmt.Errorf("failed to clear %s: %w", directory, err)
	}
	if err := s.checkpointer.Checkpoint(directory); err != nil {
		return "", 0, fmt.Errorf("failed to checkpoint at block %d: %w", blockNumber, err)
	}
	return directory, blockNumber, nil
}

// Close releases the store's resources.
func (s *stateStub) Close() error {
	if err := s.database.Close(); err != nil {
		return fmt.Errorf("failed to close the state stub: %w", err)
	}
	return nil
}

// New opens a StateStub in the configured directory.
func New(config *Config) (StateStub, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	checkpoints := filepath.Join(config.Path, checkpointsDirName)
	if err := os.MkdirAll(checkpoints, 0o750); err != nil {
		return nil, fmt.Errorf("failed to create %s: %w", checkpoints, err)
	}

	pebbleConfig := pebbledb.DefaultConfig()
	pebbleConfig.DataDir = filepath.Join(config.Path, databaseDirName)
	pebbleConfig.EnableMetrics = !config.DisableMetrics
	database, err := pebbledb.Open(context.Background(), &pebbleConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to open the state stub database: %w", err)
	}

	checkpointer, ok := database.(types.Checkpointable)
	if !ok {
		_ = database.Close()
		return nil, fmt.Errorf("the state stub database cannot checkpoint")
	}

	return &stateStub{
		config:       config,
		database:     database,
		checkpointer: checkpointer,
		checkpoints:  checkpoints,
	}, nil
}
