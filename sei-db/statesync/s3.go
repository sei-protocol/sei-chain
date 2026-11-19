package statesync

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/s3/transfermanager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
)

// abortTimeout bounds the abort of a failed multipart upload, which runs after the upload context
// is canceled. Close of the storage waits for it; the bucket lifecycle rule removes the parts of
// an abort that does not complete.
const abortTimeout = 5 * time.Second

// S3Store is an ObjectStore in one S3 bucket.
type S3Store struct {
	bucket   string
	client   *s3.Client
	uploader *transfermanager.Client
}

var _ ObjectStore = (*S3Store)(nil)

// NewS3Store returns the S3Store of bucket. It takes the credentials from the AWS SDK default chain,
// and the region from region, or from the AWS SDK default configuration when region is empty. It
// returns an error when neither gives a region.
func NewS3Store(ctx context.Context, bucket, region string) (*S3Store, error) {
	return newS3Store(ctx, bucket, region)
}

// newS3Store is NewS3Store with options for the S3 client.
func newS3Store(ctx context.Context, bucket, region string, clientOpts ...func(*s3.Options)) (*S3Store, error) {
	if bucket == "" {
		return nil, errors.New("s3 bucket is empty")
	}
	var opts []func(*awsconfig.LoadOptions) error
	if region != "" {
		opts = append(opts, awsconfig.WithRegion(region))
	}
	cfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}
	if cfg.Region == "" {
		return nil, fmt.Errorf("s3 bucket %s: no AWS region is configured", bucket)
	}
	client := s3.NewFromConfig(cfg, clientOpts...)
	uploader := transfermanager.New(client, func(o *transfermanager.Options) {
		o.FailTimeout = abortTimeout
	})
	return &S3Store{bucket: bucket, client: client, uploader: uploader}, nil
}

// Put uploads the object at key, in parts when it is large.
func (s *S3Store) Put(ctx context.Context, key string, body io.Reader, size int64, pre Precondition) error {
	in := &transfermanager.UploadObjectInput{
		Bucket:        aws.String(s.bucket),
		Key:           aws.String(key),
		Body:          body,
		ContentLength: aws.Int64(size),
	}
	if pre.Absent {
		in.IfNoneMatch = aws.String("*")
	}
	if pre.Version != "" {
		in.IfMatch = aws.String(pre.Version)
	}
	_, err := s.uploader.UploadObject(ctx, in)
	if err != nil {
		if preconditionFailed(err, pre) {
			return fmt.Errorf("upload %s: %w: %w", s.url(key), ErrPreconditionFailed, err)
		}
		return fmt.Errorf("upload %s: %w", s.url(key), err)
	}
	return nil
}

// preconditionFailed reports whether err is the S3 answer to a request whose key did not meet pre.
// S3 answers PreconditionFailed, or NoSuchKey to If-Match when no object has the key.
func preconditionFailed(err error, pre Precondition) bool {
	var apiErr smithy.APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	switch apiErr.ErrorCode() {
	case "PreconditionFailed":
		return true
	case "NoSuchKey":
		return pre.Version != ""
	}
	return false
}

// Get returns the content of the object at key, with its ETag as the version and its
// Last-Modified time.
func (s *S3Store) Get(ctx context.Context, key string) (io.ReadCloser, ObjectInfo, error) {
	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		var noKey *s3types.NoSuchKey
		if errors.As(err, &noKey) {
			return nil, ObjectInfo{}, fmt.Errorf("%s: %w", s.url(key), ErrObjectNotFound)
		}
		return nil, ObjectInfo{}, fmt.Errorf("download %s: %w", s.url(key), err)
	}
	return out.Body, ObjectInfo{Version: aws.ToString(out.ETag), Modified: aws.ToTime(out.LastModified)}, nil
}

func (s *S3Store) url(key string) string {
	return "s3://" + s.bucket + "/" + key
}
