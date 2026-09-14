package gacha

import (
	"bot/internal/locale"
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

const maxWishlistInputs = 50

type WishEntry struct {
	ID   int64
	Name string
}
type WishBatchResult struct {
	Changed, Unchanged           []WishEntry
	Missing, Ambiguous, Overflow []string
	Count, Limit                 int
}

// SplitWishlistInput preserves spaces in names. Numeric IDs may also be separated
// by spaces; names can be separated with commas, semicolons, pipes or newlines.
func SplitWishlistInput(input string) ([]string, error) {
	if len(input) > 2000 {
		return nil, userError(locale.Text("gacha.wishlist.input_limit"))
	}
	parts := strings.FieldsFunc(input, func(r rune) bool { return r == ',' || r == ';' || r == '|' || r == '\n' })
	if len(parts) == 1 {
		fields := strings.Fields(parts[0])
		numeric := len(fields) > 1
		for _, field := range fields {
			if id, err := strconv.ParseInt(strings.TrimPrefix(field, "#"), 10, 64); err != nil || id <= 0 {
				numeric = false
			}
		}
		if numeric {
			parts = fields
		}
	}
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if v := strings.TrimSpace(part); v != "" {
			out = append(out, v)
		}
	}
	if len(out) == 0 {
		return nil, userError(locale.Text("gacha.game.enter_a_character_name_or_id"))
	}
	if len(out) > maxWishlistInputs {
		return nil, userError(locale.Text("gacha.wishlist.input_limit"))
	}
	return out, nil
}

func lockWishlist(ctx context.Context, tx *sql.Tx, guild, user string) error {
	if err := ensurePlayer(ctx, tx, guild, user); err != nil {
		return err
	}
	var n int
	return tx.QueryRowContext(ctx, `SELECT 1 FROM gacha_players WHERE guild_id=$1 AND user_id=$2 FOR UPDATE`, guild, user).Scan(&n)
}

// resolveWish searches existing wishes for removal, regardless of editorial
// visibility or available portraits. It never resolves through priced cards.
func resolveWish(ctx context.Context, tx *sql.Tx, guild, user, query string, remove bool) (WishEntry, bool, error) {
	id, _ := strconv.ParseInt(strings.TrimPrefix(query, "#"), 10, 64)

	// Resolve IDs and canonical names through their indexes before expanding
	// aliases or searching substrings across the catalog. Canonical names take
	// precedence over aliases; duplicate canonical names remain ambiguous.
	exactSQL := `SELECT c.id,COALESCE(ga.alias,c.name)
 FROM gacha_characters c
 LEFT JOIN gacha_guild_character_aliases ga ON ga.guild_id=$1 AND ga.character_id=c.id
 WHERE `
	var match any = query
	if id > 0 {
		exactSQL += `c.id=$3`
		match = id
	} else {
		exactSQL += `lower(c.name)=lower($3)`
	}
	exactSQL += ` AND ((NOT $4::boolean AND ($5::boolean OR (c.enabled AND c.archived_at IS NULL AND EXISTS(SELECT 1 FROM gacha_assets WHERE character_id=c.id AND status='approved'))))
 OR ($4::boolean AND EXISTS(SELECT 1 FROM gacha_wishes WHERE guild_id=$1 AND user_id=$2 AND character_id=c.id))) ORDER BY c.id LIMIT 2`
	rows, err := tx.QueryContext(ctx, exactSQL, guild, user, match, remove, id > 0)
	if err != nil {
		return WishEntry{}, false, err
	}
	var exact []WishEntry
	for rows.Next() {
		var entry WishEntry
		if err := rows.Scan(&entry.ID, &entry.Name); err != nil {
			rows.Close()
			return WishEntry{}, false, err
		}
		exact = append(exact, entry)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return WishEntry{}, false, err
	}
	if len(exact) > 0 {
		return exact[0], len(exact) > 1, nil
	}
	if id > 0 {
		return WishEntry{}, false, sql.ErrNoRows
	}
	// Materialize name matches before portrait checks, so a partial search
	// cannot probe the asset table once for every character in the catalog.
	rows, err = tx.QueryContext(ctx, `
WITH matches AS MATERIALIZED (
 SELECT c.id,COALESCE(ga.alias,c.name) AS name,c.enabled,c.archived_at,
 CASE WHEN lower(c.name)=lower($3) OR lower(c.native_name)=lower($3) OR lower(ga.alias)=lower($3)
 OR EXISTS(SELECT 1 FROM jsonb_array_elements_text(CASE WHEN jsonb_typeof(c.aliases)='array' THEN c.aliases ELSE '[]'::jsonb END) a WHERE lower(a)=lower($3)) THEN 0 ELSE 1 END AS priority
 FROM gacha_characters c
 LEFT JOIN gacha_guild_character_aliases ga ON ga.guild_id=$1 AND ga.character_id=c.id
 WHERE (NOT $4::boolean OR EXISTS(SELECT 1 FROM gacha_wishes WHERE guild_id=$1 AND user_id=$2 AND character_id=c.id))
 AND (strpos(lower(c.name),lower($3))>0 OR strpos(lower(c.native_name),lower($3))>0 OR strpos(lower(ga.alias),lower($3))>0
 OR EXISTS(SELECT 1 FROM jsonb_array_elements_text(CASE WHEN jsonb_typeof(c.aliases)='array' THEN c.aliases ELSE '[]'::jsonb END) a WHERE strpos(lower(a),lower($3))>0))
)
SELECT id,name,priority FROM matches
WHERE $4::boolean OR (enabled AND archived_at IS NULL AND EXISTS(SELECT 1 FROM gacha_assets WHERE character_id=matches.id AND status='approved'))
ORDER BY priority,id LIMIT 2`, guild, user, query, remove)
	if err != nil {
		return WishEntry{}, false, err
	}
	defer rows.Close()
	var found []WishEntry
	var priorities []int
	for rows.Next() {
		var entry WishEntry
		var priority int
		if err = rows.Scan(&entry.ID, &entry.Name, &priority); err != nil {
			return WishEntry{}, false, err
		}
		found = append(found, entry)
		priorities = append(priorities, priority)
	}
	if err = rows.Err(); err != nil {
		return WishEntry{}, false, err
	}
	if len(found) == 0 {
		return WishEntry{}, false, sql.ErrNoRows
	}
	return found[0], len(found) > 1 && priorities[0] == priorities[1], nil
}

