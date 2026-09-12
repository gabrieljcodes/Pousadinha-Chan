package gacha

import (
	"context"
	"database/sql"
	"math/big"
)

// CalculateValue is the exact-integer counterpart of the SQL valuation function.
// It rejects values beyond the wallet's storage range instead of imposing a game cap.
func CalculateValue(favorites, claimed, keys int64) (int64, error) {
	base := new(big.Int).Mul(big.NewInt(max(0, favorites)), big.NewInt(15))
	base.Quo(base, big.NewInt(1000))
	if base.Cmp(big.NewInt(10)) < 0 {
		base.SetInt64(10)
	}
	server := new(big.Int).Add(big.NewInt(max(0, claimed)), big.NewInt(10000))
	keyBonus := new(big.Int).Mul(big.NewInt(max(0, keys)), big.NewInt(200))
	milestones := new(big.Int).Mul(big.NewInt(max(0, keys)/10), big.NewInt(1000))
	keyBonus.Add(keyBonus, milestones)
	keyBonus.Add(keyBonus, big.NewInt(10000))
	value := new(big.Int).Mul(base, server)
	value.Mul(value, keyBonus)
	value.Quo(value, big.NewInt(100000000))
	if !value.IsInt64() {
		return 0, userError("This value exceeds the wallet's supported numeric range.")
	}
	return value.Int64(), nil
}

type rowQuerier interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

// Price a whole result page with one guild-scoped snapshot, rather than N+1 queries.
func priceCards(ctx context.Context, q rowQuerier, guild string, cards ...*Card) error {
	if len(cards) == 0 {
		return nil
	}
	ids := make([]int64, 0, len(cards))
	byID := map[int64][]*Card{}
	for _, c := range cards {
		ids = append(ids, c.ID)
		byID[c.ID] = append(byID[c.ID], c)
	}
	rows, e := q.QueryContext(ctx, `WITH population AS (SELECT count(*) AS claimed FROM gacha_collection WHERE guild_id=$1)
 SELECT c.id,COALESCE(col.user_id,''),COALESCE(col.keys,0),population.claimed,
 gacha_character_value(c.favourites,population.claimed,COALESCE(col.keys,0))::bigint
 FROM gacha_characters c CROSS JOIN population LEFT JOIN gacha_collection col ON col.character_id=c.id AND col.guild_id=$1
 WHERE c.id=ANY($2::bigint[])`, guild, ids)
	if e != nil {
		return e
	}
	defer rows.Close()
	for rows.Next() {
		var id, keys, claimed, value int64
		var owner string
		if e = rows.Scan(&id, &owner, &keys, &claimed, &value); e != nil {
			return e
		}
		for _, c := range byID[id] {
			c.Owner = owner
			c.Keys = keys
			c.Claimed = claimed
			c.Value = value
		}
	}
	return rows.Err()
}

func awardKey(ctx context.Context, tx *sql.Tx, guild, user string, r *Roll) error {
	var epoch string
	e := tx.QueryRowContext(ctx, `UPDATE gacha_collection SET keys=keys+1 WHERE guild_id=$1 AND character_id=$2 AND user_id=$3 RETURNING key_epoch::text`, guild, r.Card.ID, user).Scan(&epoch)
	if e == sql.ErrNoRows {
		return nil
	}
	if e != nil {
		return e
	}
	_, e = tx.ExecContext(ctx, `UPDATE gacha_rolls SET key_awarded=true,key_epoch=$2::uuid WHERE id=$1`, r.ID, epoch)
	if e != nil {
		return e
	}
	r.KeyEarned = true
	return nil
}
