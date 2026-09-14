package mediastore

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
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
