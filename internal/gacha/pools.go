package gacha

import (
	"bot/internal/locale"

	"strings"
)

// Base value is 1.5% of favorites, rounded down, with a 10-coin floor.
// Split before multiplying so even a large input cannot overflow intermediates.
func CharacterValue(favourites int) int64 {
	likes := int64(max(0, favourites))
	return max(10, likes/1000*15+(likes%1000)*15/1000)
}

type Pool struct{ Code, Name, Gender, Kind string }

var Pools = []Pool{
	{"roll", locale.Text("gacha.pools.all_characters"), "", ""},
	{"w", locale.Text("gacha.pools.female_characters"), "female", ""}, {"h", locale.Text("gacha.pools.male_characters"), "male", ""},
	{"wa", locale.Text("gacha.pools.female_anime_characters"), "female", "anime"}, {"ha", locale.Text("gacha.pools.male_anime_characters"), "male", "anime"}, {"ma", locale.Text("gacha.pools.all_anime_characters"), "", "anime"},
	{"wg", locale.Text("gacha.pools.female_game_characters"), "female", "game"}, {"hg", locale.Text("gacha.pools.male_game_characters"), "male", "game"}, {"mg", locale.Text("gacha.pools.all_game_characters"), "", "game"},
}

func poolFor(code string) (Pool, error) {
	for _, p := range Pools {
		if p.Code == code {
			return p, nil
		}
	}
	return Pool{}, userError(locale.Text("gacha.pools.unknown_roll_pool_use_roll_w_h"))
}
func normalizeAction(action string) string {
	action = strings.ToLower(action)
	aliases := map[string]string{
		"info": "character", "profile": "status", "wishlist": "wishes",
		"top": "topchar", "harem-ranking": "ranking", "pesquisar": "search",
		"apelido": "alias", "series": "series", "work": "series",
		"anime": "series", "obra": "series", "obras": "series",
	}
	if a, ok := aliases[action]; ok {
		return a
	}
	return action
}

type userError string

func (e userError) Error() string { return string(e) }
func invalidID() error {
	return userError(locale.Text("gacha.pools.enter_a_positive_character_id_find_ids"))
}
func valueLabel(c Card) string {
	return locale.Text("gacha.pools.coins.formatted", locale.Data{"Value": c.Value})
}
