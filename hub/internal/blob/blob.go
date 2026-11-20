// Package blob stores files (outputs, mirrored directory contents) by key, in S3-compatible
// storage (RustFS) or a local directory.
package blob

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

var ErrNotFound = errors.New("blob not found")

type Store interface {
	Put(ctx context.Context, key string, r io.Reader, size int64, mime string) error
	Get(ctx context.Context, key string) (io.ReadCloser, int64, error)
	Exists(ctx context.Context, key string) (bool, error)
}

// Dir keeps blobs under a local directory.
type Dir struct{ Root string }

func (d Dir) path(key string) (string, error) {
	clean := filepath.Clean("/" + key)
	if strings.Contains(key, "..") {
		return "", fmt.Errorf("bad blob key %q", key)
	}
	return filepath.Join(d.Root, clean), nil
}

func (d Dir) Put(_ context.Context, key string, r io.Reader, _ int64, _ string) error {
	p, err := d.path(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), ".tmp-*")
	if err != nil {
		return err
	}
	if _, err := io.Copy(tmp, r); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), p)
}

func (d Dir) Get(_ context.Context, key string) (io.ReadCloser, int64, error) {
	p, err := d.path(key)
	if err != nil {
		return nil, 0, err
	}
	f, err := os.Open(p)
	if errors.Is(err, os.ErrNotExist) {
		return nil, 0, ErrNotFound
	} else if err != nil {
		return nil, 0, err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, 0, err
	}
	return f, st.Size(), nil
}

func (d Dir) Exists(_ context.Context, key string) (bool, error) {
	p, err := d.path(key)
	if err != nil {
		return false, err
	}
	_, err = os.Stat(p)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

// S3 keeps blobs in an S3-compatible bucket (RustFS in development).
type S3 struct {
	Client *minio.Client
	Bucket string
}

func NewS3(ctx context.Context, endpoint, accessKey, secretKey, bucket string, secure bool) (*S3, error) {
	c, err := minio.New(endpoint, &minio.Options{Creds: credentials.NewStaticV4(accessKey, secretKey, ""), Secure: secure})
	if err != nil {
		return nil, err
	}
	ok, err := c.BucketExists(ctx, bucket)
	if err != nil {
		return nil, fmt.Errorf("object storage at %s: %w", endpoint, err)
	}
	if !ok {
		if err := c.MakeBucket(ctx, bucket, minio.MakeBucketOptions{}); err != nil {
			return nil, err
		}
	}
	return &S3{Client: c, Bucket: bucket}, nil
}

func (s *S3) Put(ctx context.Context, key string, r io.Reader, size int64, mime string) error {
	_, err := s.Client.PutObject(ctx, s.Bucket, key, r, size, minio.PutObjectOptions{ContentType: mime})
	return err
}

func (s *S3) Get(ctx context.Context, key string) (io.ReadCloser, int64, error) {
	obj, err := s.Client.GetObject(ctx, s.Bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, 0, err
	}
	st, err := obj.Stat()
	if err != nil {
		obj.Close()
		if minio.ToErrorResponse(err).Code == "NoSuchKey" {
			return nil, 0, ErrNotFound
		}
		return nil, 0, err
	}
	return obj, st.Size, nil
}

func (s *S3) Exists(ctx context.Context, key string) (bool, error) {
	_, err := s.Client.StatObject(ctx, s.Bucket, key, minio.StatObjectOptions{})
	if err == nil {
		return true, nil
	}
	if minio.ToErrorResponse(err).Code == "NoSuchKey" {
		return false, nil
	}
	return false, err
}

// ContentKey is where a file with this SHA-256 lives (content-addressed, shared by mirrors).
func ContentKey(sha string) string { return "content/" + sha[:2] + "/" + sha }
