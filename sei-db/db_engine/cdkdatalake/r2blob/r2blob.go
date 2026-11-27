// Package r2blob registers the r2 bucket URL scheme, for Cloudflare R2, on blob.DefaultURLMux.
package r2blob

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"regexp"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"gocloud.dev/blob"
	"gocloud.dev/blob/s3blob"
)

func init() {
	blob.DefaultURLMux().RegisterBucket(Scheme, new(URLOpener))
}

// Scheme is the URL scheme r2blob registers its URLOpener under on blob.DefaultURLMux.
const Scheme = "r2"

// The environment variables holding the S3 credentials of an R2 API token.
const (
	AccessKeyIDEnv     = "R2_ACCESS_KEY_ID"
	SecretAccessKeyEnv = "R2_SECRET_ACCESS_KEY"
)

// accountIDParam is the query parameter naming the Cloudflare account that owns the bucket.
const accountIDParam = "account_id"

// region is the region R2 requires S3 clients to sign for.
const region = "auto"

// accountIDRegex matches the account IDs URLOpener accepts.
var accountIDRegex = regexp.MustCompile(`^[a-zA-Z0-9]+$`)

// URLOpener opens R2 URLs like "r2://my-bucket?account_id=<cloudflare-account-id>", with the
// credentials in R2_ACCESS_KEY_ID and R2_SECRET_ACCESS_KEY.
type URLOpener struct {
	// Sends the bucket's requests. Nil uses the AWS SDK's default client.
	HTTPClient s3.HTTPClient
}

// OpenBucketURL opens the R2 bucket u names.
func (o *URLOpener) OpenBucketURL(ctx context.Context, u *url.URL) (*blob.Bucket, error) {
	query := u.Query()
	accountID := query.Get(accountIDParam)
	query.Del(accountIDParam)
	if len(query) > 0 {
		return nil, fmt.Errorf("open bucket %v: unknown query parameters %v", u, query)
	}
	if !accountIDRegex.MatchString(accountID) {
		return nil, fmt.Errorf("open bucket %v: %s %q must match %s", u, accountIDParam, accountID, accountIDRegex)
	}
	accessKeyID, secretAccessKey := os.Getenv(AccessKeyIDEnv), os.Getenv(SecretAccessKeyEnv)
	if accessKeyID == "" || secretAccessKey == "" {
		return nil, fmt.Errorf("open bucket %v: %s and %s must be set", u, AccessKeyIDEnv, SecretAccessKeyEnv)
	}

	// Checksums only where an operation requires them, which is what S3-compatible stores support.
	checksums := aws.RequestChecksumCalculationWhenRequired
	client := s3.New(s3.Options{
		Region:                     region,
		BaseEndpoint:               aws.String(endpoint(accountID)),
		Credentials:                credentials.NewStaticCredentialsProvider(accessKeyID, secretAccessKey, ""),
		HTTPClient:                 o.HTTPClient,
		RequestChecksumCalculation: checksums,
		ResponseChecksumValidation: aws.ResponseChecksumValidationWhenRequired,
	})
	bucket, err := s3blob.OpenBucket(ctx, client, u.Host, &s3blob.Options{RequestChecksumCalculation: checksums})
	if err != nil {
		return nil, fmt.Errorf("open bucket %v: %w", u, err)
	}
	return bucket, nil
}

// endpoint returns the S3 API endpoint of the R2 buckets of accountID.
func endpoint(accountID string) string {
	return "https://" + accountID + ".r2.cloudflarestorage.com"
}
