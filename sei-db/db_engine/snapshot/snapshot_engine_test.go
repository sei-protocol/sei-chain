package snapshot

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/common/threading"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
)

func TestNewSnapshotEngineValid(t *testing.T) {
	engine := newTestEngineWithDB(t, newTestDB(nil), 8, 1<<20)
	require.NotNil(t, engine)
}

func TestConfigValidateRejectsBadFields(t *testing.T) {
	base := func() *SnapshotEngineConfig {
		c := DefaultTestSnapshotEngineConfig()
		c.MetricsName = "test"
		return c
	}
	require.NoError(t, base().Validate(), "baseline config should be valid")

	cases := []struct {
		name string
		mut  func(*SnapshotEngineConfig)
	}{
		{"shardCountZero", func(c *SnapshotEngineConfig) { c.ShardCount = 0 }},
		{"shardCountNotPowerOfTwo", func(c *SnapshotEngineConfig) { c.ShardCount = 3 }},
		{"maxSizeZero", func(c *SnapshotEngineConfig) { c.MaxSize = 0 }},
		{"overheadZero", func(c *SnapshotEngineConfig) { c.EstimatedOverheadPerEntry = 0 }},
		{"metricsNameEmpty", func(c *SnapshotEngineConfig) { c.MetricsName = "" }},
		{"maxUnretiredZero", func(c *SnapshotEngineConfig) { c.MaxUnretiredVersions = 0 }},
		{"targetKeysZero", func(c *SnapshotEngineConfig) { c.TargetKeysPerFlush = 0 }},
		{"hashKeyEmpty", func(c *SnapshotEngineConfig) { c.HashKey = "" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := base()
			tc.mut(c)
			require.Error(t, c.Validate())
		})
	}
}

func TestNewSnapshotEngineRejectsInvalidConfig(t *testing.T) {
	c := DefaultTestSnapshotEngineConfig()
	c.MetricsName = "" // invalid
	pool := threading.NewAdHocPool()
	defer pool.Close()
	_, err := NewSnapshotEngine(context.Background(), c, newTestDB(nil), pool, pool)
	require.Error(t, err)
}

func TestEngineSetGetDelete(t *testing.T) {
	engine := newTestEngineWithDB(t, newTestDB(nil), 4, 1<<20)

	engine.Set([]byte("k"), []byte("v"))
	val, found, err := engine.Get([]byte("k"), true)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, []byte("v"), val)

	engine.Delete([]byte("k"))
	_, found, err = engine.Get([]byte("k"), true)
	require.NoError(t, err)
	require.False(t, found)
}

func TestEngineSetNilIsDelete(t *testing.T) {
	engine, _ := newTestEngine(t, map[string][]byte{"k": []byte("v")}, 1, 1<<20)
	engine.Set([]byte("k"), nil)
	_, found, err := engine.Get([]byte("k"), true)
	require.NoError(t, err)
	require.False(t, found)
}

func TestEngineReadThroughFromSeededDB(t *testing.T) {
	engine, _ := newTestEngine(t, map[string][]byte{"seeded": []byte("value")}, 4, 1<<20)
	val, found, err := engine.Get([]byte("seeded"), true)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, []byte("value"), val)
}

func TestEngineGetNotFound(t *testing.T) {
	engine := newTestEngineWithDB(t, newTestDB(nil), 1, 1<<20)
	_, found, err := engine.Get([]byte("missing"), true)
	require.NoError(t, err)
	require.False(t, found)
}

func TestEngineGetPropagatesDBError(t *testing.T) {
	db := newTestDB(nil)
	engine := newTestEngineWithDB(t, db, 1, 1<<20)
	db.getErr = errors.New("db boom") // inject after open (open itself reads the hash key)
	_, _, err := engine.Get([]byte("k"), true)
	require.Error(t, err)
	require.ErrorContains(t, err, "db boom")
}

func TestEngineBatchSetThenBatchGet(t *testing.T) {
	engine := newTestEngineWithDB(t, newTestDB(nil), 4, 1<<20)
	require.NoError(t, engine.BatchSet([]Mutation{
		{Key: []byte("a"), Value: []byte("1")},
		{Key: []byte("b"), Value: []byte("2")},
		{Key: []byte("c"), Value: nil}, // delete of a non-existent key
	}))

	req := map[string]types.BatchGetResult{"a": {}, "b": {}, "c": {}, "missing": {}}
	require.NoError(t, engine.BatchGet(req))
	require.Equal(t, []byte("1"), req["a"].Value)
	require.Equal(t, []byte("2"), req["b"].Value)
	require.False(t, req["c"].IsFound())
	require.False(t, req["missing"].IsFound())
}

func TestInitialHashReadFromDBOnOpen(t *testing.T) {
	hashKey := DefaultTestSnapshotEngineConfig().HashKey
	engine, _ := newTestEngine(t, map[string][]byte{hashKey: []byte("prior-hash")}, 2, 1<<20)
	require.Equal(t, []byte("prior-hash"), engine.InitialHash())
}

func TestInitialHashNilWhenNeverFlushed(t *testing.T) {
	engine := newTestEngineWithDB(t, newTestDB(nil), 2, 1<<20)
	require.Nil(t, engine.InitialHash())
}

func TestFlushSyncTrueStillRoundTrips(t *testing.T) {
	cfg := newTestConfig(1, 1<<20)
	cfg.FlushSync = true
	db := newTestDB(nil)
	engine := newTestEngineWithConfig(t, cfg, db)

	engine.Set([]byte("k"), []byte("v"))
	snap, err := engine.Snapshot()
	require.NoError(t, err)
	require.NoError(t, snap.SetHash(testHash))
	awaitFlushed(t, snap, time.Second)
	require.NoError(t, snap.Release())

	kv, ok := db.get("k")
	require.True(t, ok)
	require.Equal(t, []byte("v"), kv)
}

// TestMetricsEnabledDoesNotBreakEngine is a smoke test: with metrics on and a fast scrape interval,
// the background collect loop and snapshot phase timer run without disturbing normal operation.
func TestMetricsEnabledDoesNotBreakEngine(t *testing.T) {
	cfg := newTestConfig(2, 1<<20)
	cfg.MetricsEnabled = true
	cfg.MetricsScrapeIntervalSeconds = 0.001
	engine := newTestEngineWithConfig(t, cfg, newTestDB(nil))

	for i := 0; i < 20; i++ {
		engine.Set([]byte{byte(i)}, []byte("v"))
	}
	snapshotAndHashRelease(t, engine)
	time.Sleep(10 * time.Millisecond) // let the metrics scrape loop fire at least once

	val, found, err := engine.Get([]byte{0}, true)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, []byte("v"), val)
}

func TestEngineConcurrentSetGet(t *testing.T) {
	engine := newTestEngineWithDB(t, newTestDB(nil), 8, 1<<20)
	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			k := []byte{byte(i)}
			engine.Set(k, k)
			_, _, err := engine.Get(k, true)
			require.NoError(t, err)
		}(i)
	}
	wg.Wait()
}
