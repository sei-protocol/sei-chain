// homepack packs a set of directories/files from a node home into a
// zstd-compressed tar stream and uploads it to S3 (or writes it to a local
// file), and unpacks such an archive back into a home. It exists to move
// forked flatkv-only homes (which must be cloned byte-for-byte, including the
// flatkv working WALs and forged tendermint databases) between machines that
// have nothing but the binary itself — no aws cli, no zstd, no python.
//
// Usage:
//
//	homepack pack   --home <dir> --out <s3://bucket/key | file> [--include rel1,rel2,...]
//	homepack unpack --home <dir> --from <s3://bucket/key | file>
//
// pack streams: nothing is staged on disk, so a 100GB home needs no scratch
// space. unpack streams the download straight into the extracted layout.
package main

import (
	"archive/tar"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"github.com/klauspost/compress/zstd"
)

// defaultIncludes is the byte-for-byte clone set for a forked flatkv-only
// home: application state (flatkv incl. working WALs), historical query
// store, wasm blobs, forged consensus databases and genesis, and the
// generated validator keys. memiavl is deliberately absent.
var defaultIncludes = []string{
	"config",
	"wasm",
	"fork-validators",
	"data/priv_validator_state.json",
	"data/state_commit/flatkv",
	"data/state_store",
	"data/tendermint",
}

func main() {
	if len(os.Args) < 2 {
		fatalf("usage: homepack pack|unpack ...")
	}
	switch os.Args[1] {
	case "pack":
		fs := flag.NewFlagSet("pack", flag.ExitOnError)
		home := fs.String("home", "", "node home to pack")
		out := fs.String("out", "", "destination: s3://bucket/key or local file path")
		include := fs.String("include", "", "comma-separated home-relative paths (default: forked-home clone set)")
		_ = fs.Parse(os.Args[2:])
		if *home == "" || *out == "" {
			fatalf("pack requires --home and --out")
		}
		includes := defaultIncludes
		if *include != "" {
			includes = strings.Split(*include, ",")
		}
		if err := pack(*home, *out, includes); err != nil {
			fatalf("pack: %v", err)
		}
	case "unpack":
		fs := flag.NewFlagSet("unpack", flag.ExitOnError)
		home := fs.String("home", "", "node home to unpack into")
		from := fs.String("from", "", "source: s3://bucket/key or local file path")
		_ = fs.Parse(os.Args[2:])
		if *home == "" || *from == "" {
			fatalf("unpack requires --home and --from")
		}
		if err := unpack(*home, *from); err != nil {
			fatalf("unpack: %v", err)
		}
	default:
		fatalf("unknown subcommand %q", os.Args[1])
	}
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}

func parseS3(uri string) (bucket, key string, ok bool) {
	rest, found := strings.CutPrefix(uri, "s3://")
	if !found {
		return "", "", false
	}
	bucket, key, found = strings.Cut(rest, "/")
	if !found || bucket == "" || key == "" {
		return "", "", false
	}
	return bucket, key, true
}

func pack(home string, out string, includes []string) error {
	pr, pw := io.Pipe()
	errCh := make(chan error, 1)
	go func() {
		errCh <- writeTarZst(pw, home, includes)
	}()

	var uploadErr error
	if bucket, key, ok := parseS3(out); ok {
		sess, err := session.NewSessionWithOptions(session.Options{SharedConfigState: session.SharedConfigEnable})
		if err != nil {
			return fmt.Errorf("create AWS session: %w", err)
		}
		uploader := s3manager.NewUploader(sess, func(u *s3manager.Uploader) {
			u.PartSize = 256 << 20 // 256MiB parts: 10k-part cap comfortably covers multi-TB archives
			u.Concurrency = 4
		})
		_, uploadErr = uploader.Upload(&s3manager.UploadInput{
			Bucket: aws.String(bucket),
			Key:    aws.String(key),
			Body:   pr,
		})
	} else {
		f, err := os.Create(out)
		if err != nil {
			return fmt.Errorf("create output file: %w", err)
		}
		_, uploadErr = io.Copy(f, pr)
		if cerr := f.Close(); uploadErr == nil {
			uploadErr = cerr
		}
	}
	// Unblock the producer if the consumer failed mid-stream.
	_ = pr.CloseWithError(uploadErr)
	packErr := <-errCh
	if uploadErr != nil {
		return fmt.Errorf("write archive: %w", uploadErr)
	}
	if packErr != nil {
		return fmt.Errorf("pack home: %w", packErr)
	}
	fmt.Printf("packed %s -> %s\n", home, out)
	return nil
}

