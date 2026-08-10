package snapshot

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/common/threading"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
)

func TestNewSnapshotEngineValid(t *testing.T) {
	engine := newTestEngineWithDB(t, newTestDB(nil), 8, 1<<20)
	require.NotNil(t, engine)
}

func TestConfigValidateRejectsBadFields(t *testing.T) {
	base := func() *SnapshotEngineConfig {
		return DefaultTestSnapshotEngineConfig()
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
		{"nameEmpty", func(c *SnapshotEngineConfig) { c.Name = "" }},
		{"scrapeIntervalZeroWithMetricsEnabled", func(c *SnapshotEngineConfig) {
			c.MetricsEnabled = true
			c.MetricsScrapeIntervalSeconds = 0
		}},
		{"maxUnflushedZero", func(c *SnapshotEngineConfig) { c.MaxUnflushedVersions = 0 }},
		{"targetBytesZero", func(c *SnapshotEngineConfig) { c.TargetBytesPerFlush = 0 }},
		{"reservedPrefixEmpty", func(c *SnapshotEngineConfig) { c.ReservedPrefix = "" }},
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
	c.ShardCount = 3 // invalid: not a power of two
	pool := threading.NewAdHocPool()
	defer pool.Close()
	_, err := NewSnapshotEngine(c, newTestDB(nil), pool, pool)
	require.Error(t, err)
}

func TestEngineSetGetDelete(t *testing.T) {
	engine := newTestEngineWithDB(t, newTestDB(nil), 4, 1<<20)

	require.NoError(t, engine.Set([]byte("k"), []byte("v")))
	val, found, err := engine.Get([]byte("k"), true)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, []byte("v"), val)

	require.NoError(t, engine.Delete([]byte("k")))
	_, found, err = engine.Get([]byte("k"), true)
	require.NoError(t, err)
	require.False(t, found)
}

func TestEngineSetNilIsDelete(t *testing.T) {
	engine, _ := newTestEngine(t, map[string][]byte{"k": []byte("v")}, 1, 1<<20)
	require.NoError(t, engine.Set([]byte("k"), nil))
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
	require.NoError(t, engine.BatchSet([]*proto.KVPair{
		{Key: []byte("a"), Value: []byte("1")},
		{Key: []byte("b"), Value: []byte("2")},
		{Key: []byte("c"), Delete: true}, // delete of a non-existent key
	}))

	got, err := engine.BatchGet([][]byte{[]byte("a"), []byte("b"), []byte("c"), []byte("missing")})
	require.NoError(t, err)
	require.Equal(t, []byte("1"), got["a"])
	require.Equal(t, []byte("2"), got["b"])
	_, cPresent := got["c"]
	require.False(t, cPresent, "deleted key must be absent")
	_, missingPresent := got["missing"]
	require.False(t, missingPresent, "not-found key must be absent")
}

func TestNameReportsConfiguredName(t *testing.T) {
	cfg := newTestConfig(1, 1<<20)
	cfg.Name = "account"
	engine := newTestEngineWithConfig(t, cfg, newTestDB(nil))
	require.Equal(t, "account", engine.Name())
}

func TestFlushSyncTrueStillRoundTrips(t *testing.T) {
	cfg := newTestConfig(1, 1<<20)
	cfg.FlushSync = true
	db := newTestDB(nil)
	engine := newTestEngineWithConfig(t, cfg, db)

	require.NoError(t, engine.Set([]byte("k"), []byte("v")))
	snap, err := engine.Commit()
	require.NoError(t, err)
	require.NoError(t, snap.Finalize(hashWrites(testHash)))
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
		require.NoError(t, engine.Set([]byte{byte(i)}, []byte("v")))
	}
	commitFinalizeRelease(t, engine)
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
			require.NoError(t, engine.Set(k, k))
			_, _, err := engine.Get(k, true)
			require.NoError(t, err)
		}(i)
	}
	wg.Wait()
}
