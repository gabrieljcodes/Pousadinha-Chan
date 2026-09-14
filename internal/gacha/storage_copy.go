package gacha

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
)

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
		workers = 16
	}
	if workers > 64 {
		return fmt.Errorf("media copy supports at most 64 workers")
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
							// Preserve originals on copy failure; repair requires an explicit operation.

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

		if _, err = fmt.Fprintf(out, "Media entries %s: %d (through asset %d)\n", map[bool]string{false: "planned", true: "processed"}[apply], total, after); err != nil {
			return err
		}
	}
	if failedCount > 0 {
		return fmt.Errorf("media copy failed for %d entries; original files were retained", failedCount)
	}
	return nil
}

// CopyDiskMediaToS3 walks the configured media directory, uploads missing image
// objects and verifies existing remote copies against local originals.
func (s *Store) CopyDiskMediaToS3(ctx context.Context, apply bool, skipMal bool, workers int, out io.Writer) error {
	if s.Config.Storage.Backend != "s3" {
		return fmt.Errorf("set GACHA_STORAGE_BACKEND=s3 before copying media")
	}
	storage, err := s.MediaStorage()
	if err != nil {
		return err
	}
	if workers <= 0 {
		workers = 16
	}
	if workers > 64 {
		return fmt.Errorf("media copy supports at most 64 workers")
	}

	mediaDir := s.Config.MediaDir
	if mediaDir == "" {
		mediaDir = "data/gacha"
	}

	type diskEntry struct {
		key, mime string
	}

	var entries []diskEntry
	err = filepath.WalkDir(mediaDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(mediaDir, path)
		if err != nil {
			return err
		}
		key := filepath.ToSlash(rel)
		if skipMal && strings.HasPrefix(key, "mal/") {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(key))
		var mimeType string
		switch ext {
		case ".png":
			mimeType = "image/png"
		case ".jpg", ".jpeg":
			mimeType = "image/jpeg"
		case ".gif":
			mimeType = "image/gif"
		case ".webp":
			mimeType = "image/webp"
		default:
			return nil // Skip non-image files like .log, .pid
		}
		entries = append(entries, diskEntry{key: key, mime: mimeType})
		return nil
	})
	if err != nil {
		return err
	}

	fmt.Fprintf(out, "Found %d media files on disk in %s (skipMal=%v)\n", len(entries), mediaDir, skipMal)

	if !apply {
		fmt.Fprintf(out, "Dry run completed. Pass --apply to upload.\n")
		return nil
	}

	jobCh := make(chan diskEntry, len(entries))
	for _, item := range entries {
		jobCh <- item
	}
	close(jobCh)

	var wg sync.WaitGroup
	var errMu sync.Mutex
	var processed int64
	var failedCount int

	for w := 0; w < workers; w++ {
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

					errMu.Lock()
					failedCount++
					fmt.Fprintf(out, "WARNING: disk asset (%s) failed: %v\n", item.key, copyErr)
					errMu.Unlock()
				}

				curr := atomic.AddInt64(&processed, 1)
				if curr%500 == 0 || curr == int64(len(entries)) {
					errMu.Lock()
					fmt.Fprintf(out, "Disk media entries processed: %d / %d\n", curr, len(entries))
					errMu.Unlock()
				}
			}
		}()
	}
	wg.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}

	if failedCount > 0 {
		return fmt.Errorf("disk media copy failed for %d entries; original files were retained", failedCount)
	} else {
		fmt.Fprintf(out, "All %d disk media files verified/uploaded successfully!\n", len(entries))
	}
	return nil
}
