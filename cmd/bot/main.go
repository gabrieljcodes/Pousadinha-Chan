package main

import (
	"bot/internal/api"
	"bot/internal/commands"
	"bot/internal/database"
	"bot/internal/events"
	"bot/internal/gacha"
	"bot/internal/games"
	"bot/internal/locale"
	"bot/internal/polymarket"
	"bot/internal/stockmarket"
	"bot/pkg/config"
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/joho/godotenv"
)

func main() {
	_ = godotenv.Load()

	// Load Configuration
	config.Load()

	token := os.Getenv("DISCORD_TOKEN")
	if token == "" {
		log.Fatal("DISCORD_TOKEN not found in environment variables")
	}

	database.Initialize()
	defer database.DB.Close()
	locale.SetGuildLanguageResolver(database.GetGuildLanguageCached)
	gachaConfig, err := gacha.LoadConfig()
	if err != nil {
		log.Fatal(err)
	}
	if gachaConfig.Enabled || os.Getenv("CATALOG_ADMIN_PASSWORD") != "" {
		if !config.Bot.EnableAPI {
			log.Fatal("Gacha and the catalog editor require ENABLE_API=true")
		}
		store := &gacha.Store{DB: database.DB.GetDB(), Config: gachaConfig}
		defer store.CloseMedia()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		err = store.Migrate(ctx)
		cancel()
		if err != nil {
			log.Fatalf("Gacha migration failed: %v", err)
		}
		api.CatalogStore = store
		if gachaConfig.Enabled {
			if cacheErr := store.ConnectPoolCache(os.Getenv("VALKEY_URL")); cacheErr != nil {
				log.Print(cacheErr)
			}
			defer store.ClosePoolCache()
			warmCtx, warmCancel := context.WithTimeout(context.Background(), time.Minute)
			warmErr := store.WarmRollPools(warmCtx)
			if warmErr != nil {
				log.Printf("Gacha pool warmup failed: %v", warmErr)
			} else {
				stats := store.PoolCacheStats()
				log.Printf("Gacha pools ready: characters=%d builds=%d shared_hits=%d cache_errors=%d", stats.Characters, stats.Builds, stats.SharedHits, stats.CacheErrors)
			}
			if purged, purgeErr := store.PurgeExpiredRolls(warmCtx); purgeErr != nil {
				log.Printf("Gacha expired rolls cleanup: %v", purgeErr)
			} else if purged > 0 {
				log.Printf("Gacha expired rolls purged: %d", purged)
			}
			warmCancel()
			gacha.Default = store
		}
	}

	// Start API Server
	if config.Bot.EnableAPI {
		go api.Start()
	} else {
		log.Println("API is disabled in config.json")
	}

	// Create Discord Session

	dg, err := discordgo.New("Bot " + token)
	if err != nil {
		log.Fatal("Error creating Discord session: ", err)
	}

	// Register Handlers
	dg.AddHandler(commands.SlashHandler)
	dg.AddHandler(commands.ComponentsHandler)
	dg.AddHandler(events.VoiceStateUpdate)
	if gachaConfig.Enabled {
		dg.AddHandler(gacha.PrefixHandler)
	}

	// Identify Intent
	dg.Identify.Intents = discordgo.IntentsGuilds | discordgo.IntentsGuildVoiceStates | discordgo.IntentsGuildMessages | discordgo.IntentMessageContent

	// Open Websocket
	err = dg.Open()
	if err != nil {
		log.Fatal("Error opening connection: ", err)
	}

	// Set Bot User ID for collecting lost bets
	database.BotUserID = dg.State.User.ID
	log.Printf("Bot User ID: %s", database.BotUserID)

	// Initialize voice sessions for users already in voice channels
	events.InitializeVoiceSessions(dg)

	// Start Stock Market
	stockmarket.Start(dg)

	// Start Roulette
	games.StartRoulette(dg)

	// Start Event Betting
	games.StartEventBetting(dg)

	// Start Polymarket Oracle
	polymarket.StartOracle(dg)

	// Start Jogo do Bicho Daily Manager
	games.StartBichoManager(dg)

	// Start loan overdue collector background worker
	commands.StartLoanWorker(dg)

	// Synchronize Slash Commands with Discord only if schema changed to preserve client cache
	if err := commands.SyncCommands(dg, dg.State.User.ID, config.Bot.DevGuildID); err != nil {
		log.Printf("Warning: cannot synchronize slash commands: %v", err)
	}

	log.Println("Bot is now running. Press CTRL-C to exit.")

	// Wait here until CTRL-C or other term signal is received.
	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	<-sc

	log.Println("Shutting down...")

	// Pay all users in active voice sessions
	events.CloseAllVoiceSessions()

	// Cleanly close down the Discord session.
	// Optionally remove commands on exit to avoid clutter if dev
	// for _, v := range registeredCommands {
	// 	dg.ApplicationCommandDelete(dg.State.User.ID, "", v.ID)
	// }
	dg.Close()
}
