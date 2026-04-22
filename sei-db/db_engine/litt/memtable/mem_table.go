package memtable

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Layr-Labs/eigenda/common/structures"
	"github.com/Layr-Labs/eigenda/litt"
	"github.com/Layr-Labs/eigenda/litt/types"
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
	expirationQueue *structures.Queue[*expirationRecord]

	// Protects access to data and expirationQueue.
	//
	// This implementation could be made with smaller granularity locks to improve multithreaded performance,
	// at the cost of code complexity. But since this implementation is primary intended for use in tests,
	// such optimization is not necessary.
	lock sync.RWMutex

	shutdown atomic.Bool
}

// NewMemTable creates a new in-memory table.
func NewMemTable(config *litt.Config, name string) litt.ManagedTable {

	table := &memTable{
		clock:           config.Clock,
		name:            name,
		ttl:             config.TTL,
		data:            make(map[string][]byte),
		expirationQueue: structures.NewQueue[*expirationRecord](1024),
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

func (m *memTable) Put(key []byte, value []byte) error {
	stringKey := string(key)
	expiration := &expirationRecord{
		creationTime: m.clock(),
		key:          stringKey,
	}

	m.lock.Lock()
	defer m.lock.Unlock()

	_, ok := m.data[stringKey]
	if ok {
		return fmt.Errorf("key %x already exists", key)
	}
	m.data[stringKey] = value
	m.expirationQueue.Push(expiration)

	return nil
}

func (m *memTable) PutBatch(batch []*types.KVPair) error {
	for _, kv := range batch {
		err := m.Put(kv.Key, kv.Value)
		if err != nil {
			return err
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

func (m *memTable) SetShardingFactor(shardingFactor uint32) error {
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
