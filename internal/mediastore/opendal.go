package mediastore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"sync"
	"time"

	opendal "github.com/apache/opendal/bindings/go"
)

// OpenDAL's synchronous FFI calls have native timeouts; a Go context cannot
// interrupt a call already inside Rust. Check cancellation between operations,
// and never free an operator or reader while a native call is running.
func newOperator(scheme opendal.Scheme, options opendal.OperatorOptions) (*opendal.Operator, error) {
	op, err := opendal.NewOperator(scheme, options, opendal.WithRetry(opendal.RetryMaxTimes(2)), opendal.WithTimeout(30*time.Second, 30*time.Second))
	return op, nativeError(err)
}

func nativeError(err error) error {
	var native *opendal.Error
	if !errors.As(err, &native) {
		return err
	}
	// Native error messages can contain signed URLs. Keep only the public code.
	switch native.Code() {
	case opendal.CodeNotFound:
		return fs.ErrNotExist
	case opendal.CodePermissioDenied:
		return fs.ErrPermission
	case opendal.CodeAlreadyExists:
		return fs.ErrExist
	case opendal.CodeIsADirectory, opendal.CodeNotADirectory:
		return fs.ErrInvalid
	default:
		return fmt.Errorf("OpenDAL operation failed (code %d)", native.Code())
	}
}

// nativeReader owns both the OpenDAL reader and the pinned filesystem descriptor.
// Serializing Close with Read/Seek prevents freeing native memory during an IO.
type nativeReader struct {
	mu     sync.Mutex
	ctx    context.Context
	reader *opendal.Reader
	file   *os.File
	closed bool
}

func (r *nativeReader) Read(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return 0, fs.ErrClosed
	}
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := r.reader.Read(p)
	return n, nativeError(err)
}
func (r *nativeReader) Seek(offset int64, whence int) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return 0, fs.ErrClosed
	}
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := r.reader.Seek(offset, whence)
	return n, nativeError(err)
}
func (r *nativeReader) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil
	}
	r.closed = true
	return errors.Join(nativeError(r.reader.Close()), r.file.Close())
}

type leasedReader struct {
	io.ReadSeekCloser
	once    sync.Once
	release func()
	err     error
}

func (r *leasedReader) Close() error {
	r.once.Do(func() { r.err = r.ReadSeekCloser.Close(); r.release() })
	return r.err
}
