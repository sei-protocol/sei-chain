package statesync

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/require"

	ssarchive "github.com/sei-protocol/sei-chain/sei-db/statesync"
)

// fakeChecker is an accessChecker in memory. getErr and writeErr, when set, fail Get and CheckWrite.
type fakeChecker struct {
	*memStore
	getErr   error
	writeErr error
	writes   []string
}

func newFakeChecker() *fakeChecker {
	return &fakeChecker{memStore: newMemStore()}
}

func (f *fakeChecker) Get(ctx context.Context, key string) (io.ReadCloser, ssarchive.ObjectInfo, error) {
	if f.getErr != nil {
		return nil, ssarchive.ObjectInfo{}, f.getErr
	}
	return f.memStore.Get(ctx, key)
}

func (f *fakeChecker) Region() string { return "eu-central-1" }

func (f *fakeChecker) CredentialSource(context.Context) (string, error) { return "test-provider", nil }

func (f *fakeChecker) CheckWrite(_ context.Context, key string) error {
	f.writes = append(f.writes, key)
	return f.writeErr
}

func TestCheckReadsABucketWithNoArchive(t *testing.T) {
	store := newFakeChecker()
	var out bytes.Buffer

	require.NoError(t, runCheck(t.Context(), &out, "snapshots", store, testChainID, false))

	require.Contains(t, out.String(), "bucket: snapshots\n")
	require.Contains(t, out.String(), "region: eu-central-1\n")
	require.Contains(t, out.String(), "credentials: test-provider\n")
	require.Contains(t, out.String(), "read: ok, no archive of chain "+testChainID+" yet\n")
	require.Empty(t, store.writes, "no write check without --write")
}

func TestCheckPrintsTheNewestArchive(t *testing.T) {
	store := newFakeChecker()
	_, err := ssarchive.AdvanceLatest(t.Context(), store, testChainID, 7)
	require.NoError(t, err)
	var out bytes.Buffer

	require.NoError(t, runCheck(t.Context(), &out, "snapshots", store, testChainID, false))

	require.Contains(t, out.String(), "is at height 7\n")
}

func TestCheckWrite(t *testing.T) {
	store := newFakeChecker()
	var out bytes.Buffer

	require.NoError(t, runCheck(t.Context(), &out, "snapshots", store, testChainID, true))

	require.Equal(t, []string{ssarchive.WriteCheckKey(testChainID)}, store.writes)
	require.Contains(t, out.String(), "write: ok\n")
}

func TestCheckReportsTheFailedStep(t *testing.T) {
	denied := errors.New("AccessDenied")
	for name, tc := range map[string]struct {
		store *fakeChecker
		want  string
	}{
		"read":  {store: &fakeChecker{memStore: newMemStore(), getErr: denied}, want: "read check"},
		"write": {store: &fakeChecker{memStore: newMemStore(), writeErr: denied}, want: "write check"},
	} {
		t.Run(name, func(t *testing.T) {
			var out bytes.Buffer

			err := runCheck(t.Context(), &out, "snapshots", tc.store, testChainID, true)

			require.ErrorIs(t, err, denied)
			require.ErrorContains(t, err, tc.want)
			require.NotContains(t, out.String(), "write: ok")
		})
	}
}
