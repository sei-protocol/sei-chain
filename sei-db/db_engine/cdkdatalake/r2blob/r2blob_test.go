package r2blob_test

import (
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gocloud.dev/blob"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/cdkdatalake/r2blob"
)

// recordingClient answers every request with an empty 200 and records the last one.
type recordingClient struct {
	last *http.Request
}

func (c *recordingClient) Do(request *http.Request) (*http.Response, error) {
	c.last = request
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Etag": {`"etag"`}},
		Body:       io.NopCloser(strings.NewReader("")),
		Request:    request,
	}, nil
}

func setCredentials(t *testing.T) {
	t.Setenv(r2blob.AccessKeyIDEnv, "key-id")
	t.Setenv(r2blob.SecretAccessKeyEnv, "secret")
}

func TestBucketSendsSignedRequestsToTheAccountEndpoint(t *testing.T) {
	setCredentials(t)
	client := &recordingClient{}
	opener := &r2blob.URLOpener{HTTPClient: client}
	u, err := url.Parse("r2://my-bucket?account_id=abc123")
	require.NoError(t, err)
	bucket, err := opener.OpenBucketURL(t.Context(), u)
	require.NoError(t, err)
	defer func() { require.NoError(t, bucket.Close()) }()

	require.NoError(t, bucket.WriteAll(t.Context(), "object", []byte("value"), nil))
	require.Equal(t, "my-bucket.abc123.r2.cloudflarestorage.com", client.last.URL.Host)
	require.Equal(t, "/object", client.last.URL.Path)
	authorization := client.last.Header.Get("Authorization")
	require.Contains(t, authorization, "Credential=key-id/")
	require.Contains(t, authorization, "/auto/s3/aws4_request")
	require.Empty(t, client.last.Header.Get("X-Amz-Sdk-Checksum-Algorithm"))
}

func TestOpenBucketAcceptsAPrefix(t *testing.T) {
	setCredentials(t)
	bucket, err := blob.OpenBucket(t.Context(), "r2://my-bucket?account_id=abc123&prefix=node-1/")
	require.NoError(t, err)
	require.NoError(t, bucket.Close())
}

func TestOpenBucketRejectsUnusableURLs(t *testing.T) {
	setCredentials(t)
	for name, bucketURL := range map[string]string{
		"no account":      "r2://my-bucket",
		"invalid account": "r2://my-bucket?account_id=abc.evil.com",
		"unknown param":   "r2://my-bucket?account_id=abc123&region=us-east-1",
		"no bucket":       "r2://?account_id=abc123",
	} {
		_, err := blob.OpenBucket(t.Context(), bucketURL)
		require.Error(t, err, name)
	}
}

func TestOpenBucketNeedsCredentials(t *testing.T) {
	for name, env := range map[string][2]string{
		"no access key id":     {"", "secret"},
		"no secret access key": {"key-id", ""},
	} {
		t.Setenv(r2blob.AccessKeyIDEnv, env[0])
		t.Setenv(r2blob.SecretAccessKeyEnv, env[1])
		_, err := blob.OpenBucket(t.Context(), "r2://my-bucket?account_id=abc123")
		require.Error(t, err, name)
	}
}
