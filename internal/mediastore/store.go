// Package mediastore stores immutable, relative media keys independently of their public URLs.
package mediastore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path"
	"strings"
	"sync"
	"time"
)

type Config struct {
	Backend   string
	Directory string
	Bucket    string
	Region    string
	Endpoint  string
	Prefix    string
	PathStyle bool
}

func ValidKey(key string) bool {
	return fs.ValidPath(key) && key != "." && !strings.ContainsAny(key, "\\\x00") && path.Clean(key) == key
}

func (c Config) Validate() error {
	if c.Backend != "" && c.Backend != "filesystem" && c.Backend != "s3" {
		return fmt.Errorf("GACHA_STORAGE_BACKEND must be filesystem or s3")
	}
	if c.Backend != "s3" {
		return nil
	}
	if c.Bucket == "" || c.Region == "" {
		return fmt.Errorf("S3 storage requires GACHA_S3_BUCKET and GACHA_S3_REGION")
	}
	if c.Prefix != "" && !ValidKey(c.Prefix) {
		return fmt.Errorf("GACHA_S3_PREFIX must be a relative object prefix without trailing slash")
	}
	if c.Endpoint != "" {
		u, err := url.Parse(c.Endpoint)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
			return fmt.Errorf("GACHA_S3_ENDPOINT must be an HTTP(S) origin")
		}
	}
	return nil
}

type Object struct {
	io.ReadSeekCloser
	Size     int64
	Modified time.Time
	ETag     string
}

type backend interface {
	Open(context.Context, string) (*Object, error)
	Put(context.Context, string, io.ReadSeeker, string) error
	Delete(context.Context, string) error
	Close()
}

// Store writes to the selected backend. In S3 mode, local files remain readable
// until copied to S3. Only a definitive not-found permits local fallback.
// The key namespace must be immutable and identical across both backends.
type Store struct {
	mu      sync.RWMutex
	closed  bool
	primary backend
	local   *filesystem
	remote  *s3Backend
}

func New(ctx context.Context, cfg Config) (*Store, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if cfg.Directory == "" {
		cfg.Directory = "data/gacha"
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	local, err := newFilesystem(cfg.Directory)
	if err != nil {
		return nil, err
	}
	s := &Store{primary: local, local: local}
	if cfg.Backend == "s3" {
		remote, err := newS3(ctx, cfg)
		if err != nil {
			local.Close()
			return nil, err
		}
		s.primary, s.remote = remote, remote
	}
	return s, nil
}

func (s *Store) Open(ctx context.Context, key string) (*Object, error) {
	if !ValidKey(key) {
		return nil, fs.ErrInvalid
	}
	release, err := s.acquire()
	if err != nil {
		return nil, err
	}
	object, err := s.primary.Open(ctx, key)
	if s.remote != nil && errors.Is(err, fs.ErrNotExist) {
		object, err = s.local.Open(ctx, key)
	}
	if err != nil {
		release()
		return nil, err
	}
	object.ReadSeekCloser = &leasedReader{ReadSeekCloser: object.ReadSeekCloser, release: release}
	return object, nil
}

func (s *Store) PutFile(ctx context.Context, key, filename, contentType string) error {
	release, lockErr := s.acquire()
	if lockErr != nil {
		return lockErr
	}
	defer release()
	if !ValidKey(key) {
		return fs.ErrInvalid
	}
	file, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer file.Close()
	return s.primary.Put(ctx, key, file, contentType)
}

// Delete removes both copies so fallback cannot resurrect a deleted object.
func (s *Store) Delete(ctx context.Context, key string) error {
	release, lockErr := s.acquire()
	if lockErr != nil {
		return lockErr
	}
	defer release()
	if !ValidKey(key) {
		return fs.ErrInvalid
	}
	if err := s.primary.Delete(ctx, key); err != nil {
		return err
	}
	if s.remote != nil {
		return s.local.Delete(ctx, key)
	}
	return nil
}

// CopyLocal verifies existing remote bytes or publishes and verifies a local
// object. It never removes the source or overwrites different remote content.
func (s *Store) CopyLocal(ctx context.Context, key, contentType string) error {
	release, lockErr := s.acquire()
	if lockErr != nil {
		return lockErr
	}
	defer release()
	if s.remote == nil {
		return fmt.Errorf("media copy requires the s3 backend")
	}
	if !ValidKey(key) {
		return fs.ErrInvalid
	}
	src, err := s.local.Open(ctx, key)
	if errors.Is(err, fs.ErrNotExist) {
		remote, remoteErr := s.primary.Open(ctx, key)
		if remoteErr != nil {
			return remoteErr
		}
		return remote.Close()
	}
	if err != nil {
		return err
	}
	defer src.Close()
	dst, err := s.primary.Open(ctx, key)
	if errors.Is(err, fs.ErrNotExist) {
		if err = s.remote.putIfAbsent(ctx, key, src, contentType); err != nil {
			return err
		}
		dst, err = s.primary.Open(ctx, key)
	}
	if err != nil {
		return err
	}
	defer dst.Close()
	return verifyObjects(src, dst)
}

func (s *Store) acquire() (func(), error) {
	s.mu.RLock()
	if s.closed {
		s.mu.RUnlock()
		return nil, fs.ErrClosed
	}
	return s.mu.RUnlock, nil
}

// Close waits for outstanding objects to close before freeing native operators.
// Callers must close every Object, including HEAD/conditional HTTP responses.
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	s.primary.Close()
	if s.remote != nil {
		s.local.Close()
	}
	return nil
}
