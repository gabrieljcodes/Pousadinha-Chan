package gacha

import (
	"bot/internal/locale"
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"github.com/google/uuid"
	"math/big"
	"strconv"
	"strings"
	"time"
)

var ErrLimit = errors.New(locale.Text("gacha.game.no_rolls_left_in_this_hourly_window"))
var ErrEmpty = errors.New(locale.Text("gacha.game.no_enabled_characters_with_approved_images_match"))
var ErrClaim = errors.New(locale.Text("gacha.game.this_character_is_owned_the_roll_expired"))

type Card struct {
	Value, Keys, Claimed                          int64
	ID                                            int64
	Name, Work, Image, Source, Attribution, Owner string
	OriginalName                                  string
	Favourites                                    int
}
type Roll struct {
	KeyEarned bool
	WishSpawn bool
	ID        string
	Card      Card
	Expires   time.Time
}

const cardSelect = `SELECT c.id,c.name,c.favourites,COALESCE((SELECT w.title FROM gacha_character_works cw JOIN gacha_works w ON w.id=cw.work_id WHERE cw.character_id=c.id ORDER BY CASE cw.role WHEN 'MAIN' THEN 0 ELSE 1 END,w.id LIMIT 1), 'Original'),COALESCE(a.path,''),COALESCE(a.source_url,''),COALESCE(a.attribution,'') FROM gacha_characters c LEFT JOIN LATERAL (SELECT path,source_url,attribution FROM gacha_assets WHERE character_id=c.id AND status='approved' ORDER BY is_primary DESC,id LIMIT 1) a ON true `

type scanner interface{ Scan(...any) error }

func scanCard(row scanner) (Card, error) {
	var c Card
	e := row.Scan(&c.ID, &c.Name, &c.Favourites, &c.Work, &c.Image, &c.Source, &c.Attribution)
	return c, e
}

