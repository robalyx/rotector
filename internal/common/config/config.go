package config

import (
	"github.com/spf13/viper"
)

// Config represents the entire application configuration.
type Config struct {
	Logging  Logging
	Database Database
	Redis    Redis
	Roblox   Roblox
	OpenAI   OpenAI
	Discord  Discord
}

// Logging contains logging-related configuration.
type Logging struct {
	Level         string
	MaxLogsToKeep int `mapstructure:"max_logs_to_keep"`
}

// Database contains database connection configuration.
type Database struct {
	Host     string
	Port     int
	User     string
	Password string
	DBName   string `mapstructure:"db_name"`
}

// Redis contains Redis connection configuration.
type Redis struct {
	Host     string
	Port     int
	Username string
	Password string
}

// Roblox contains Roblox-specific configuration.
type Roblox struct {
	CookiesFile string `mapstructure:"cookies_file"`
	ProxiesFile string `mapstructure:"proxies_file"`
}

// OpenAI contains OpenAI API configuration.
type OpenAI struct {
	APIKey string `mapstructure:"api_key"`
}

// Discord contains Discord bot configuration.
type Discord struct {
	Token string
}

// LoadConfig loads the configuration from the specified file.
func LoadConfig() (*Config, error) {
	viper.SetConfigName("config")
	viper.SetConfigType("toml")

	// Add default search paths
	viper.AddConfigPath("$HOME/.rotector")
	viper.AddConfigPath("/etc/rotector")
	viper.AddConfigPath(".")

	if err := viper.ReadInConfig(); err != nil {
		return nil, err
	}

	var config Config
	if err := viper.Unmarshal(&config); err != nil {
		return nil, err
	}

	return &config, nil
}
