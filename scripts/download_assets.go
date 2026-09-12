package main

import (
	"bytes"
	"context"
	"database/sql"
	"flag"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

type DownloadTask struct {
	AssetID     int64
	CharacterID int64
	SourceURL   string
	Retries     int
}

type CompletedTask struct {
	AssetID int64
	RelPath string
	Width   int
	Height  int
}

type RateLimiter struct {
	mu           sync.RWMutex
	pausedUntil  time.Time
	backoffSec   int
	ratelimitHit int64
}

func (rl *RateLimiter) WaitIfPaused(ctx context.Context) {
	rl.mu.RLock()
	until := rl.pausedUntil
	rl.mu.RUnlock()

	if time.Now().Before(until) {
		waitDur := time.Until(until)
		select {
		case <-time.After(waitDur):
		case <-ctx.Done():
		}
	}
}

func (rl *RateLimiter) TriggerRateLimit() time.Duration {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	atomic.AddInt64(&rl.ratelimitHit, 1)
	if rl.backoffSec < 5 {
		rl.backoffSec = 5
	} else if rl.backoffSec < 60 {
		rl.backoffSec *= 2
	}
	wait := time.Duration(rl.backoffSec) * time.Second
	rl.pausedUntil = time.Now().Add(wait)
	return wait
}

func (rl *RateLimiter) OnSuccess() {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	// Gradually recover backoff
	if rl.backoffSec > 5 {
		rl.backoffSec = 5
	}
}

func main() {
	concurrencyFlag := flag.Int("concurrency", 16, "Number of concurrent download workers")
	limitFlag := flag.Int("limit", 0, "Limit number of images to download (0 = all pending)")
	mediaDirFlag := flag.String("media-dir", "data/gacha", "Base directory where images are saved")
	dbFlag := flag.String("db", "", "PostgreSQL connection string (defaults to localhost:5435/pousadinha)")
	flag.Parse()

	connStr := *dbFlag
	if connStr == "" {
		connStr = os.Getenv("LOCAL_DATABASE_URL")
	}
	if connStr == "" {
		connStr = os.Getenv("DATABASE_URL")
	}
	if connStr == "" || strings.Contains(connStr, "postgres:5432") {
		connStr = "postgres://postgres:postgres@localhost:5435/pousadinha?sslmode=disable"
	}

	db, err := sql.Open("pgx", connStr)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	log.Printf("Connected to PostgreSQL: %s", maskConnStr(connStr))

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// Ensure media directory exists
	baseDir := *mediaDirFlag
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		log.Fatalf("Failed to create media-dir: %v", err)
	}

	// Query pending assets ordered by character popularity
	query := `
		SELECT a.id, a.character_id, a.source_url
		FROM gacha_assets a
		JOIN gacha_characters c ON c.id = a.character_id
		WHERE (a.path IS NULL OR a.path = '') AND a.source_url LIKE 'http%'
		ORDER BY c.favourites DESC
	`
	if *limitFlag > 0 {
		query += fmt.Sprintf(" LIMIT %d", *limitFlag)
	}

	log.Println("Querying pending assets from database...")
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		log.Fatalf("Failed to query assets: %v", err)
	}
	defer rows.Close()

	var tasks []DownloadTask
	for rows.Next() {
		var t DownloadTask
		if err := rows.Scan(&t.AssetID, &t.CharacterID, &t.SourceURL); err == nil {
			tasks = append(tasks, t)
		}
	}
	rows.Close()

	totalTasks := len(tasks)
	if totalTasks == 0 {
		log.Println("All character assets already have local paths! Nothing to download.")
		return
	}
	log.Printf("Found %d assets pending download.", totalTasks)

	// Channels
	taskChan := make(chan DownloadTask, 500)
	completedChan := make(chan CompletedTask, 500)

	var (
		downloadedCount int64
		skippedCount    int64
		failedCount     int64
		totalBytes      int64
		rateLimiter     RateLimiter
	)

	// Optimized HTTP Client with Connection Pooling
	client := &http.Client{
		Transport: &http.Transport{
			MaxIdleConns:        150,
			MaxIdleConnsPerHost: 50,
			IdleConnTimeout:     90 * time.Second,
			DialContext: (&net.Dialer{
				Timeout:   8 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			TLSHandshakeTimeout: 8 * time.Second,
		},
		Timeout: 15 * time.Second,
	}

	// 1. Start Database Batch Writer
	var writerWg sync.WaitGroup
	writerWg.Add(1)
	go func() {
		defer writerWg.Done()
		batch := make([]CompletedTask, 0, 100)
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()

		flush := func() {
			if len(batch) == 0 {
				return
			}
			tx, err := db.Begin()
			if err != nil {
				log.Printf("[DB WRITER] Begin error: %v", err)
				return
			}
			stmt, err := tx.Prepare("UPDATE gacha_assets SET path = $1, width = $2, height = $3 WHERE id = $4")
			if err != nil {
				tx.Rollback()
				log.Printf("[DB WRITER] Prepare error: %v", err)
				return
			}
			for _, item := range batch {
				_, _ = stmt.Exec(item.RelPath, item.Width, item.Height, item.AssetID)
			}
			stmt.Close()
			if err := tx.Commit(); err != nil {
				log.Printf("[DB WRITER] Commit error: %v", err)
			}
			batch = batch[:0]
		}

		for {
			select {
			case item, ok := <-completedChan:
				if !ok {
					flush()
					return
				}
				batch = append(batch, item)
				if len(batch) >= 100 {
					flush()
				}
			case <-ticker.C:
				flush()
			}
		}
	}()

	// 2. Start Worker Pool
	var workersWg sync.WaitGroup
	numWorkers := *concurrencyFlag
	for w := 0; w < numWorkers; w++ {
		workersWg.Add(1)
		go func() {
			defer workersWg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case task, ok := <-taskChan:
					if !ok {
						return
					}

					rateLimiter.WaitIfPaused(ctx)
					if ctx.Err() != nil {
						return
					}

					shard := task.CharacterID / 1000
					shardDir := filepath.Join(baseDir, "mal", fmt.Sprintf("%d", shard))
					_ = os.MkdirAll(shardDir, 0755)

					ext := ".jpg"
					if strings.HasSuffix(strings.ToLower(task.SourceURL), ".webp") {
						ext = ".webp"
					}
					fileName := fmt.Sprintf("%d%s", task.CharacterID, ext)
					fullPath := filepath.Join(shardDir, fileName)
					relPath := filepath.Join("mal", fmt.Sprintf("%d", shard), fileName)

					// Check if file already exists on disk
					if fi, err := os.Stat(fullPath); err == nil && fi.Size() > 100 {
						w, h := 225, 350
						if f, err := os.Open(fullPath); err == nil {
							if cfg, _, err := image.DecodeConfig(f); err == nil && cfg.Width > 0 && cfg.Height > 0 {
								w, h = cfg.Width, cfg.Height
							}
							f.Close()
						}
						atomic.AddInt64(&skippedCount, 1)
						completedChan <- CompletedTask{
							AssetID: task.AssetID,
							RelPath: relPath,
							Width:   w,
							Height:  h,
						}
						continue
					}

					// Fetch from remote CDN
					req, err := http.NewRequestWithContext(ctx, "GET", task.SourceURL, nil)
					if err != nil {
						atomic.AddInt64(&failedCount, 1)
						continue
					}
					req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/128.0.0.0 Safari/537.36")
					req.Header.Set("Accept", "image/avif,image/webp,image/apng,image/svg+xml,image/*,*/*;q=0.8")
					req.Header.Set("Referer", "https://myanimelist.net/")

					resp, err := client.Do(req)
					if err != nil {
						if task.Retries < 3 && ctx.Err() == nil {
							task.Retries++
							time.Sleep(200 * time.Millisecond)
							select {
							case taskChan <- task:
							default:
							}
						} else {
							atomic.AddInt64(&failedCount, 1)
						}
						continue
					}

					// Rate limit or server error check
					if resp.StatusCode == 429 || resp.StatusCode == 403 || resp.StatusCode == 503 {
						resp.Body.Close()
						wait := rateLimiter.TriggerRateLimit()
						log.Printf("[RATE LIMIT] HTTP %d for char %d (%s). Pausing all workers for %v...", resp.StatusCode, task.CharacterID, task.SourceURL, wait)
						if task.Retries < 5 {
							task.Retries++
							select {
							case taskChan <- task:
							default:
							}
						}
						continue
					}

					if resp.StatusCode != 200 {
						resp.Body.Close()
						atomic.AddInt64(&failedCount, 1)
						continue
					}

					rateLimiter.OnSuccess()

					bodyBytes, err := io.ReadAll(resp.Body)
					resp.Body.Close()
					if err != nil || len(bodyBytes) < 50 {
						atomic.AddInt64(&failedCount, 1)
						continue
					}

					// Atomic file write via temp file
					tmpPath := fullPath + ".tmp"
					if err := os.WriteFile(tmpPath, bodyBytes, 0644); err != nil {
						atomic.AddInt64(&failedCount, 1)
						continue
					}
					if err := os.Rename(tmpPath, fullPath); err != nil {
						_ = os.Remove(tmpPath)
						atomic.AddInt64(&failedCount, 1)
						continue
					}

					w, h := 225, 350
					if cfg, _, err := image.DecodeConfig(bytes.NewReader(bodyBytes)); err == nil && cfg.Width > 0 && cfg.Height > 0 {
						w, h = cfg.Width, cfg.Height
					}

					atomic.AddInt64(&downloadedCount, 1)
					atomic.AddInt64(&totalBytes, int64(len(bodyBytes)))

					completedChan <- CompletedTask{
						AssetID: task.AssetID,
						RelPath: relPath,
						Width:   w,
						Height:  h,
					}
				}
			}
		}()
	}

	// 3. Progress Reporter
	var reporterWg sync.WaitGroup
	reporterWg.Add(1)
	startTime := time.Now()
	go func() {
		defer reporterWg.Done()
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()

		var lastCount int64
		lastTime := startTime

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				dl := atomic.LoadInt64(&downloadedCount)
				sk := atomic.LoadInt64(&skippedCount)
				fl := atomic.LoadInt64(&failedCount)
				bytes := atomic.LoadInt64(&totalBytes)
				now := time.Now()

				totalProcessed := dl + sk + fl
				diffDl := dl - lastCount
				intervalSec := now.Sub(lastTime).Seconds()
				speed := float64(diffDl) / intervalSec
				if speed < 0 {
					speed = 0
				}
				lastCount = dl
				lastTime = now

				percent := float64(totalProcessed) / float64(totalTasks) * 100
				var etaStr string
				if speed > 0.1 {
					rem := float64(int64(totalTasks) - totalProcessed)
					etaSec := rem / speed
					etaStr = (time.Duration(etaSec) * time.Second).Round(time.Second).String()
				} else {
					etaStr = "calculating..."
				}

				mb := float64(bytes) / (1024 * 1024)
				rlHits := atomic.LoadInt64(&rateLimiter.ratelimitHit)
				log.Printf("[PROGRESS] %d/%d (%.1f%%) | +%d new (%.1f img/s) | %d cached | %.1f MB | RateLimits: %d | ETA: %s",
					totalProcessed, totalTasks, percent, dl, speed, sk, mb, rlHits, etaStr)

				if totalProcessed >= int64(totalTasks) {
					return
				}
			}
		}
	}()

	// 4. Feed Tasks
	go func() {
		for _, task := range tasks {
			select {
			case <-ctx.Done():
				break
			case taskChan <- task:
			}
		}
		close(taskChan)
	}()

	// Wait for workers to finish
	workersWg.Wait()
	close(completedChan)
	writerWg.Wait()
	cancel()
	reporterWg.Wait()

	elapsed := time.Since(startTime).Round(time.Second)
	finalDl := atomic.LoadInt64(&downloadedCount)
	finalSk := atomic.LoadInt64(&skippedCount)
	finalFl := atomic.LoadInt64(&failedCount)
	finalMB := float64(atomic.LoadInt64(&totalBytes)) / (1024 * 1024)

	log.Printf("=== DOWNLOAD SESSION FINISHED ===")
	log.Printf("Total Processed: %d / %d", finalDl+finalSk+finalFl, totalTasks)
	log.Printf("Downloaded New:  %d (%.2f MB)", finalDl, finalMB)
	log.Printf("Already Cached:  %d", finalSk)
	log.Printf("Failed/Skipped:  %d", finalFl)
	log.Printf("Total Elapsed:   %v", elapsed)
}

func maskConnStr(conn string) string {
	if i := strings.Index(conn, "://"); i != -1 {
		start := i + 3
		if at := strings.Index(conn[start:], "@"); at != -1 {
			userPass := conn[start : start+at]
			if colon := strings.Index(userPass, ":"); colon != -1 {
				user := userPass[:colon]
				return conn[:start] + user + ":****@" + conn[start+at+1:]
			}
		}
	}
	return conn
}
