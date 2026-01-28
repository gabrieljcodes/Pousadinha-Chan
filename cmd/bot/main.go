package main

import (
	"estudocoin/internal/commands"
	"estudocoin/pkg/config"
	"estudocoin/internal/database"
	"estudocoin/internal/events"
	"estudocoin/internal/api"
	"estudocoin/internal/stockmarket"
	"log"
	"os"
	"os/signal"
	"syscall"

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

	// Start Stock Market
	stockmarket.Start(dg)

	// Register Slash Commands
	log.Println("Registering slash commands...")
	registeredCommands := make([]*discordgo.ApplicationCommand, len(commands.SlashCommands))
	for i, v := range commands.SlashCommands {
		cmd, err := dg.ApplicationCommandCreate(dg.State.User.ID, "", v)
		if err != nil {
			log.Panicf("Cannot create '%v' command: %v", v.Name, err)
		}
		registeredCommands[i] = cmd
	}

	log.Println("Bot is now running. Press CTRL-C to exit.")
	
	// Wait here until CTRL-C or other term signal is received.
	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	<-sc

	// Cleanly close down the Discord session.
	// Optionally remove commands on exit to avoid clutter if dev
	// for _, v := range registeredCommands {
	// 	dg.ApplicationCommandDelete(dg.State.User.ID, "", v.ID)
	// }
	dg.Close()
}