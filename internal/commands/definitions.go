package commands

import (
	"bot/internal/locale"
	"github.com/bwmarrin/discordgo"
)

var minAmount float64 = 1.0

func ptr(f float64) *float64 {
	return &f
}

var SlashCommands = []*discordgo.ApplicationCommand{
	{
		Name:        "help",
		Description: locale.Text("commands.definitions.show_all_commands_and_features"),
	},
	{
		Name:        "daily",
		Description: locale.Text("commands.definitions.collect_your_daily_reward"),
	},
	{
		Name:        "balance",
		Description: locale.Text("commands.definitions.check_your_or_someone_else_s_balance"),
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionUser,
				Name:        "user",
				Description: locale.Text("commands.definitions.the_user_to_check"),
				Required:    false,
			},
		},
	},
	{
		Name:        "leaderboard",
		Description: locale.Text("commands.definitions.view_rankings_and_leaderboards"),
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "category",
				Description: locale.Text("commands.definitions.category_of_the_leaderboard"),
				Required:    false,
				Choices: []*discordgo.ApplicationCommandOptionChoice{
					{
						Name:  locale.Text("commands.definitions.net_worth"),
						Value: "networth",
					},
					{
						Name:  locale.Text("commands.definitions.wallet_balance"),
						Value: "wallet",
					},
					{
						Name:  locale.Text("commands.definitions.daily_streak"),
						Value: "streak",
					},
				},
			},
		},
	},
	{
		Name:        "pay",
		Description: locale.Text("commands.definitions.transfer_coins_to_another_user"),
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionUser,
				Name:        "user",
				Description: locale.Text("commands.definitions.recipient_of_the_coins"),
				Required:    true,
			},
			{
				Type:        discordgo.ApplicationCommandOptionInteger,
				Name:        "amount",
				Description: locale.Text("commands.definitions.amount_to_transfer"),
				Required:    true,
				MinValue:    &minAmount,
			},
		},
	},
	{
		Name:        "shop",
		Description: locale.Text("commands.definitions.view_available_items_in_the_shop"),
	},
	{
		Name:        "buy",
		Description: locale.Text("commands.definitions.buy_items_from_the_shop"),
		Options: []*discordgo.ApplicationCommandOption{
			{
				Name:        "nickname",
				Description: locale.Text("commands.definitions.change_your_own_nickname"),
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Options: []*discordgo.ApplicationCommandOption{
					{
						Type:        discordgo.ApplicationCommandOptionString,
						Name:        "new_name",
						Description: locale.Text("commands.definitions.the_new_nickname"),
						Required:    true,
					},
				},
			},
			{
				Name:        "rename",
				Description: locale.Text("commands.definitions.change_someone_else_s_nickname"),
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Options: []*discordgo.ApplicationCommandOption{
					{
						Type:        discordgo.ApplicationCommandOptionUser,
						Name:        "user",
						Description: locale.Text("commands.definitions.the_user_to_rename"),
						Required:    true,
					},
					{
						Type:        discordgo.ApplicationCommandOptionString,
						Name:        "new_name",
						Description: locale.Text("commands.definitions.the_new_nickname"),
						Required:    true,
					},
				},
			},
			{
				Name:        "timeout",
				Description: locale.Text("commands.definitions.timeout_a_user_from_text_and_voice"),
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Options: []*discordgo.ApplicationCommandOption{
					{
						Type:        discordgo.ApplicationCommandOptionUser,
						Name:        "user",
						Description: locale.Text("commands.definitions.the_user_to_timeout"),
						Required:    true,
					},
					{
						Type:        discordgo.ApplicationCommandOptionInteger,
						Name:        "minutes",
						Description: locale.Text("commands.definitions.duration_in_minutes"),
						Required:    true,
						MinValue:    &minAmount,
						MaxValue:    1440.0,
					},
				},
			},
			{
				Name:        "mute",
				Description: locale.Text("commands.definitions.mute_a_user_in_voice_calls"),
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Options: []*discordgo.ApplicationCommandOption{
					{
						Type:        discordgo.ApplicationCommandOptionUser,
						Name:        "user",
						Description: locale.Text("commands.definitions.the_user_to_voice_mute"),
						Required:    true,
					},
					{
						Type:        discordgo.ApplicationCommandOptionInteger,
						Name:        "minutes",
						Description: locale.Text("commands.definitions.duration_in_minutes"),
						Required:    true,
						MinValue:    &minAmount,
						MaxValue:    1440.0,
					},
				},
			},
		},
	},
	{
		Name:        "apikey",
		Description: locale.Text("commands.definitions.manage_your_api_keys"),
		Options: []*discordgo.ApplicationCommandOption{
			{
				Name:        "create",
				Description: locale.Text("commands.definitions.create_a_new_api_key_sent_via"),
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Options: []*discordgo.ApplicationCommandOption{
					{
						Type:        discordgo.ApplicationCommandOptionString,
						Name:        "name",
						Description: locale.Text("commands.definitions.optional_name_for_the_key"),
						Required:    false,
					},
				},
			},
			{
				Name:        "list",
				Description: locale.Text("commands.definitions.list_your_active_api_keys"),
				Type:        discordgo.ApplicationCommandOptionSubCommand,
			},
			{
				Name:        "delete",
				Description: locale.Text("commands.definitions.delete_an_api_key"),
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Options: []*discordgo.ApplicationCommandOption{
					{
						Type:        discordgo.ApplicationCommandOptionString,
						Name:        "prefix",
						Description: locale.Text("commands.definitions.the_first_few_characters_of_the_key"),
						Required:    true,
					},
				},
			},
		},
	},
	{
		Name:        "webhook",
		Description: locale.Text("commands.definitions.manage_your_webhook_for_api_notifications"),
		Options: []*discordgo.ApplicationCommandOption{
			{
				Name:        "set",
				Description: locale.Text("commands.definitions.set_your_webhook_url"),
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Options: []*discordgo.ApplicationCommandOption{
					{
						Type:        discordgo.ApplicationCommandOptionString,
						Name:        "url",
						Description: locale.Text("commands.definitions.the_url_to_receive_post_requests"),
						Required:    true,
					},
				},
			},
			{
				Name:        "test",
				Description: locale.Text("commands.definitions.send_a_test_payload_to_your_configured"),
				Type:        discordgo.ApplicationCommandOptionSubCommand,
			},
			{
				Name:        "delete",
				Description: locale.Text("commands.definitions.remove_your_webhook_configuration"),
				Type:        discordgo.ApplicationCommandOptionSubCommand,
			},
		},
	},
	{
		Name:        "bet",
		Description: locale.Text("commands.definitions.play_casino_games"),
		Options: []*discordgo.ApplicationCommandOption{
			{
				Name:        "aviator",
				Description: locale.Text("commands.definitions.play_the_aviator_crash_game"),
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Options: []*discordgo.ApplicationCommandOption{
					{
						Type:        discordgo.ApplicationCommandOptionInteger,
						Name:        "amount",
						Description: locale.Text("commands.definitions.amount_to_bet_min"),
						Required:    true,
						MinValue:    &minAmount,
					},
					{
						Type:        discordgo.ApplicationCommandOptionNumber,
						Name:        "auto_cashout",
						Description: locale.Text("commands.definitions.optional_target_multiplier_to_automatically_cash_out"),
						Required:    false,
					},
				},
			},
			{
				Name:        "cups",
				Description: locale.Text("commands.definitions.play_the_cup_game_double_or_nothing"),
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Options: []*discordgo.ApplicationCommandOption{
					{
						Type:        discordgo.ApplicationCommandOptionInteger,
						Name:        "amount",
						Description: locale.Text("commands.definitions.amount_to_bet_min_fb12ba"),
						Required:    true,
						MinValue:    &minAmount,
					},
				},
			},
			{
				Name:        "slots",
				Description: locale.Text("commands.definitions.play_the_slot_machine"),
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Options: []*discordgo.ApplicationCommandOption{
					{
						Type:        discordgo.ApplicationCommandOptionInteger,
						Name:        "amount",
						Description: locale.Text("commands.definitions.amount_to_bet_min_a2c68e"),
						Required:    true,
						MinValue:    ptr(float64(10)),
					},
				},
			},
			{
				Name:        "mines",
				Description: locale.Text("commands.definitions.play_the_mines_casino_game"),
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Options: []*discordgo.ApplicationCommandOption{
					{
						Type:        discordgo.ApplicationCommandOptionInteger,
						Name:        "amount",
						Description: locale.Text("commands.definitions.amount_to_bet_min_a2c68e"),
						Required:    true,
						MinValue:    ptr(float64(10)),
					},
					{
						Type:        discordgo.ApplicationCommandOptionInteger,
						Name:        "mines",
						Description: locale.Text("commands.definitions.number_of_mines_on_the_board_to"),
						Required:    false,
						MinValue:    ptr(float64(1)),
						MaxValue:    float64(19),
					},
				},
			},
		},
	},
	{
		Name:        "slots",
		Description: locale.Text("commands.definitions.play_the_slot_machine"),
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionInteger,
				Name:        "amount",
				Description: locale.Text("commands.definitions.amount_to_bet_min_a2c68e"),
				Required:    true,
				MinValue:    ptr(float64(10)),
			},
		},
	},
	{
		Name:        "blackjack",
		Description: locale.Text("commands.definitions.play_a_game_of_blackjack"),
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionInteger,
				Name:        "bet",
				Description: locale.Text("commands.definitions.amount_to_bet_min_a2c68e"),
				Required:    true,
				MinValue:    ptr(float64(10)),
			},
		},
	},
	{
		Name:        "mines",
		Description: locale.Text("commands.definitions.play_the_mines_casino_game"),
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionInteger,
				Name:        "bet",
				Description: locale.Text("commands.definitions.amount_to_bet_min_a2c68e"),
				Required:    true,
				MinValue:    ptr(float64(10)),
			},
			{
				Type:        discordgo.ApplicationCommandOptionInteger,
				Name:        "mines",
				Description: locale.Text("commands.definitions.number_of_mines_on_the_board_to"),
				Required:    false,
				MinValue:    ptr(float64(1)),
				MaxValue:    float64(19),
			},
		},
	},
	{
		Name:        "wheel",
		Description: locale.Text("commands.definitions.casino_european_roulette"),
		Options: []*discordgo.ApplicationCommandOption{
			{
				Name:        "status",
				Description: locale.Text("commands.definitions.check_time_until_next_spin_and_active"),
				Type:        discordgo.ApplicationCommandOptionSubCommand,
			},
			{
				Name:        "bet",
				Description: locale.Text("commands.definitions.place_a_bet_on_the_upcoming_roulette"),
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Options: []*discordgo.ApplicationCommandOption{
					{
						Type:        discordgo.ApplicationCommandOptionString,
						Name:        "type",
						Description: locale.Text("commands.definitions.bet_type_number_red_black_even_odd"),
						Required:    true,
						Choices: []*discordgo.ApplicationCommandOptionChoice{
							{Name: locale.Text("commands.definitions.red"), Value: "red"},
							{Name: locale.Text("commands.definitions.black"), Value: "black"},
							{Name: locale.Text("commands.definitions.even"), Value: "even"},
							{Name: locale.Text("commands.definitions.odd"), Value: "odd"},
							{Name: locale.Text("commands.definitions.low"), Value: "low"},
							{Name: locale.Text("commands.definitions.high"), Value: "high"},
							{Name: locale.Text("commands.definitions.st_dozen"), Value: "1st"},
							{Name: locale.Text("commands.definitions.nd_dozen"), Value: "2nd"},
							{Name: locale.Text("commands.definitions.rd_dozen"), Value: "3rd"},
							{Name: locale.Text("commands.definitions.specific_number"), Value: "number"},
						},
					},
					{
						Type:        discordgo.ApplicationCommandOptionInteger,
						Name:        "amount",
						Description: locale.Text("commands.definitions.amount_to_bet_min_fb12ba"),
						Required:    true,
						MinValue:    ptr(float64(50)),
					},
					{
						Type:        discordgo.ApplicationCommandOptionInteger,
						Name:        "number",
						Description: locale.Text("commands.definitions.number_to_bet_on_only_required_if"),
						Required:    false,
						MinValue:    ptr(float64(0)),
						MaxValue:    36,
					},
				},
			},
		},
	},
	{
		Name:        "roulette",
		Description: locale.Text("commands.definitions.russian_roulette_pvp_duel"),
		Options: []*discordgo.ApplicationCommandOption{
			{
				Name:        "challenge",
				Description: locale.Text("commands.definitions.challenge_another_user_to_russian_roulette_winner"),
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Options: []*discordgo.ApplicationCommandOption{
					{
						Type:        discordgo.ApplicationCommandOptionUser,
						Name:        "user",
						Description: locale.Text("commands.definitions.user_to_challenge"),
						Required:    true,
					},
					{
						Type:        discordgo.ApplicationCommandOptionInteger,
						Name:        "amount",
						Description: locale.Text("commands.definitions.amount_to_bet_min_fb12ba"),
						Required:    true,
						MinValue:    ptr(float64(50)),
					},
				},
			},
		},
	},
	{
		Name:        "loan",
		Description: locale.Text("commands.definitions.loan_system_lend_or_borrow_money"),
		Options: []*discordgo.ApplicationCommandOption{
			{
				Name:        "offer",
				Description: locale.Text("commands.definitions.offer_a_loan_to_another_user"),
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Options: []*discordgo.ApplicationCommandOption{
					{
						Type:        discordgo.ApplicationCommandOptionUser,
						Name:        "user",
						Description: locale.Text("commands.definitions.the_user_to_lend_money_to"),
						Required:    true,
					},
					{
						Type:        discordgo.ApplicationCommandOptionInteger,
						Name:        "amount",
						Description: locale.Text("commands.definitions.amount_to_lend"),
						Required:    true,
						MinValue:    &minAmount,
					},
					{
						Type:        discordgo.ApplicationCommandOptionNumber,
						Name:        "interest",
						Description: locale.Text("commands.definitions.interest_rate_percentage"),
						Required:    true,
						MinValue:    ptr(0.0),
						MaxValue:    100.0,
					},
					{
						Type:        discordgo.ApplicationCommandOptionInteger,
						Name:        "days",
						Description: locale.Text("commands.definitions.days_until_payment_is_due"),
						Required:    true,
						MinValue:    ptr(1.0),
						MaxValue:    365.0,
					},
				},
			},
			{
				Name:        "pay",
				Description: locale.Text("commands.definitions.pay_an_active_loan"),
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Options: []*discordgo.ApplicationCommandOption{
					{
						Type:        discordgo.ApplicationCommandOptionString,
						Name:        "loan_id",
						Description: locale.Text("commands.definitions.the_loan_id_to_pay_optional_pays"),
						Required:    false,
					},
				},
			},
			{
				Name:        "list",
				Description: locale.Text("commands.definitions.list_your_active_loans"),
				Type:        discordgo.ApplicationCommandOptionSubCommand,
			},
		},
	},
	{
		Name:        "stock",
		Description: locale.Text("commands.definitions.stock_market_trading_commands"),
		Options: []*discordgo.ApplicationCommandOption{
			{
				Name:        "market",
				Description: locale.Text("commands.definitions.view_current_stock_market_prices"),
				Type:        discordgo.ApplicationCommandOptionSubCommand,
			},
			{
				Name:        "buy",
				Description: locale.Text("commands.definitions.buy_shares_of_a_company"),
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Options: []*discordgo.ApplicationCommandOption{
					{
						Type:        discordgo.ApplicationCommandOptionString,
						Name:        "ticker",
						Description: locale.Text("commands.definitions.company_ticker_symbol_e_g_nvda_aapl"),
						Required:    true,
					},
					{
						Type:        discordgo.ApplicationCommandOptionInteger,
						Name:        "amount",
						Description: locale.Text("commands.definitions.amount_to_invest"),
						Required:    true,
						MinValue:    &minAmount,
					},
				},
			},
			{
				Name:        "sell",
				Description: locale.Text("commands.definitions.sell_shares_of_a_company"),
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Options: []*discordgo.ApplicationCommandOption{
					{
						Type:        discordgo.ApplicationCommandOptionString,
						Name:        "ticker",
						Description: locale.Text("commands.definitions.company_ticker_symbol_e_g_nvda_aapl"),
						Required:    true,
					},
					{
						Type:        discordgo.ApplicationCommandOptionString,
						Name:        "shares",
						Description: locale.Text("commands.definitions.number_of_shares_to_sell_or_all"),
						Required:    true,
					},
				},
			},
			{
				Name:        "portfolio",
				Description: locale.Text("commands.definitions.view_your_stock_portfolio_cost_basis_and"),
				Type:        discordgo.ApplicationCommandOptionSubCommand,
			},
		},
	},
	{
		Name:        "crypto",
		Description: locale.Text("commands.definitions.cryptocurrency_trading_commands"),
		Options: []*discordgo.ApplicationCommandOption{
			{
				Name:        "market",
				Description: locale.Text("commands.definitions.view_current_crypto_prices"),
				Type:        discordgo.ApplicationCommandOptionSubCommand,
			},
			{
				Name:        "buy",
				Description: locale.Text("commands.definitions.buy_cryptocurrency"),
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Options: []*discordgo.ApplicationCommandOption{
					{
						Type:        discordgo.ApplicationCommandOptionString,
						Name:        "symbol",
						Description: locale.Text("commands.definitions.crypto_symbol_e_g_btc_eth_sol"),
						Required:    true,
					},
					{
						Type:        discordgo.ApplicationCommandOptionInteger,
						Name:        "amount",
						Description: locale.Text("commands.definitions.amount_to_invest"),
						Required:    true,
						MinValue:    &minAmount,
					},
				},
			},
			{
				Name:        "sell",
				Description: locale.Text("commands.definitions.sell_cryptocurrency"),
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Options: []*discordgo.ApplicationCommandOption{
					{
						Type:        discordgo.ApplicationCommandOptionString,
						Name:        "symbol",
						Description: locale.Text("commands.definitions.crypto_symbol_e_g_btc_eth_sol"),
						Required:    true,
					},
					{
						Type:        discordgo.ApplicationCommandOptionString,
						Name:        "amount",
						Description: locale.Text("commands.definitions.coins_amount_to_sell_or_all"),
						Required:    true,
					},
				},
			},
			{
				Name:        "portfolio",
				Description: locale.Text("commands.definitions.view_your_crypto_portfolio_cost_basis_and"),
				Type:        discordgo.ApplicationCommandOptionSubCommand,
			},
		},
	},
	{
		Name:        "poly",
		Description: locale.Text("commands.definitions.polymarket_prediction_markets_trade_shares_on_real"),
		Options: []*discordgo.ApplicationCommandOption{
			{
				Name:        "trending",
				Description: locale.Text("commands.definitions.view_top_volume_trending_markets_on_polymarket"),
				Type:        discordgo.ApplicationCommandOptionSubCommand,
			},
			{
				Name:        "search",
				Description: locale.Text("commands.definitions.search_for_real_world_prediction_markets_on"),
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Options: []*discordgo.ApplicationCommandOption{
					{
						Type:        discordgo.ApplicationCommandOptionString,
						Name:        "query",
						Description: locale.Text("commands.definitions.search_keywords_e_g_bitcoin_trump_champions"),
						Required:    true,
					},
				},
			},
			{
				Name:        "import",
				Description: locale.Text("commands.definitions.import_a_polymarket_event_into_the_server"),
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Options: []*discordgo.ApplicationCommandOption{
					{
						Type:        discordgo.ApplicationCommandOptionString,
						Name:        "query",
						Description: locale.Text("commands.definitions.polymarket_slug_id_or_full_url"),
						Required:    true,
					},
					{
						Type:        discordgo.ApplicationCommandOptionString,
						Name:        "candidate",
						Description: locale.Text("commands.definitions.candidate_or_option_name_e_g_lula"),
						Required:    false,
					},
				},
			},
			{
				Name:        "suggest",
				Description: locale.Text("commands.definitions.suggest_a_polymarket_event_for_admin_approval"),
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Options: []*discordgo.ApplicationCommandOption{
					{
						Type:        discordgo.ApplicationCommandOptionString,
						Name:        "query",
						Description: locale.Text("commands.definitions.polymarket_slug_id_or_full_url"),
						Required:    true,
					},
					{
						Type:        discordgo.ApplicationCommandOptionString,
						Name:        "candidate",
						Description: locale.Text("commands.definitions.candidate_or_option_name_e_g_lula"),
						Required:    false,
					},
				},
			},
			{
				Name:        "cancel",
				Description: locale.Text("commands.definitions.cancel_an_imported_market_and_refund_all"),
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Options: []*discordgo.ApplicationCommandOption{
					{
						Type:        discordgo.ApplicationCommandOptionString,
						Name:        "market_id",
						Description: locale.Text("commands.definitions.market_id_e_g_poly_or"),
						Required:    true,
					},
				},
			},
			{
				Name:        "view",
				Description: locale.Text("commands.definitions.view_details_and_live_odds_for_an"),
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Options: []*discordgo.ApplicationCommandOption{
					{
						Type:        discordgo.ApplicationCommandOptionString,
						Name:        "id",
						Description: locale.Text("commands.definitions.market_id_e_g_poly"),
						Required:    true,
					},
				},
			},
			{
				Name:        "portfolio",
				Description: locale.Text("commands.definitions.view_your_polymarket_shares_positions_and_unrealized"),
				Type:        discordgo.ApplicationCommandOptionSubCommand,
			},
			{
				Name:        "config",
				Description: locale.Text("commands.definitions.configure_polymarket_settings_admin_only"),
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Options: []*discordgo.ApplicationCommandOption{
					{
						Type:        discordgo.ApplicationCommandOptionChannel,
						Name:        "channel",
						Description: locale.Text("commands.definitions.dedicated_channel_for_polymarket_prediction_markets"),
						Required:    false,
					},
					{
						Type:        discordgo.ApplicationCommandOptionString,
						Name:        "import_mode",
						Description: locale.Text("commands.definitions.who_can_import_markets"),
						Required:    false,
						Choices: []*discordgo.ApplicationCommandOptionChoice{
							{Name: locale.Text("commands.definitions.admins_only"), Value: "admin_only"},
							{Name: locale.Text("commands.definitions.all_users"), Value: "all_users"},
						},
					},
					{
						Type:        discordgo.ApplicationCommandOptionNumber,
						Name:        "house_edge",
						Description: locale.Text("commands.definitions.house_edge_fee_percentage_e_g_for"),
						Required:    false,
					},
				},
			},
		},
	},
	{
		Name:        "bicho",
		Description: locale.Text("commands.definitions.daily_brazilian_animal_lottery_jogo_do_bicho"),
		Options: []*discordgo.ApplicationCommandOption{
			{
				Name:        "panel",
				Description: locale.Text("commands.definitions.display_the_active_round_panel_with_betting"),
				Type:        discordgo.ApplicationCommandOptionSubCommand,
			},
			{
				Name:        "table",
				Description: locale.Text("commands.definitions.view_the_table_of_animals_groups_and"),
				Type:        discordgo.ApplicationCommandOptionSubCommand,
			},
			{
				Name:        "my-bets",
				Description: locale.Text("commands.definitions.view_your_active_tickets_in_the_current"),
				Type:        discordgo.ApplicationCommandOptionSubCommand,
			},
			{
				Name:        "bet",
				Description: locale.Text("commands.definitions.place_a_bet_in_jogo_do_bicho"),
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Options: []*discordgo.ApplicationCommandOption{
					{
						Type:        discordgo.ApplicationCommandOptionString,
						Name:        "modality",
						Description: locale.Text("commands.definitions.betting_modality"),
						Required:    true,
						Choices: []*discordgo.ApplicationCommandOptionChoice{
							{Name: locale.Text("commands.definitions.group_x_head_x_board"), Value: "group"},
							{Name: locale.Text("commands.definitions.tens_x_head_x_board"), Value: "tens"},
							{Name: locale.Text("commands.definitions.hundreds_x_head_x_board"), Value: "hundreds"},
							{Name: locale.Text("commands.definitions.thousands_x_head_x_board"), Value: "thousands"},
							{Name: locale.Text("commands.definitions.animal_pair_x"), Value: "pair"},
							{Name: locale.Text("commands.definitions.animal_trio_x"), Value: "trio"},
						},
					},
					{
						Type:        discordgo.ApplicationCommandOptionString,
						Name:        "target",
						Description: locale.Text("commands.definitions.animal_or_number_e_g_monkey_or"),
						Required:    true,
					},
					{
						Type:        discordgo.ApplicationCommandOptionInteger,
						Name:        "amount",
						Description: locale.Text("commands.definitions.amount_to_bet"),
						Required:    true,
					},
					{
						Type:        discordgo.ApplicationCommandOptionString,
						Name:        "position",
						Description: locale.Text("commands.definitions.head_st_prize_or_board_st_to"),
						Required:    false,
						Choices: []*discordgo.ApplicationCommandOptionChoice{
							{Name: locale.Text("commands.bicho_cmd.head_st_prize"), Value: "head"},
							{Name: locale.Text("commands.bicho_cmd.board_st_to_th_prizes"), Value: "board"},
						},
					},
				},
			},
			{
				Name:        "draw",
				Description: locale.Text("commands.definitions.trigger_the_lottery_draw_immediately_admin_only"),
				Type:        discordgo.ApplicationCommandOptionSubCommand,
			},
			{
				Name:        "config",
				Description: locale.Text("commands.definitions.configure_lottery_channel_and_daily_schedule_admin"),
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Options: []*discordgo.ApplicationCommandOption{
					{
						Type:        discordgo.ApplicationCommandOptionChannel,
						Name:        "channel",
						Description: locale.Text("commands.definitions.dedicated_channel_for_jogo_do_bicho"),
						Required:    false,
					},
					{
						Type:        discordgo.ApplicationCommandOptionInteger,
						Name:        "hour",
						Description: locale.Text("commands.definitions.daily_draw_hour_to"),
						Required:    false,
					},
					{
						Type:        discordgo.ApplicationCommandOptionInteger,
						Name:        "minute",
						Description: locale.Text("commands.definitions.daily_draw_minute_to"),
						Required:    false,
					},
					{
						Type:        discordgo.ApplicationCommandOptionInteger,
						Name:        "min_bet",
						Description: locale.Text("commands.definitions.minimum_bet_amount_allowed"),
						Required:    false,
					},
					{
						Type:        discordgo.ApplicationCommandOptionBoolean,
						Name:        "enabled",
						Description: locale.Text("commands.definitions.enable_or_disable_jogo_do_bicho"),
						Required:    false,
					},
				},
			},
		},
	},
}
