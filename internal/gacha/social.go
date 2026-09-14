package gacha

import (
	"bot/internal/locale"
	"context"
	"database/sql"
	"encoding/json"
	"sort"
	"strings"
	"time"
)

type Action struct {
	ID, Kind, Guild, Channel, Proposer, Recipient string
	Offered, Requested                            int64
	OfferedToken, RequestedToken                  string
	Payout                                        int64
	Status                                        string
	Expires                                       time.Time
}

const actionSelect = `SELECT id::text,kind,guild_id,channel_id,proposer_id,COALESCE(recipient_id,''),offered_id,COALESCE(requested_id,0),offered_token::text,COALESCE(requested_token::text,''),payout,status,expires_at FROM gacha_actions `

func scanAction(row scanner) (Action, error) {
	var a Action
	e := row.Scan(&a.ID, &a.Kind, &a.Guild, &a.Channel, &a.Proposer, &a.Recipient, &a.Offered, &a.Requested, &a.OfferedToken, &a.RequestedToken, &a.Payout, &a.Status, &a.Expires)
	return a, e
}

// All ownership mutations acquire player locks in the same order, then wallet rows,
// then collection rows. Actions also lock their own row before acquiring players.
func lockPlayers(ctx context.Context, tx *sql.Tx, guild string, users ...string) error {
	sort.Strings(users)
	for i, user := range users {
		if i > 0 && users[i-1] == user {
			continue
		}
		if e := ensurePlayer(ctx, tx, guild, user); e != nil {
			return e
		}
		var n int
		if e := tx.QueryRowContext(ctx, `SELECT rolls_used FROM gacha_players WHERE guild_id=$1 AND user_id=$2 FOR UPDATE`, guild, user).Scan(&n); e != nil {
			return e
		}
	}
	return nil
}
func ownership(ctx context.Context, tx *sql.Tx, guild, user string, id int64) (string, int, error) {
	var token string
	var likes int
	e := tx.QueryRowContext(ctx, `SELECT col.ownership_token::text,c.favourites FROM gacha_collection col JOIN gacha_characters c ON c.id=col.character_id WHERE col.guild_id=$1 AND col.user_id=$2 AND col.character_id=$3 FOR UPDATE OF col`, guild, user, id).Scan(&token, &likes)
	if e == sql.ErrNoRows {
		return "", 0, userError(locale.Text("gacha.social.character_is_not_in_the_expected_owner.formatted", locale.Data{"Id": id}))
	}
	return token, likes, e
}
func (s *Store) CreateAction(ctx context.Context, guild, channel, proposer, recipient, request, kind string, offered, requested int64) (Action, error) {
	var a Action
	if guild == "" || channel == "" || proposer == "" || request == "" {
		return a, userError(locale.Text("gacha.social.use_this_command_in_a_server_channel"))
	}
	if offered <= 0 {
		return a, invalidID()
	}
	switch kind {
	case "divorce":
		if recipient != "" || requested != 0 {
			return a, userError(locale.Text("gacha.social.divorce_accepts_one_character"))
		}
	case "trade":
		if requested <= 0 || requested == offered {
			return a, userError(locale.Text("gacha.social.a_trade_needs_two_different_character_ids"))
		}
	case "gift":
		if requested != 0 {
			return a, userError(locale.Text("gacha.social.a_gift_accepts_one_character"))
		}
	default:
		return a, userError(locale.Text("gacha.social.unknown_action"))
	}
	if kind != "divorce" && (recipient == "" || recipient == proposer) {
		return a, userError(locale.Text("gacha.social.choose_another_server_member"))
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return a, e
	}
	defer tx.Rollback()
	users := []string{proposer}
	if recipient != "" {
		users = append(users, recipient)
	}
	if e = lockPlayers(ctx, tx, guild, users...); e != nil {
		return a, e
	}
	a, e = scanAction(tx.QueryRowContext(ctx, actionSelect+` WHERE request_id=$1`, request))
	if e == nil {
		if a.Guild != guild || a.Proposer != proposer || a.Channel != channel {
			return Action{}, userError(locale.Text("gacha.social.this_request_belongs_to_another_action"))
		}
		return a, nil
	}
	if e != sql.ErrNoRows {
		return a, e
	}
	var pending int
	e = tx.QueryRowContext(ctx, `SELECT count(*) FROM gacha_actions WHERE guild_id=$1 AND proposer_id=$2 AND status='pending' AND expires_at>clock_timestamp()`, guild, proposer).Scan(&pending)
	if e != nil {
		return a, e
	}
	if pending >= 10 {
		return a, userError(locale.Text("gacha.social.you_already_have_pending_offers_cancel_one"))
	}
	token, _, e := ownership(ctx, tx, guild, proposer, offered)
	if e != nil {
		return a, e
	}
	wantedToken := ""
	if kind == "trade" {
		wantedToken, _, e = ownership(ctx, tx, guild, recipient, requested)
		if e != nil {
			return a, e
		}
	}
	payout := int64(0)
	if kind == "divorce" {
		c := Card{ID: offered}
		if e = priceCards(ctx, tx, guild, &c); e != nil {
			return a, e
		}
		payout = c.Value
	}
	var id string
	e = tx.QueryRowContext(ctx, `INSERT INTO gacha_actions(request_id,guild_id,channel_id,kind,proposer_id,recipient_id,offered_id,requested_id,offered_token,requested_token,payout) VALUES($1,$2,$3,$4,$5,NULLIF($6,''),$7,NULLIF($8,0),$9::uuid,NULLIF($10,'')::uuid,$11) RETURNING id::text`, request, guild, channel, kind, proposer, recipient, offered, requested, token, wantedToken, payout).Scan(&id)
	if e != nil {
		return a, e
	}
	a, e = scanAction(tx.QueryRowContext(ctx, actionSelect+` WHERE id=$1`, id))
	if e != nil {
		return a, e
	}
	return a, tx.Commit()
}
func (s *Store) ResolveAction(ctx context.Context, guild, channel, user, id, decision string) (Action, error) {
	var a Action
	if decision != "accept" && decision != "decline" && decision != "cancel" {
		return a, userError(locale.Text("gacha.social.unknown_action_response"))
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return a, e
	}
	defer tx.Rollback()
	a, e = scanAction(tx.QueryRowContext(ctx, actionSelect+` WHERE id=$1::uuid AND guild_id=$2 AND channel_id=$3 FOR UPDATE`, id, guild, channel))
	if e == sql.ErrNoRows {
		return a, userError(locale.Text("gacha.social.offer_not_found_in_this_channel"))
	}
	if e != nil {
		return a, e
	}
	allowed := a.Recipient
	if a.Kind == "divorce" || decision == "cancel" {
		allowed = a.Proposer
	}
	if user != allowed {
		return a, userError(locale.Text("gacha.social.only_the_named_participant_can_respond_to"))
	}
	if a.Kind == "divorce" && decision == "decline" {
		return a, userError(locale.Text("gacha.social.use_cancel_to_keep_your_character"))
	}
	if a.Status != "pending" {
		return a, nil
	}
	finish := func(status string) (Action, error) {
		a.Status = status
		_, e := tx.ExecContext(ctx, `UPDATE gacha_actions SET status=$2,resolved_at=clock_timestamp() WHERE id=$1`, a.ID, status)
		if e != nil {
			return a, e
		}
		return a, tx.Commit()
	}
	var live bool
	if e = tx.QueryRowContext(ctx, `SELECT expires_at>clock_timestamp() FROM gacha_actions WHERE id=$1`, a.ID).Scan(&live); e != nil {
		return a, e
	}
	if !live {
		return finish("expired")
	}
	if decision == "cancel" {
		return finish("cancelled")
	}
	if decision == "decline" {
		return finish("declined")
	}
	users := []string{a.Proposer}
	if a.Recipient != "" {
		users = append(users, a.Recipient)
	}
	if e = lockPlayers(ctx, tx, guild, users...); e != nil {
		return a, e
	}
	// Serialize wallet credit before collection mutation; failure rolls back both.
	if a.Kind == "divorce" {
		var balance int64
		e = tx.QueryRowContext(ctx, `SELECT balance FROM guild_members WHERE guild_id=$1 AND user_id=$2 FOR UPDATE`, guild, a.Proposer).Scan(&balance)
		if e != nil {
			return a, e
		}
	}
	token, _, e := ownership(ctx, tx, guild, a.Proposer, a.Offered)
	if _, ok := e.(userError); ok {
		return finish("stale")
	}
	if e != nil {
		return a, e
	}
	if token != a.OfferedToken {
		return finish("stale")
	}
	if a.Kind == "trade" {
		token, _, e = ownership(ctx, tx, guild, a.Recipient, a.Requested)
		if _, ok := e.(userError); ok {
			return finish("stale")
		}
		if e != nil {
			return a, e
		}
		if token != a.RequestedToken {
			return finish("stale")
		}
	}
	// Recheck expiration after waiting for locks.
	if e = tx.QueryRowContext(ctx, `SELECT expires_at>clock_timestamp() FROM gacha_actions WHERE id=$1`, a.ID).Scan(&live); e != nil {
		return a, e
	}
	if !live {
		return finish("expired")
	}
	if a.Kind == "divorce" {
		if _, e = tx.ExecContext(ctx, `DELETE FROM gacha_collection WHERE guild_id=$1 AND character_id=$2`, guild, a.Offered); e != nil {
			return a, e
		}
		if _, e = tx.ExecContext(ctx, `UPDATE guild_members SET balance=balance+$3,updated_at=now() WHERE guild_id=$1 AND user_id=$2`, guild, a.Proposer, a.Payout); e != nil {
			return a, e
		}
	} else {
		if _, e = tx.ExecContext(ctx, `UPDATE gacha_collection SET user_id=$3,ownership_token=gen_random_uuid(),claimed_at=now() WHERE guild_id=$1 AND character_id=$2`, guild, a.Offered, a.Recipient); e != nil {
			return a, e
		}
		if a.Kind == "trade" {
			if _, e = tx.ExecContext(ctx, `UPDATE gacha_collection SET user_id=$3,ownership_token=gen_random_uuid(),claimed_at=now() WHERE guild_id=$1 AND character_id=$2`, guild, a.Requested, a.Proposer); e != nil {
				return a, e
			}
		}
	}
	return finish("completed")
}
func (s *Store) Offers(ctx context.Context, guild, user string) ([]Action, error) {
	rows, e := s.DB.QueryContext(ctx, actionSelect+` WHERE guild_id=$1 AND (proposer_id=$2 OR recipient_id=$2) AND status='pending' AND expires_at>clock_timestamp() ORDER BY created_at DESC LIMIT 10`, guild, user)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var out []Action
	for rows.Next() {
		a, e := scanAction(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

const characterValueSQL = `gacha_character_value(c.favourites,pop.claimed,col.keys)`
const populationSQL = `WITH pop AS (SELECT gacha_claimed_count($1) AS claimed) `

func (s *Store) HaremSummary(ctx context.Context, guild, user string) (int, int64, error) {
	var count int
	var value int64
	e := s.DB.QueryRowContext(ctx, populationSQL+`SELECT count(*),COALESCE(sum(`+characterValueSQL+`),0)::bigint FROM gacha_collection col JOIN gacha_characters c ON c.id=col.character_id CROSS JOIN pop WHERE col.guild_id=$1 AND col.user_id=$2`, guild, user).Scan(&count, &value)
	return count, value, e
}

type Ranking struct {
	User  string
	Count int
	Value int64
}

func (s *Store) Rankings(ctx context.Context, guild string, page int) ([]Ranking, error) {
	if page < 1 || page > 100000 {
		return nil, userError(locale.Text("gacha.discord.invalid_page"))
	}
	rows, e := s.DB.QueryContext(ctx, populationSQL+`SELECT col.user_id,count(*),sum(`+characterValueSQL+`)::bigint FROM gacha_collection col JOIN gacha_characters c ON c.id=col.character_id CROSS JOIN pop WHERE col.guild_id=$1 GROUP BY col.user_id ORDER BY sum(`+characterValueSQL+`) DESC,col.user_id LIMIT 10 OFFSET $2`, guild, (page-1)*10)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Ranking{}
	for rows.Next() {
		var r Ranking
		if e = rows.Scan(&r.User, &r.Count, &r.Value); e != nil {
			return nil, e
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

type AliasResult struct {
	CanonicalName string
	ActiveAlias   string
	Available     []string
	Reset         bool
}

// SetCharacterAlias changes or lists the display alias of a character the user is married to in the guild.
func (s *Store) SetCharacterAlias(ctx context.Context, guild, user string, charID int64, newAlias string) (AliasResult, error) {
	if guild == "" || user == "" {
		return AliasResult{}, userError(locale.Text("gacha.social.use_this_command_in_a_server_channel"))
	}
	if charID <= 0 {
		return AliasResult{}, invalidID()
	}

	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return AliasResult{}, err
	}
	defer tx.Rollback()

	// 1. Verify caller is currently married to the character in this guild
	_, _, err = ownership(ctx, tx, guild, user, charID)
	if err != nil {
		return AliasResult{}, userError(locale.Text("gacha.social.must_be_married_to_set_alias"))
	}

	// 2. Fetch canonical name and character's registered alternative aliases
	var name, aliasesJSON string
	err = tx.QueryRowContext(ctx, `SELECT name, COALESCE(aliases::text, '[]') FROM gacha_characters WHERE id=$1`, charID).Scan(&name, &aliasesJSON)
	if err != nil {
		return AliasResult{}, err
	}
	var aliases []string
	_ = json.Unmarshal([]byte(aliasesJSON), &aliases)

	// Filter out empty aliases or duplicates of canonical name
	var validAliases []string
	for _, a := range aliases {
		trimmed := strings.TrimSpace(a)
		if trimmed != "" && !strings.EqualFold(trimmed, name) {
			validAliases = append(validAliases, trimmed)
		}
	}

	// 3. Query current active guild alias (if any)
	var currentAlias string
	_ = tx.QueryRowContext(ctx, `SELECT alias FROM gacha_guild_character_aliases WHERE guild_id=$1 AND character_id=$2`, guild, charID).Scan(&currentAlias)

	// 4. If newAlias is empty, caller just wants to list available aliases
	trimmed := strings.TrimSpace(newAlias)
	if trimmed == "" {
		return AliasResult{
			CanonicalName: name,
			ActiveAlias:   currentAlias,
			Available:     validAliases,
		}, nil
	}

	// 5. If reset requested
	if strings.EqualFold(trimmed, "default") || strings.EqualFold(trimmed, "reset") || strings.EqualFold(trimmed, name) {
		if _, err = tx.ExecContext(ctx, `DELETE FROM gacha_guild_character_aliases WHERE guild_id=$1 AND character_id=$2`, guild, charID); err != nil {
			return AliasResult{}, err
		}
		if err = tx.Commit(); err != nil {
			return AliasResult{}, err
		}
		return AliasResult{
			CanonicalName: name,
			ActiveAlias:   "",
			Available:     validAliases,
			Reset:         true,
		}, nil
	}

	if len(validAliases) == 0 {
		return AliasResult{CanonicalName: name}, userError(locale.Text("gacha.social.character_has_no_aliases.formatted", locale.Data{"Name": name}))
	}

	// 6. Match against registered aliases (case-insensitive)
	var matched string
	for _, a := range validAliases {
		if strings.EqualFold(a, trimmed) {
			matched = a
			break
		}
	}
	if matched == "" {
		return AliasResult{}, userError(locale.Text("gacha.social.invalid_character_alias.formatted", locale.Data{
			"Alias":     trimmed,
			"Name":      name,
			"Available": strings.Join(validAliases, ", "),
		}))
	}

	// 7. Upsert guild alias
	_, err = tx.ExecContext(ctx, `INSERT INTO gacha_guild_character_aliases(guild_id, character_id, alias, set_by, updated_at)
		VALUES($1, $2, $3, $4, now())
		ON CONFLICT (guild_id, character_id)
		DO UPDATE SET alias=EXCLUDED.alias, set_by=EXCLUDED.set_by, updated_at=now()`, guild, charID, matched, user)
	if err != nil {
		return AliasResult{}, err
	}
	if err = tx.Commit(); err != nil {
		return AliasResult{}, err
	}

	return AliasResult{
		CanonicalName: name,
		ActiveAlias:   matched,
		Available:     validAliases,
	}, nil
}
