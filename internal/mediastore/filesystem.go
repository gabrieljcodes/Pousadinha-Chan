package mediastore

import (
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"strings"

	odfs "github.com/apache/opendal-go-services/fs"
	opendal "github.com/apache/opendal/bindings/go"
	"github.com/google/uuid"
	"sync"
)

type filesystem struct {
	directory string
	op        *opendal.Operator
	mu        sync.Mutex
}

func newFilesystem(directory string) (*filesystem, error) {
	// OpenDAL receives descriptor paths only, never user-supplied paths under /.
	// os.Root confines resolution; /proc/self/fd pins the resolved inode while
	// the OpenDAL fs service performs IO. This avoids check-then-open symlink races.
	op, err := newOperator(odfs.Scheme, opendal.OperatorOptions{"root": "/"})
	if err != nil {
		return nil, err
	}
	return &filesystem{directory: directory, op: op}, nil
}
func descriptorPath(f *os.File) string { return fmt.Sprintf("proc/self/fd/%d", f.Fd()) }
func (s *filesystem) Close()           { s.op.Close() }

func (s *filesystem) Open(ctx context.Context, key string) (*Object, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(s.directory)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	file, err := root.Open(key)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, fs.ErrPermission) {
			return nil, err
		}
		return nil, fmt.Errorf("%w: %v", fs.ErrInvalid, err)
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		file.Close()
		if err != nil {
			return nil, err
		}
		return nil, fs.ErrInvalid
	}
	s.mu.Lock()
	meta, err := s.op.Stat(descriptorPath(file))
	var reader *opendal.Reader
	if err == nil {
		reader, err = s.op.Reader(descriptorPath(file))
	}
	s.mu.Unlock()
	if err != nil {
		file.Close()
		return nil, nativeError(err)
	}
	return &Object{ReadSeekCloser: &nativeReader{ctx: ctx, reader: reader, file: file}, Size: int64(meta.ContentLength()), Modified: meta.LastModified()}, nil
}

func (s *filesystem) Put(ctx context.Context, key string, src io.ReadSeeker, _ string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.MkdirAll(s.directory, 0750); err != nil {
		return err
	}
	root, err := os.OpenRoot(s.directory)
	if err != nil {
		return err
	}
	defer root.Close()
	if err = root.MkdirAll(path.Dir(key), 0755); err != nil {
		return err
	}
	parent, err := root.Open(path.Dir(key))
	if err != nil {
		return err
	}
	defer parent.Close()
	temp := ".pending-" + uuid.NewString()
	// Create the temporary file through the confined root. The parent descriptor
	// is retained until publication, including across concurrent directory renames.
	parentRoot, err := os.OpenRoot("/" + descriptorPath(parent))
	if err != nil {
		return err
	}
	defer parentRoot.Close()
	file, err := parentRoot.OpenFile(temp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer file.Close()
	defer parentRoot.Remove(temp)
	s.mu.Lock()
	writer, err := s.op.Writer(descriptorPath(file))
	s.mu.Unlock()
	if err != nil {
		return nativeError(err)
	}
	// Keep chunks below the binding's 256 KiB Write limit. Do not use src.WriteTo.
	_, copyErr := io.CopyBuffer(writer, &contextReader{ctx: ctx, reader: src}, make([]byte, 64<<10))
	closeErr := nativeError(writer.Close()) // Close also frees the native writer on error.
	if err = errors.Join(copyErr, closeErr); err != nil {
		return err
	}
	if err = file.Sync(); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	err = nativeError(s.op.Rename(path.Join(descriptorPath(parent), temp), path.Join(descriptorPath(parent), path.Base(key))))
	s.mu.Unlock()
	return err
}

func (s *filesystem) Delete(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	root, err := os.OpenRoot(s.directory)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer root.Close()
	parent, err := root.Open(path.Dir(key))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer parent.Close()
	// Remove a directory entry relative to a pinned, confined parent. A leaf
	// symlink is unlinked rather than followed by the fs backend.
	s.mu.Lock()
	err = nativeError(s.op.Delete(path.Join(descriptorPath(parent), path.Base(key))))
	s.mu.Unlock()
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

func verifyObjects(a, b *Object) error {
	if a.Size != b.Size {
		return fmt.Errorf("media size mismatch; local file retained")
	}
	if b.ETag != "" {
		clean := strings.Trim(b.ETag, "\"")
		if len(clean) == 32 && !strings.Contains(clean, "-") {
			if _, err := a.Seek(0, io.SeekStart); err != nil {
				return err
			}
			h := md5.New()
			if _, err := io.Copy(h, a); err != nil {
				return err
			}
			if hex.EncodeToString(h.Sum(nil)) == strings.ToLower(clean) {
				return nil
			}
			return fmt.Errorf("media checksum mismatch; local file retained")
		}
	}
	if _, err := a.Seek(0, io.SeekStart); err != nil {
		return err
	}
	ah, bh := sha256.New(), sha256.New()
	if _, err := io.Copy(ah, a); err != nil {
		return err
	}
	if _, err := io.Copy(bh, b); err != nil {
		return err
	}
	if string(ah.Sum(nil)) != string(bh.Sum(nil)) {
		return fmt.Errorf("media checksum mismatch; local file retained")
	}
	return nil
}
