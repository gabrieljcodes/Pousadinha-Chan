package main

import (
	"bot/internal/api"
	"bot/internal/commands"
	"bot/internal/database"
	"bot/internal/events"
	"bot/internal/gacha"
	"bot/internal/games"
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
	gachaConfig, err := gacha.LoadConfig()
	if err != nil {
		log.Fatal(err)
	}
	if gachaConfig.Enabled || os.Getenv("CATALOG_ADMIN_PASSWORD") != "" {
		if !config.Bot.EnableAPI {
			log.Fatal("Gacha and the catalog editor require ENABLE_API=true")
		}
		store := &gacha.Store{DB: database.DB.GetDB(), Config: gachaConfig}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		err = store.Migrate(ctx)
		cancel()
		if err != nil {
			log.Fatalf("Gacha migration failed: %v", err)
		}
		api.CatalogStore = store
		if gachaConfig.Enabled {
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
	dg.AddHandler(commands.MessageCreate)
	dg.AddHandler(commands.SlashHandler)
	dg.AddHandler(commands.ComponentsHandler)
	dg.AddHandler(events.VoiceStateUpdate)

	// Identify Intent
	dg.Identify.Intents = discordgo.IntentsGuilds | discordgo.IntentsGuildMessages | discordgo.IntentsGuildVoiceStates | discordgo.IntentsMessageContent

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

	// Register Slash Commands in a single bulk request to avoid Discord rate limits
	log.Println("Registering slash commands...")
	_, err = dg.ApplicationCommandBulkOverwrite(dg.State.User.ID, "", commands.SlashCommands)
	if err != nil {
		log.Printf("Warning: cannot bulk overwrite slash commands: %v", err)
	} else {
		log.Printf("Successfully registered %d slash commands.", len(commands.SlashCommands))
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
