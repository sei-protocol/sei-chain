package statesync

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/stretchr/testify/require"
)

const testBucket = "snapshots"

// fakeS3 is an S3 endpoint that keeps objects in memory and honors If-Match and If-None-Match. It
// answers ConditionalRequestConflict to the next conflicts conditional PUTs.
type fakeS3 struct {
	mu        sync.Mutex
	objects   map[string]memObject
	etags     int
	conflicts int
}

func (f *fakeS3) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := strings.TrimPrefix(r.URL.Path, "/"+testBucket+"/")
	obj, ok := f.objects[key]
	switch r.Method {
	case http.MethodGet:
		if !ok {
			writeS3Error(w, http.StatusNotFound, "NoSuchKey")
			return
		}
		w.Header().Set("ETag", obj.version)
		w.Header().Set("Last-Modified", obj.modified.Format(http.TimeFormat))
		_, _ = w.Write(obj.data)
	case http.MethodPut:
		if f.conflicts > 0 && (r.Header.Get("If-Match") != "" || r.Header.Get("If-None-Match") != "") {
			f.conflicts--
			writeS3Error(w, http.StatusConflict, "ConditionalRequestConflict")
			return
		}
		if ifMatch := r.Header.Get("If-Match"); ifMatch != "" && !ok {
			writeS3Error(w, http.StatusNotFound, "NoSuchKey")
			return
		}
		if (r.Header.Get("If-None-Match") == "*" && ok) || (r.Header.Get("If-Match") != "" && r.Header.Get("If-Match") != obj.version) {
			writeS3Error(w, http.StatusPreconditionFailed, "PreconditionFailed")
			return
		}
		data, err := io.ReadAll(r.Body)
		if err != nil {
			writeS3Error(w, http.StatusBadRequest, "IncompleteBody")
			return
		}
		f.etags++
		etag := fmt.Sprintf("%q", fmt.Sprint(f.etags))
		f.objects[key] = memObject{data: data, version: etag, modified: time.Now().UTC().Truncate(time.Second)}
		w.Header().Set("ETag", etag)
	default:
		writeS3Error(w, http.StatusMethodNotAllowed, "MethodNotAllowed")
	}
}

