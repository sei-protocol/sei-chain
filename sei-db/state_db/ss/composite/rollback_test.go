package composite

import (
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/stretchr/testify/require"
)

type recordingRollbackStore struct {
	types.StateStore
	name        string
	events      *[]string
	suspensions int
	coverageErr error
}

func (s *recordingRollbackStore) record(event string) {
	*s.events = append(*s.events, s.name+"-"+event)
}

func (s *recordingRollbackStore) SuspendChangelogPruning() {
	s.suspensions++
	s.record("suspend")
}

func (s *recordingRollbackStore) ResumeChangelogPruning() {
	s.suspensions--
	s.record("resume")
}

func (s *recordingRollbackStore) CheckRollbackCoverage(_ int64) error {
	s.record("check")
	if s.suspensions == 0 {
		return fmt.Errorf("%s changelog pruning was not suspended", s.name)
	}
	return s.coverageErr
}

func (s *recordingRollbackStore) Rollback(_ int64) error {
	s.record("rollback")
	if s.suspensions == 0 {
		return fmt.Errorf("%s changelog pruning was not suspended", s.name)
	}
	return nil
}

func TestCompositeRollbackSuspendsEveryChangelogBeforePreflight(t *testing.T) {
	events := []string{}
	cosmosStore := &recordingRollbackStore{name: "cosmos", events: &events}
	evmStore := &recordingRollbackStore{name: "evm", events: &events}
	store := &CompositeStateStore{
		cosmosStore: cosmosStore,
		evmStore:    evmStore,
	}

	require.NoError(t, store.Rollback(3))
	require.Equal(t, []string{
		"cosmos-suspend",
		"evm-suspend",
		"cosmos-check",
		"evm-check",
		"cosmos-rollback",
		"evm-rollback",
		"evm-resume",
		"cosmos-resume",
	}, events)
	require.Zero(t, cosmosStore.suspensions)
	require.Zero(t, evmStore.suspensions)
}

func TestCompositeRollbackResumesEveryChangelogAfterPreflightFailure(t *testing.T) {
	events := []string{}
	cosmosStore := &recordingRollbackStore{name: "cosmos", events: &events}
	evmStore := &recordingRollbackStore{
		name:        "evm",
		events:      &events,
		coverageErr: errors.New("coverage moved"),
	}
	store := &CompositeStateStore{
		cosmosStore: cosmosStore,
		evmStore:    evmStore,
	}

	err := store.Rollback(3)
	require.ErrorContains(t, err, "coverage moved")
	require.Equal(t, []string{
		"cosmos-suspend",
		"evm-suspend",
		"cosmos-check",
		"evm-check",
		"evm-resume",
		"cosmos-resume",
	}, events)
	require.Zero(t, cosmosStore.suspensions)
	require.Zero(t, evmStore.suspensions)
}

func TestCompositeRollbackWithEVMSplit(t *testing.T) {
	for _, separate := range []bool{false, true} {
		t.Run(func() string {
			if separate {
				return "separate-subdbs"
			}
			return "unified-evm-db"
		}(), func(t *testing.T) {
			dir := t.TempDir()
			ssConfig := config.DefaultStateStoreConfig()
			ssConfig.Backend = config.PebbleDBBackend
			ssConfig.AsyncWriteBuffer = 100
			ssConfig.KeepRecent = 100000
			ssConfig.EVMSplit = true
			ssConfig.SeparateEVMSubDBs = separate
			ssConfig.EVMDBDirectory = filepath.Join(dir, "evm_ss")

			store, err := NewCompositeStateStore(ssConfig, dir)
			require.NoError(t, err)
			defer store.Close()

			addr := make([]byte, 20)
			slot := make([]byte, 32)
			storageKey := append([]byte{0x03}, append(addr, slot...)...)
			for version := int64(1); version <= 5; version++ {
				require.NoError(t, store.ApplyChangesetAsync(version, []*proto.NamedChangeSet{
					{
						Name: "bank",
						Changeset: proto.ChangeSet{
							Pairs: []*proto.KVPair{{Key: []byte("balance"), Value: []byte{byte(version)}}},
						},
					},
					{
						Name: "evm",
						Changeset: proto.ChangeSet{
							Pairs: []*proto.KVPair{{Key: storageKey, Value: []byte{byte(version)}}},
						},
					},
				}))
			}

			require.NoError(t, store.Rollback(3))
			require.Equal(t, int64(3), store.GetLatestVersion())

			val, err := store.Get("bank", 3, []byte("balance"))
			require.NoError(t, err)
			require.Equal(t, []byte{3}, val)

			val, err = store.Get("evm", 3, storageKey)
			require.NoError(t, err)
			require.Equal(t, []byte{3}, val)

			require.NoError(t, store.ApplyChangesetAsync(4, []*proto.NamedChangeSet{
				{
					Name: "bank",
					Changeset: proto.ChangeSet{
						Pairs: []*proto.KVPair{{Key: []byte("balance"), Value: []byte{4}}},
					},
				},
				{
					Name: "evm",
					Changeset: proto.ChangeSet{
						Pairs: []*proto.KVPair{{Key: storageKey, Value: []byte{4}}},
					},
				},
			}))
			store.waitForPendingWrites()

			val, err = store.Get("bank", 4, []byte("balance"))
			require.NoError(t, err)
			require.Equal(t, []byte{4}, val)

			val, err = store.Get("evm", 4, storageKey)
			require.NoError(t, err)
			require.Equal(t, []byte{4}, val)
		})
	}
}
