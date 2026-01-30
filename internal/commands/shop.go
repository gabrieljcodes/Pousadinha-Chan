package commands

import (
	"estudocoin/internal/database"
	"estudocoin/internal/games"
	"estudocoin/pkg/config"
	"estudocoin/pkg/utils"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
)

func CmdShop(s *discordgo.Session, m *discordgo.MessageCreate) {
	sym := config.Bot.CurrencySymbol
	desc := fmt.Sprintf(`
**Available Items:**

1. **Change Own Nickname**
   Cost: %d %s
   Command: `+"`!buy nickname <new name>`"+`

2. **Change Other's Nickname**
   Cost: %d %s
   Command: `+"`!buy rename @user <new name>`"+`

3. **Punishment/Timeout User** (Server timeout - text & voice)
   Cost: %d %s per minute
   Command: `+"`!buy punishment @user <minutes>`"+`

4. **Mute User** (Voice only - must be in call)
   Cost: %d %s per minute
   Command: `+"`!buy mute @user <minutes>`"+`
`, config.Economy.CostNicknameSelf, sym, config.Economy.CostNicknameOther, sym, config.Economy.CostPerMinutePunishment, sym, config.Economy.CostPerMinuteMute, sym)

	s.ChannelMessageSendEmbed(m.ChannelID, utils.GoldEmbed(fmt.Sprintf("🛒 %s Shop", config.Bot.BotName), desc))
}

