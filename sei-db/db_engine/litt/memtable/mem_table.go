package memtable

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/types"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/util"
)

var _ litt.ManagedTable = &memTable{}

// expirationRecord is a record of when a key was inserted into the table.
type expirationRecord struct {
	// The time at which the key was inserted into the table.
	creationTime time.Time
	// A stringified version of the key.
	key string
}

// memTable is a simple implementation of a Table that stores its data in memory.
type memTable struct {
	// A function that returns the current time.
	clock func() time.Time

	// The name of the table.
	name string

	// The time-to-live for data in this table.
	ttl time.Duration

	// The actual data store.
	data map[string][]byte

	// Keeps track of when data should be deleted.
	expirationQueue *util.Queue[*expirationRecord]

	// Protects access to data and expirationQueue.
	//
	// This implementation could be made with smaller granularity locks to improve multithreaded performance,
	// at the cost of code complexity. But since this implementation is primary intended for use in tests,
	// such optimization is not necessary.
	lock sync.RWMutex

	shutdown atomic.Bool
}

// NewMemTable creates a new in-memory table.
func NewMemTable(config *litt.Config, runtimeConfig *litt.RuntimeConfig, name string) litt.ManagedTable {

	table := &memTable{
		clock:           runtimeConfig.Clock,
		name:            name,
		ttl:             config.TTL,
		data:            make(map[string][]byte),
		expirationQueue: util.NewQueue[*expirationRecord](1024),
	}

	if config.GCPeriod > 0 {
		ticker := time.NewTicker(config.GCPeriod)
		go func() {
			defer ticker.Stop()
			for !table.shutdown.Load() {
				<-ticker.C
				err := table.RunGC()
				if err != nil {
					panic(err) // this is a class designed for use in testing, not worth properly handling errors
				}
			}
		}()
	}

	return table
}

func (m *memTable) Size() uint64 {
	// Technically speaking, this table stores zero bytes on disk, and this method
	// is contractually obligated to return only the size of the data on disk.
	return 0
}

func (m *memTable) Name() string {
	return m.name
}

func (m *memTable) KeyCount() uint64 {
	m.lock.RLock()
	defer m.lock.RUnlock()
	return uint64(len(m.data))
}

func (m *memTable) Put(key []byte, value []byte, secondaryKeys ...*types.SecondaryKey) error {
	// Validate first so a failed validation never leaves a partial insert behind.
	if key == nil {
		return fmt.Errorf("nil keys are not supported")
	}
	if value == nil {
		return fmt.Errorf("nil values are not supported")
	}
	seen := make(map[string]struct{}, 1+len(secondaryKeys))
	seen[string(key)] = struct{}{}
	for _, sk := range secondaryKeys {
		if sk == nil {
			return fmt.Errorf("nil secondary key is not supported")
		}
		if sk.Key == nil {
			return fmt.Errorf("nil secondary key bytes are not supported")
		}
		end := uint64(sk.Offset) + uint64(sk.Length)
		if end > uint64(len(value)) {
			return fmt.Errorf(
				"secondary key range [%d, %d) exceeds value length %d", sk.Offset, end, len(value))
		}
		skKey := string(sk.Key)
		if _, dup := seen[skKey]; dup {
			return fmt.Errorf("duplicate key %x within Put", sk.Key)
		}
		seen[skKey] = struct{}{}
	}

	stringKey := string(key)
	now := m.clock()

	m.lock.Lock()
	defer m.lock.Unlock()

	if _, ok := m.data[stringKey]; ok {
		return fmt.Errorf("key %x already exists", key)
	}
	for _, sk := range secondaryKeys {
		if _, ok := m.data[string(sk.Key)]; ok {
			return fmt.Errorf("secondary key %x already exists", sk.Key)
		}
	}

	m.data[stringKey] = value
	m.expirationQueue.Push(&expirationRecord{creationTime: now, key: stringKey})
	for _, sk := range secondaryKeys {
		skString := string(sk.Key)
		m.data[skString] = value[sk.Offset : sk.Offset+sk.Length]
		m.expirationQueue.Push(&expirationRecord{creationTime: now, key: skString})
	}

	return nil
}

