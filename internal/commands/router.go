package commands

import (
	"bot/internal/database"
	"bot/internal/gacha"
	"bot/internal/games"
	"bot/pkg/config"
	"github.com/bwmarrin/discordgo"
)

// CommandCategory classifies slash commands into functional domains for authorization and routing.
type CommandCategory string

const (
	CategoryGeneral    CommandCategory = "general"
	CategoryEconomy    CommandCategory = "economy"
	CategoryGames      CommandCategory = "games"
	CategoryGacha      CommandCategory = "gacha"
	CategoryPolymarket CommandCategory = "polymarket"
	CategoryBicho      CommandCategory = "bicho"
	CategoryAdmin      CommandCategory = "admin"
)

// SlashRoute represents the routing target, category, and permissions for a top-level slash command.
type SlashRoute struct {
	Name      string
	Category  CommandCategory
	Handler   func(s *discordgo.Session, i *discordgo.InteractionCreate)
	AdminOnly bool
}

// commandRegistry is the single source of truth for command routing and authorization.
var commandRegistry = map[string]SlashRoute{
	// General
	"help":     {Name: "help", Category: CategoryGeneral, Handler: HandleSlashHelp},
	"language": {Name: "language", Category: CategoryGeneral, Handler: HandleSlashLanguage},

	// Economy
	"daily":       {Name: "daily", Category: CategoryEconomy, Handler: handleSlashDaily},
	"balance":     {Name: "balance", Category: CategoryEconomy, Handler: handleSlashBalance},
	"leaderboard": {Name: "leaderboard", Category: CategoryEconomy, Handler: handleSlashLeaderboard},
	"pay":         {Name: "pay", Category: CategoryEconomy, Handler: handleSlashPay},
	"shop":        {Name: "shop", Category: CategoryEconomy, Handler: handleSlashShop},
	"buy":         {Name: "buy", Category: CategoryEconomy, Handler: handleSlashBuy},
	"loan":        {Name: "loan", Category: CategoryEconomy, Handler: handleSlashLoan},
	"stock":       {Name: "stock", Category: CategoryEconomy, Handler: handleSlashStock},
	"crypto":      {Name: "crypto", Category: CategoryEconomy, Handler: handleSlashCrypto},

	// Games
	"event":     {Name: "event", Category: CategoryGames, Handler: games.HandleEventCommand},
	"bet":       {Name: "bet", Category: CategoryGames, Handler: handleSlashBet},
	"slots":     {Name: "slots", Category: CategoryGames, Handler: handleSlashSlots},
	"wheel":     {Name: "wheel", Category: CategoryGames, Handler: handleSlashWheel},
	"roulette":  {Name: "roulette", Category: CategoryGames, Handler: handleSlashRoulette},
	"blackjack": {Name: "blackjack", Category: CategoryGames, Handler: handleSlashBlackjack},
	"mines":     {Name: "mines", Category: CategoryGames, Handler: handleSlashMines},

	// Polymarket & Jogo do Bicho
	"poly":  {Name: "poly", Category: CategoryPolymarket, Handler: HandleSlashPolymarket},
	"bicho": {Name: "bicho", Category: CategoryBicho, Handler: HandleSlashBicho},

	// Admin / Developer
	"apikey":  {Name: "apikey", Category: CategoryAdmin, Handler: HandleSlashApiKey, AdminOnly: true},
	"webhook": {Name: "webhook", Category: CategoryAdmin, Handler: HandleSlashWebhook, AdminOnly: true},

	// Gacha
	"roll":          {Name: "roll", Category: CategoryGacha, Handler: gacha.Slash},
	"rolls":         {Name: "rolls", Category: CategoryGacha, Handler: gacha.Slash},
	"top":           {Name: "top", Category: CategoryGacha, Handler: gacha.Slash},
	"info":          {Name: "info", Category: CategoryGacha, Handler: gacha.Slash},
	"harem":         {Name: "harem", Category: CategoryGacha, Handler: gacha.Slash},
	"profile":       {Name: "profile", Category: CategoryGacha, Handler: gacha.Slash},
	"gallery":       {Name: "gallery", Category: CategoryGacha, Handler: gacha.Slash},
	"wishlist":      {Name: "wishlist", Category: CategoryGacha, Handler: gacha.Slash},
	"wish":          {Name: "wish", Category: CategoryGacha, Handler: gacha.Slash},
	"unwish":        {Name: "unwish", Category: CategoryGacha, Handler: gacha.Slash},
	"wishclear":     {Name: "wishclear", Category: CategoryGacha, Handler: gacha.Slash},
	"trade":         {Name: "trade", Category: CategoryGacha, Handler: gacha.Slash},
	"gift":          {Name: "gift", Category: CategoryGacha, Handler: gacha.Slash},
	"divorce":       {Name: "divorce", Category: CategoryGacha, Handler: gacha.Slash},
	"keys":          {Name: "keys", Category: CategoryGacha, Handler: gacha.Slash},
	"offers":        {Name: "offers", Category: CategoryGacha, Handler: gacha.Slash},
	"search":        {Name: "search", Category: CategoryGacha, Handler: gacha.Slash},
	"harem-ranking": {Name: "harem-ranking", Category: CategoryGacha, Handler: gacha.Slash},
	"alias":         {Name: "alias", Category: CategoryGacha, Handler: gacha.Slash},
	"series":        {Name: "series", Category: CategoryGacha, Handler: gacha.Slash},
	"gachashop":     {Name: "gachashop", Category: CategoryGacha, Handler: gacha.Slash},
	"inventory":     {Name: "inventory", Category: CategoryGacha, Handler: gacha.Slash},
	"gachabuy":      {Name: "gachabuy", Category: CategoryGacha, Handler: gacha.Slash},
	"use":           {Name: "use", Category: CategoryGacha, Handler: gacha.Slash},
	"open":          {Name: "open", Category: CategoryGacha, Handler: gacha.Slash},
	"mycustoms":     {Name: "mycustoms", Category: CategoryGacha, Handler: gacha.Slash},
	"builds":        {Name: "builds", Category: CategoryGacha, Handler: gacha.Slash},
	"learnskill":    {Name: "learnskill", Category: CategoryGacha, Handler: gacha.Slash},
	"respec":        {Name: "respec", Category: CategoryGacha, Handler: gacha.Slash},
	"addcustom":     {Name: "addcustom", Category: CategoryGacha, Handler: handleAddCustomSlash},
	"removecustom":  {Name: "removecustom", Category: CategoryGacha, Handler: handleRemoveCustomSlash},
	"customimage":   {Name: "customimage", Category: CategoryGacha, Handler: handleCustomImageSlash},
	"gachaconfig":   {Name: "gachaconfig", Category: CategoryGacha, Handler: gacha.HandleConfigSlash, AdminOnly: true},
}

