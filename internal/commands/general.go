package commands

import (
	"estudocoin/pkg/config"
	"estudocoin/pkg/utils"
	"fmt"

	"github.com/bwmarrin/discordgo"
)

func GetHelpEmbed(s *discordgo.Session) *discordgo.MessageEmbed {
	embed := utils.NewEmbed()
	embed.Title = fmt.Sprintf("📘 %s Help", config.Bot.BotName)
	embed.Description = "Here is the complete list of commands and features available."
	embed.Color = utils.ColorBlue
	embed.Thumbnail = &discordgo.MessageEmbedThumbnail{
		URL: s.State.User.AvatarURL(""),
	}
	sym := config.Bot.CurrencySymbol

	embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
		Name: "💰 Economy",
		Value: fmt.Sprintf("`!daily` / `/daily`\nCollect your daily reward (**%d %s**).\n*Shows remaining time if not available.*\n\n"+
			"`!balance` / `/balance [user]`\nCheck your wallet or someone else's.\n\n"+
			"`!leaderboard` / `/leaderboard`\nSee the richest users.\n\n"+
			"`!pay` / `/pay <user> <amount>`\nTransfer coins to another user.", config.Economy.DailyAmount, sym),
		Inline: false,
	})

	embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
		Name: "🛒 Shop",
		Value: fmt.Sprintf("`!shop` / `/shop`\nView available items.\n\n"+
			"`!buy nickname <n>`\nChange your own nickname (**%d %s**).\n\n"+
			"`!buy rename @user <n>`\nChange someone else's nickname (**%d %s**).\n\n"+
			"`!buy mute @user <min>`\nTimeout a user for X minutes (**%d %s/min**).\n*Note: Mutes are accumulative!*",
			config.Economy.CostNicknameSelf, sym, config.Economy.CostNicknameOther, sym, config.Economy.CostPerMinuteMute, sym),
		Inline: false,
	})

	embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
		Name: "🎲 Gambling",
		Value: "`!bet aviator <amount>` / `/bet aviator`\nPlay the Aviator crash game. The multiplier rises every second.\n*Watch out for turbulence! Cash out before it crashes.*\n\n" +
			"`!bet cups <amount>` / `/bet cups`\nFind the hidden coin under 6 cups.\n*Win 2x -> Double again or Cash Out.*\n\n" +
			"`!bet blackjack <amount>` / `/blackjack <bet>`\nPlay classic Blackjack against the dealer.\n*Hit, Stand, Double Down, or take Insurance. Blackjack pays 3:2!*\n\n" +
			"`!bet slots <amount>` / `/slots <amount>`\nSpin the slot machine! Match symbols to win.\n*3 matching = Jackpot | 2 matching = Win | Jackpot up to 25x!*\n\n" +
			"`!roulette @user <amount>`\nChallenge another user to Russian Roulette.\n*Players take turns pulling the trigger. Survivor takes the entire pot!*",
		Inline: false,
	})

	embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
		Name: "📈 Stock Market",
		Value: "`!stock market`\nView available stocks and current prices.\n\n" +
			"`!stock buy <ticker> <amount>`\nBuy shares with your coins.\n\n" +
			"`!stock sell <ticker> <shares|all>`\nSell your shares for coins.\n\n" +
			"`!stock portfolio`\nView your investments and total portfolio value.",
		Inline: false,
	})

	embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
		Name: "🎡 Casino Roulette",
		Value: "`!wheel`\nView roulette betting options and time until next spin.\n\n" +
			"`!wheel number <0-36> <amount>` - **35:1**\n" +
			"`!wheel red <amount>` / `!wheel black <amount>` - **1:1**\n" +
			"`!wheel even <amount>` / `!wheel odd <amount>` - **1:1**\n" +
			"`!wheel low <amount>` (1-18) / `!wheel high <amount>` (19-36) - **1:1**\n" +
			"`!wheel dozen <1st/2nd/3rd> <amount>` - **2:1**\n\n" +
			"*Rounds occur every 10 minutes. Betting closes when the wheel spins!*",
		Inline: false,
	})

	embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
		Name: "🎲 Event Betting (Admin Only to Create)",
		Value: "`!createevent <question> | <opt1> | <opt2> | ... | <minutes>`\n" +
			"Create a betting event with dynamic odds. Example: `!createevent Will it rain? | Yes | No | 30`\n\n" +
			"`!betevent <event_id> <option_number> <amount>` - Place a bet\n" +
			"`!events` - List all active events\n" +
			"`!event <event_id>` - View specific event details and odds\n" +
			"`!closeevent <event_id>` - Close betting early (creator only)\n" +
			"`!result <event_id> <option_number>` - Set winner and pay out (creator only)\n\n" +
			"*Dynamic odds: Less popular options pay more! House takes 5% to prevent exploits.*",
		Inline: false,
	})

	embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
		Name: "🎙️ Voice Rewards (Passive)",
		Value: fmt.Sprintf("Earn **%d %s per minute** by staying in voice channels.\n*Requirements:*\n• At least 2 people in the channel.\n• You must not be Muted or Deafened.", config.Economy.VoiceCoinsPerMinute, sym),
		Inline: false,
	})

	embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
		Name: "🔧 Developer & API",
		Value: fmt.Sprintf("`/apikey create`\nGenerate an API Key to integrate with %s.\n\n"+
			"`/apikey list`\nView your active keys.\n\n"+
			"`/webhook set <url>`\nConfigure a URL to receive notifications when you receive coins.", config.Bot.BotName),
		Inline: false,
	})

	embed.Footer = &discordgo.MessageEmbedFooter{
		Text: fmt.Sprintf("%s v1.1 • Use slash commands for a better experience!", config.Bot.BotName),
	}

	return embed
}

func CmdHelp(s *discordgo.Session, m *discordgo.MessageCreate) {
	s.ChannelMessageSendEmbed(m.ChannelID, GetHelpEmbed(s))
}

func HandleSlashHelp(s *discordgo.Session, i *discordgo.InteractionCreate) {
	respondEmbed(s, i, GetHelpEmbed(s))
}
