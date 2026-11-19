package statesync

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	ssarchive "github.com/sei-protocol/sei-chain/sei-db/statesync"
)

func checkCmd() *cobra.Command {
	var bucket, region string
	var write bool
	cmd := &cobra.Command{
		Use:   "check",
		Short: "Check that this node can reach the state sync archive bucket",
		Long: `Check that this node can reach the state sync archive bucket of its chain, with the AWS
credentials and region that the node uses. It prints the bucket, the region and the source of the
credentials, then reads latest.txt, which needs s3:GetObject and s3:ListBucket.

With --write, it also starts a multipart upload and aborts it, which needs s3:PutObject and
s3:AbortMultipartUpload and leaves no object. A node that uploads archives needs --write to pass.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			t, err := resolveTarget(cmd)
			if err != nil {
				return err
			}
			if bucket == "" {
				bucket = t.stateSync.ArchiveBucket
			}
			if bucket == "" {
				return errors.New("set --bucket, or [giga.state_sync] archive_bucket in app.toml")
			}
			if region == "" {
				region = t.stateSync.ArchiveRegion
			}
			return withSignals(cmd.Context(), func(ctx context.Context) error {
				store, err := ssarchive.NewS3Store(ctx, bucket, region)
				if err != nil {
					return err
				}
				return runCheck(ctx, cmd.OutOrStdout(), bucket, store, t.chainID, write)
			})
		},
	}
	cmd.Flags().StringVar(&bucket, "bucket", "", "bucket to check (default: archive_bucket)")
	cmd.Flags().StringVar(&region, "region", "", "AWS region of the bucket (default: archive_region, then the AWS SDK default)")
	cmd.Flags().BoolVar(&write, "write", false, "also check the permissions of an archive upload")
	return cmd
}

// accessChecker is an archive store that reports how it reaches its bucket.
type accessChecker interface {
	ssarchive.ObjectStore
	Region() string
	CredentialSource(ctx context.Context) (string, error)
	CheckWrite(ctx context.Context, key string) error
}

// runCheck checks that store can read the archives of chain chainID in bucket, and with write,
// that it can upload one. It writes one line for each step to out, and returns the error of the
// first step that fails.
func runCheck(ctx context.Context, out io.Writer, bucket string, store accessChecker, chainID string, write bool) error {
	_, _ = fmt.Fprintf(out, "bucket: %s\nregion: %s\n", bucket, store.Region())
	source, err := store.CredentialSource(ctx)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(out, "credentials: %s\n", source)

	latest, err := ssarchive.ReadLatest(ctx, store, chainID)
	switch {
	case errors.Is(err, ssarchive.ErrObjectNotFound):
		_, _ = fmt.Fprintf(out, "read: ok, no archive of chain %s yet\n", chainID)
	case err != nil:
		return fmt.Errorf("read check: %w", err)
	default:
		_, _ = fmt.Fprintf(out, "read: ok, newest archive of chain %s is at height %d\n", chainID, latest)
	}

	if !write {
		return nil
	}
	if err := store.CheckWrite(ctx, ssarchive.WriteCheckKey(chainID)); err != nil {
		return fmt.Errorf("write check: %w", err)
	}
	_, _ = fmt.Fprintln(out, "write: ok")
	return nil
}
