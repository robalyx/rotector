package config

import (
	"time"

	"github.com/spf13/viper"
)

// Config represents the entire application configuration.
type Config struct {
	Common CommonConfig
	Bot    BotConfig
	Worker WorkerConfig
}

// CommonConfig contains configuration shared between bot and worker.
type CommonConfig struct {
	Debug          Debug
	RateLimit      RateLimit
	CircuitBreaker CircuitBreaker
	PostgreSQL     PostgreSQL
	Redis          Redis
	OpenAI         OpenAI
}

// BotConfig contains Discord bot specific configuration.
type BotConfig struct {
	Discord Discord
}

// WorkerConfig contains worker specific configuration.
type WorkerConfig struct{}

// Debug contains debug-related configuration.
type Debug struct {
	LogLevel      string `mapstructure:"log_level"`
	MaxLogsToKeep int    `mapstructure:"max_logs_to_keep"`
	QueryLogging  bool   `mapstructure:"query_logging"`
	EnablePprof   bool   `mapstructure:"enable_pprof"`
	PprofPort     int    `mapstructure:"pprof_port"`
}

// RateLimit contains rate limit configuration.
type RateLimit struct {
	RequestsPerSecond float64 `mapstructure:"requests_per_second"`
	BurstSize         int     `mapstructure:"burst_size"`
}

// CircuitBreaker contains circuit breaker configuration.
// It prevents cascading failures by temporarily stopping requests after errors.
type CircuitBreaker struct {
	MaxFailures      uint32        `mapstructure:"max_failures"`
	FailureThreshold time.Duration `mapstructure:"failure_threshold"`
	RecoveryTimeout  time.Duration `mapstructure:"recovery_timeout"`
}

// PostgreSQL contains database connection configuration.
// DBName specifies which database to use within the PostgreSQL server.
type PostgreSQL struct {
	Host     string
	Port     int
	User     string
	Password string
	DBName   string `mapstructure:"db_name"`
}

// Redis contains Redis connection configuration.
// Username is optional and can be empty for default authentication.
type Redis struct {
	Host     string
	Port     int
	Username string
	Password string
}

// OpenAI contains OpenAI API configuration.
// APIKey must be provided for authentication.
type OpenAI struct {
	APIKey string `mapstructure:"api_key"`
}

// Discord contains Discord bot configuration.
// Token must be provided for bot authentication.
type Discord struct {
	Token string
}

// LoadConfig loads the configuration from the specified file.
func LoadConfig() (*Config, error) {
	viper.SetConfigName("common")
	viper.SetConfigType("toml")

	// Add default search paths
	viper.AddConfigPath("$HOME/.rotector/config")
	viper.AddConfigPath("/etc/rotector/config")
	viper.AddConfigPath("/app/config")
	viper.AddConfigPath("/config")
	viper.AddConfigPath(".")

	if err := viper.ReadInConfig(); err != nil {
		return nil, err
	}

	// Load bot config
	viper.SetConfigName("bot")
	if err := viper.MergeInConfig(); err != nil {
		return nil, err
	}

	// Load worker config
	viper.SetConfigName("worker")
	if err := viper.MergeInConfig(); err != nil {
		return nil, err
	}

	var config Config
	if err := viper.Unmarshal(&config); err != nil {
		return nil, err
	}

	return &config, nil
}
