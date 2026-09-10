package gacha

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"math/big"
	"time"
)

var ErrLimit = errors.New("Você usou todos os sorteios desta hora. Tente novamente após a renovação.")
var ErrEmpty = errors.New("O catálogo ainda não tem personagens com imagens aprovadas.")
var ErrClaim = errors.New("Este personagem já foi reservado, o sorteio expirou ou sua reserva ainda está em recarga.")

type Card struct {
	ID                                            int64
	Name, Work, Image, Source, Attribution, Owner string
	Favourites                                    int
}
type Roll struct {
	ID      string
	Card    Card
	Expires time.Time
}

const cardSelect = `SELECT c.id,c.name,c.favourites,COALESCE((SELECT w.title FROM gacha_character_works cw JOIN gacha_works w ON w.id=cw.work_id WHERE cw.character_id=c.id ORDER BY CASE cw.role WHEN 'MAIN' THEN 0 ELSE 1 END,w.id LIMIT 1), 'Original'),COALESCE(a.path,''),COALESCE(a.source_url,''),COALESCE(a.attribution,'') FROM gacha_characters c LEFT JOIN LATERAL (SELECT path,source_url,attribution FROM gacha_assets WHERE character_id=c.id AND status='approved' ORDER BY id LIMIT 1) a ON true `

type scanner interface{ Scan(...any) error }

func scanCard(row scanner) (Card, error) {
	var c Card
	e := row.Scan(&c.ID, &c.Name, &c.Favourites, &c.Work, &c.Image, &c.Source, &c.Attribution)
	return c, e
}
func ensurePlayer(ctx context.Context, tx *sql.Tx, guild, user string) error {
	if guild == "" || user == "" {
		return errors.New("Use este comando em um servidor.")
	}
	if _, e := tx.ExecContext(ctx, `INSERT INTO users(id,balance) VALUES($1,0) ON CONFLICT DO NOTHING`, user); e != nil {
		return e
	}
	_, e := tx.ExecContext(ctx, `INSERT INTO gacha_players(guild_id,user_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, guild, user)
	return e
}
func (s *Store) Roll(ctx context.Context, guild, channel, user, request string) (Roll, error) {
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
	e = tx.QueryRowContext(ctx, `SELECT id,character_id,expires_at FROM gacha_rolls WHERE request_id=$1 AND guild_id=$2 AND user_id=$3`, request, guild, user).Scan(&r.ID, &cid, &r.Expires)
	if e == nil {
		r.Card, e = scanCard(tx.QueryRowContext(ctx, cardSelect+` WHERE c.id=$1`, cid))
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
	const eligible = `c.enabled AND EXISTS(SELECT 1 FROM gacha_assets a WHERE a.character_id=c.id AND a.status='approved')`
	e = tx.QueryRowContext(ctx, `SELECT count(*) FROM gacha_characters c WHERE `+eligible).Scan(&count)
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
	r.Card, e = scanCard(tx.QueryRowContext(ctx, cardSelect+` WHERE `+eligible+` ORDER BY c.id OFFSET $1 LIMIT 1`, n.Int64()))
	if e != nil {
		return r, e
	}
	r.ID = uuid.NewString()
	e = tx.QueryRowContext(ctx, `INSERT INTO gacha_rolls(id,guild_id,channel_id,user_id,character_id,expires_at,request_id) VALUES($1,$2,$3,$4,$5,now()+interval '45 seconds',$6) RETURNING expires_at`, r.ID, guild, channel, user, r.Card.ID, request).Scan(&r.Expires)
	if e != nil {
		return r, e
	}
	e = tx.QueryRowContext(ctx, `SELECT user_id FROM gacha_collection WHERE guild_id=$1 AND character_id=$2`, guild, r.Card.ID).Scan(&r.Card.Owner)
	if e != nil && e != sql.ErrNoRows {
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
		return nil, fmt.Errorf("Página inválida.")
	}
	rows, e := s.DB.QueryContext(ctx, cardSelect+` WHERE ($1='' OR EXISTS(SELECT 1 FROM gacha_collection col WHERE col.character_id=c.id AND col.guild_id=$2 AND col.user_id=$1)) AND ($3='' OR c.name ILIKE '%'||$3||'%' OR c.id::text=$3) ORDER BY c.favourites DESC,c.id LIMIT 10 OFFSET $4`, user, guild, search, (page-1)*10)
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
	return out, rows.Err()
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
		e = tx.QueryRowContext(ctx, `SELECT count(*) FROM gacha_wishes WHERE guild_id=$1 AND user_id=$2`, guild, user).Scan(&n)
		if e != nil {
			return e
		}
		if n >= 20 {
			return errors.New("Limite de 20 desejos. Remova um antes de adicionar outro.")
		}
		var res sql.Result
		res, e = tx.ExecContext(ctx, `INSERT INTO gacha_wishes(guild_id,user_id,character_id) SELECT $1,$2,id FROM gacha_characters WHERE id=$3 ON CONFLICT(guild_id,user_id,character_id) DO UPDATE SET character_id=excluded.character_id`, guild, user, id)
		if e == nil {
			n, err := res.RowsAffected()
			if err != nil {
				return err
			}
			if n == 0 {
				return fmt.Errorf("personagem não encontrado")
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
	e = tx.QueryRowContext(ctx, `UPDATE gacha_rolls SET expires_at=created_at WHERE request_id=$1 AND claimed_by IS NULL AND expires_at>created_at RETURNING guild_id,user_id,created_at`, request).Scan(&guild, &user, &created)
	if e == sql.ErrNoRows {
		return nil
	}
	if e != nil {
		return e
	}
	_, e = tx.ExecContext(ctx, `UPDATE gacha_players SET rolls_used=GREATEST(0,rolls_used-1) WHERE guild_id=$1 AND user_id=$2 AND window_start<=$3`, guild, user, created)
	if e != nil {
		return e
	}
	return tx.Commit()
}
