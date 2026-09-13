package gacha

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"math/big"
	"sync"
	"sync/atomic"
	"time"

	"github.com/valkey-io/valkey-go"
)

// Snapshots are immutable after publication. Only IDs are cached; ownership,
// prices, images and eligibility are read from PostgreSQL for the selected card.
type poolSnapshot struct {
	Namespace string             `json:"namespace"`
	Revision  int64              `json:"revision"`
	IDs       map[string][]int64 `json:"ids"`
}
type poolRuntime struct {
	snapshot    atomic.Pointer[poolSnapshot]
	checked     atomic.Int64
	refresh     sync.Mutex
	builds      atomic.Uint64
	sharedHits  atomic.Uint64
	cacheErrors atomic.Uint64
	client      valkey.Client // configured before serving requests
}

// ConnectPoolCache is optional and must be called before serving requests.
// A connection failure leaves the process using PostgreSQL and local snapshots.
func (s *Store) ConnectPoolCache(address string) error {
	if address == "" {
		return nil
	}
	opt, err := valkey.ParseURL(address)
	if err != nil {
		return errors.New("invalid VALKEY_URL")
	}
	opt.DisableCache = true
	opt.DisableRetry = true
	opt.Dialer.Timeout = time.Second
	opt.ConnWriteTimeout = time.Second
	client, err := valkey.NewClient(opt)
	if err != nil {
		return errors.New("Valkey unavailable; using local catalog pools")
	}
	s.runtime.client = client
	return nil
}
func (s *Store) ClosePoolCache() {
	if s.runtime.client != nil {
		s.runtime.client.Close()
	}
}

func (s *Store) catalogPools(ctx context.Context, force bool) (*poolSnapshot, error) {
	r := &s.runtime
	if snapshot := r.snapshot.Load(); !force && snapshot != nil && time.Now().UnixNano() < r.checked.Load() {
		return snapshot, nil
	}
	r.refresh.Lock()
	defer r.refresh.Unlock()
	if snapshot := r.snapshot.Load(); !force && snapshot != nil && time.Now().UnixNano() < r.checked.Load() {
		return snapshot, nil
	}
	var namespace string
	var revision int64
	if err := s.DB.QueryRowContext(ctx, `SELECT namespace::text,revision FROM gacha_catalog_revision WHERE singleton`).Scan(&namespace, &revision); err != nil {
		return nil, err
	}
	if snapshot := r.snapshot.Load(); snapshot != nil && snapshot.Namespace == namespace && snapshot.Revision == revision {
		r.checked.Store(time.Now().Add(time.Second).UnixNano())
		return snapshot, nil
	}
	key := "gacha:pools:v1:" + namespace
	var snapshot *poolSnapshot
	if r.client != nil {
		cacheCtx, cancel := context.WithTimeout(ctx, 250*time.Millisecond)
		data, err := r.client.Do(cacheCtx, r.client.B().Get().Key(key).Build()).ToString()
		cancel()
		if err != nil && !valkey.IsValkeyNil(err) {
			r.cacheErrors.Add(1)
		}
		var cached poolSnapshot
		if err == nil && json.Unmarshal([]byte(data), &cached) == nil && cached.Namespace == namespace && cached.Revision == revision && validPools(&cached) {
			snapshot = &cached
			r.sharedHits.Add(1)
		}
	}
	if snapshot == nil {
		var err error
		snapshot, err = s.readPools(ctx)
		if err != nil {
			return nil, err
		}
		r.builds.Add(1)
		if r.client != nil {
			if data, err := json.Marshal(snapshot); err == nil {
				cacheCtx, cancel := context.WithTimeout(ctx, 250*time.Millisecond)
				// A late writer may replace a newer snapshot; readers always check the
				// authoritative revision, so this only causes a cache miss, never bad rolls.
				if err := r.client.Do(cacheCtx, r.client.B().Set().Key(key).Value(string(data)).ExSeconds(300).Build()).Error(); err != nil {
					r.cacheErrors.Add(1)
				}
				cancel()
			}
		}
	}
	r.snapshot.Store(snapshot)
	r.checked.Store(time.Now().Add(time.Second).UnixNano())
	return snapshot, nil
}

func validPools(s *poolSnapshot) bool {
	for _, pool := range Pools {
		ids, exists := s.IDs[pool.Code]
		if !exists {
			return false
		}
		for i, id := range ids {
			if id <= 0 || (i > 0 && id <= ids[i-1]) {
				return false
			}
		}
	}
	return true
}

func (s *Store) readPools(ctx context.Context) (*poolSnapshot, error) {
	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	snapshot := &poolSnapshot{IDs: make(map[string][]int64, len(Pools))}
	for _, pool := range Pools {
		snapshot.IDs[pool.Code] = nil
	}
	if err = tx.QueryRowContext(ctx, `SELECT namespace::text,revision FROM gacha_catalog_revision WHERE singleton`).Scan(&snapshot.Namespace, &snapshot.Revision); err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT c.id,lower(trim(c.gender)),
 EXISTS(SELECT 1 FROM gacha_character_works cw JOIN gacha_works w ON w.id=cw.work_id WHERE cw.character_id=c.id AND w.kind='anime'),
 EXISTS(SELECT 1 FROM gacha_character_works cw JOIN gacha_works w ON w.id=cw.work_id WHERE cw.character_id=c.id AND w.kind='game')
 FROM gacha_characters c WHERE c.enabled AND EXISTS(SELECT 1 FROM gacha_assets a WHERE a.character_id=c.id AND a.status='approved') ORDER BY c.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var gender string
		var anime, game bool
		if err = rows.Scan(&id, &gender, &anime, &game); err != nil {
			return nil, err
		}
		for _, pool := range Pools {
			if (pool.Gender == "" || pool.Gender == gender) && (pool.Kind == "" || pool.Kind == "anime" && anime || pool.Kind == "game" && game) {
				snapshot.IDs[pool.Code] = append(snapshot.IDs[pool.Code], id)
			}
		}
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	return snapshot, tx.Commit()
}

var errStalePool = errors.New("catalog pool changed")

func sampleID(ids []int64) (int64, error) {
	if len(ids) == 0 {
		return 0, ErrEmpty
	}
	n, err := rand.Int(rand.Reader, big.NewInt(int64(len(ids))))
	if err != nil {
		return 0, err
	}
	return ids[n.Int64()], nil
}

// PoolCacheStats exposes cumulative process counters for diagnostics. Failures
// concern the optional cache; they do not indicate failed economic transactions.
type PoolCacheStats struct {
	Builds, SharedHits, CacheErrors uint64
	Characters                      int
}

func (s *Store) PoolCacheStats() PoolCacheStats {
	stats := PoolCacheStats{Builds: s.runtime.builds.Load(), SharedHits: s.runtime.sharedHits.Load(), CacheErrors: s.runtime.cacheErrors.Load()}
	if snapshot := s.runtime.snapshot.Load(); snapshot != nil {
		stats.Characters = len(snapshot.IDs["roll"])
	}
	return stats
}

// WarmRollPools moves the first catalog scan out of the first Discord request.
func (s *Store) WarmRollPools(ctx context.Context) error {
	_, err := s.catalogPools(ctx, true)
	return err
}
