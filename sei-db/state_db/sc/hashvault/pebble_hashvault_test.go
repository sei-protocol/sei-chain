package hashvault

import (
	"bytes"
	"context"
	"path/filepath"
	"sync"
	"testing"

	"github.com/cockroachdb/pebble/v2"
	"github.com/stretchr/testify/require"
)

// newTestPebbleVault constructs an unsafe Pebble vault rooted in t.TempDir() and arranges for it
// to be closed at end-of-test.
func newTestPebbleVault(t *testing.T, configMutators ...func(*HashVaultConfig)) *PebbleHashVault {
	t.Helper()
	cfg := DefaultHashVaultConfig()
	cfg.DataDir = filepath.Join(t.TempDir(), "vault")
	for _, m := range configMutators {
		m(&cfg)
	}
	v, err := NewUnsafePebbleHashVault(context.Background(), cfg)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = v.Close(context.Background())
	})
	return v
}

// reopenTestPebbleVault closes v then reopens a fresh PebbleHashVault at the same DataDir. The
// returned vault is cleaned up at end-of-test.
func reopenTestPebbleVault(t *testing.T, v *PebbleHashVault) *PebbleHashVault {
	t.Helper()
	dir := v.config.DataDir
	require.NoError(t, v.Close(context.Background()))
	cfg := DefaultHashVaultConfig()
	cfg.DataDir = dir
	reopened, err := NewUnsafePebbleHashVault(context.Background(), cfg)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = reopened.Close(context.Background())
	})
	return reopened
}

// directWritePebble opens the Pebble dir at path, applies fn to the db, then closes. Used by
// corruption tests to poke at on-disk values without going through the HashVault.
func directWritePebble(t *testing.T, path string, fn func(*pebble.DB)) {
	t.Helper()
	db, err := pebble.Open(path, &pebble.Options{})
	require.NoError(t, err)
	defer func() { require.NoError(t, db.Close()) }()
	fn(db)
}

func TestRestartRecoversPruneBoundary(t *testing.T) {
	ctx := context.Background()
	v := newTestPebbleVault(t)

	require.NoError(t, v.Prune(ctx, 50))
	v2 := reopenTestPebbleVault(t, v)

	// Strictly below the recovered boundary is rejected.
	require.ErrorIs(t, v2.CommitToHash(ctx, 25, bytesOfLen(0xAA, 32)), ErrBelowPruneBoundary)
	require.ErrorIs(t, v2.CommitToHash(ctx, 49, bytesOfLen(0xAA, 32)), ErrBelowPruneBoundary)
	// At and above the boundary are allowed.
	require.NoError(t, v2.CommitToHash(ctx, 50, bytesOfLen(0xAA, 32)))
	require.NoError(t, v2.CommitToHash(ctx, 51, bytesOfLen(0xAA, 32)))
}

func TestRestartRecoversHashes(t *testing.T) {
	ctx := context.Background()
	v := newTestPebbleVault(t)

	hash := bytesOfLen(0xCD, 32)
	require.NoError(t, v.CommitToHash(ctx, 99, hash))

	v2 := reopenTestPebbleVault(t, v)

	// Re-commit the same hash: succeeds.
	require.NoError(t, v2.CommitToHash(ctx, 99, hash))
	// Different hash: locked out.
	require.ErrorIs(t,
		v2.CommitToHash(ctx, 99, bytesOfLen(0xFF, 32)),
		ErrHashMismatch,
	)
}

func TestPruneRemovesDataOnDisk(t *testing.T) {
	// The shared suite already covers the externally-visible Prune contract; this test exists to
	// pin the on-disk effect — i.e. that the deleted heights are actually gone from Pebble (and
	// not merely shadowed by the in-memory boundary check).
	ctx := context.Background()
	const total = uint64(50)

	v := newTestPebbleVault(t)
	for h := uint64(1); h <= total; h++ {
		require.NoError(t, v.CommitToHash(ctx, h, bytesOfLen(byte(h), 32)))
	}

	require.NoError(t, v.Prune(ctx, total))

	dir := v.config.DataDir
	require.NoError(t, v.Close(ctx))

	var remaining []uint64
	directWritePebble(t, dir, func(db *pebble.DB) {
		iter, err := db.NewIter(&pebble.IterOptions{
			LowerBound: hashKey(0),
			UpperBound: hashKeyUpperBound(),
		})
		require.NoError(t, err)
		defer func() { _ = iter.Close() }()
		for iter.First(); iter.Valid(); iter.Next() {
			h, err := decodeHashKey(iter.Key())
			require.NoError(t, err)
			remaining = append(remaining, h)
		}
	})
	// Per the Prune contract the boundary block itself is kept, so only height==total should remain.
	require.Equal(t, []uint64{total}, remaining,
		"only the prune-boundary block should remain after Prune")
}

