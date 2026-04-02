package evm

import (
	"fmt"

	commonevm "github.com/sei-protocol/sei-chain/sei-db/common/evm"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/ss/backend"
	iavl "github.com/sei-protocol/sei-chain/sei-iavl"
)

var _ types.StateStore = (*EVMStateStore)(nil)

// EVMStateStore manages a single MVCC DB for all EVM sub-types.
// Keys are namespaced by store key prefix (s/k:<storeType>/) within the DB.
type EVMStateStore struct {
	db  types.StateStore
	dir string
}

// NewEVMStateStore opens a single MVCC DB for all EVM state.
func NewEVMStateStore(dir string, ssConfig config.StateStoreConfig) (*EVMStateStore, error) {
	opener := backend.ResolveBackend(ssConfig.Backend)

	cfg := ssConfig
	cfg.DBDirectory = dir
	cfg.UseDefaultComparer = true

	db, err := opener(dir, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to open EVM MVCC DB: %w", err)
	}

	return &EVMStateStore{db: db, dir: dir}, nil
}

func (s *EVMStateStore) routeKey(key []byte) (string, []byte) {
	storeType, strippedKey := commonevm.ParseEVMKey(key)
	if storeType == StoreEmpty {
		return "", nil
	}
	return StoreTypeName(storeType), strippedKey
}

func (s *EVMStateStore) Get(_ string, version int64, key []byte) ([]byte, error) {
	storeKey, strippedKey := s.routeKey(key)
	if storeKey == "" {
		return nil, nil
	}
	return s.db.Get(storeKey, version, strippedKey)
}

func (s *EVMStateStore) Has(_ string, version int64, key []byte) (bool, error) {
	storeKey, strippedKey := s.routeKey(key)
	if storeKey == "" {
		return false, nil
	}
	return s.db.Has(storeKey, version, strippedKey)
}

func (s *EVMStateStore) Iterator(_ string, _ int64, _, _ []byte) (types.DBIterator, error) {
	return nil, fmt.Errorf("EVMStateStore: cross-type iteration not supported; use Cosmos_SS")
}

func (s *EVMStateStore) ReverseIterator(_ string, _ int64, _, _ []byte) (types.DBIterator, error) {
	return nil, fmt.Errorf("EVMStateStore: cross-type reverse iteration not supported; use Cosmos_SS")
}

func (s *EVMStateStore) RawIterate(_ string, _ func([]byte, []byte, int64) bool) (bool, error) {
	return false, fmt.Errorf("EVMStateStore: RawIterate not supported")
}

func (s *EVMStateStore) GetLatestVersion() int64 {
	return s.db.GetLatestVersion()
}

func (s *EVMStateStore) SetLatestVersion(version int64) error {
	return s.db.SetLatestVersion(version)
}

func (s *EVMStateStore) GetEarliestVersion() int64 {
	return s.db.GetEarliestVersion()
}

func (s *EVMStateStore) SetEarliestVersion(version int64, ignoreVersion bool) error {
	return s.db.SetEarliestVersion(version, ignoreVersion)
}

func (s *EVMStateStore) ApplyChangesetSync(version int64, changesets []*proto.NamedChangeSet) error {
	rekeyed := s.rekeyChangesets(changesets)
	if len(rekeyed) == 0 {
		return nil
	}
	return s.db.ApplyChangesetSync(version, rekeyed)
}

func (s *EVMStateStore) ApplyChangesetAsync(version int64, changesets []*proto.NamedChangeSet) error {
	rekeyed := s.rekeyChangesets(changesets)
	if len(rekeyed) == 0 {
		return nil
	}
	return s.db.ApplyChangesetAsync(version, rekeyed)
}

// rekeyChangesets splits EVM changesets by sub-type store key.
func (s *EVMStateStore) rekeyChangesets(changesets []*proto.NamedChangeSet) []*proto.NamedChangeSet {
	grouped := make(map[string][]*iavl.KVPair, NumEVMStoreTypes)
	for _, cs := range changesets {
		if cs.Name != EVMStoreKey {
			continue
		}
		for _, kvPair := range cs.Changeset.Pairs {
			storeKey, strippedKey := s.routeKey(kvPair.Key)
			if storeKey == "" {
				continue
			}
			grouped[storeKey] = append(grouped[storeKey], &iavl.KVPair{
				Key:    strippedKey,
				Value:  kvPair.Value,
				Delete: kvPair.Delete,
			})
		}
	}
	result := make([]*proto.NamedChangeSet, 0, len(grouped))
	for name, pairs := range grouped {
		result = append(result, &proto.NamedChangeSet{
			Name:      name,
			Changeset: iavl.ChangeSet{Pairs: pairs},
		})
	}
	return result
}

func (s *EVMStateStore) Import(version int64, ch <-chan types.SnapshotNode) error {
	const flushThreshold = 10000
	grouped := make(map[string][]*iavl.KVPair, NumEVMStoreTypes)
	pending := 0

	flush := func() error {
		if len(grouped) == 0 {
			return nil
		}
		rekeyed := make([]*proto.NamedChangeSet, 0, len(grouped))
		for name, pairs := range grouped {
			rekeyed = append(rekeyed, &proto.NamedChangeSet{
				Name:      name,
				Changeset: iavl.ChangeSet{Pairs: pairs},
			})
		}
		if err := s.db.ApplyChangesetSync(version, rekeyed); err != nil {
			return err
		}
		grouped = make(map[string][]*iavl.KVPair, NumEVMStoreTypes)
		pending = 0
		return nil
	}

	for node := range ch {
		storeKey, strippedKey := s.routeKey(node.Key)
		if storeKey == "" {
			continue
		}
		grouped[storeKey] = append(grouped[storeKey], &iavl.KVPair{
			Key:   strippedKey,
			Value: node.Value,
		})
		pending++
		if pending >= flushThreshold {
			if err := flush(); err != nil {
				return err
			}
		}
	}
	return flush()
}

func (s *EVMStateStore) Prune(version int64) error {
	return s.db.Prune(version)
}

func (s *EVMStateStore) Close() error {
	return s.db.Close()
}
