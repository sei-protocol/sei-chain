package mvcc

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/test"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	iavl "github.com/sei-protocol/sei-chain/sei-iavl/proto"
)

func TestStorageTestSuite(t *testing.T) {
	pebbleConfig := config.DefaultStateStoreConfig()
	pebbleConfig.Backend = "pebbledb"
	s := &sstest.StorageTestSuite{
		BaseStorageTestSuite: sstest.BaseStorageTestSuite{
			NewDB: func(dir string, config config.StateStoreConfig) (types.StateStore, error) {
				return OpenDB(dir, config)
			},
			Config:         pebbleConfig,
			EmptyBatchSize: 12,
		},
	}

	suite.Run(t, s)
}

// TestStorageTestSuiteDefaultComparer runs the base storage test suite with Pebble's DefaultComparer
// instead of MVCCComparer. This is useful for new databases that don't need backwards compatibility.
// Note: Iterator tests are not included because DefaultComparer doesn't have the Split function
// configured for MVCC key encoding, so NextPrefix/SeekLT operations won't work correctly.
// BaseStorageTestSuite contains only tests that work with both comparers.
func TestStorageTestSuiteDefaultComparer(t *testing.T) {
	pebbleConfig := config.DefaultStateStoreConfig()
	pebbleConfig.Backend = "pebbledb"
	pebbleConfig.UseDefaultComparer = true

	s := &sstest.BaseStorageTestSuite{
		NewDB: func(dir string, config config.StateStoreConfig) (types.StateStore, error) {
			return OpenDB(dir, config)
		},
		Config:         pebbleConfig,
		EmptyBatchSize: 12,
	}

	suite.Run(t, s)
}

type countingCollector struct {
	mu     sync.Mutex
	counts map[string]int
}

func (c *countingCollector) RecordReadTrace(event types.ReadTraceEvent) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.counts == nil {
		c.counts = map[string]int{}
	}
	c.counts[event.StoreKey+"."+event.Layer+"."+event.Operation]++
}

func (c *countingCollector) Count(name string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.counts[name]
}

func TestTraceOptimizedHistoricalGetsReuseIteratorAndCache(t *testing.T) {
	cfg := config.DefaultStateStoreConfig()
	cfg.Backend = "pebbledb"

	dbi, err := OpenDB(t.TempDir(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = dbi.Close() }()

	err = dbi.ApplyChangesetSync(10, []*proto.NamedChangeSet{
		{
			Name: "bank",
			Changeset: iavl.ChangeSet{
				Pairs: []*iavl.KVPair{
					{Key: []byte("balance/alice"), Value: []byte("100")},
					{Key: []byte("balance/bob"), Value: []byte("200")},
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	traceable, ok := dbi.(types.TraceableStateStore)
	if !ok {
		t.Fatal("database should be traceable")
	}
	collector := &countingCollector{}
	traced := traceable.WithReadTraceCollector(collector)
	defer func() { _ = traced.Close() }()

	val, err := traced.Get("bank", 10, []byte("balance/alice"))
	if err != nil {
		t.Fatal(err)
	}
	if string(val) != "100" {
		t.Fatalf("expected first read value 100, got %q", string(val))
	}

	val, err = traced.Get("bank", 10, []byte("balance/alice"))
	if err != nil {
		t.Fatal(err)
	}
	if string(val) != "100" {
		t.Fatalf("expected cached read value 100, got %q", string(val))
	}

	val, err = traced.Get("bank", 10, []byte("balance/bob"))
	if err != nil {
		t.Fatal(err)
	}
	if string(val) != "200" {
		t.Fatalf("expected second key value 200, got %q", string(val))
	}

	if got := collector.Count("bank.pebble.newIter"); got != 1 {
		t.Fatalf("expected one iterator creation, got %d", got)
	}
	if got := collector.Count("bank.pebble.last"); got != 0 {
		t.Fatalf("expected zero Last() calls in optimized path, got %d", got)
	}
	if got := collector.Count("bank.pebble.seekLT"); got != 2 {
		t.Fatalf("expected two SeekLT() calls for two cache misses, got %d", got)
	}
}