func writeS3Error(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(status)
	_, _ = fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?><Error><Code>%s</Code><Message>%s</Message></Error>`, code, code)
}

// isolateAWSConfig points the AWS SDK at no shared config, no profile and static credentials.
func isolateAWSConfig(t *testing.T) {
	t.Setenv("AWS_CONFIG_FILE", filepath.Join(t.TempDir(), "config"))
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", filepath.Join(t.TempDir(), "credentials"))
	t.Setenv("AWS_PROFILE", "")
	t.Setenv("AWS_REGION", "")
	t.Setenv("AWS_DEFAULT_REGION", "")
	t.Setenv("AWS_ACCESS_KEY_ID", "test")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test")
	t.Setenv("AWS_SESSION_TOKEN", "")
}

func newFakeS3Store(t *testing.T) *S3Store {
	t.Helper()
	store, _ := newFakeS3StoreWith(t)
	return store
}

// newFakeS3StoreWith returns an S3Store on a new fakeS3, and the fakeS3.
func newFakeS3StoreWith(t *testing.T) (*S3Store, *fakeS3) {
	t.Helper()
	isolateAWSConfig(t)
	fake := &fakeS3{objects: map[string]memObject{}}
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	store, err := newS3Store(t.Context(), testBucket, "eu-central-1", func(o *s3.Options) {
		o.BaseEndpoint = aws.String(srv.URL)
		o.UsePathStyle = true
		o.RequestChecksumCalculation = aws.RequestChecksumCalculationWhenRequired
	})
	require.NoError(t, err)
	return store, fake
}

func putString(t *testing.T, store *S3Store, key, content string, pre Precondition) error {
	t.Helper()
	return store.Put(t.Context(), key, strings.NewReader(content), int64(len(content)), pre)
}

func TestS3StoreGetMissingKeyIsNotFound(t *testing.T) {
	store := newFakeS3Store(t)

	_, _, err := store.Get(t.Context(), "missing")

	require.ErrorIs(t, err, ErrObjectNotFound)
}

func TestS3StorePutAbsent(t *testing.T) {
	store := newFakeS3Store(t)

	require.NoError(t, putString(t, store, "k", "first", Precondition{Absent: true}))
	err := putString(t, store, "k", "second", Precondition{Absent: true})

	require.ErrorIs(t, err, ErrPreconditionFailed)
	body, info, err := store.Get(t.Context(), "k")
	require.NoError(t, err)
	require.NotEmpty(t, info.Version)
	require.WithinDuration(t, time.Now(), info.Modified, time.Minute)
	data, err := io.ReadAll(body)
	require.NoError(t, err)
	require.NoError(t, body.Close())
	require.Equal(t, "first", string(data))
}

func TestS3StorePutVersion(t *testing.T) {
	store := newFakeS3Store(t)
	require.NoError(t, putString(t, store, "k", "first", Precondition{}))
	body, info, err := store.Get(t.Context(), "k")
	require.NoError(t, err)
	require.NoError(t, body.Close())

	require.ErrorIs(t, putString(t, store, "k", "other", Precondition{Version: `"stale"`}), ErrPreconditionFailed)
	require.NoError(t, putString(t, store, "k", "second", Precondition{Version: info.Version}))
	require.ErrorIs(t, putString(t, store, "gone", "x", Precondition{Version: info.Version}), ErrPreconditionFailed)
}

func TestS3StoreAdvanceLatest(t *testing.T) {
	store := newFakeS3Store(t)

	_, err := ReadLatest(t.Context(), store, testChainID)
	require.ErrorIs(t, err, ErrObjectNotFound)
	newest, err := AdvanceLatest(t.Context(), store, testChainID, 8)
	require.NoError(t, err)
	require.Equal(t, int64(8), newest)
	newest, err = AdvanceLatest(t.Context(), store, testChainID, 3)
	require.NoError(t, err)
	require.Equal(t, int64(8), newest)
	newest, err = AdvanceLatest(t.Context(), store, testChainID, 12)
	require.NoError(t, err)
	require.Equal(t, int64(12), newest)
}

func TestS3StorePutWriteConflict(t *testing.T) {
	store, fake := newFakeS3StoreWith(t)
	fake.conflicts = 1

	err := putString(t, store, "k", "first", Precondition{Absent: true})

	require.ErrorIs(t, err, ErrWriteConflict)
	require.NotErrorIs(t, err, ErrPreconditionFailed)
	require.Empty(t, fake.objects)
}

func TestS3StoreAdvanceLatestRetriesAWriteConflict(t *testing.T) {
	store, fake := newFakeS3StoreWith(t)
	fake.conflicts = 1

	newest, err := AdvanceLatest(t.Context(), store, testChainID, 8)

	require.NoError(t, err)
	require.Equal(t, int64(8), newest)
}

func TestS3StoreClaimUploadLosesAWriteConflict(t *testing.T) {
	store, fake := newFakeS3StoreWith(t)
	fake.conflicts = 1

	got, err := ClaimUpload(t.Context(), store, testChainID, 40, "node-a", time.Now(), claimTimeout)

	require.NoError(t, err)
	require.False(t, got)
}

func TestNewS3StoreNeedsARegion(t *testing.T) {
	isolateAWSConfig(t)

	_, err := NewS3Store(t.Context(), testBucket, "")

	require.ErrorContains(t, err, "no AWS region")
}

func TestNewS3StoreNeedsABucket(t *testing.T) {
	isolateAWSConfig(t)

	_, err := NewS3Store(t.Context(), "", "eu-central-1")

	require.Error(t, err)
}