func TestKeyEncodingRoundtrip(t *testing.T) {
	cases := []uint64{0, 1, 7, 1 << 30, 1<<63 - 1, ^uint64(0)}
	for _, h := range cases {
		k := hashKey(h)
		require.Len(t, k, len(hashKeyPrefix)+heightDigits)
		require.True(t, bytes.HasPrefix(k, hashKeyPrefix))
		got, err := decodeHashKey(k)
		require.NoError(t, err)
		require.Equal(t, h, got)
	}
}

func TestKeyEncodingOrderingMatchesNumeric(t *testing.T) {
	// Spot-check that lex order over hashKey() matches numeric order, including non-adjacent
	// magnitudes. This is the whole reason for zero-padded fixed-width encoding.
	heights := []uint64{0, 1, 9, 10, 99, 100, 1<<32 - 1, 1 << 32, 1<<63 - 1, ^uint64(0) - 1, ^uint64(0)}
	for i := 0; i+1 < len(heights); i++ {
		a, b := hashKey(heights[i]), hashKey(heights[i+1])
		require.Lessf(t, bytes.Compare(a, b), 0,
			"hashKey(%d)=%q must sort before hashKey(%d)=%q", heights[i], a, heights[i+1], b)
	}
}

func TestKeyEncodingHumanReadable(t *testing.T) {
	// Pin the on-disk layout so a future "let's switch back to binary BE for size" PR has to
	// explicitly delete this test.
	require.Equal(t, "h00000000000000000042", string(hashKey(42)))
	require.Equal(t, "h18446744073709551615", string(hashKey(^uint64(0))))
}

func TestDecodeHashKeyRejectsMalformed(t *testing.T) {
	_, err := decodeHashKey([]byte("h0000000000000000004"))
	require.ErrorIs(t, err, ErrCorruption, "short length")
	_, err = decodeHashKey([]byte("x00000000000000000042"))
	require.ErrorIs(t, err, ErrCorruption, "wrong prefix")
	_, err = decodeHashKey([]byte("h0000000000000000004x"))
	require.ErrorIs(t, err, ErrCorruption, "non-digit byte")
}

func TestValueCodecRoundtrip(t *testing.T) {
	hash := bytesOfLen(0xAA, 32)
	for _, h := range []uint64{0, 1, 7, 1 << 30, ^uint64(0)} {
		raw := encodeHashValue(h, hash)
		got, err := decodeHashValue(h, raw)
		require.NoError(t, err)
		require.Equal(t, hash, got)
	}

	for _, b := range []uint64{0, 1, 1234567890, ^uint64(0)} {
		raw := encodeBoundaryValue(b)
		got, err := decodeBoundaryValue(raw)
		require.NoError(t, err)
		require.Equal(t, b, got)
	}
}

func TestBoundaryValueIsUnpaddedDecimal(t *testing.T) {
	// Boundary encoding is variable-width by design (one row, no range scan to satisfy). Pin it
	// so a future "let's pad for symmetry with keys" change has to explicitly delete this test.
	require.Equal(t, "0", string(encodeBoundaryValue(0)))
	require.Equal(t, "42", string(encodeBoundaryValue(42)))
	require.Equal(t, "18446744073709551615", string(encodeBoundaryValue(^uint64(0))))
}

func TestDecodeBoundaryValueRejectsMalformed(t *testing.T) {
	_, err := decodeBoundaryValue(nil)
	require.ErrorIs(t, err, ErrCorruption, "empty")
	_, err = decodeBoundaryValue([]byte{})
	require.ErrorIs(t, err, ErrCorruption, "zero length")
	_, err = decodeBoundaryValue([]byte("123abc"))
	require.ErrorIs(t, err, ErrCorruption, "non-digit byte")
	// 21 digits cannot fit in uint64.
	_, err = decodeBoundaryValue([]byte("184467440737095516150"))
	require.ErrorIs(t, err, ErrCorruption, "too long")
}

func TestValueCodecHeightTamper(t *testing.T) {
	// The whole reason we feed the height into the SHA is to detect a value that was stored under
	// a different key from the one we're now reading. Decoding under the wrong height MUST fail.
	hash := bytesOfLen(0x77, 32)
	raw := encodeHashValue(100, hash)
	_, err := decodeHashValue(101, raw)
	require.ErrorIs(t, err, ErrCorruption)
}