// UpdateWishlist serializes the entire batch with single-item writes and clear
// confirmations. Entries beyond capacity are skipped in input order.
func (s *Store) UpdateWishlist(ctx context.Context, guild, user string, inputs []string, remove bool) (WishBatchResult, error) {
	result := WishBatchResult{Limit: s.WishlistLimit()}
	if guild == "" || user == "" {
		return result, userError(locale.Text("gacha.social.use_this_command_in_a_server_channel"))
	}
	if len(inputs) == 0 || len(inputs) > maxWishlistInputs {
		return result, userError(locale.Text("gacha.wishlist.input_limit"))
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	if err = lockWishlist(ctx, tx, guild, user); err != nil {
		return result, err
	}
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM gacha_wishes WHERE guild_id=$1 AND user_id=$2`, guild, user).Scan(&result.Count); err != nil {
		return result, err
	}
	for _, query := range inputs {
		if !remove && result.Count >= result.Limit {
			result.Overflow = append(result.Overflow, query)
			continue
		}
		entry, ambiguous, err := resolveWish(ctx, tx, guild, user, query, remove)
		if err == sql.ErrNoRows {
			result.Missing = append(result.Missing, query)
			continue
		}
		if err != nil {
			return result, err
		}
		if ambiguous {
			result.Ambiguous = append(result.Ambiguous, query)
			continue
		}
		var changed sql.Result
		if remove {
			changed, err = tx.ExecContext(ctx, `DELETE FROM gacha_wishes WHERE guild_id=$1 AND user_id=$2 AND character_id=$3`, guild, user, entry.ID)
		} else {
			changed, err = tx.ExecContext(ctx, `INSERT INTO gacha_wishes(guild_id,user_id,character_id) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, guild, user, entry.ID)
		}
		if err != nil {
			return result, err
		}
		n, err := changed.RowsAffected()
		if err != nil {
			return result, err
		}
		if n == 0 {
			result.Unchanged = append(result.Unchanged, entry)
		} else {
			result.Changed = append(result.Changed, entry)
			if remove {
				result.Count--
			} else {
				result.Count++
			}
		}
	}
	return result, tx.Commit()
}

// PrepareWishlistClear keeps one expiring, single-use confirmation per user and
// guild. Restarting or routing the click to another replica does not lose it.
func (s *Store) PrepareWishlistClear(ctx context.Context, guild, channel, user string) (string, int, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return "", 0, err
	}
	defer tx.Rollback()
	if err = lockWishlist(ctx, tx, guild, user); err != nil {
		return "", 0, err
	}
	var count int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM gacha_wishes WHERE guild_id=$1 AND user_id=$2`, guild, user).Scan(&count); err != nil {
		return "", 0, err
	}
	if count == 0 {
		return "", 0, nil
	}
	token := uuid.NewString()
	_, err = tx.ExecContext(ctx, `INSERT INTO gacha_wishlist_confirmations(guild_id,user_id,channel_id,token,expires_at) VALUES($1,$2,$3,$4,now()+interval '5 minutes') ON CONFLICT(guild_id,user_id) DO UPDATE SET channel_id=excluded.channel_id,token=excluded.token,expires_at=excluded.expires_at`, guild, user, channel, token)
	if err != nil {
		return "", 0, err
	}
	return token, count, tx.Commit()
}

func (s *Store) ResolveWishlistClear(ctx context.Context, guild, channel, user, token string, confirm bool) (int64, error) {
	if _, err := uuid.Parse(token); err != nil {
		return 0, userError(locale.Text("gacha.wishlist.confirmation_expired"))
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if err = lockWishlist(ctx, tx, guild, user); err != nil {
		return 0, err
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM gacha_wishlist_confirmations WHERE guild_id=$1 AND user_id=$2 AND channel_id=$3 AND token=$4 AND expires_at>now()`, guild, user, channel, token)
	if err != nil {
		return 0, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	if n != 1 {
		return 0, userError(locale.Text("gacha.wishlist.confirmation_expired"))
	}
	var removed int64
	if confirm {
		result, err = tx.ExecContext(ctx, `DELETE FROM gacha_wishes WHERE guild_id=$1 AND user_id=$2`, guild, user)
		if err != nil {
			return 0, err
		}
		removed, err = result.RowsAffected()
		if err != nil {
			return 0, fmt.Errorf("count removed wishes: %w", err)
		}
	}
	return removed, tx.Commit()
}
