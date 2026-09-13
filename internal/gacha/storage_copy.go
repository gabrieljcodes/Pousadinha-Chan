package gacha

import (
	"context"
	"fmt"
	"io"
	"sync"
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
	var after int64
	total := 0
	for {
		rows, err := s.DB.QueryContext(ctx, `SELECT id,path,media_type FROM gacha_assets WHERE id>$1 AND path IS NOT NULL AND path<>'' ORDER BY id LIMIT 500`, after)
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
			var firstErr error
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
						errMu.Lock()
						hasErr := firstErr != nil
						errMu.Unlock()
						if hasErr {
							return
						}

						if copyErr := storage.CopyLocal(ctx, item.key, item.mime); copyErr != nil {
							errMu.Lock()
							if firstErr == nil {
								firstErr = fmt.Errorf("asset %d (%s): %w", item.id, item.key, copyErr)
							}
							errMu.Unlock()
							return
						}
					}
				}()
			}
			wg.Wait()
			if firstErr != nil {
				return firstErr
			}
		}

		total += len(page)
		after = page[len(page)-1].id

		if _, err = fmt.Fprintf(out, "Media entries %s: %d (through asset %d)\n", map[bool]string{false: "planned", true: "ready"}[apply], total, after); err != nil {
			return err
		}
	}
	return nil
}
