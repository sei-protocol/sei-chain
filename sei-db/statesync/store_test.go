package statesync

import (
	"bytes"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestKeys(t *testing.T) {
	require.Equal(t, "giga-state-sync/pacific-1/00000000000000001200.tar", ArchiveKey("pacific-1", 1200))
	require.Equal(t, "giga-state-sync/pacific-1/latest.txt", LatestKey("pacific-1"))
	require.Equal(t, "giga-state-sync/pacific-1/upload-claim", ClaimKey("pacific-1"))
}

func TestArchiveKeysSortByHeight(t *testing.T) {
	require.Less(t, ArchiveKey(testChainID, 9), ArchiveKey(testChainID, 10))
}

func TestLatestRoundTrip(t *testing.T) {
	store := newMemStore()
	_, err := ReadLatest(t.Context(), store, testChainID)
	require.ErrorIs(t, err, ErrObjectNotFound)

	newest, err := AdvanceLatest(t.Context(), store, testChainID, 42)
	require.NoError(t, err)
	require.Equal(t, int64(42), newest)
	got, err := ReadLatest(t.Context(), store, testChainID)
	require.NoError(t, err)
	require.Equal(t, int64(42), got)
}

func TestAdvanceLatestKeepsAHigherHeight(t *testing.T) {
	store := newMemStore()
	_, err := AdvanceLatest(t.Context(), store, testChainID, 7)
	require.NoError(t, err)

	newest, err := AdvanceLatest(t.Context(), store, testChainID, 5)
	require.NoError(t, err)

	require.Equal(t, int64(7), newest)
	got, err := ReadLatest(t.Context(), store, testChainID)
	require.NoError(t, err)
	require.Equal(t, int64(7), got)
}

func TestAdvanceLatestKeepsAHeightWrittenDuringTheWrite(t *testing.T) {
	store := newMemStore()
	_, err := AdvanceLatest(t.Context(), store, testChainID, 3)
	require.NoError(t, err)
	store.beforePut = func(string) {
		store.beforePut = nil
		store.putLatest(t, "9\n")
	}

	newest, err := AdvanceLatest(t.Context(), store, testChainID, 5)
	require.NoError(t, err)

	require.Equal(t, int64(9), newest)
	got, err := ReadLatest(t.Context(), store, testChainID)
	require.NoError(t, err)
	require.Equal(t, int64(9), got)
}

func TestAdvanceLatestKeepsAFirstHeightWrittenDuringTheWrite(t *testing.T) {
	store := newMemStore()
	store.beforePut = func(string) {
		store.beforePut = nil
		store.putLatest(t, "9\n")
	}

	newest, err := AdvanceLatest(t.Context(), store, testChainID, 5)
	require.NoError(t, err)

	require.Equal(t, int64(9), newest)
}

func TestAdvanceLatestFailsWhenEachWriteRaces(t *testing.T) {
	store := newMemStore()
	_, err := AdvanceLatest(t.Context(), store, testChainID, 1)
	require.NoError(t, err)
	store.beforePut = func(string) {
		hook := store.beforePut
		store.beforePut = nil
		store.putLatest(t, "1\n")
		store.beforePut = hook
	}

	_, err = AdvanceLatest(t.Context(), store, testChainID, 5)

	require.ErrorContains(t, err, "changed during each of")
}

func TestReadLatestRejects(t *testing.T) {
	for name, content := range map[string][]byte{
		"not a number": []byte("latest\n"),
		"zero":         []byte("0\n"),
		"negative":     []byte("-3\n"),
		"too large":    bytes.Repeat([]byte("1"), maxLatestSize+1),
	} {
		t.Run(name, func(t *testing.T) {
			store := newMemStore()
			store.putLatest(t, string(content))
			_, err := ReadLatest(t.Context(), store, testChainID)
			require.Error(t, err)
		})
	}
}

func TestReadLatestInfoReturnsTheWriteTime(t *testing.T) {
	store := newMemStore()
	written := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return written }
	_, err := AdvanceLatest(t.Context(), store, testChainID, 42)
	require.NoError(t, err)

	height, info, err := ReadLatestInfo(t.Context(), store, testChainID)

	require.NoError(t, err)
	require.Equal(t, int64(42), height)
	require.Equal(t, written, info.Modified)
	require.NotEmpty(t, info.Version)
}

const claimTimeout = time.Hour

// readClaim returns the content of the upload claim of the test chain.
func readClaim(t *testing.T, store *memStore) string {
	t.Helper()
	body, _, err := store.Get(t.Context(), ClaimKey(testChainID))
	require.NoError(t, err)
	defer func() { _ = body.Close() }()
	data, err := io.ReadAll(body)
	require.NoError(t, err)
	return string(data)
}

// putClaim stores content as the upload claim of the test chain.
func putClaim(t *testing.T, store *memStore, content string) {
	t.Helper()
	require.NoError(t, store.Put(t.Context(), ClaimKey(testChainID), strings.NewReader(content), int64(len(content)), Precondition{}))
}

func TestClaimUpload(t *testing.T) {
	written := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	fresh := written.Add(claimTimeout - time.Second)
	stale := written.Add(claimTimeout)
	for name, tc := range map[string]struct {
		claim     string
		now       time.Time
		want      bool
		wantClaim string
	}{
		"no claim":               {now: fresh, want: true, wantClaim: "40 node-a\n"},
		"same round, other node": {claim: "40 node-b\n", now: fresh, want: false, wantClaim: "40 node-b\n"},
		"same round, this node":  {claim: "40 node-a\n", now: stale, want: true, wantClaim: "40 node-a\n"},
		"same round, timed out":  {claim: "40 node-b\n", now: stale, want: true, wantClaim: "40 node-a\n"},
		"earlier round":          {claim: "30 node-b\n", now: fresh, want: true, wantClaim: "40 node-a\n"},
		"later round":            {claim: "50 node-b\n", now: stale, want: false, wantClaim: "50 node-b\n"},
		"damaged claim":          {claim: "node-b\n", now: fresh, want: true, wantClaim: "40 node-a\n"},
	} {
		t.Run(name, func(t *testing.T) {
			store := newMemStore()
			store.now = func() time.Time { return written }
			if tc.claim != "" {
				putClaim(t, store, tc.claim)
			}

			got, err := ClaimUpload(t.Context(), store, testChainID, 40, "node-a", tc.now, claimTimeout)

			require.NoError(t, err)
			require.Equal(t, tc.want, got)
			require.Equal(t, tc.wantClaim, readClaim(t, store))
		})
	}
}

func TestClaimUploadLosesARaceForTheClaim(t *testing.T) {
	store := newMemStore()
	store.beforePut = func(string) {
		store.beforePut = nil
		putClaim(t, store, "40 node-b\n")
	}

	got, err := ClaimUpload(t.Context(), store, testChainID, 40, "node-a", time.Now(), claimTimeout)

	require.NoError(t, err)
	require.False(t, got)
	require.Equal(t, "40 node-b\n", readClaim(t, store))
}