func writeTarZst(pw *io.PipeWriter, home string, includes []string) error {
	zw, err := zstd.NewWriter(pw, zstd.WithEncoderLevel(zstd.SpeedDefault), zstd.WithEncoderConcurrency(8))
	if err != nil {
		pw.CloseWithError(err)
		return err
	}
	tw := tar.NewWriter(zw)
	var total int64
	for _, rel := range includes {
		abs := filepath.Join(home, rel)
		if _, err := os.Lstat(abs); os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "skip missing %s\n", rel)
			continue
		}
		if err := addTree(tw, home, abs, &total); err != nil {
			err = fmt.Errorf("add %s: %w", rel, err)
			pw.CloseWithError(err)
			return err
		}
	}
	if err := tw.Close(); err != nil {
		pw.CloseWithError(err)
		return err
	}
	if err := zw.Close(); err != nil {
		pw.CloseWithError(err)
		return err
	}
	fmt.Fprintf(os.Stderr, "tar stream complete, %d bytes raw\n", total)
	return pw.Close()
}

func addTree(tw *tar.Writer, home, root string, total *int64) error {
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(home, path)
		if err != nil {
			return err
		}
		var link string
		if info.Mode()&os.ModeSymlink != 0 {
			if link, err = os.Readlink(path); err != nil {
				return err
			}
		}
		hdr, err := tar.FileInfoHeader(info, link)
		if err != nil {
			return err
		}
		hdr.Name = filepath.ToSlash(rel)
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		n, err := io.Copy(tw, f)
		_ = f.Close()
		*total += n
		return err
	})
}

func unpack(home string, from string) error {
	var src io.ReadCloser
	if bucket, key, ok := parseS3(from); ok {
		sess, err := session.NewSessionWithOptions(session.Options{SharedConfigState: session.SharedConfigEnable})
		if err != nil {
			return fmt.Errorf("create AWS session: %w", err)
		}
		obj, err := s3.New(sess).GetObject(&s3.GetObjectInput{
			Bucket: aws.String(bucket),
			Key:    aws.String(key),
		})
		if err != nil {
			return fmt.Errorf("get s3 object: %w", err)
		}
		src = obj.Body
	} else {
		f, err := os.Open(from)
		if err != nil {
			return fmt.Errorf("open archive: %w", err)
		}
		src = f
	}
	defer func() { _ = src.Close() }()

	zr, err := zstd.NewReader(src)
	if err != nil {
		return fmt.Errorf("open zstd stream: %w", err)
	}
	defer zr.Close()
	tr := tar.NewReader(zr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read tar entry: %w", err)
		}
		dest := filepath.Join(home, filepath.FromSlash(hdr.Name))
		if !strings.HasPrefix(filepath.Clean(dest), filepath.Clean(home)+string(os.PathSeparator)) && filepath.Clean(dest) != filepath.Clean(home) {
			return fmt.Errorf("archive entry %q escapes home", hdr.Name)
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(dest, hdr.FileInfo().Mode().Perm()); err != nil {
				return err
			}
		case tar.TypeSymlink:
			if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
				return err
			}
			_ = os.Remove(dest)
			if err := os.Symlink(hdr.Linkname, dest); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
				return err
			}
			f, err := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, hdr.FileInfo().Mode().Perm())
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, tr); err != nil { //nolint:gosec // trusted archive produced by homepack pack
				_ = f.Close()
				return err
			}
			if err := f.Close(); err != nil {
				return err
			}
		default:
			fmt.Fprintf(os.Stderr, "skip entry %s (type %d)\n", hdr.Name, hdr.Typeflag)
		}
	}
	fmt.Printf("unpacked %s -> %s\n", from, home)
	return nil
}
