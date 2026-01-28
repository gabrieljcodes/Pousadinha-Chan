package commands

import "github.com/bwmarrin/discordgo"

var minAmount float64 = 1.0

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
				Name:        "mute",
				Description: "Timeout/Mute a user",
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Options: []*discordgo.ApplicationCommandOption{
					{
						Type:        discordgo.ApplicationCommandOptionUser,
						Name:        "user",
						Description: "The user to mute",
						Required:    true,
					},
					{
						Type:        discordgo.ApplicationCommandOptionInteger,
						Name:        "minutes",
						Description: "Duration in minutes",
						Required:    true,
						MinValue:    &minAmount,
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
						MinValue:    &minAmount, // Assuming minAmount is defined as float 1.0 but logic handles 100
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
				Name:        "blackjack",
				Description: "Play a game of Blackjack",
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Options: []*discordgo.ApplicationCommandOption{
					{
						Type:        discordgo.ApplicationCommandOptionInteger,
						Name:        "amount",
						Description: "Amount to bet (Min 100)",
						Required:    true,
						MinValue:    &minAmount,
					},
				},
			},
		},
	},
}
