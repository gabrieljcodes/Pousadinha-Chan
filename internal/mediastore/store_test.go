package mediastore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

func sourceFile(t *testing.T, data string) string {
	t.Helper()
	name := filepath.Join(t.TempDir(), "source")
	if err := os.WriteFile(name, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	return name
}
func contents(t *testing.T, s *Store, key string) string {
	t.Helper()
	obj, err := s.Open(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	defer obj.Close()
	data, err := io.ReadAll(obj)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
func TestFilesystemConfinementAndAtomicPublication(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s, err := New(ctx, Config{Directory: dir})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	source := sourceFile(t, "first")
	key := "work/character/card.png"
	if err = s.PutFile(ctx, key, source, "image/png"); err != nil {
		t.Fatal(err)
	}
	if got := contents(t, s, key); got != "first" {
		t.Fatal(got)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if err = s.PutFile(canceled, key, sourceFile(t, "second"), "image/png"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if got := contents(t, s, key); got != "first" {
		t.Fatal("canceled upload replaced live object", got)
	}
	outside := t.TempDir()
	if err = os.WriteFile(filepath.Join(outside, "secret"), []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(outside, filepath.Join(dir, "escape")); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Open(ctx, "escape/secret"); err == nil {
		t.Fatal("read escaped root")
	}
	if err = s.PutFile(ctx, "escape/new", source, "image/png"); err == nil {
		t.Fatal("write escaped root")
	}
	if err = s.Delete(ctx, "escape/secret"); err == nil {
		t.Fatal("delete escaped root")
	}
	for _, invalid := range []string{"../secret", "/absolute", "a/../b", "a//b", ".", "a\\b", "a\x00b"} {
		if _, err = s.Open(ctx, invalid); !errors.Is(err, fs.ErrInvalid) {
			t.Errorf("key %q: %v", invalid, err)
		}
		if err = s.PutFile(ctx, invalid, source, "image/png"); !errors.Is(err, fs.ErrInvalid) {
			t.Errorf("write key %q: %v", invalid, err)
		}
	}
	if err = s.Delete(ctx, key); err != nil {
		t.Fatal(err)
	}
	if err = s.Delete(ctx, key); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Open(ctx, key); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal(err)
	}
}

func TestS3HTTPRangeHeadAndErrorFallback(t *testing.T) {
	var mu sync.Mutex
	gets := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("X-Amz-Algorithm") != "AWS4-HMAC-SHA256" || r.URL.Query().Get("X-Amz-Signature") == "" {
			t.Error("unsigned request")
		}
		if strings.HasSuffix(r.URL.Path, "denied") {
			w.WriteHeader(403)
			return
		}
		if strings.HasSuffix(r.URL.Path, "missing") {
			w.WriteHeader(404)
			return
		}
		if r.URL.Path != "/bucket/prefix/card.gif" {
			t.Errorf("unexpected key %s", r.URL.Path)
			w.WriteHeader(404)
			return
		}
		w.Header().Set("ETag", `"version-1"`)
		w.Header().Set("Last-Modified", time.Unix(1700000000, 0).UTC().Format(http.TimeFormat))
		if r.Method == "HEAD" {
			w.Header().Set("Content-Length", "10")
			return
		}
		mu.Lock()
		gets++
		mu.Unlock()
		if r.Header.Get("If-Match") != `"version-1"` {
			t.Error("missing version guard")
		}
		var start int
		if _, err := fmt.Sscanf(r.Header.Get("Range"), "bytes=%d-", &start); err != nil {
			t.Error(err)
		}
		body := "0123456789"[start:]
		w.Header().Set("Content-Length", fmt.Sprint(len(body)))
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-9/10", start))
		w.WriteHeader(206)
		io.WriteString(w, body)
	}))
	defer server.Close()
	store := testS3Store(t, server.URL, "bucket", "prefix")
	local := store.local
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		obj, err := store.Open(r.Context(), "card.gif")
		if err != nil {
			t.Error(err)
			w.WriteHeader(500)
			return
		}
		defer obj.Close()
		w.Header().Set("Content-Type", "image/gif")
		w.Header().Set("ETag", obj.ETag)
		http.ServeContent(w, r, "card.gif", obj.Modified, obj)
	})
	head := httptest.NewRecorder()
	handler.ServeHTTP(head, httptest.NewRequest("HEAD", "/", nil))
	if head.Code != 200 || head.Body.Len() != 0 || head.Header().Get("Content-Length") != "10" {
		t.Fatal("bad HEAD", head)
	}
	mu.Lock()
	n := gets
	mu.Unlock()
	if n != 0 {
		t.Fatal("HEAD downloaded object")
	}
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Range", "bytes=2-5")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != 206 || rec.Body.String() != "2345" {
		t.Fatalf("range: %d %q", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest("GET", "/", nil)
	req.Header.Set("If-None-Match", `"version-1"`)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != 304 {
		t.Fatal(rec.Code)
	}
	for _, key := range []string{"missing", "denied"} {
		if err := local.Put(context.Background(), key, strings.NewReader("local"), "image/png"); err != nil {
			t.Fatal(err)
		}
	}
	if got := contents(t, store, "missing"); got != "local" {
		t.Fatal(got)
	}
	if _, err := store.Open(context.Background(), "denied"); err == nil || errors.Is(err, fs.ErrNotExist) {
		t.Fatal("access denied must not fall back", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.Open(ctx, "card.gif"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func testS3Store(t *testing.T, endpoint, bucket, prefix string) *Store {
	t.Helper()
	access, secret := os.Getenv("GACHA_TEST_S3_ACCESS_KEY"), os.Getenv("GACHA_TEST_S3_SECRET_KEY")
	if access == "" {
		access, secret = "storage-test", "storage-test-password"
	}
	t.Setenv("AWS_ACCESS_KEY_ID", access)
	t.Setenv("AWS_SECRET_ACCESS_KEY", secret)
	t.Setenv("AWS_SESSION_TOKEN", "")
	store, err := New(context.Background(), Config{Backend: "s3", Directory: t.TempDir(), Bucket: bucket, Region: "us-east-1", Endpoint: endpoint, Prefix: prefix, PathStyle: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func TestS3Integration(t *testing.T) {
	endpoint := os.Getenv("GACHA_TEST_S3_ENDPOINT")
	if endpoint == "" {
		t.Skip("set GACHA_TEST_S3_ENDPOINT for a disposable S3 service")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	bucket := os.Getenv("GACHA_TEST_S3_BUCKET")
	if bucket == "" {
		bucket = "gacha-test"
	}
	store := testS3Store(t, endpoint, bucket, "storage-test-"+uuid.NewString())
	b, local := store.remote, store.local
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		for _, key := range []string{"legacy.png", "new.gif", "mismatch.png"} {
			if err := b.Delete(cleanup, key); err != nil {
				t.Error(err)
			}
		}
	})
	if err := local.Put(ctx, "legacy.png", strings.NewReader("legacy-image"), "image/png"); err != nil {
		t.Fatal(err)
	}
	if got := contents(t, store, "legacy.png"); got != "legacy-image" {
		t.Fatal(got)
	}
	for range 2 {
		if err := store.CopyLocal(ctx, "legacy.png", "image/png"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(filepath.Join(local.directory, "legacy.png")); err != nil {
		t.Fatal("copy removed source", err)
	}
	if err := store.PutFile(ctx, "new.gif", sourceFile(t, "new-image"), "image/gif"); err != nil {
		t.Fatal(err)
	}
	if got := contents(t, store, "new.gif"); got != "new-image" {
		t.Fatal(got)
	}
	if err := store.CopyLocal(ctx, "new.gif", "image/gif"); err != nil {
		t.Fatal("remote-only asset", err)
	}
	if err := local.Put(ctx, "mismatch.png", strings.NewReader("local"), "image/png"); err != nil {
		t.Fatal(err)
	}
	if err := b.Put(ctx, "mismatch.png", strings.NewReader("other"), "image/png"); err != nil {
		t.Fatal(err)
	}
	if err := b.putIfAbsent(ctx, "mismatch.png", strings.NewReader("newer"), "image/png"); err != nil {
		t.Fatal(err)
	}
	if got := contents(t, store, "mismatch.png"); got != "other" {
		t.Fatal("conditional publication overwrote existing content")
	}
	if err := store.CopyLocal(ctx, "mismatch.png", "image/png"); err == nil {
		t.Fatal("copy accepted mismatched content")
	}
	if got := contents(t, store, "mismatch.png"); got != "other" {
		t.Fatal("copy overwrote mismatched object")
	}
	if err := store.Delete(ctx, "legacy.png"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Open(ctx, "legacy.png"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("delete left a readable copy", err)
	}
}

func TestConfiguration(t *testing.T) {
	for _, cfg := range []Config{{Backend: "other"}, {Backend: "s3"}, {Backend: "s3", Bucket: "bucket", Region: "us-east-1", Prefix: "../escape"}, {Backend: "s3", Bucket: "bucket", Region: "us-east-1", Endpoint: "https://user:pass@example.com"}} {
		if err := cfg.Validate(); err == nil {
			t.Fatalf("accepted invalid config %#v", cfg)
		}
	}
}

func TestOpenDALReaderPinsFileAndStoreCloseWaits(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store, err := New(ctx, Config{Directory: dir})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	if err = store.PutFile(ctx, "card.png", sourceFile(t, "original-card"), "image/png"); err != nil {
		t.Fatal(err)
	}
	object, err := store.Open(ctx, "card.png")
	if err != nil {
		t.Fatal(err)
	}
	defer object.Close()
	// The native Reader is lazy. Replacing the catalog path before its first
	// Read must not change the already-resolved inode or expose a symlink target.
	if err = os.Rename(filepath.Join(dir, "card.png"), filepath.Join(dir, "saved.png")); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(sourceFile(t, "private-secret"), filepath.Join(dir, "card.png")); err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(object)
	if err != nil || string(data) != "original-card" {
		t.Fatalf("pinned reader: %q %v", data, err)
	}
	if _, err = store.Open(ctx, "card.png"); err == nil {
		t.Fatal("new reader followed escaping symlink")
	}
	done := make(chan error, 1)
	go func() { done <- store.Close() }()
	select {
	case err = <-done:
		t.Fatal("operator freed with an open reader", err)
	case <-time.After(20 * time.Millisecond):
	}
	if err = object.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err = <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Close did not release operators")
	}
	if _, err = store.Open(ctx, "saved.png"); !errors.Is(err, fs.ErrClosed) {
		t.Fatal("closed store reopened", err)
	}
	if err = object.Close(); err != nil {
		t.Fatal("double reader close", err)
	}
	if err = store.Close(); err != nil {
		t.Fatal("double operator close", err)
	}
}

func TestOpenDALCancelledReader(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	store, err := New(ctx, Config{Directory: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err = store.PutFile(ctx, "card.png", sourceFile(t, "image"), "image/png"); err != nil {
		t.Fatal(err)
	}
	object, err := store.Open(ctx, "card.png")
	if err != nil {
		t.Fatal(err)
	}
	defer object.Close()
	cancel()
	if _, err = object.Read(make([]byte, 1)); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