func CmdBuy(s *discordgo.Session, m *discordgo.MessageCreate, args []string) {
	if len(args) < 1 {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.InfoEmbed("Shop", "Use `!shop` to see available items."))
		return
	}

	item := strings.ToLower(args[0])
	userID := m.Author.ID

	switch item {
	case "nickname":
		if len(args) < 2 {
			s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Usage: `!buy nickname <new name>`"))
			return
		}
		newName := strings.Join(args[1:], " ")
		if database.GetBalance(userID) < config.Economy.CostNicknameSelf {
			s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Insufficient funds."))
			return
		}
		
		err := s.GuildMemberNickname(m.GuildID, userID, newName)
		if err != nil {
			s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Could not change nickname (check my permissions)."))
			return
		}

		database.CollectLostBet(userID, config.Economy.CostNicknameSelf)
		s.ChannelMessageSendEmbed(m.ChannelID, utils.SuccessEmbed("Purchase Successful", "Your nickname has been changed!"))

	case "rename":
		if len(m.Mentions) == 0 || len(args) < 3 {
			s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Usage: `!buy rename @user <new name>`"))
			return
		}
		targetUser := m.Mentions[0]
		
		// Find name after mention
		nameStartIndex := 2
		newName := strings.Join(args[nameStartIndex:], " ")

		if database.GetBalance(userID) < config.Economy.CostNicknameOther {
			s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Insufficient funds."))
			return
		}

		err := s.GuildMemberNickname(m.GuildID, targetUser.ID, newName)
		if err != nil {
			s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Error changing nickname (check permissions/hierarchy)."))
			return
		}

		database.CollectLostBet(userID, config.Economy.CostNicknameOther)
		s.ChannelMessageSendEmbed(m.ChannelID, utils.SuccessEmbed("Purchase Successful", fmt.Sprintf("Nickname of %s changed.", targetUser.Username)))

	case "punishment", "timeout":
		if len(m.Mentions) == 0 || len(args) < 3 {
			s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Usage: `!buy punishment @user <minutes>`"))
			return
		}
		targetUser := m.Mentions[0]
		
		minutesStr := args[len(args)-1] 
		minutes, err := strconv.Atoi(minutesStr)
		if err != nil || minutes <= 0 {
			s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Invalid time."))
			return
		}

		cost := minutes * config.Economy.CostPerMinutePunishment
		if database.GetBalance(userID) < cost {
			s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed(fmt.Sprintf("Insufficient funds. Cost: %d %s.", cost, config.Bot.CurrencySymbol)))
			return
		}

		// Verificar se o usuário está em um jogo ativo
		if games.IsUserInGame(targetUser.ID) {
			msg, _ := s.ChannelMessageSendEmbed(m.ChannelID, utils.InfoEmbed("⏳ Aguardando", 
				fmt.Sprintf("%s está em um jogo ativo. Aguardando o jogo terminar para aplicar o punishment...", targetUser.Username)))
			
			// Esperar o jogo acabar
			games.WaitForGameFinish(targetUser.ID)
			
			// Deletar mensagem de espera
			if msg != nil {
				s.ChannelMessageDelete(m.ChannelID, msg.ID)
			}
		}

		// Check existing timeout
		member, err := s.GuildMember(m.GuildID, targetUser.ID)
		if err != nil {
			s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Member not found."))
			return
		}

		var until time.Time
		if member.CommunicationDisabledUntil != nil && member.CommunicationDisabledUntil.After(time.Now()) {
			// Extend existing
			until = member.CommunicationDisabledUntil.Add(time.Duration(minutes) * time.Minute)
		} else {
			// Start new
			until = time.Now().Add(time.Duration(minutes) * time.Minute)
		}

		err = s.GuildMemberTimeout(m.GuildID, targetUser.ID, &until)
		if err != nil {
			s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Error applying timeout (check permissions/hierarchy)."))
			return
		}

		database.CollectLostBet(userID, cost)
		s.ChannelMessageSendEmbed(m.ChannelID, utils.SuccessEmbed("Punishment Applied!", fmt.Sprintf("%s has been timed out until %s.", targetUser.Username, until.Format("15:04:05"))))

	case "mute":
		if len(m.Mentions) == 0 || len(args) < 3 {
			s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Usage: `!buy mute @user <minutes>`"))
			return
		}
		targetUser := m.Mentions[0]
		
		minutesStr := args[len(args)-1] 
		minutes, err := strconv.Atoi(minutesStr)
		if err != nil || minutes <= 0 {
			s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Invalid time."))
			return
		}

		cost := minutes * config.Economy.CostPerMinuteMute
		if database.GetBalance(userID) < cost {
			s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed(fmt.Sprintf("Insufficient funds. Cost: %d %s.", cost, config.Bot.CurrencySymbol)))
			return
		}

		// Verificar se o usuário está em um jogo ativo
		if games.IsUserInGame(targetUser.ID) {
			msg, _ := s.ChannelMessageSendEmbed(m.ChannelID, utils.InfoEmbed("⏳ Aguardando", 
				fmt.Sprintf("%s está em um jogo ativo. Aguardando o jogo terminar para aplicar o mute...", targetUser.Username)))
			
			// Esperar o jogo acabar
			games.WaitForGameFinish(targetUser.ID)
			
			// Deletar mensagem de espera
			if msg != nil {
				s.ChannelMessageDelete(m.ChannelID, msg.ID)
			}
		}

		// Check if target user is in a voice channel
		voiceState, err := s.State.VoiceState(m.GuildID, targetUser.ID)
		if err != nil || voiceState == nil || voiceState.ChannelID == "" {
			s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed(fmt.Sprintf("%s is not in a voice channel! You can only mute users who are currently in a call.", targetUser.Username)))
			return
		}

		// Apply server mute (voice only, not timeout)
		err = s.GuildMemberMute(m.GuildID, targetUser.ID, true)
		if err != nil {
			s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Error muting user (check permissions/hierarchy)."))
			return
		}

		// Remove coins
		database.CollectLostBet(userID, cost)

		// Schedule unmute after duration
		go func() {
			time.Sleep(time.Duration(minutes) * time.Minute)
			s.GuildMemberMute(m.GuildID, targetUser.ID, false)
		}()

		s.ChannelMessageSendEmbed(m.ChannelID, utils.SuccessEmbed("User Muted!", fmt.Sprintf("%s has been muted in voice for %d minutes.", targetUser.Username, minutes)))

	default:
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Item not found."))
	}
}
