package gacha

import (
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

var ErrLimit = errors.New("No rolls left in this hourly window. Check !gacha status for the reset.")
var ErrEmpty = errors.New("No enabled characters with approved images match this pool. No roll was spent.")
var ErrClaim = errors.New("This character is owned, the roll expired, or your claim is on cooldown.")

type Card struct {
	Value, Keys, Claimed                          int64
	ID                                            int64
	Name, Work, Image, Source, Attribution, Owner string
	Favourites                                    int
}
type Roll struct {
	KeyEarned bool
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
		return userError("Use this command in a server.")
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
	_, e = tx.ExecContext(ctx, `UPDATE gacha_players SET window_start=now(),rolls_used=0 WHERE guild_id=$1 AND user_id=$2 AND window_start <= now()-interval '1 hour'`, guild, user)
	if e != nil {
		return r, e
	}
	e = tx.QueryRowContext(ctx, `UPDATE gacha_players SET rolls_used=rolls_used+1 WHERE guild_id=$1 AND user_id=$2 AND rolls_used<$3 RETURNING rolls_used`, guild, user, s.Config.RollsPerHour).Scan(&used)
	if e == sql.ErrNoRows {
		return r, ErrLimit
	}
	if e != nil {
		return r, e
	}
	// Uniform selection: indexed stable order + cryptographic random offset. No popularity-based pay advantage.
	var count int64
	const eligible = `c.enabled AND EXISTS(SELECT 1 FROM gacha_assets a WHERE a.character_id=c.id AND a.status='approved') AND ($1='' OR lower(trim(c.gender))=$1) AND ($2='' OR EXISTS(SELECT 1 FROM gacha_character_works cw JOIN gacha_works w ON w.id=cw.work_id WHERE cw.character_id=c.id AND w.kind=$2))`
	e = tx.QueryRowContext(ctx, `SELECT count(*) FROM gacha_characters c WHERE `+eligible, pool.Gender, pool.Kind).Scan(&count)
	if e != nil {
		return r, e
	}
	if count == 0 {
		return r, ErrEmpty
	}
	n, e := rand.Int(rand.Reader, big.NewInt(count))
	if e != nil {
		return r, e
	}
	r.Card, e = scanCard(tx.QueryRowContext(ctx, cardSelect+` WHERE `+eligible+` ORDER BY c.id OFFSET $3 LIMIT 1`, pool.Gender, pool.Kind, n.Int64()))
	if e != nil {
		return r, e
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
	_, e = tx.ExecContext(ctx, `UPDATE gacha_players SET claim_after=now()+($3 * interval '1 hour') WHERE guild_id=$1 AND user_id=$2`, guild, user, s.Config.ClaimHours)
	if e != nil {
		return e
	}
	return tx.Commit()
}
func (s *Store) Cards(ctx context.Context, guild, user, search string, page int) ([]Card, error) {
	if page < 1 || page > 100000 {
		return nil, userError("Invalid page.")
	}
	rows, e := s.DB.QueryContext(ctx, cardSelect+` LEFT JOIN gacha_collection valued ON valued.character_id=c.id AND valued.guild_id=$2 CROSS JOIN (SELECT count(*) AS claimed FROM gacha_collection WHERE guild_id=$2) population WHERE ($1='' OR EXISTS(SELECT 1 FROM gacha_collection col WHERE col.character_id=c.id AND col.guild_id=$2 AND col.user_id=$1)) AND ($3='' OR c.name ILIKE '%'||$3||'%' OR c.id::text=$3 OR c.native_name ILIKE '%'||$3||'%' OR EXISTS(SELECT 1 FROM jsonb_array_elements_text(c.aliases) alias WHERE alias ILIKE '%'||$3||'%')) ORDER BY gacha_character_value(c.favourites,population.claimed,COALESCE(valued.keys,0)) DESC,c.id LIMIT 10 OFFSET $4`, user, guild, search, (page-1)*10)
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
		if n >= 20 {
			return userError("Your wishlist is full (20 characters). Remove a wish first.")
		}
		var res sql.Result
		res, e = tx.ExecContext(ctx, `INSERT INTO gacha_wishes(guild_id,user_id,character_id) SELECT $1,$2,id FROM gacha_characters WHERE id=$3 ON CONFLICT(guild_id,user_id,character_id) DO UPDATE SET character_id=excluded.character_id`, guild, user, id)
		if e == nil {
			n, err := res.RowsAffected()
			if err != nil {
				return err
			}
			if n == 0 {
				return userError("Character not found.")
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
		return Card{}, nil, userError("Enter a character name or ID.")
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

	// Text search: matches name, native_name, or aliases.
	const searchSQL = cardSelect + `
WHERE c.enabled AND EXISTS(SELECT 1 FROM gacha_assets a WHERE a.character_id=c.id AND a.status='approved')
  AND (c.name ILIKE '%'||$1||'%' OR c.native_name ILIKE '%'||$1||'%' OR EXISTS(SELECT 1 FROM jsonb_array_elements_text(c.aliases) al WHERE al ILIKE '%'||$1||'%'))
ORDER BY
  CASE WHEN lower(c.name) = lower($1) THEN 0
       WHEN lower(c.name) LIKE lower($1)||'%' THEN 1
       ELSE 2 END,
  c.favourites DESC, c.id ASC
LIMIT 5`

	rows, err := s.DB.QueryContext(ctx, searchSQL, query)
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
CROSS JOIN (SELECT count(*) AS claimed FROM gacha_collection WHERE guild_id=$1) pop
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
CROSS JOIN (SELECT count(*) AS claimed FROM gacha_collection WHERE guild_id=$1) pop
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
