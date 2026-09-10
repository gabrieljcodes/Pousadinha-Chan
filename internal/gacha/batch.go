package gacha

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"io"
	"os"
	"time"
)

type BatchProvider interface {
	CatalogProvider
	CharacterIDs(context.Context, int) ([]int64, bool, error)
}
type BatchOptions struct {
	Name                     string
	Target                   int
	AutoApprove, RetryFailed bool
}
type BatchStats struct{ Imported, Pending, Skipped, Failed int }

func (s *Store) BatchStats(ctx context.Context, name string) (BatchStats, error) {
	var v BatchStats
	e := s.DB.QueryRowContext(ctx, `SELECT count(*) FILTER(WHERE status='imported'),count(*) FILTER(WHERE status='pending'),count(*) FILTER(WHERE status='skipped'),count(*) FILTER(WHERE status='failed') FROM gacha_import_items WHERE job=$1`, name).Scan(&v.Imported, &v.Pending, &v.Skipped, &v.Failed)
	return v, e
}
func (s *Store) publishPortrait(ctx context.Context, cid, aid int64) error {
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	// Never overturn a rejection or approve an unrelated booru asset, including hash deduplication.
	var path string
	e = tx.QueryRowContext(ctx, `SELECT a.path FROM gacha_assets a JOIN gacha_character_sources cs ON cs.character_id=a.character_id AND cs.provider='anilist' AND cs.external_id=a.external_id WHERE a.id=$1 AND a.character_id=$2 AND a.provider='anilist' AND a.status IN ('pending','approved') FOR UPDATE OF a`, aid, cid).Scan(&path)
	if e != nil {
		return fmt.Errorf("portrait is missing, rejected, or not from AniList: %w", e)
	}
	root, e := os.OpenRoot(s.Config.MediaDir)
	if e != nil {
		return e
	}
	defer root.Close()
	f, e := root.Open(path)
	if e != nil {
		return e
	}
	st, e := f.Stat()
	f.Close()
	if e != nil {
		return e
	}
	if !st.Mode().IsRegular() || st.Size() == 0 {
		return fmt.Errorf("portrait file is empty or invalid")
	}
	_, e = tx.ExecContext(ctx, `UPDATE gacha_assets SET status='approved',reviewed_by=CASE WHEN status='pending' THEN 'auto:anilist-portrait:v1' ELSE reviewed_by END,reviewed_at=CASE WHEN status='pending' THEN now() ELSE reviewed_at END WHERE id=$1`, aid)
	if e != nil {
		return e
	}
	_, e = tx.ExecContext(ctx, `UPDATE gacha_characters SET enabled=true WHERE id=$1 AND NOT auto_publish_blocked`, cid)
	if e != nil {
		return e
	}
	return tx.Commit()
}

// ImportOne is restart-safe: metadata and assets are upserted before publication.
func (s *Store) ImportOne(ctx context.Context, p CatalogProvider, id int64, auto bool) (int64, error) {
	c, e := p.Character(ctx, id)
	if e != nil {
		return 0, e
	}
	if len(c.Works) == 0 {
		return 0, errNotAnime
	}
	if c.Portrait == "" {
		return 0, fmt.Errorf("character has no portrait")
	}
	cid, e := s.Import(ctx, c)
	if e != nil {
		return 0, e
	}
	aid, e := s.AddPortrait(ctx, cid, c)
	if e != nil {
		return cid, e
	}
	if auto {
		if c.Provider != "anilist" {
			return cid, fmt.Errorf("automatic approval is restricted to AniList portraits")
		}
		if e = s.publishPortrait(ctx, cid, aid); e != nil {
			return cid, e
		}
	}
	return cid, nil
}

var errNotAnime = errors.New("no eligible anime works")

