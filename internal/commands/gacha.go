package commands

import (
	"bot/internal/locale"
	"github.com/bwmarrin/discordgo"
)

func init() {
	min := 1.0
	dm := false

	page := func() *discordgo.ApplicationCommandOption {
		return &discordgo.ApplicationCommandOption{
			Type:        discordgo.ApplicationCommandOptionInteger,
			Name:        "page",
			Description: locale.Text("commands.discovery.page"),
			MinValue:    &min,
			MaxValue:    100000,
		}
	}
	member := func(name, desc string, required bool) *discordgo.ApplicationCommandOption {
		return &discordgo.ApplicationCommandOption{
			Type:        discordgo.ApplicationCommandOptionUser,
			Name:        name,
			Description: desc,
			Required:    required,
		}
	}
	character := func(name, description string, required bool) *discordgo.ApplicationCommandOption {
		return &discordgo.ApplicationCommandOption{
			Type:        discordgo.ApplicationCommandOptionString,
			Name:        name,
			Description: description,
			Required:    required,
		}
	}

	poolChoices := []*discordgo.ApplicationCommandOptionChoice{
		{Name: locale.Text("commands.gacha.waifus_anime"), Value: "wa"},
		{Name: locale.Text("commands.gacha.husbandos_anime"), Value: "ha"},
		{Name: locale.Text("commands.gacha.all_anime_characters"), Value: "ma"},
		{Name: locale.Text("commands.gacha.waifus_games"), Value: "wg"},
		{Name: locale.Text("commands.gacha.husbandos_games"), Value: "hg"},
		{Name: locale.Text("commands.gacha.all_game_characters"), Value: "mg"},
		{Name: locale.Text("commands.gacha.all_female_characters"), Value: "w"},
		{Name: locale.Text("commands.gacha.all_male_characters"), Value: "h"},
		{Name: locale.Text("commands.gacha.all_characters"), Value: "roll"},
	}

	claimChoices := []*discordgo.ApplicationCommandOptionChoice{
		{Name: locale.Text("commands.gacha.all_characters_b9180c"), Value: "all"},
		{Name: locale.Text("commands.gacha.unclaimed_characters"), Value: "unclaimed"},
		{Name: locale.Text("commands.gacha.claimed_characters"), Value: "claimed"},
	}

	genderChoices := []*discordgo.ApplicationCommandOptionChoice{
		{Name: locale.Text("commands.gacha.all_genders"), Value: "all"},
		{Name: locale.Text("commands.gacha.female_characters"), Value: "female"},
		{Name: locale.Text("commands.gacha.male_characters"), Value: "male"},
	}

	haremModeChoices := []*discordgo.ApplicationCommandOptionChoice{
		{Name: locale.Text("commands.gacha.compact_list"), Value: "list"},
		{Name: locale.Text("commands.gacha.photo_view"), Value: "visual"},
	}

	wishlistActionChoices := []*discordgo.ApplicationCommandOptionChoice{
		{Name: locale.Text("commands.gacha.view_wishlist"), Value: "wishes"},
		{Name: locale.Text("commands.gacha.add_a_character"), Value: "wish"},
		{Name: locale.Text("commands.gacha.remove_a_character"), Value: "unwish"},
	}

	SlashCommands = append(SlashCommands,
		// 1. /roll
		&discordgo.ApplicationCommand{
			Name:         "roll",
			Description:  locale.Text("commands.gacha.roll_an_anime_or_game_character_to"),
			DMPermission: &dm,
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionString,
					Name:        "pool",
					Description: locale.Text("commands.gacha.character_pool_female_male_anime_or_games"),
					Choices:     poolChoices,
				},
			},
		},

		// 2. /top
		&discordgo.ApplicationCommand{
			Name:         "top",
			Description:  locale.Text("commands.gacha.browse_the_most_popular_characters_in_the"),
			DMPermission: &dm,
			Options: []*discordgo.ApplicationCommandOption{
				{Type: discordgo.ApplicationCommandOptionString, Name: "gender", Description: locale.Text("commands.gacha.filter_by_character_gender"), Choices: genderChoices},
				{Type: discordgo.ApplicationCommandOptionString, Name: "claim", Description: locale.Text("commands.gacha.filter_by_ownership_in_this_server"), Choices: claimChoices},
				page(),
			},
		},

		// 3. /info
		&discordgo.ApplicationCommand{
			Name:         "info",
			Description:  locale.Text("commands.gacha.view_a_character_s_official_image_work"),
			DMPermission: &dm,
			Options: []*discordgo.ApplicationCommandOption{
				{Type: discordgo.ApplicationCommandOptionString, Name: "character", Description: locale.Text("commands.gacha.character_name_or_id"), Required: true},
			},
		},

		// 4. /harem
		&discordgo.ApplicationCommand{
			Name:         "harem",
			Description:  locale.Text("commands.gacha.browse_your_character_collection_or_another_member"),
			DMPermission: &dm,
			Options: []*discordgo.ApplicationCommandOption{
				member("member", locale.Text("commands.gacha.server_member_defaults_to_you"), false),
				{Type: discordgo.ApplicationCommandOptionString, Name: "mode", Description: locale.Text("commands.gacha.display_format_list_or_photos"), Choices: haremModeChoices},
				page(),
			},
		},

		// 5. /perfil
		&discordgo.ApplicationCommand{
			Name:         "profile",
			Description:  locale.Text("commands.gacha.view_remaining_rolls_claim_cooldown_and_gacha"),
			DMPermission: &dm,
		},

		// 6. /galeria
		&discordgo.ApplicationCommand{
			Name:         "gallery",
			Description:  locale.Text("commands.gacha.browse_a_character_s_approved_images"),
			DMPermission: &dm,
			Options: []*discordgo.ApplicationCommandOption{
				character("character", locale.Text("commands.discovery.character_id"), true),
				page(),
			},
		},

		// 7. /wishlist
		&discordgo.ApplicationCommand{
			Name:         "wishlist",
			Description:  locale.Text("commands.gacha.manage_your_character_wishlist"),
			DMPermission: &dm,
			Options: []*discordgo.ApplicationCommandOption{
				{Type: discordgo.ApplicationCommandOptionString, Name: "action", Description: locale.Text("commands.gacha.wishlist_action"), Required: false, Choices: wishlistActionChoices},
				{Type: discordgo.ApplicationCommandOptionString, Name: "character", Description: locale.Text("commands.gacha.character_id_required_to_add_or_remove"), Required: false},
			},
		},

		// /wish
		&discordgo.ApplicationCommand{
			Name:         "wish",
			Description:  locale.Text("commands.gacha.add_a_character_to_your_wishlist"),
			DMPermission: &dm,
			Options: []*discordgo.ApplicationCommandOption{
				{Type: discordgo.ApplicationCommandOptionString, Name: "character", Description: locale.Text("commands.gacha.character_name_or_id"), Required: true},
			},
		},

		// /unwish
		&discordgo.ApplicationCommand{
			Name:         "unwish",
			Description:  locale.Text("commands.gacha.remove_a_character_from_your_wishlist"),
			DMPermission: &dm,
			Options: []*discordgo.ApplicationCommandOption{
				{Type: discordgo.ApplicationCommandOptionString, Name: "character", Description: locale.Text("commands.gacha.character_name_or_id"), Required: true},
			},
		},

		// 8. /troca
		&discordgo.ApplicationCommand{
			Name:         "trade",
			Description:  locale.Text("commands.gacha.offer_a_character_trade_to_another_member"),
			DMPermission: &dm,
			Options: []*discordgo.ApplicationCommandOption{
				member("member", locale.Text("commands.gacha.member_to_trade_with"), true),
				character("offer", locale.Text("commands.gacha.id_of_the_character_you_offer"), true),
				character("receive", locale.Text("commands.gacha.id_of_the_character_you_want_in"), true),
			},
		},

		// 9. /presente
		&discordgo.ApplicationCommand{
			Name:         "gift",
			Description:  locale.Text("commands.gacha.offer_a_character_from_your_harem_as"),
			DMPermission: &dm,
			Options: []*discordgo.ApplicationCommandOption{
				member("member", locale.Text("commands.gacha.gift_recipient"), true),
				character("character", locale.Text("commands.gacha.id_of_the_character_you_want_to"), true),
			},
		},

		// 10. /divorcio
		&discordgo.ApplicationCommand{
			Name:         "divorce",
			Description:  locale.Text("commands.gacha.release_a_character_in_exchange_for_server"),
			DMPermission: &dm,
			Options: []*discordgo.ApplicationCommandOption{
				character("character", locale.Text("commands.gacha.id_of_the_character_to_release"), true),
			},
		},

		// 11. /alias
		&discordgo.ApplicationCommand{
			Name:         "alias",
			Description:  locale.Text("commands.gacha.change_alias_of_married_character"),
			DMPermission: &dm,
			Options: []*discordgo.ApplicationCommandOption{
				character("character", locale.Text("commands.gacha.id_or_name_of_the_character"), true),
				{
					Type:        discordgo.ApplicationCommandOptionString,
					Name:        "alias",
					Description: locale.Text("commands.gacha.alias_to_set_or_list"),
					Required:    false,
				},
			},
		},
	)
}