func (m *memTable) PutBatch(batch []*types.PutRequest) error {
	// Stateless validation pass: matches single-Put validation rules. If any request is
	// invalid, the entire batch is rejected before any writes are applied. This mirrors the
	// validation-atomic behavior of DiskTable.PutBatch.
	for _, req := range batch {
		if req.Key == nil {
			return fmt.Errorf("nil keys are not supported")
		}
		if req.Value == nil {
			return fmt.Errorf("nil values are not supported")
		}
		seen := make(map[string]struct{}, 1+len(req.SecondaryKeys))
		seen[string(req.Key)] = struct{}{}
		for _, sk := range req.SecondaryKeys {
			if sk == nil {
				return fmt.Errorf("nil secondary key is not supported")
			}
			if sk.Key == nil {
				return fmt.Errorf("nil secondary key bytes are not supported")
			}
			end := uint64(sk.Offset) + uint64(sk.Length)
			if end > uint64(len(req.Value)) {
				return fmt.Errorf(
					"secondary key range [%d, %d) exceeds value length %d", sk.Offset, end, len(req.Value))
			}
			skKey := string(sk.Key)
			if _, dup := seen[skKey]; dup {
				return fmt.Errorf("duplicate key %x within PutRequest", sk.Key)
			}
			seen[skKey] = struct{}{}
		}
	}

	now := m.clock()

	m.lock.Lock()
	defer m.lock.Unlock()

	// Collision pass: ensure no key in any request already exists in the table. Held under the
	// same lock as the apply pass, so the batch as a whole succeeds or fails atomically.
	for _, req := range batch {
		if _, ok := m.data[string(req.Key)]; ok {
			return fmt.Errorf("key %x already exists", req.Key)
		}
		for _, sk := range req.SecondaryKeys {
			if _, ok := m.data[string(sk.Key)]; ok {
				return fmt.Errorf("secondary key %x already exists", sk.Key)
			}
		}
	}

	for _, req := range batch {
		stringKey := string(req.Key)
		m.data[stringKey] = req.Value
		m.expirationQueue.Push(&expirationRecord{creationTime: now, key: stringKey})
		for _, sk := range req.SecondaryKeys {
			skString := string(sk.Key)
			m.data[skString] = req.Value[sk.Offset : sk.Offset+sk.Length]
			m.expirationQueue.Push(&expirationRecord{creationTime: now, key: skString})
		}
	}

	return nil
}

func (m *memTable) Get(key []byte) (value []byte, exists bool, err error) {
	value, exists, _, err = m.CacheAwareGet(key, false)
	return value, exists, err
}

func (m *memTable) CacheAwareGet(key []byte, _ bool) (value []byte, exists bool, hot bool, err error) {
	m.lock.RLock()
	defer m.lock.RUnlock()

	value, exists = m.data[string(key)]
	if !exists {
		return nil, false, false, nil
	}

	return value, true, true, nil
}

func (m *memTable) Exists(key []byte) (exists bool, err error) {
	m.lock.RLock()
	defer m.lock.RUnlock()
	_, exists = m.data[string(key)]
	return exists, nil
}

func (m *memTable) Flush() error {
	// This is a no-op for a memory table. Memory tables are ephemeral by nature.
	return nil
}

func (m *memTable) SetTTL(ttl time.Duration) error {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.ttl = ttl
	return nil
}

func (m *memTable) Destroy() error {
	m.lock.Lock()
	defer m.lock.Unlock()

	m.data = make(map[string][]byte)
	m.expirationQueue.Clear()

	return nil
}

func (m *memTable) Close() error {
	m.shutdown.Store(true)
	return nil
}

func (m *memTable) SetWriteCacheSize(size uint64) error {
	return nil
}

func (m *memTable) SetReadCacheSize(size uint64) error {
	return nil
}

func (m *memTable) SetShardingFactor(shardingFactor uint8) error {
	// the memory table has no concept of sharding
	return nil
}

func (m *memTable) RunGC() error {
	m.lock.Lock()
	defer m.lock.Unlock()

	if m.ttl == 0 {
		return nil
	}

	now := m.clock()
	earliestPermittedCreationTime := now.Add(-m.ttl)

	for {
		expiration, ok := m.expirationQueue.TryPeek()
		if !ok {
			break
		}
		if expiration.creationTime.After(earliestPermittedCreationTime) {
			break
		}
		m.expirationQueue.Pop()
		delete(m.data, expiration.key)
	}

	return nil
}