func (s *Store) RunBatch(ctx context.Context, p BatchProvider, opt BatchOptions, out io.Writer) error {
	return s.runBatch(ctx, p, opt, out, func(ctx context.Context, id int64) (int64, error) { return s.ImportOne(ctx, p, id, opt.AutoApprove) })
}
func (s *Store) runBatch(ctx context.Context, p BatchProvider, opt BatchOptions, out io.Writer, importOne func(context.Context, int64) (int64, error)) error {
	if opt.Name == "" || len(opt.Name) > 100 || opt.Target < 1 || opt.Target > 100000 {
		return fmt.Errorf("invalid job name or target (1..100000)")
	}
	if out == nil {
		out = io.Discard
	}
	owner := uuid.NewString()
	renew := func() error {
		var got string
		e := s.DB.QueryRowContext(ctx, `INSERT INTO gacha_import_leases(provider,owner,expires_at) VALUES('anilist',$1,clock_timestamp()+interval '5 minutes') ON CONFLICT(provider) DO UPDATE SET owner=excluded.owner,expires_at=excluded.expires_at WHERE gacha_import_leases.owner=$1 OR gacha_import_leases.expires_at<clock_timestamp() RETURNING owner`, owner).Scan(&got)
		if e == sql.ErrNoRows {
			return fmt.Errorf("another AniList batch importer is active; wait for its lease to expire")
		}
		return e
	}
	if e := renew(); e != nil {
		return e
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = s.DB.ExecContext(cleanup, `DELETE FROM gacha_import_leases WHERE provider='anilist' AND owner=$1`, owner)
	}()
	_, e := s.DB.ExecContext(ctx, `INSERT INTO gacha_import_jobs(name,target,auto_approve) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, opt.Name, opt.Target, opt.AutoApprove)
	if e != nil {
		return e
	}
	var target int
	var auto bool
	e = s.DB.QueryRowContext(ctx, `SELECT target,auto_approve FROM gacha_import_jobs WHERE name=$1`, opt.Name).Scan(&target, &auto)
	if e != nil {
		return e
	}
	if target != opt.Target || auto != opt.AutoApprove {
		return fmt.Errorf("existing job has different target or approval policy; resume with original options")
	}
	if opt.RetryFailed {
		_, e = s.DB.ExecContext(ctx, `UPDATE gacha_import_items SET status='pending' WHERE job=$1 AND status='failed'`, opt.Name)
		if e != nil {
			return e
		}
	}
	consecutiveErrors := 0
	for {
		if e = ctx.Err(); e != nil {
			return e
		}
		if e = renew(); e != nil {
			return e
		}
		stats, e := s.BatchStats(ctx, opt.Name)
		if e != nil {
			return e
		}
		fmt.Fprintf(out, "job=%s imported=%d/%d pending=%d skipped=%d failed=%d\n", opt.Name, stats.Imported, target, stats.Pending, stats.Skipped, stats.Failed)
		if stats.Imported >= target {
			return nil
		}
		var id int64
		e = s.DB.QueryRowContext(ctx, `SELECT external_id FROM gacha_import_items WHERE job=$1 AND status='pending' ORDER BY external_id LIMIT 1`, opt.Name).Scan(&id)
		if e == sql.ErrNoRows {
			var page int
			var exhausted bool
			e = s.DB.QueryRowContext(ctx, `SELECT next_page,exhausted FROM gacha_import_jobs WHERE name=$1`, opt.Name).Scan(&page, &exhausted)
			if e != nil {
				return e
			}
			if exhausted {
				return fmt.Errorf("source exhausted: imported %d of %d (failed=%d)", stats.Imported, target, stats.Failed)
			}
			step, cancel := context.WithTimeout(ctx, 3*time.Minute)
			ids, more, e := p.CharacterIDs(step, page)
			cancel()
			if e != nil {
				return e
			}
			if e = renew(); e != nil {
				return e
			}
			tx, e := s.DB.BeginTx(ctx, nil)
			if e != nil {
				return e
			}
			for _, id := range ids {
				if _, e = tx.ExecContext(ctx, `INSERT INTO gacha_import_items(job,external_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, opt.Name, id); e != nil {
					break
				}
			}
			if e == nil {
				_, e = tx.ExecContext(ctx, `UPDATE gacha_import_jobs SET next_page=$2,exhausted=$3 WHERE name=$1`, opt.Name, page+1, !more)
			}
			if e != nil {
				tx.Rollback()
				return e
			}
			if e = tx.Commit(); e != nil {
				return e
			}
			continue
		}
		if e != nil {
			return e
		}
		step, cancel := context.WithTimeout(ctx, 3*time.Minute)
		cid, importErr := importOne(step, id)
		cancel()
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if e = renew(); e != nil {
			return e
		}
		status, message := "imported", ""
		if errors.Is(importErr, errNotAnime) {
			status = "skipped"
			message = importErr.Error()
			consecutiveErrors = 0
		} else if importErr != nil {
			status = "failed"
			message = importErr.Error()
			consecutiveErrors++
		} else {
			consecutiveErrors = 0
		}
		// Renewable lease exceeds the per-item deadline; a killed worker resumes this pending item.
		_, e = s.DB.ExecContext(ctx, `UPDATE gacha_import_items SET status=$3,attempts=attempts+1,character_id=NULLIF($4,0),last_error=$5,updated_at=now() WHERE job=$1 AND external_id=$2`, opt.Name, id, status, cid, clip(message, 500))
		if e != nil {
			return e
		}
		if importErr != nil {
			fmt.Fprintf(out, "anilist_id=%d status=%s error=%s\n", id, status, message)
		}
		var apiError *AniListHTTPError
		if errors.As(importErr, &apiError) && (apiError.Status == 401 || apiError.Status == 403) {
			return importErr
		}
		if consecutiveErrors >= 5 {
			return fmt.Errorf("paused after 5 consecutive failures; resolve the cause and resume with --retry-failed")
		}
	}
}