func TestValueCodecBitFlip(t *testing.T) {
	hash := bytesOfLen(0x77, 32)
	raw := encodeHashValue(100, hash)

	// Flip one bit in every byte position and confirm each flip is caught.
	for i := 0; i < len(raw); i++ {
		corrupted := bytes.Clone(raw)
		corrupted[i] ^= 0x01
		_, err := decodeHashValue(100, corrupted)
		require.ErrorIsf(t, err, ErrCorruption, "expected ErrCorruption at byte %d", i)
	}
}

func TestValueCodecShortValue(t *testing.T) {
	// A value shorter than the trailer can't carry a valid checksum.
	for n := 0; n < checksumSize; n++ {
		_, err := decodeHashValue(0, make([]byte, n))
		require.ErrorIs(t, err, ErrCorruption)
	}
}

func TestCommitDetectsDiskCorruption(t *testing.T) {
	ctx := context.Background()
	v := newTestPebbleVault(t)
	hash := bytesOfLen(0xAA, 32)
	require.NoError(t, v.CommitToHash(ctx, 42, hash))

	dir := v.config.DataDir
	require.NoError(t, v.Close(ctx))

	// Flip one bit of the stored value via direct Pebble access.
	directWritePebble(t, dir, func(db *pebble.DB) {
		key := hashKey(42)
		raw, closer, err := db.Get(key)
		require.NoError(t, err)
		corrupted := bytes.Clone(raw)
		_ = closer.Close()
		corrupted[0] ^= 0x01
		require.NoError(t, db.Set(key, corrupted, pebble.Sync))
	})

	reopenCfg := DefaultHashVaultConfig()
	reopenCfg.DataDir = dir
	v2, err := NewUnsafePebbleHashVault(ctx, reopenCfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = v2.Close(ctx) })

	err = v2.CommitToHash(ctx, 42, hash)
	require.ErrorIs(t, err, ErrCorruption)
}

func TestStartupRejectsMalformedBoundary(t *testing.T) {
	// The boundary value has no checksum (it's just GC bookkeeping), so a flipped byte is
	// indistinguishable from a legitimately-written boundary and is accepted silently. The only
	// failure mode startup still catches is a length mismatch, which indicates the record was
	// truncated/extended outside Pebble's normal write path.
	ctx := context.Background()
	v := newTestPebbleVault(t)
	require.NoError(t, v.Prune(ctx, 7))

	dir := v.config.DataDir
	require.NoError(t, v.Close(ctx))

	directWritePebble(t, dir, func(db *pebble.DB) {
		require.NoError(t, db.Set(pruneBoundaryKey, []byte{0x00, 0x01, 0x02}, pebble.Sync))
	})

	reopenCfg := DefaultHashVaultConfig()
	reopenCfg.DataDir = dir
	_, err := NewUnsafePebbleHashVault(ctx, reopenCfg)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrCorruption)
}

func TestProductionConstructorForcesFsync(t *testing.T) {
	ctx := context.Background()
	// Caller asks for no fsync, but the production constructor must override that.
	cfg := DefaultHashVaultConfig()
	cfg.DataDir = filepath.Join(t.TempDir(), "vault")
	cfg.Fsync = false
	v, err := NewPebbleHashVault(ctx, cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = v.Close(ctx) })

	require.True(t, v.config.Fsync, "NewPebbleHashVault must force Fsync=true")
	require.Equal(t, pebble.Sync, v.writeOpts, "writeOpts must be pebble.Sync in production")
}

func TestUnsafeConstructorHonorsFsync(t *testing.T) {
	ctx := context.Background()
	cfg := DefaultHashVaultConfig()
	cfg.DataDir = filepath.Join(t.TempDir(), "vault")
	cfg.Fsync = false
	v, err := NewUnsafePebbleHashVault(ctx, cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = v.Close(ctx) })

	require.False(t, v.config.Fsync)
	require.Equal(t, pebble.NoSync, v.writeOpts)
}

func TestContextCancelledCommit(t *testing.T) {
	// A pre-cancelled ctx must short-circuit before we touch any state. We don't otherwise check
	// the ctx mid-operation (the work is all local, fast, and uninterruptible once started).
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	v := newTestPebbleVault(t)
	err := v.CommitToHash(ctx, 1, bytesOfLen(0xAA, 32))
	require.ErrorIs(t, err, context.Canceled)
}

// Cross-check: nothing in the production code accidentally panics on simultaneous Close+Commit.
func TestCloseConcurrentWithCommits(t *testing.T) {
	ctx := context.Background()
	v := newTestPebbleVault(t)

	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(h uint64) {
			defer wg.Done()
			_ = v.CommitToHash(ctx, h, bytesOfLen(byte(h), 32))
		}(uint64(i + 1))
	}
	// Race Close against the commits.
	_ = v.Close(ctx)
	wg.Wait()
}
