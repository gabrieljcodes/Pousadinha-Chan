package mediastore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"sync"
	"time"

	ods3 "github.com/apache/opendal-go-services/s3"
	opendal "github.com/apache/opendal/bindings/go"
)

const presignLifetime = 10 * time.Minute

type s3Backend struct {
	op     *opendal.Operator
	client *http.Client
	signMu sync.Mutex
}

func newS3(ctx context.Context, cfg Config) (*s3Backend, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	options := opendal.OperatorOptions{"bucket": cfg.Bucket, "region": cfg.Region, "root": cfg.Prefix, "enable_virtual_host_style": strconv.FormatBool(!cfg.PathStyle)}
	if cfg.Endpoint != "" {
		options["endpoint"] = cfg.Endpoint
	}
	// Pass Go's environment explicitly: godotenv/t.Setenv need not update libc's
	// environment when CGO is disabled. OpenDAL still owns credential loading/signing.
	for key, env := range map[string]string{"access_key_id": "AWS_ACCESS_KEY_ID", "secret_access_key": "AWS_SECRET_ACCESS_KEY", "session_token": "AWS_SESSION_TOKEN", "profile": "AWS_PROFILE", "disable_ec2_metadata": "AWS_EC2_METADATA_DISABLED"} {
		if value := os.Getenv(env); value != "" {
			options[key] = value
		}
	}
	op, err := newOperator(ods3.Scheme, options)
	if err != nil {
		return nil, err
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = 30 * time.Second
	transport.MaxIdleConns = 100
	transport.MaxIdleConnsPerHost = 100
	return &s3Backend{op: op, client: &http.Client{Transport: transport, Timeout: 2 * time.Minute, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}
func (s *s3Backend) Close() { s.client.CloseIdleConnections(); s.op.Close() }

// The Go binding does not yet expose *_with options for streaming IO. OpenDAL's
// presign API supplies the complete authenticated request; net/http transports
// it with request cancellation, Range, If-Match and conditional publication.
// These signed requests never leave the server or appear in error messages.
func (s *s3Backend) request(ctx context.Context, key string, sign func(string, time.Duration) (*http.Request, error)) (*http.Request, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.signMu.Lock()
	request, err := sign(key, presignLifetime)
	s.signMu.Unlock()
	if err != nil {
		return nil, nativeError(err)
	}
	return request.WithContext(ctx), ctx.Err()
}

func (s *s3Backend) do(request *http.Request, body io.ReadSeeker) (*http.Response, error) {
	for attempt := 0; attempt < 3; attempt++ {
		if err := request.Context().Err(); err != nil {
			return nil, err
		}
		req := request.Clone(request.Context())
		if body != nil {
			if _, err := body.Seek(0, io.SeekStart); err != nil {
				return nil, err
			}
			req.Body = io.NopCloser(body)
		}
		response, err := s.client.Do(req)
		retry := err != nil
		if err == nil {
			retry = response.StatusCode == 429 || response.StatusCode == 500 || response.StatusCode == 502 || response.StatusCode == 503 || response.StatusCode == 504
		}
		if !retry || attempt == 2 {
			if err != nil {
				var urlErr *url.Error
				if errors.As(err, &urlErr) {
					err = urlErr.Err
				} // Strip signed URL from diagnostics.
				return nil, fmt.Errorf("S3 transport: %w", err)
			}
			return response, nil
		}
		if response != nil {
			response.Body.Close()
		}
		timer := time.NewTimer(time.Duration(1<<attempt) * 200 * time.Millisecond)
		select {
		case <-request.Context().Done():
			timer.Stop()
			return nil, request.Context().Err()
		case <-timer.C:
		}
	}
	panic("unreachable")
}
func statusError(status int) error {
	switch status {
	case 404:
		return fs.ErrNotExist
	case 401, 403:
		return fs.ErrPermission
	case 412:
		return fs.ErrExist
	default:
		return fmt.Errorf("S3 returned HTTP %d", status)
	}
}
func (s *s3Backend) Open(ctx context.Context, key string) (*Object, error) {
	req, err := s.request(ctx, key, s.op.PresignStat)
	if err != nil {
		return nil, err
	}
	response, err := s.do(req, nil)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return nil, statusError(response.StatusCode)
	}
	if response.ContentLength < 0 {
		return nil, fmt.Errorf("S3 object has no content length")
	}
	modified, _ := http.ParseTime(response.Header.Get("Last-Modified"))
	etag := response.Header.Get("ETag")
	reader := &s3Reader{store: s, ctx: ctx, key: key, size: response.ContentLength, etag: etag}
	return &Object{ReadSeekCloser: reader, Size: response.ContentLength, Modified: modified, ETag: etag}, nil
}
func (s *s3Backend) Put(ctx context.Context, key string, src io.ReadSeeker, contentType string) error {
	return s.put(ctx, key, src, contentType, false)
}
func (s *s3Backend) putIfAbsent(ctx context.Context, key string, src io.ReadSeeker, contentType string) error {
	err := s.put(ctx, key, src, contentType, true)
	if errors.Is(err, fs.ErrExist) {
		return nil
	}
	return err
}
func (s *s3Backend) put(ctx context.Context, key string, src io.ReadSeeker, contentType string, absent bool) error {
	req, err := s.request(ctx, key, s.op.PresignWrite)
	if err != nil {
		return err
	}
	req.ContentLength, err = src.Seek(0, io.SeekEnd)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", contentType)
	if absent {
		req.Header.Set("If-None-Match", "*")
	}
	response, err := s.do(req, src)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if absent && response.StatusCode == http.StatusNotImplemented {
		// S3-compatible providers (e.g. Backblaze B2, Ceph, older MinIO) do not implement
		// conditional If-None-Match on PUT and return 501. Retry unconditionally.
		return s.put(ctx, key, src, contentType, false)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return statusError(response.StatusCode)
	}
	return nil
}

// The S3 service does not support PresignDelete in the pinned binding. Use
// OpenDAL's native deletion, with its timeout/retry layers and a pre-call context check.
func (s *s3Backend) Delete(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	err := nativeError(s.op.Delete(key))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

type s3Reader struct {
	mu           sync.Mutex
	store        *s3Backend
	ctx          context.Context
	key, etag    string
	size, offset int64
	body         io.ReadCloser
	closed       bool
}

func (r *s3Reader) Read(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return 0, fs.ErrClosed
	}
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	if len(p) == 0 {
		return 0, nil
	}
	if r.offset >= r.size {
		return 0, io.EOF
	}
	if r.body == nil {
		req, err := r.store.request(r.ctx, r.key, r.store.op.PresignRead)
		if err != nil {
			return 0, err
		}
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", r.offset))
		if r.etag != "" {
			req.Header.Set("If-Match", r.etag)
		}
		response, err := r.store.do(req, nil)
		if err != nil {
			return 0, err
		}
		if response.StatusCode != 206 && !(r.offset == 0 && response.StatusCode == 200) {
			response.Body.Close()
			return 0, statusError(response.StatusCode)
		}
		r.body = response.Body
	}
	n, err := r.body.Read(p)
	r.offset += int64(n)
	if err == io.EOF && r.offset < r.size {
		err = io.ErrUnexpectedEOF
	}
	return n, err
}
func (r *s3Reader) Seek(offset int64, whence int) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return 0, fs.ErrClosed
	}
	switch whence {
	case io.SeekStart:
	case io.SeekCurrent:
		offset += r.offset
	case io.SeekEnd:
		offset += r.size
	default:
		return 0, fs.ErrInvalid
	}
	if offset < 0 {
		return 0, fs.ErrInvalid
	}
	if offset != r.offset && r.body != nil {
		r.body.Close()
		r.body = nil
	}
	r.offset = offset
	return offset, nil
}
func (r *s3Reader) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil
	}
	r.closed = true
	if r.body != nil {
		return r.body.Close()
	}
	return nil
}
