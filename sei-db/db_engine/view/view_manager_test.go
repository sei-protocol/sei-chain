package view

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/common/threading"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
)

func TestNewViewManagerValid(t *testing.T) {
	manager := newTestManagerWithDB(t, newTestDB(nil), 8, 1<<20)
	require.NotNil(t, manager)
}

func TestConfigValidateRejectsBadFields(t *testing.T) {
	base := func() *ViewManagerConfig {
		return DefaultTestViewManagerConfig()
	}
	require.NoError(t, base().Validate(), "baseline config should be valid")

	cases := []struct {
		name string
		mut  func(*ViewManagerConfig)
	}{
		{"shardCountZero", func(c *ViewManagerConfig) { c.ShardCount = 0 }},
		{"shardCountNotPowerOfTwo", func(c *ViewManagerConfig) { c.ShardCount = 3 }},
		{"maxSizeZero", func(c *ViewManagerConfig) { c.MaxSize = 0 }},
		{"overheadZero", func(c *ViewManagerConfig) { c.EstimatedOverheadPerEntry = 0 }},
		{"nameEmpty", func(c *ViewManagerConfig) { c.Name = "" }},
		{"scrapeIntervalZeroWithMetricsEnabled", func(c *ViewManagerConfig) {
			c.MetricsEnabled = true
			c.MetricsScrapeIntervalSeconds = 0
		}},
		{"maxUnflushedZero", func(c *ViewManagerConfig) { c.MaxUnflushedVersions = 0 }},
		{"targetBytesZero", func(c *ViewManagerConfig) { c.TargetBytesPerFlush = 0 }},
		{"reservedPrefixEmpty", func(c *ViewManagerConfig) { c.ReservedPrefix = "" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := base()
			tc.mut(c)
			require.Error(t, c.Validate())
		})
	}
}

func TestNewViewManagerRejectsInvalidConfig(t *testing.T) {
	c := DefaultTestViewManagerConfig()
	c.ShardCount = 3 // invalid: not a power of two
	pool := threading.NewAdHocPool()
	defer pool.Close()
	_, err := NewViewManager(c, newTestDB(nil), pool, pool)
	require.Error(t, err)
}

func TestManagerSetGetDelete(t *testing.T) {
	manager := newTestManagerWithDB(t, newTestDB(nil), 4, 1<<20)

	require.NoError(t, manager.Set([]byte("k"), []byte("v")))
	val, found, err := manager.Get([]byte("k"), true)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, []byte("v"), val)

	require.NoError(t, manager.Delete([]byte("k")))
	_, found, err = manager.Get([]byte("k"), true)
	require.NoError(t, err)
	require.False(t, found)
}

func TestManagerSetNilIsDelete(t *testing.T) {
	manager, _ := newTestManager(t, map[string][]byte{"k": []byte("v")}, 1, 1<<20)
	require.NoError(t, manager.Set([]byte("k"), nil))
	_, found, err := manager.Get([]byte("k"), true)
	require.NoError(t, err)
	require.False(t, found)
}

func TestManagerReadThroughFromSeededDB(t *testing.T) {
	manager, _ := newTestManager(t, map[string][]byte{"seeded": []byte("value")}, 4, 1<<20)
	val, found, err := manager.Get([]byte("seeded"), true)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, []byte("value"), val)
}

func TestManagerGetNotFound(t *testing.T) {
	manager := newTestManagerWithDB(t, newTestDB(nil), 1, 1<<20)
	_, found, err := manager.Get([]byte("missing"), true)
	require.NoError(t, err)
	require.False(t, found)
}

func TestManagerGetPropagatesDBError(t *testing.T) {
	db := newTestDB(nil)
	manager := newTestManagerWithDB(t, db, 1, 1<<20)
	db.getErr = errors.New("db boom") // inject after open (open itself reads the hash key)
	_, _, err := manager.Get([]byte("k"), true)
	require.Error(t, err)
	require.ErrorContains(t, err, "db boom")
}

func TestManagerBatchSetThenBatchGet(t *testing.T) {
	manager := newTestManagerWithDB(t, newTestDB(nil), 4, 1<<20)
	require.NoError(t, manager.BatchSet([]*proto.KVPair{
		{Key: []byte("a"), Value: []byte("1")},
		{Key: []byte("b"), Value: []byte("2")},
		{Key: []byte("c"), Delete: true}, // delete of a non-existent key
	}))

	got, err := manager.BatchGet([][]byte{[]byte("a"), []byte("b"), []byte("c"), []byte("missing")})
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
	manager := newTestManagerWithConfig(t, cfg, newTestDB(nil))
	require.Equal(t, "account", manager.Name())
}

func TestFlushSyncTrueStillRoundTrips(t *testing.T) {
	cfg := newTestConfig(1, 1<<20)
	cfg.FlushSync = true
	db := newTestDB(nil)
	manager := newTestManagerWithConfig(t, cfg, db)

	require.NoError(t, manager.Set([]byte("k"), []byte("v")))
	view, err := manager.Commit()
	require.NoError(t, err)
	require.NoError(t, view.Finalize(hashWrites(testHash)))
	awaitFlushed(t, view, time.Second)
	require.NoError(t, view.Release())

	kv, ok := db.get("k")
	require.True(t, ok)
	require.Equal(t, []byte("v"), kv)
}

// TestMetricsEnabledDoesNotBreakManager is a smoke test: with metrics on and a fast scrape interval,
// the background collect loop and view phase timer run without disturbing normal operation.
func TestMetricsEnabledDoesNotBreakManager(t *testing.T) {
	cfg := newTestConfig(2, 1<<20)
	cfg.MetricsEnabled = true
	cfg.MetricsScrapeIntervalSeconds = 0.001
	manager := newTestManagerWithConfig(t, cfg, newTestDB(nil))

	for i := 0; i < 20; i++ {
		require.NoError(t, manager.Set([]byte{byte(i)}, []byte("v")))
	}
	commitFinalizeRelease(t, manager)
	time.Sleep(10 * time.Millisecond) // let the metrics scrape loop fire at least once

	val, found, err := manager.Get([]byte{0}, true)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, []byte("v"), val)
}

func TestManagerConcurrentSetGet(t *testing.T) {
	manager := newTestManagerWithDB(t, newTestDB(nil), 8, 1<<20)
	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			k := []byte{byte(i)}
			require.NoError(t, manager.Set(k, k))
			_, _, err := manager.Get(k, true)
			require.NoError(t, err)
		}(i)
	}
	wg.Wait()
}
