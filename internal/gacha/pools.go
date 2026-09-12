package gacha

import (
	"fmt"
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
	{"roll", "All characters", "", ""},
	{"w", "Female characters", "female", ""}, {"h", "Male characters", "male", ""},
	{"wa", "Female anime characters", "female", "anime"}, {"ha", "Male anime characters", "male", "anime"}, {"ma", "All anime characters", "", "anime"},
	{"wg", "Female game characters", "female", "game"}, {"hg", "Male game characters", "male", "game"}, {"mg", "All game characters", "", "game"},
}

func poolFor(code string) (Pool, error) {
	for _, p := range Pools {
		if p.Code == code {
			return p, nil
		}
	}
	return Pool{}, userError("Unknown roll pool. Use roll, w, h, wa, ha, ma, wg, hg or mg.")
}
func normalizeAction(action string) string {
	action = strings.ToLower(action)
	aliases := map[string]string{
		"sortear": "roll", "collection": "harem", "colecao": "harem", "mm": "harem",
		"mmi": "harem_visual", "buscar": "search", "personagem": "character",
		"im": "character", "char": "character", "info": "character",
		"galeria": "gallery", "desejar": "wish", "remover": "unwish",
		"desejos": "wishes", "tu": "status", "divorciar": "divorce",
		"topchar": "topchar", "topc": "topchar", "topu": "topchar_unclaimed",
		"topw": "topchar_waifu", "toph": "topchar_husbando",
	}
	if a, ok := aliases[action]; ok {
		return a
	}
	return action
}

type userError string

func (e userError) Error() string { return string(e) }
func invalidID() error {
	return userError("Enter a positive character ID. Find IDs with !gacha search <name>.")
}
func valueLabel(c Card) string { return fmt.Sprintf("%d coins", c.Value) }
