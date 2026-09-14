package gacha

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
)

// tryHealAsset checks data.zip for a clean copy of the file if local read fails.
func tryHealAsset(key string) bool {
	if _, err := os.Stat("data.zip"); err != nil {
		return false
	}
	r, err := zip.OpenReader("data.zip")
	if err != nil {
		return false
	}
	defer r.Close()

	candidates := []string{
		"data/gacha/" + key,
		key,
	}

	targetPath := filepath.Join("data/gacha", key)

	for _, f := range r.File {
		for _, c := range candidates {
			if f.Name == c {
				rc, err := f.Open()
				if err != nil {
					return false
				}
				defer rc.Close()

				_ = os.MkdirAll(filepath.Dir(targetPath), 0755)
				_ = os.Remove(targetPath)
				out, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
				if err != nil {
					return false
				}
				_, copyErr := io.Copy(out, rc)
				closeErr := out.Close()
				return copyErr == nil && closeErr == nil
			}
		}
	}
	return false
}

// CopyMediaToS3 walks bounded database pages and retains every local source.
// Existing remote objects are verified against their local originals. Assets
// uploaded directly to S3 are checked for existence. No catalog rows change.
func (s *Store) CopyMediaToS3(ctx context.Context, apply bool, workers int, out io.Writer) error {
	if s.Config.Storage.Backend != "s3" {
		return fmt.Errorf("set GACHA_STORAGE_BACKEND=s3 before copying media")
	}
	storage, err := s.MediaStorage()
	if err != nil {
		return err
	}
	if workers <= 0 {
		workers = 64
	}
	var after int64
	total := 0
	failedCount := 0
	for {
		rows, err := s.DB.QueryContext(ctx, `SELECT id,path,media_type FROM gacha_assets WHERE id>$1 AND path IS NOT NULL AND path<>'' ORDER BY id LIMIT 1000`, after)
		if err != nil {
			return err
		}
		type entry struct {
			id        int64
			key, mime string
		}
		var page []entry
		for rows.Next() {
			var item entry
			if err = rows.Scan(&item.id, &item.key, &item.mime); err != nil {
				rows.Close()
				return err
			}
			page = append(page, item)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		if len(page) == 0 {
			break
		}

		if apply {
			jobCh := make(chan entry, len(page))
			for _, item := range page {
				jobCh <- item
			}
			close(jobCh)

			var wg sync.WaitGroup
			var errMu sync.Mutex

			wCount := workers
			if wCount > len(page) {
				wCount = len(page)
			}

			for w := 0; w < wCount; w++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for item := range jobCh {
						if ctx.Err() != nil {
							return
						}

						if copyErr := storage.CopyLocal(ctx, item.key, item.mime); copyErr != nil {
							if ctx.Err() != nil {
								return
							}
							// Attempt auto-healing from data.zip if available
							if tryHealAsset(item.key) {
								if retryErr := storage.CopyLocal(ctx, item.key, item.mime); retryErr == nil {
									continue
								}
							}
							errMu.Lock()
							failedCount++
							fmt.Fprintf(out, "WARNING: asset %d (%s) failed: %v\n", item.id, item.key, copyErr)
							errMu.Unlock()
						}
					}
				}()
			}
			wg.Wait()
			if ctx.Err() != nil {
				return ctx.Err()
			}
		}

		total += len(page)
		after = page[len(page)-1].id

		if _, err = fmt.Fprintf(out, "Media entries %s: %d (through asset %d)\n", map[bool]string{false: "planned", true: "ready"}[apply], total, after); err != nil {
			return err
		}
	}
	if failedCount > 0 {
		fmt.Fprintf(out, "Migration completed with %d warning(s)\n", failedCount)
	}
	return nil
}