func ensurePlayer(ctx context.Context, tx *sql.Tx, guild, user string) error {
	if guild == "" || user == "" {
		return userError(locale.Text("gacha.discord.use_this_command_in_a_server"))
	}
	if _, e := tx.ExecContext(ctx, `INSERT INTO users(id,balance) VALUES($1,0) ON CONFLICT DO NOTHING`, user); e != nil {
		return e
	}
	if _, e := tx.ExecContext(ctx, `INSERT INTO guild_members(guild_id,user_id,balance) VALUES($1,$2,0) ON CONFLICT DO NOTHING`, guild, user); e != nil {
		return e
	}
	_, e := tx.ExecContext(ctx, `INSERT INTO gacha_players(guild_id,user_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, guild, user)
	return e
}
func (s *Store) Roll(ctx context.Context, guild, channel, user, request string) (Roll, error) {
	return s.RollPool(ctx, guild, channel, user, request, "roll")
}
func (s *Store) RollPool(ctx context.Context, guild, channel, user, request, code string) (Roll, error) {
	var r Roll
	pool, e := poolFor(code)
	if e != nil {
		return r, e
	}
	// Build/refresh outside the player transaction, before acquiring its lock.
	// Replay handling still runs if the cache cannot load or the pool is empty.
	for attempt := 0; attempt < 2; attempt++ {
		snapshot, cacheErr := s.catalogPools(ctx, attempt > 0)
		r, e = s.rollPoolSnapshot(ctx, guild, channel, user, request, pool, snapshot, cacheErr)
		if !errors.Is(e, errStalePool) {
			return r, e
		}
	}
	return r, ErrEmpty
}

func (s *Store) rollPoolSnapshot(ctx context.Context, guild, channel, user, request string, pool Pool, snapshot *poolSnapshot, cacheErr error) (Roll, error) {
	var r Roll
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return r, e
	}
	defer tx.Rollback()
	if e = ensurePlayer(ctx, tx, guild, user); e != nil {
		return r, e
	}
	var used int
	e = tx.QueryRowContext(ctx, `SELECT rolls_used FROM gacha_players WHERE guild_id=$1 AND user_id=$2 FOR UPDATE`, guild, user).Scan(&used)
	if e != nil {
		return r, e
	}
	var cid int64
	e = tx.QueryRowContext(ctx, `SELECT id,character_id,expires_at,key_awarded AND NOT key_revoked FROM gacha_rolls WHERE request_id=$1 AND guild_id=$2 AND user_id=$3`, request, guild, user).Scan(&r.ID, &cid, &r.Expires, &r.KeyEarned)
	if e == nil {
		r.Card, e = scanCard(tx.QueryRowContext(ctx, cardSelect+` WHERE c.id=$1`, cid))
		if e != nil {
			return r, e
		}
		e = priceCards(ctx, tx, guild, &r.Card)
		return r, e
	}
	if e != sql.ErrNoRows {
		return r, e
	}
	schedule := s.GuildSchedule(ctx, guild)
	rollWin := schedule.RollWindow(time.Now())
	_, e = tx.ExecContext(ctx, `UPDATE gacha_players SET window_start=$3,rolls_used=0 WHERE guild_id=$1 AND user_id=$2 AND window_start<$3`, guild, user, rollWin.CurrentStart)
	if e != nil {
		return r, e
	}
	e = tx.QueryRowContext(ctx, `UPDATE gacha_players SET rolls_used=rolls_used+1 WHERE guild_id=$1 AND user_id=$2 AND rolls_used<$3 RETURNING rolls_used`, guild, user, schedule.RollsPerHour).Scan(&used)
	if e == sql.ErrNoRows {
		return r, ErrLimit
	}
	if e != nil {
		return r, e
	}
	if cacheErr != nil {
		return r, cacheErr
	}
	const eligible = `c.id=$3 AND c.enabled AND EXISTS(SELECT 1 FROM gacha_assets a WHERE a.character_id=c.id AND a.status='approved') AND ($1='' OR lower(trim(c.gender))=$1) AND ($2='' OR EXISTS(SELECT 1 FROM gacha_character_works cw JOIN gacha_works w ON w.id=cw.work_id WHERE cw.character_id=c.id AND w.kind=$2))`
	found := false

	// Wish bonus: if enabled, roller has a bonus percent chance to drop one of their eligible wished characters.
	if s.Config.WishBonusPercent > 0 {
		n, randErr := rand.Int(rand.Reader, big.NewInt(10000))
		if randErr == nil && float64(n.Int64())/100.0 < s.Config.WishBonusPercent {
			const wishEligibleSQL = `
SELECT w.character_id
FROM gacha_wishes w
JOIN gacha_characters c ON c.id = w.character_id
WHERE w.guild_id = $1 AND w.user_id = $2
  AND c.enabled
  AND EXISTS(SELECT 1 FROM gacha_assets a WHERE a.character_id=c.id AND a.status='approved')
  AND ($3 = '' OR lower(trim(c.gender)) = $3)
  AND ($4 = '' OR EXISTS(SELECT 1 FROM gacha_character_works cw JOIN gacha_works gw ON gw.id=cw.work_id WHERE cw.character_id=c.id AND gw.kind=$4))`
			rows, qErr := tx.QueryContext(ctx, wishEligibleSQL, guild, user, pool.Gender, pool.Kind)
			if qErr == nil {
				var wishIDs []int64
				for rows.Next() {
					var wid int64
					if scanErr := rows.Scan(&wid); scanErr == nil {
						wishIDs = append(wishIDs, wid)
					}
				}
				rows.Close()
				if len(wishIDs) > 0 {
					pIdx, idxErr := rand.Int(rand.Reader, big.NewInt(int64(len(wishIDs))))
					if idxErr == nil {
						chosenID := wishIDs[pIdx.Int64()]
						wCard, cErr := scanCard(tx.QueryRowContext(ctx, cardSelect+` WHERE c.id=$1`, chosenID))
						if cErr == nil {
							r.Card = wCard
							r.WishSpawn = true
							found = true
						}
					}
				}
			}
		}
	}

	// Rejection sampling handles a snapshot racing an editorial change. Each
	// attempt validates current eligibility using a primary-key lookup.
	if !found {
		for attempt := 0; attempt < 8; attempt++ {
			id, sampleErr := sampleID(snapshot.IDs[pool.Code])
			if sampleErr == ErrEmpty {
				return r, errStalePool
			}
			if sampleErr != nil {
				return r, sampleErr
			}
			r.Card, e = scanCard(tx.QueryRowContext(ctx, cardSelect+` WHERE `+eligible, pool.Gender, pool.Kind, id))
			if e == sql.ErrNoRows {
				continue
			}
			if e != nil {
				return r, e
			}
			found = true
			break
		}
	}
	if !found {
		return r, errStalePool
	}
	if !r.WishSpawn {
		var isWished bool
		_ = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM gacha_wishes WHERE guild_id=$1 AND user_id=$2 AND character_id=$3)`, guild, user, r.Card.ID).Scan(&isWished)
		if isWished {
			r.WishSpawn = true
		}
	}
	r.ID = uuid.NewString()
	e = tx.QueryRowContext(ctx, `INSERT INTO gacha_rolls(id,guild_id,channel_id,user_id,character_id,expires_at,request_id) VALUES($1,$2,$3,$4,$5,now()+interval '45 seconds',$6) RETURNING expires_at`, r.ID, guild, channel, user, r.Card.ID, request).Scan(&r.Expires)
	if e != nil {
		return r, e
	}
	if e = awardKey(ctx, tx, guild, user, &r); e != nil {
		return r, e
	}
	if e = priceCards(ctx, tx, guild, &r.Card); e != nil {
		return r, e
	}
	return r, tx.Commit()
}
func (s *Store) Claim(ctx context.Context, guild, channel, user, roll string) error {
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if e = ensurePlayer(ctx, tx, guild, user); e != nil {
		return e
	}
	var ready bool
	e = tx.QueryRowContext(ctx, `SELECT claim_after<=now() FROM gacha_players WHERE guild_id=$1 AND user_id=$2 FOR UPDATE`, guild, user).Scan(&ready)
	if e != nil {
		return e
	}
	if !ready {
		return ErrClaim
	}
	var cid int64
	e = tx.QueryRowContext(ctx, `UPDATE gacha_rolls SET claimed_by=$1 WHERE id=$2 AND guild_id=$3 AND channel_id=$4 AND expires_at>clock_timestamp() AND claimed_by IS NULL RETURNING character_id`, user, roll, guild, channel).Scan(&cid)
	if e == sql.ErrNoRows {
		return ErrClaim
	}
	if e != nil {
		return e
	}
	res, e := tx.ExecContext(ctx, `INSERT INTO gacha_collection(guild_id,character_id,user_id) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, guild, cid, user)
	if e != nil {
		return e
	}
	n, e := res.RowsAffected()
	if e != nil {
		return e
	}
	if n == 0 {
		return ErrClaim
	}
	schedule := s.GuildSchedule(ctx, guild)
	claimWin := schedule.ClaimWindow(time.Now())
	_, e = tx.ExecContext(ctx, `UPDATE gacha_players SET claim_after=$3 WHERE guild_id=$1 AND user_id=$2`, guild, user, claimWin.NextReset)
	if e != nil {
		return e
	}
	return tx.Commit()
}

// SearchEntry represents a character search result item with name, work, and favorites.
type SearchEntry struct {
	ID         int64
	Name       string
	Work       string
	Favourites int
	Owner      string
	Rank       int
}

// SearchCharacters searches all enabled characters with approved assets matching query
// in their name, native_name, or aliases, ordered by favourites descending.
func (s *Store) SearchCharacters(ctx context.Context, guild, query string, page, limit int) ([]SearchEntry, int64, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, 0, userError(locale.Text("gacha.game.enter_a_character_name_or_id"))
	}
	if page < 1 || page > 100000 {
		return nil, 0, userError(locale.Text("gacha.discord.invalid_page"))
	}
	if limit <= 0 || limit > 100 {
		limit = 10
	}

	var numericID int64
	if id, err := strconv.ParseInt(query, 10, 64); err == nil && id > 0 {
		numericID = id
	}

	const countSQL = `
SELECT count(*)
FROM gacha_characters c
LEFT JOIN gacha_guild_character_aliases ga ON ga.character_id=c.id AND ga.guild_id=$1
WHERE c.enabled 
  AND EXISTS(SELECT 1 FROM gacha_assets a WHERE a.character_id=c.id AND a.status='approved')
  AND (
    c.name ILIKE '%'||$2||'%' 
    OR c.native_name ILIKE '%'||$2||'%' 
    OR EXISTS(SELECT 1 FROM jsonb_array_elements_text(CASE WHEN jsonb_typeof(c.aliases)='array' THEN c.aliases ELSE '[]'::jsonb END) al WHERE al ILIKE '%'||$2||'%')
    OR ga.alias ILIKE '%'||$2||'%'
    OR ($3 > 0 AND c.id = $3)
  )`

	var total int64
	if err := s.DB.QueryRowContext(ctx, countSQL, guild, query, numericID).Scan(&total); err != nil {
		return nil, 0, err
	}
	if total == 0 {
		return nil, 0, nil
	}

	const searchSQL = `
SELECT c.id, COALESCE(ga.alias, c.name) AS name, c.favourites,
       COALESCE((SELECT w.title FROM gacha_character_works cw JOIN gacha_works w ON w.id=cw.work_id WHERE cw.character_id=c.id ORDER BY CASE cw.role WHEN 'MAIN' THEN 0 ELSE 1 END,w.id LIMIT 1), 'Original') AS work,
       COALESCE(col.user_id, '') AS owner
FROM gacha_characters c
LEFT JOIN gacha_collection col ON col.character_id=c.id AND col.guild_id=$1
LEFT JOIN gacha_guild_character_aliases ga ON ga.character_id=c.id AND ga.guild_id=$1
WHERE c.enabled 
  AND EXISTS(SELECT 1 FROM gacha_assets a WHERE a.character_id=c.id AND a.status='approved')
  AND (
    c.name ILIKE '%'||$2||'%' 
    OR c.native_name ILIKE '%'||$2||'%' 
    OR EXISTS(SELECT 1 FROM jsonb_array_elements_text(CASE WHEN jsonb_typeof(c.aliases)='array' THEN c.aliases ELSE '[]'::jsonb END) al WHERE al ILIKE '%'||$2||'%')
    OR ga.alias ILIKE '%'||$2||'%'
    OR ($3 > 0 AND c.id = $3)
  )
ORDER BY
  c.favourites DESC, c.id ASC
LIMIT $4 OFFSET $5`

	rows, err := s.DB.QueryContext(ctx, searchSQL, guild, query, numericID, limit, (page-1)*limit)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var entries []SearchEntry
	startRank := (page-1)*limit + 1
	for rows.Next() {
		var e SearchEntry
		if err := rows.Scan(&e.ID, &e.Name, &e.Favourites, &e.Work, &e.Owner); err != nil {
			return nil, 0, err
		}
		e.Rank = startRank + len(entries)
		entries = append(entries, e)
	}
	return entries, total, rows.Err()
}

// SearchCharactersByWork searches characters belonging to works matching query, ordered by favourites descending.
// It returns the list of entries, total distinct characters, primary matched work title, and any error.
func (s *Store) SearchCharactersByWork(ctx context.Context, guild, query string, page, limit int) ([]SearchEntry, int64, string, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, 0, "", userError(locale.Text("gacha.game.enter_a_series_name_or_id"))
	}
	if page < 1 || page > 100000 {
		return nil, 0, "", userError(locale.Text("gacha.discord.invalid_page"))
	}
	if limit <= 0 || limit > 100 {
		limit = 10
	}

	var numericID int64
	if id, err := strconv.ParseInt(query, 10, 64); err == nil && id > 0 {
		numericID = id
	}

	const findWorkSQL = `
SELECT id, title,
       CASE 
         WHEN lower(title) = lower($2) THEN 0
         WHEN title ILIKE $2||'%' THEN 1
         ELSE 2
       END AS match_prio
FROM gacha_works
WHERE ($1 > 0 AND id = $1)
   OR title ILIKE '%'||$2||'%' 
   OR native_title ILIKE '%'||$2||'%'
ORDER BY match_prio ASC, id ASC
LIMIT 1`

	var workID int64
	var primaryTitle string
	var prio int
	err := s.DB.QueryRowContext(ctx, findWorkSQL, numericID, query).Scan(&workID, &primaryTitle, &prio)
	if err == sql.ErrNoRows {
		return nil, 0, "", nil
	}
	if err != nil {
		return nil, 0, "", err
	}

	const countSQL = `
WITH matched_works AS (
  SELECT id
  FROM gacha_works
  WHERE ($1 > 0 AND id = $1)
     OR title ILIKE '%'||$2||'%'
     OR native_title ILIKE '%'||$2||'%'
)
SELECT count(DISTINCT c.id)
FROM gacha_characters c
JOIN gacha_character_works cw ON cw.character_id = c.id
JOIN matched_works mw ON mw.id = cw.work_id
WHERE c.enabled
  AND EXISTS(SELECT 1 FROM gacha_assets a WHERE a.character_id = c.id AND a.status = 'approved')`

	var total int64
	if err := s.DB.QueryRowContext(ctx, countSQL, numericID, query).Scan(&total); err != nil {
		return nil, 0, primaryTitle, err
	}
	if total == 0 {
		return nil, 0, primaryTitle, nil
	}

	const searchSQL = `
WITH matched_works AS (
  SELECT id, title,
         CASE 
           WHEN lower(title) = lower($2) THEN 0
           WHEN title ILIKE $2||'%' THEN 1
           ELSE 2
         END AS match_prio
  FROM gacha_works
  WHERE ($1 > 0 AND id = $1)
     OR title ILIKE '%'||$2||'%'
     OR native_title ILIKE '%'||$2||'%'
),
chars AS (
  SELECT DISTINCT c.id, c.name, c.favourites
  FROM gacha_characters c
  JOIN gacha_character_works cw ON cw.character_id = c.id
  JOIN matched_works mw ON mw.id = cw.work_id
  WHERE c.enabled 
    AND EXISTS(SELECT 1 FROM gacha_assets a WHERE a.character_id = c.id AND a.status = 'approved')
)
SELECT c.id, 
       COALESCE(ga.alias, c.name) AS name, 
       c.favourites,
       COALESCE((SELECT mw.title 
                 FROM gacha_character_works cw 
                 JOIN matched_works mw ON mw.id = cw.work_id 
                 WHERE cw.character_id = c.id 
                 ORDER BY mw.match_prio ASC, mw.id ASC LIMIT 1),
                (SELECT w.title FROM gacha_character_works cw JOIN gacha_works w ON w.id=cw.work_id WHERE cw.character_id=c.id LIMIT 1)) AS work,
       COALESCE(col.user_id, '') AS owner
FROM chars c
LEFT JOIN gacha_collection col ON col.character_id = c.id AND col.guild_id = $3
LEFT JOIN gacha_guild_character_aliases ga ON ga.character_id = c.id AND ga.guild_id = $3
ORDER BY c.favourites DESC, c.id ASC
LIMIT $4 OFFSET $5`

	rows, err := s.DB.QueryContext(ctx, searchSQL, numericID, query, guild, limit, (page-1)*limit)
	if err != nil {
		return nil, 0, primaryTitle, err
	}
	defer rows.Close()

	var entries []SearchEntry
	startRank := (page-1)*limit + 1
	for rows.Next() {
		var e SearchEntry
		if err := rows.Scan(&e.ID, &e.Name, &e.Favourites, &e.Work, &e.Owner); err != nil {
			return nil, 0, primaryTitle, err
		}
		e.Rank = startRank + len(entries)
		entries = append(entries, e)
	}
	return entries, total, primaryTitle, rows.Err()
}

func (s *Store) Cards(ctx context.Context, guild, user, search string, page int) ([]Card, error) {
	if page < 1 || page > 100000 {
		return nil, userError(locale.Text("gacha.discord.invalid_page"))
	}
	rows, e := s.DB.QueryContext(ctx, cardSelect+` LEFT JOIN gacha_collection valued ON valued.character_id=c.id AND valued.guild_id=$2 LEFT JOIN gacha_guild_character_aliases ga ON ga.character_id=c.id AND ga.guild_id=$2 CROSS JOIN (SELECT gacha_claimed_count($2) AS claimed) population WHERE ($1='' OR EXISTS(SELECT 1 FROM gacha_collection col WHERE col.character_id=c.id AND col.guild_id=$2 AND col.user_id=$1)) AND ($3='' OR c.name ILIKE '%'||$3||'%' OR c.id::text=$3 OR c.native_name ILIKE '%'||$3||'%' OR EXISTS(SELECT 1 FROM jsonb_array_elements_text(CASE WHEN jsonb_typeof(c.aliases)='array' THEN c.aliases ELSE '[]'::jsonb END) alias WHERE alias ILIKE '%'||$3||'%') OR ga.alias ILIKE '%'||$3||'%') ORDER BY gacha_character_value(c.favourites,population.claimed,COALESCE(valued.keys,0)) DESC,c.id LIMIT 10 OFFSET $4`, user, guild, search, (page-1)*10)

	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var out []Card
	for rows.Next() {
		c, e := scanCard(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, c)
	}
	if e = rows.Err(); e != nil {
		return nil, e
	}
	rows.Close()
	cards := make([]*Card, len(out))
	for n := range out {
		cards[n] = &out[n]
	}
	if e = priceCards(ctx, s.DB, guild, cards...); e != nil {
		return nil, e
	}
	return out, nil
}
func (s *Store) WishlistLimit() int {
	if s != nil && s.Config.WishlistLimit > 0 {
		return s.Config.WishlistLimit
	}
	return 5
}

func (s *Store) Wish(ctx context.Context, guild, user string, id int64, remove bool) error {
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if e = ensurePlayer(ctx, tx, guild, user); e != nil {
		return e
	}
	var n int
	e = tx.QueryRowContext(ctx, `SELECT rolls_used FROM gacha_players WHERE guild_id=$1 AND user_id=$2 FOR UPDATE`, guild, user).Scan(&n)
	if e != nil {
		return e
	}
	if remove {
		_, e = tx.ExecContext(ctx, `DELETE FROM gacha_wishes WHERE guild_id=$1 AND user_id=$2 AND character_id=$3`, guild, user, id)
	} else {
		e = tx.QueryRowContext(ctx, `SELECT count(*) FROM gacha_wishes WHERE guild_id=$1 AND user_id=$2 AND character_id<>$3`, guild, user, id).Scan(&n)
		if e != nil {
			return e
		}
		limit := s.WishlistLimit()
		if n >= limit {
			return userError(locale.Text("gacha.game.your_wishlist_is_full_characters_remove_a", locale.Data{"Limit": limit}))
		}
		var res sql.Result
		res, e = tx.ExecContext(ctx, `INSERT INTO gacha_wishes(guild_id,user_id,character_id) SELECT $1,$2,id FROM gacha_characters WHERE id=$3 ON CONFLICT(guild_id,user_id,character_id) DO UPDATE SET character_id=excluded.character_id`, guild, user, id)
		if e == nil {
			n, err := res.RowsAffected()
			if err != nil {
				return err
			}
			if n == 0 {
				return userError(locale.Text("gacha.discord.character_not_found"))
			}
		}
	}
	if e != nil {
		return e
	}
	return tx.Commit()
}

// CancelDelivery refunds a roll only when Discord rejected delivery before anyone claimed it.
// Keeping the request row prevents duplicate gateway events from spending another roll.
func (s *Store) CancelDelivery(ctx context.Context, request string) error {
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var guild, user string
	var created time.Time
	var cid int64
	var epoch string
	var awarded bool
	e = tx.QueryRowContext(ctx, `SELECT guild_id,user_id FROM gacha_rolls WHERE request_id=$1`, request).Scan(&guild, &user)
	if e == sql.ErrNoRows {
		return nil
	}
	if e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, `SELECT 1 FROM gacha_players WHERE guild_id=$1 AND user_id=$2 FOR UPDATE`, guild, user); e != nil {
		return e
	}
	e = tx.QueryRowContext(ctx, `UPDATE gacha_rolls SET expires_at=created_at WHERE request_id=$1 AND claimed_by IS NULL AND expires_at>created_at RETURNING guild_id,user_id,created_at,character_id,COALESCE(key_epoch::text,''),key_awarded AND NOT key_revoked`, request).Scan(&guild, &user, &created, &cid, &epoch, &awarded)
	if e == sql.ErrNoRows {
		return nil
	}
	if e != nil {
		return e
	}
	if awarded {
		if _, e = tx.ExecContext(ctx, `UPDATE gacha_collection SET keys=GREATEST(0,keys-1) WHERE guild_id=$1 AND character_id=$2 AND key_epoch=$3::uuid`, guild, cid, epoch); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `UPDATE gacha_rolls SET key_revoked=true WHERE request_id=$1`, request); e != nil {
			return e
		}
	}
	_, e = tx.ExecContext(ctx, `UPDATE gacha_players SET rolls_used=GREATEST(0,rolls_used-1) WHERE guild_id=$1 AND user_id=$2 AND window_start<=$3`, guild, user, created)
	if e != nil {
		return e
	}
	return tx.Commit()
}

// FindCharacter finds a character by numeric ID or best matching name/alias.
// Returns the best match Card, a slice of alternate matching Cards (if any), and any error.
func (s *Store) FindCharacter(ctx context.Context, guild string, query string) (Card, []Card, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return Card{}, nil, userError(locale.Text("gacha.game.enter_a_character_name_or_id"))
	}

	if id, err := strconv.ParseInt(query, 10, 64); err == nil && id > 0 {
		c, err := scanCard(s.DB.QueryRowContext(ctx, cardSelect+` WHERE c.id=$1`, id))
		if err != nil {
			return Card{}, nil, err
		}
		_ = s.DB.QueryRowContext(ctx, `SELECT user_id FROM gacha_collection WHERE guild_id=$1 AND character_id=$2`, guild, c.ID).Scan(&c.Owner)
		if err := priceCards(ctx, s.DB, guild, &c); err != nil {
			return Card{}, nil, err
		}
		return c, nil, nil
	}

	// Text search: matches name, native_name, aliases, or guild custom alias.
	const searchSQL = cardSelect + `
LEFT JOIN gacha_guild_character_aliases ga ON ga.character_id=c.id AND ga.guild_id=$2
WHERE c.enabled AND EXISTS(SELECT 1 FROM gacha_assets a WHERE a.character_id=c.id AND a.status='approved')
  AND (c.name ILIKE '%'||$1||'%' OR c.native_name ILIKE '%'||$1||'%' OR EXISTS(SELECT 1 FROM jsonb_array_elements_text(CASE WHEN jsonb_typeof(c.aliases)='array' THEN c.aliases ELSE '[]'::jsonb END) al WHERE al ILIKE '%'||$1||'%') OR ga.alias ILIKE '%'||$1||'%')
ORDER BY
  CASE WHEN lower(COALESCE(ga.alias, c.name)) = lower($1) THEN 0
       WHEN lower(COALESCE(ga.alias, c.name)) LIKE lower($1)||'%' THEN 1
       WHEN lower(c.name) = lower($1) THEN 2
       WHEN lower(c.name) LIKE lower($1)||'%' THEN 3
       ELSE 4 END,
  c.favourites DESC, c.id ASC
LIMIT 5`

	rows, err := s.DB.QueryContext(ctx, searchSQL, query, guild)

	if err != nil {
		return Card{}, nil, err
	}
	defer rows.Close()

	var matches []Card
	for rows.Next() {
		c, err := scanCard(rows)
		if err != nil {
			return Card{}, nil, err
		}
		matches = append(matches, c)
	}
	if err := rows.Err(); err != nil {
		return Card{}, nil, err
	}
	if len(matches) == 0 {
		return Card{}, nil, sql.ErrNoRows
	}

	cards := make([]*Card, len(matches))
	for n := range matches {
		cards[n] = &matches[n]
	}
	if err := priceCards(ctx, s.DB, guild, cards...); err != nil {
		return Card{}, nil, err
	}

	_ = s.DB.QueryRowContext(ctx, `SELECT user_id FROM gacha_collection WHERE guild_id=$1 AND character_id=$2`, guild, matches[0].ID).Scan(&matches[0].Owner)
	return matches[0], matches[1:], nil
}

type TopCharEntry struct {
	Rank       int
	ID         int64
	Name       string
	Favourites int
	Gender     string
	Work       string
	Owner      string
	Value      int64
}

// TopCharacters queries characters ranked by favourites with claim status and gender filters.
func (s *Store) TopCharacters(ctx context.Context, guild string, claimFilter string, gender string, page int, limit int) ([]TopCharEntry, int64, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 10
	}

	claimFilter = strings.ToLower(strings.TrimSpace(claimFilter))
	switch claimFilter {
	case "unclaimed", "u", "livres", "livre":
		claimFilter = "unclaimed"
	case "claimed", "c", "casados", "casado", "owned":
		claimFilter = "claimed"
	default:
		claimFilter = "all"
	}

	gender = strings.ToLower(strings.TrimSpace(gender))
	switch gender {
	case "w", "f", "female", "waifu", "waifus", "mulher", "mulheres":
		gender = "female"
	case "h", "m", "male", "husbando", "husbandos", "homem", "homens":
		gender = "male"
	default:
		gender = ""
	}

	const countSQL = `
SELECT count(*)
FROM gacha_characters c
LEFT JOIN gacha_collection col ON col.character_id=c.id AND col.guild_id=$1
WHERE c.enabled AND EXISTS(SELECT 1 FROM gacha_assets a WHERE a.character_id=c.id AND a.status='approved')
  AND ($2 = '' OR lower(trim(c.gender)) LIKE $2 || '%')
  AND ($3 = 'all' OR ($3 = 'unclaimed' AND col.user_id IS NULL) OR ($3 = 'claimed' AND col.user_id IS NOT NULL))`

	var total int64
	if err := s.DB.QueryRowContext(ctx, countSQL, guild, gender, claimFilter).Scan(&total); err != nil {
		return nil, 0, err
	}

	const querySQL = `
SELECT c.id, c.name, c.favourites, c.gender,
       COALESCE((SELECT w.title FROM gacha_character_works cw JOIN gacha_works w ON w.id=cw.work_id WHERE cw.character_id=c.id ORDER BY CASE cw.role WHEN 'MAIN' THEN 0 ELSE 1 END,w.id LIMIT 1), 'Original') AS work,
       COALESCE(col.user_id, '') AS owner,
       gacha_character_value(c.favourites, pop.claimed, COALESCE(col.keys, 0)) AS value
FROM gacha_characters c
CROSS JOIN (SELECT gacha_claimed_count($1) AS claimed) pop
LEFT JOIN gacha_collection col ON col.character_id=c.id AND col.guild_id=$1
WHERE c.enabled AND EXISTS(SELECT 1 FROM gacha_assets a WHERE a.character_id=c.id AND a.status='approved')
  AND ($2 = '' OR lower(trim(c.gender)) LIKE $2 || '%')
  AND ($3 = 'all' OR ($3 = 'unclaimed' AND col.user_id IS NULL) OR ($3 = 'claimed' AND col.user_id IS NOT NULL))
ORDER BY c.favourites DESC, c.id ASC
LIMIT $4 OFFSET $5`

	rows, err := s.DB.QueryContext(ctx, querySQL, guild, gender, claimFilter, limit, (page-1)*limit)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var entries []TopCharEntry
	startRank := (page-1)*limit + 1
	for rows.Next() {
		var e TopCharEntry
		if err := rows.Scan(&e.ID, &e.Name, &e.Favourites, &e.Gender, &e.Work, &e.Owner, &e.Value); err != nil {
			return nil, 0, err
		}
		e.Rank = startRank + len(entries)
		entries = append(entries, e)
	}
	return entries, total, rows.Err()
}

// HaremCardAt fetches a single character Card at 0-indexed position 'index' from a user's harem.
func (s *Store) HaremCardAt(ctx context.Context, guild, user string, index int) (Card, int64, error) {
	var total int64
	if err := s.DB.QueryRowContext(ctx, `SELECT count(*) FROM gacha_collection WHERE guild_id=$1 AND user_id=$2`, guild, user).Scan(&total); err != nil {
		return Card{}, 0, err
	}
	if total == 0 {
		return Card{}, 0, nil
	}

	if index < 0 {
		index = 0
	} else if index >= int(total) {
		index = int(total) - 1
	}

	var cid int64
	const pickSQL = `
SELECT col.character_id
FROM gacha_collection col
JOIN gacha_characters c ON c.id=col.character_id
CROSS JOIN (SELECT gacha_claimed_count($1) AS claimed) pop
WHERE col.guild_id=$1 AND col.user_id=$2
ORDER BY gacha_character_value(c.favourites, pop.claimed, col.keys) DESC, c.id ASC
LIMIT 1 OFFSET $3`

	if err := s.DB.QueryRowContext(ctx, pickSQL, guild, user, index).Scan(&cid); err != nil {
		return Card{}, total, err
	}

	c, err := scanCard(s.DB.QueryRowContext(ctx, cardSelect+` WHERE c.id=$1`, cid))
	if err != nil {
		return Card{}, total, err
	}
	c.Owner = user
	if err := priceCards(ctx, s.DB, guild, &c); err != nil {
		return Card{}, total, err
	}
	return c, total, nil
}
