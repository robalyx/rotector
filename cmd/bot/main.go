package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/rotector/rotector/internal/bot"
	"github.com/rotector/rotector/internal/common/setup"
)

const (
	BotLogDir = "logs/bot_logs"
)

func main() {
	// Initialize application
	setup, err := setup.InitializeApp(BotLogDir)
	if err != nil {
		log.Fatalf("Failed to initialize application: %v", err)
	}
	defer setup.CleanupApp()

	// Initialize bot
	discordBot, err := bot.New(setup.Config.Discord.Token, setup.DB, setup.RoAPI, setup.Logger)
	if err != nil {
		log.Printf("Failed to create bot: %v", err)
		return
	}

	// Start the bot
	if err := discordBot.Start(); err != nil {
		log.Printf("Failed to start bot: %v", err)
		return
	}

	log.Println("Bot has been started. Waiting for interrupt signal to gracefully shutdown...")

	// Wait for interrupt signal to gracefully shutdown the bot
	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	<-sc

	// Cleanly close down the Discord session.
	discordBot.Close()
}
