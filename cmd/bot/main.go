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
	// BotLogDir specifies where bot log files are stored.
	BotLogDir = "logs/bot_logs"
)

func main() {
	// Initialize application with required dependencies
	app, err := setup.InitializeApp(BotLogDir)
	if err != nil {
		log.Fatalf("Failed to initialize application: %v", err)
	}
	defer app.Cleanup()

	// Create bot instance
	discordBot, err := bot.New(app)
	if err != nil {
		log.Printf("Failed to create bot: %v", err)
		return
	}

	// Start the bot and connect to Discord
	if err := discordBot.Start(); err != nil {
		log.Printf("Failed to start bot: %v", err)
		return
	}

	log.Println("Bot has been started. Waiting for interrupt signal to gracefully shutdown...")

	// Wait for interrupt signal to gracefully shutdown the bot
	// This ensures all pending events are processed before closing
	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	<-sc

	// Cleanly close down the Discord session
	discordBot.Close()
}