// GetSlashRoute returns the routing information for a slash command.
func GetSlashRoute(name string) (SlashRoute, bool) {
	route, ok := commandRegistry[name]
	return route, ok
}

// isGachaCommand returns true if the command belongs to the Gacha category.
func isGachaCommand(name string) bool {
	route, ok := commandRegistry[name]
	return ok && route.Category == CategoryGacha
}

// isAdmin checks whether a guild member has server administrator permissions.
func isAdmin(m *discordgo.Member) bool {
	if m == nil {
		return false
	}
	return (m.Permissions & (discordgo.PermissionAdministrator | discordgo.PermissionManageServer)) != 0
}

// isChannelAuthorized determines whether an interaction can proceed in the given channel.
func isChannelAuthorized(i *discordgo.InteractionCreate, route SlashRoute) bool {
	// 1. Global allowed channels from config.json
	if config.Bot.IsChannelAllowed(i.ChannelID) {
		return true
	}

	// 2. Admin commands can be executed in any channel by administrators
	if route.AdminOnly && isAdmin(i.Member) {
		return true
	}

	// 3. Category-specific channel delegation:
	switch route.Category {
	case CategoryGacha:
		// Gacha commands are allowed in the server's gacha channels and validated by Gacha
		return true
	case CategoryPolymarket:
		if i.GuildID != "" {
			settings, _ := database.GetGuildPolymarketSettings(i.GuildID)
			return settings != nil && settings.ChannelID == i.ChannelID
		}
	case CategoryBicho:
		if i.GuildID != "" {
			bichoSettings, _ := database.GetBichoSettings(i.GuildID)
			return bichoSettings != nil && bichoSettings.ChannelID == i.ChannelID
		}
	}

	return false
}
