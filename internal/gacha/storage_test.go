package gacha

import (
	"bot/internal/mediastore"
	"context"
	"errors"
	"io/fs"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
)

func testS3Catalog(t *testing.T, original *Store) {
	endpoint := os.Getenv("GACHA_TEST_S3_ENDPOINT")
	if endpoint == "" {
		t.Skip("set GACHA_TEST_S3_ENDPOINT for S3 catalog integration")
	}
	access, secret := os.Getenv("GACHA_TEST_S3_ACCESS_KEY"), os.Getenv("GACHA_TEST_S3_SECRET_KEY")
	if access == "" {
		access, secret = "storage-test", "storage-test-password"
	}
	t.Setenv("AWS_ACCESS_KEY_ID", access)
	t.Setenv("AWS_SECRET_ACCESS_KEY", secret)
	t.Setenv("AWS_SESSION_TOKEN", "")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	bucket := os.Getenv("GACHA_TEST_S3_BUCKET")
	if bucket == "" {
		bucket = "gacha-test"
	}
	store := &Store{DB: original.DB, Config: Config{MediaDir: t.TempDir(), Storage: mediastore.Config{Backend: "s3", Bucket: bucket, Prefix: "catalog-test-" + uuid.NewString(), Region: "us-east-1", Endpoint: endpoint, PathStyle: true}}}
	media, err := store.MediaStorage()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.CloseMedia() })
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		for _, key := range []string{"s3-portrait.png", "s3-extra.gif"} {
			if err := media.Delete(cleanup, key); err != nil {
				t.Error(err)
			}
		}
	})
	local := filepath.Join(t.TempDir(), "card")
	if err = os.WriteFile(local, []byte("card-content"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = media.PutFile(ctx, "s3-portrait.png", local, "image/png"); err != nil {
		t.Fatal(err)
	}
	if err = media.PutFile(ctx, "s3-extra.gif", local, "image/gif"); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(store.Config.MediaDir, "s3-extra.gif"), []byte("local-copy"), 0600); err != nil {
		t.Fatal(err)
	}
	cid, err := store.Import(ctx, Character{Provider: "anilist", ExternalID: "storage-portrait", Name: "Storage integration", Works: []Work{{ExternalID: "storage-work", Title: "Storage Work", Kind: "anime"}}})
	if err != nil {
		t.Fatal(err)
	}
	var aid int64
	if err = store.DB.QueryRowContext(ctx, `INSERT INTO gacha_assets(character_id,provider,external_id,source_url,sha256,path,media_type) VALUES($1,'anilist','storage-portrait','','storage-portrait','s3-portrait.png','image/png') RETURNING id`, cid).Scan(&aid); err != nil {
		t.Fatal(err)
	}
	if _, err = store.DB.ExecContext(ctx, `INSERT INTO gacha_assets(character_id,provider,external_id,source_url,sha256,path,media_type,is_extra) VALUES($1,'manual','storage-extra','','storage-extra','s3-extra.gif','image/gif',true)`, cid); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("GET", "/s3-portrait.png", nil)
	rec := httptest.NewRecorder()
	store.ServeHTTP(rec, request)
	if rec.Code != 404 {
		t.Fatal("pending S3 asset publicly served", rec.Code)
	}
	preview := httptest.NewRecorder()
	store.ServeMedia(preview, request, "s3-portrait.png", "image/png", true)
	if preview.Code != 200 || preview.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatal("private preview", preview.Code)
	}
	if err = store.publishPortrait(ctx, cid, aid); err != nil {
		t.Fatal("S3 auto-approval", err)
	}
	rec = httptest.NewRecorder()
	store.ServeHTTP(rec, request)
	if rec.Code != 200 || rec.Body.String() != "card-content" {
		t.Fatal("approved S3 asset unavailable", rec.Code)
	}
	n, err := store.DeleteExtraAssets(ctx, cid)
	if err != nil || n != 1 {
		t.Fatal("extra deletion", n, err)
	}
	if _, err = media.Open(ctx, "s3-extra.gif"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("extra survived deletion", err)
	}
	object, err := media.Open(ctx, "s3-portrait.png")
	if err != nil {
		t.Fatal("portrait deleted with extras", err)
	}
	object.Close()
	if _, err = store.DB.ExecContext(ctx, `UPDATE gacha_assets SET status='rejected' WHERE id=$1`, aid); err != nil {
		t.Fatal(err)
	}
	rec = httptest.NewRecorder()
	store.ServeHTTP(rec, request)
	if rec.Code != 404 {
		t.Fatal("rejected S3 asset publicly served", rec.Code)
	}
}
