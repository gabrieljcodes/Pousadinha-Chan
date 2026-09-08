package commands

import "github.com/bwmarrin/discordgo"

var minAmount float64 = 1.0

func ptr(f float64) *float64 {
	return &f
}

var SlashCommands = []*discordgo.ApplicationCommand{
	{
		Name:        "help",
		Description: "Show all commands and features",
	},
	{
		Name:        "daily",
		Description: "Collect your daily reward",
	},
	{
		Name:        "balance",
		Description: "Check your or someone else's balance",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionUser,
				Name:        "user",
				Description: "The user to check",
				Required:    false,
			},
		},
	},
	{
		Name:        "leaderboard",
		Description: "View rankings and leaderboards",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "category",
				Description: "Category of the leaderboard",
				Required:    false,
				Choices: []*discordgo.ApplicationCommandOptionChoice{
					{
						Name:  "Patrimônio Geral (Net Worth)",
						Value: "networth",
					},
					{
						Name:  "Saldo em Carteira (Wallet)",
						Value: "wallet",
					},
					{
						Name:  "Sequência Diária (Daily Streak)",
						Value: "streak",
					},
				},
			},
		},
	},
	{
		Name:        "pay",
		Description: "Transfer EstudoCoins to another user",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionUser,
				Name:        "user",
				Description: "Recipient of the coins",
				Required:    true,
			},
			{
				Type:        discordgo.ApplicationCommandOptionInteger,
				Name:        "amount",
				Description: "Amount to transfer",
				Required:    true,
				MinValue:    &minAmount,
			},
		},
	},
	{
		Name:        "shop",
		Description: "View available items in the shop",
	},
	{
		Name:        "buy",
		Description: "Buy items from the shop",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Name:        "nickname",
				Description: "Change your own nickname",
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Options: []*discordgo.ApplicationCommandOption{
					{
						Type:        discordgo.ApplicationCommandOptionString,
						Name:        "new_name",
						Description: "The new nickname",
						Required:    true,
					},
				},
			},
			{
				Name:        "rename",
				Description: "Change someone else's nickname",
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Options: []*discordgo.ApplicationCommandOption{
					{
						Type:        discordgo.ApplicationCommandOptionUser,
						Name:        "user",
						Description: "The user to rename",
						Required:    true,
					},
					{
						Type:        discordgo.ApplicationCommandOptionString,
						Name:        "new_name",
						Description: "The new nickname",
						Required:    true,
					},
				},
			},
			{
				Name:        "timeout",
				Description: "Timeout a user from text and voice channels",
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Options: []*discordgo.ApplicationCommandOption{
					{
						Type:        discordgo.ApplicationCommandOptionUser,
						Name:        "user",
						Description: "The user to timeout",
						Required:    true,
					},
					{
						Type:        discordgo.ApplicationCommandOptionInteger,
						Name:        "minutes",
						Description: "Duration in minutes (1-1440)",
						Required:    true,
						MinValue:    &minAmount,
						MaxValue:    1440.0,
					},
				},
			},
			{
				Name:        "mute",
				Description: "Mute a user in voice calls",
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Options: []*discordgo.ApplicationCommandOption{
					{
						Type:        discordgo.ApplicationCommandOptionUser,
						Name:        "user",
						Description: "The user to voice mute",
						Required:    true,
					},
					{
						Type:        discordgo.ApplicationCommandOptionInteger,
						Name:        "minutes",
						Description: "Duration in minutes (1-1440)",
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
		Description: "Manage your API Keys",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Name:        "create",
				Description: "Create a new API Key (Sent via DM)",
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Options: []*discordgo.ApplicationCommandOption{
					{
						Type:        discordgo.ApplicationCommandOptionString,
						Name:        "name",
						Description: "Optional name for the key",
						Required:    false,
					},
				},
			},
			{
				Name:        "list",
				Description: "List your active API Keys",
				Type:        discordgo.ApplicationCommandOptionSubCommand,
			},
			{
				Name:        "delete",
				Description: "Delete an API Key",
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Options: []*discordgo.ApplicationCommandOption{
					{
						Type:        discordgo.ApplicationCommandOptionString,
						Name:        "prefix",
						Description: "The first few characters of the key to delete",
						Required:    true,
					},
				},
			},
		},
	},
	{
		Name:        "webhook",
		Description: "Manage your Webhook for API notifications",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Name:        "set",
				Description: "Set your webhook URL",
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Options: []*discordgo.ApplicationCommandOption{
					{
						Type:        discordgo.ApplicationCommandOptionString,
						Name:        "url",
						Description: "The URL to receive POST requests",
						Required:    true,
					},
				},
			},
			{
				Name:        "test",
				Description: "Send a test payload to your configured webhook",
				Type:        discordgo.ApplicationCommandOptionSubCommand,
			},
			{
				Name:        "delete",
				Description: "Remove your webhook configuration",
				Type:        discordgo.ApplicationCommandOptionSubCommand,
			},
		},
	},
	{
		Name:        "bet",
		Description: "Play casino games",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Name:        "aviator",
				Description: "Play the Aviator crash game",
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Options: []*discordgo.ApplicationCommandOption{
					{
						Type:        discordgo.ApplicationCommandOptionInteger,
						Name:        "amount",
						Description: "Amount to bet (Min 100)",
						Required:    true,
						MinValue:    &minAmount,
					},
					{
						Type:        discordgo.ApplicationCommandOptionNumber,
						Name:        "auto_cashout",
						Description: "Optional target multiplier to automatically cash out (e.g. 2.0)",
						Required:    false,
					},
				},
			},
			{
				Name:        "cups",
				Description: "Play the Cup Game (Double or Nothing)",
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Options: []*discordgo.ApplicationCommandOption{
					{
						Type:        discordgo.ApplicationCommandOptionInteger,
						Name:        "amount",
						Description: "Amount to bet (Min 50)",
						Required:    true,
						MinValue:    &minAmount,
					},
				},
			},
			{
				Name:        "slots",
				Description: "Play the Slot Machine",
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Options: []*discordgo.ApplicationCommandOption{
					{
						Type:        discordgo.ApplicationCommandOptionInteger,
						Name:        "amount",
						Description: "Amount to bet (Min 10)",
						Required:    true,
						MinValue:    ptr(float64(10)),
					},
				},
			},
			{
				Name:        "mines",
				Description: "Play the Mines casino game (Campo Minado)",
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Options: []*discordgo.ApplicationCommandOption{
					{
						Type:        discordgo.ApplicationCommandOptionInteger,
						Name:        "amount",
						Description: "Amount to bet (Min 10)",
						Required:    true,
						MinValue:    ptr(float64(10)),
					},
					{
						Type:        discordgo.ApplicationCommandOptionInteger,
						Name:        "mines",
						Description: "Number of mines on the board (1 to 19, default 3)",
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
		Description: "Play the Slot Machine",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionInteger,
				Name:        "amount",
				Description: "Amount to bet (Min 10)",
				Required:    true,
				MinValue:    ptr(float64(10)),
			},
		},
	},
	{
		Name:        "blackjack",
		Description: "Play a game of Blackjack",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionInteger,
				Name:        "bet",
				Description: "Amount to bet (Min 10)",
				Required:    true,
				MinValue:    ptr(float64(10)),
			},
		},
	},
	{
		Name:        "mines",
		Description: "Play the Mines casino game (Campo Minado)",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionInteger,
				Name:        "bet",
				Description: "Amount to bet (Min 10)",
				Required:    true,
				MinValue:    ptr(float64(10)),
			},
			{
				Type:        discordgo.ApplicationCommandOptionInteger,
				Name:        "mines",
				Description: "Number of mines on the board (1 to 19, default 3)",
				Required:    false,
				MinValue:    ptr(float64(1)),
				MaxValue:    float64(19),
			},
		},
	},
	{
		Name:        "wheel",
		Description: "Casino European Roulette",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Name:        "status",
				Description: "Check time until next spin and active round stats",
				Type:        discordgo.ApplicationCommandOptionSubCommand,
			},
			{
				Name:        "bet",
				Description: "Place a bet on the upcoming roulette spin",
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Options: []*discordgo.ApplicationCommandOption{
					{
						Type:        discordgo.ApplicationCommandOptionString,
						Name:        "type",
						Description: "Bet type (number, red, black, even, odd, low, high, dozen)",
						Required:    true,
						Choices: []*discordgo.ApplicationCommandOptionChoice{
							{Name: "🔴 Red (1:1)", Value: "red"},
							{Name: "⚫ Black (1:1)", Value: "black"},
							{Name: "Even / Par (1:1)", Value: "even"},
							{Name: "Odd / Ímpar (1:1)", Value: "odd"},
							{Name: "Low 1-18 (1:1)", Value: "low"},
							{Name: "High 19-36 (1:1)", Value: "high"},
							{Name: "1st Dozen 1-12 (2:1)", Value: "1st"},
							{Name: "2nd Dozen 13-24 (2:1)", Value: "2nd"},
							{Name: "3rd Dozen 25-36 (2:1)", Value: "3rd"},
							{Name: "Specific Number 0-36 (35:1)", Value: "number"},
						},
					},
					{
						Type:        discordgo.ApplicationCommandOptionInteger,
						Name:        "amount",
						Description: "Amount to bet (Min 50)",
						Required:    true,
						MinValue:    ptr(float64(50)),
					},
					{
						Type:        discordgo.ApplicationCommandOptionInteger,
						Name:        "number",
						Description: "Number to bet on (0-36, only required if type is number)",
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
		Description: "Russian Roulette PvP duel",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Name:        "challenge",
				Description: "Challenge another user to Russian Roulette (Winner takes all)",
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Options: []*discordgo.ApplicationCommandOption{
					{
						Type:        discordgo.ApplicationCommandOptionUser,
						Name:        "user",
						Description: "User to challenge",
						Required:    true,
					},
					{
						Type:        discordgo.ApplicationCommandOptionInteger,
						Name:        "amount",
						Description: "Amount to bet (Min 50)",
						Required:    true,
						MinValue:    ptr(float64(50)),
					},
				},
			},
		},
	},
	{
		Name:        "loan",
		Description: "Loan system - Lend or borrow money",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Name:        "offer",
				Description: "Offer a loan to another user",
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Options: []*discordgo.ApplicationCommandOption{
					{
						Type:        discordgo.ApplicationCommandOptionUser,
						Name:        "user",
						Description: "The user to lend money to",
						Required:    true,
					},
					{
						Type:        discordgo.ApplicationCommandOptionInteger,
						Name:        "amount",
						Description: "Amount to lend",
						Required:    true,
						MinValue:    &minAmount,
					},
					{
						Type:        discordgo.ApplicationCommandOptionNumber,
						Name:        "interest",
						Description: "Interest rate percentage (0-100)",
						Required:    true,
						MinValue:    ptr(0.0),
						MaxValue:    100.0,
					},
					{
						Type:        discordgo.ApplicationCommandOptionInteger,
						Name:        "days",
						Description: "Days until payment is due (1-365)",
						Required:    true,
						MinValue:    ptr(1.0),
						MaxValue:    365.0,
					},
				},
			},
			{
				Name:        "pay",
				Description: "Pay an active loan",
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Options: []*discordgo.ApplicationCommandOption{
					{
						Type:        discordgo.ApplicationCommandOptionString,
						Name:        "loan_id",
						Description: "The loan ID to pay (optional - pays oldest if not specified)",
						Required:    false,
					},
				},
			},
			{
				Name:        "list",
				Description: "List your active loans",
				Type:        discordgo.ApplicationCommandOptionSubCommand,
			},
		},
	},
	{
		Name:        "stock",
		Description: "Stock market trading commands",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Name:        "market",
				Description: "View current stock market prices",
				Type:        discordgo.ApplicationCommandOptionSubCommand,
			},
			{
				Name:        "buy",
				Description: "Buy shares of a company",
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Options: []*discordgo.ApplicationCommandOption{
					{
						Type:        discordgo.ApplicationCommandOptionString,
						Name:        "ticker",
						Description: "Company ticker symbol (e.g., NVDA, AAPL, BTC)",
						Required:    true,
					},
					{
						Type:        discordgo.ApplicationCommandOptionInteger,
						Name:        "amount",
						Description: "Amount of EC to invest",
						Required:    true,
						MinValue:    &minAmount,
					},
				},
			},
			{
				Name:        "sell",
				Description: "Sell shares of a company",
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Options: []*discordgo.ApplicationCommandOption{
					{
						Type:        discordgo.ApplicationCommandOptionString,
						Name:        "ticker",
						Description: "Company ticker symbol (e.g., NVDA, AAPL, BTC)",
						Required:    true,
					},
					{
						Type:        discordgo.ApplicationCommandOptionString,
						Name:        "shares",
						Description: "Number of shares to sell or 'all'",
						Required:    true,
					},
				},
			},
			{
				Name:        "portfolio",
				Description: "View your stock portfolio, cost basis, and returns",
				Type:        discordgo.ApplicationCommandOptionSubCommand,
			},
		},
	},
	{
		Name:        "crypto",
		Description: "Cryptocurrency trading commands",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Name:        "market",
				Description: "View current crypto prices",
				Type:        discordgo.ApplicationCommandOptionSubCommand,
			},
			{
				Name:        "buy",
				Description: "Buy cryptocurrency",
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Options: []*discordgo.ApplicationCommandOption{
					{
						Type:        discordgo.ApplicationCommandOptionString,
						Name:        "symbol",
						Description: "Crypto symbol (e.g., BTC, ETH, SOL, DOGE)",
						Required:    true,
					},
					{
						Type:        discordgo.ApplicationCommandOptionInteger,
						Name:        "amount",
						Description: "Amount of EC to invest",
						Required:    true,
						MinValue:    &minAmount,
					},
				},
			},
			{
				Name:        "sell",
				Description: "Sell cryptocurrency",
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Options: []*discordgo.ApplicationCommandOption{
					{
						Type:        discordgo.ApplicationCommandOptionString,
						Name:        "symbol",
						Description: "Crypto symbol (e.g., BTC, ETH, SOL, DOGE)",
						Required:    true,
					},
					{
						Type:        discordgo.ApplicationCommandOptionString,
						Name:        "amount",
						Description: "Coins amount to sell or 'all'",
						Required:    true,
					},
				},
			},
			{
				Name:        "portfolio",
				Description: "View your crypto portfolio, cost basis, and returns",
				Type:        discordgo.ApplicationCommandOptionSubCommand,
			},
		},
	},
	{
		Name:        "poly",
		Description: "Polymarket Prediction Markets - Trade shares on real-world events",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Name:        "trending",
				Description: "View top volume trending markets on Polymarket",
				Type:        discordgo.ApplicationCommandOptionSubCommand,
			},
			{
				Name:        "search",
				Description: "Search for real-world prediction markets on Polymarket",
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Options: []*discordgo.ApplicationCommandOption{
					{
						Type:        discordgo.ApplicationCommandOptionString,
						Name:        "query",
						Description: "Search keywords (e.g. Bitcoin, Trump, Champions League)",
						Required:    true,
					},
				},
			},
			{
				Name:        "import",
				Description: "Import a Polymarket event into the server's betting channel",
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Options: []*discordgo.ApplicationCommandOption{
					{
						Type:        discordgo.ApplicationCommandOptionString,
						Name:        "query",
						Description: "Polymarket slug, ID, or full URL",
						Required:    true,
					},
				},
			},
			{
				Name:        "suggest",
				Description: "Suggest a Polymarket event for admin approval",
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Options: []*discordgo.ApplicationCommandOption{
					{
						Type:        discordgo.ApplicationCommandOptionString,
						Name:        "query",
						Description: "Polymarket slug, ID, or full URL",
						Required:    true,
					},
				},
			},
			{
				Name:        "view",
				Description: "View details and live odds for an imported market",
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Options: []*discordgo.ApplicationCommandOption{
					{
						Type:        discordgo.ApplicationCommandOptionString,
						Name:        "id",
						Description: "Market ID (e.g. poly_559651)",
						Required:    true,
					},
				},
			},
			{
				Name:        "portfolio",
				Description: "View your Polymarket shares, positions, and unrealized profit",
				Type:        discordgo.ApplicationCommandOptionSubCommand,
			},
			{
				Name:        "config",
				Description: "Configure Polymarket settings (Admin only)",
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Options: []*discordgo.ApplicationCommandOption{
					{
						Type:        discordgo.ApplicationCommandOptionChannel,
						Name:        "channel",
						Description: "Dedicated channel for Polymarket prediction markets",
						Required:    false,
					},
					{
						Type:        discordgo.ApplicationCommandOptionString,
						Name:        "import_mode",
						Description: "Who can import markets",
						Required:    false,
						Choices: []*discordgo.ApplicationCommandOptionChoice{
							{Name: "Admins Only", Value: "admin_only"},
							{Name: "All Users", Value: "all_users"},
						},
					},
					{
						Type:        discordgo.ApplicationCommandOptionNumber,
						Name:        "house_edge",
						Description: "House edge fee percentage (e.g. 0.03 for 3%)",
						Required:    false,
					},
				},
			},
		},
	},
}