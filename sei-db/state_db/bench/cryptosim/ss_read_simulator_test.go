package cryptosim

import (
	"math/rand"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/stretchr/testify/require"
)

func TestSSKeyRing(t *testing.T) {
	rng := rand.New(rand.NewSource(1)) //nolint:gosec
	ring := newSSKeyRing(4)

	_, ok := ring.random(rng)
	require.False(t, ok, "empty ring must report no entry")

	for i := int64(1); i <= 10; i++ {
		ring.push(ssKeyEntry{version: i, key: []byte{byte(i)}})
	}
	// Ring holds the last 4 entries (versions 7-10).
	for i := 0; i < 50; i++ {
		e, ok := ring.random(rng)
		require.True(t, ok)
		require.GreaterOrEqual(t, e.version, int64(7))
	}
}

func TestSSKeyReservoirOlderThan(t *testing.T) {
	rng := rand.New(rand.NewSource(2)) //nolint:gosec
	res := newSSKeyReservoir(8, 42)

	res.mu.Lock()
	for i := int64(1); i <= 8; i++ {
		res.offerLocked(ssKeyEntry{version: i, key: []byte{byte(i)}})
	}
	res.mu.Unlock()

	// Only versions <= 3 qualify.
	for i := 0; i < 50; i++ {
		e, ok := res.randomOlderThan(rng, 3)
		if ok {
			require.LessOrEqual(t, e.version, int64(3))
		}
	}
	_, ok := res.randomOlderThan(rng, 0)
	require.False(t, ok, "maxVersion <= 0 must never match")
}

func TestSSKeyReservoirUniformCapacity(t *testing.T) {
	res := newSSKeyReservoir(16, 7)
	res.mu.Lock()
	for i := int64(0); i < 10_000; i++ {
		res.offerLocked(ssKeyEntry{version: i})
	}
	res.mu.Unlock()
	require.Len(t, res.entries, 16, "reservoir must stay at capacity")
	require.Equal(t, int64(10_000), res.seen)
}

func TestSSReadSimulatorSampleSkipsDeletes(t *testing.T) {
	s := &SSReadSimulator{
		ring: newSSKeyRing(16),
		res:  newSSKeyReservoir(16, 1),
	}
	s.Sample(5, []*proto.NamedChangeSet{{
		Name: "evm",
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: []byte("a"), Value: []byte("1")},
			{Key: []byte("b"), Delete: true},
			{Key: []byte("c"), Value: []byte("3")},
		}},
	}})
	require.Equal(t, int64(2), s.ring.pos.Load(), "deletes must not be sampled")

	// Nil receiver is a safe no-op (sampler not configured).
	var nilSim *SSReadSimulator
	nilSim.Sample(1, nil)
}

func TestSSQuantile(t *testing.T) {
	require.Equal(t, time.Duration(0), ssQuantile(nil, 0.5))
	sorted := []float64{0.001, 0.002, 0.003, 0.004, 0.005, 0.006, 0.007, 0.008, 0.009, 0.010}
	require.Equal(t, 5*time.Millisecond, ssQuantile(sorted, 0.5).Round(time.Millisecond))
	require.Equal(t, 9*time.Millisecond, ssQuantile(sorted, 0.99).Round(time.Millisecond))
}

func TestConfigValidatesSSReadFields(t *testing.T) {
	valid := func() *CryptoSimConfig {
		cfg := DefaultCryptoSimConfig()
		cfg.DataDir = t.TempDir()
		cfg.LogDir = t.TempDir()
		return cfg
	}
	require.NoError(t, valid().Validate())

	cfg := valid()
	cfg.SSPointReadWorkers = -1
	require.ErrorContains(t, cfg.Validate(), "SSPointReadWorkers")

	cfg = valid()
	cfg.SSColdPointReadWorkers = -1
	require.ErrorContains(t, cfg.Validate(), "SSColdPointReadWorkers")

	cfg = valid()
	cfg.SSColdReadMinAgeBlocks = -1
	require.ErrorContains(t, cfg.Validate(), "SSColdReadMinAgeBlocks")
}
