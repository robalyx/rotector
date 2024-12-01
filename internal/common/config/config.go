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
	Debug          Debug          `mapstructure:"debug"`
	RateLimit      RateLimit      `mapstructure:"rate_limit"`
	CircuitBreaker CircuitBreaker `mapstructure:"circuit_breaker"`
	PostgreSQL     PostgreSQL     `mapstructure:"postgresql"`
	Redis          Redis          `mapstructure:"redis"`
	GeminiAI       GeminiAI       `mapstructure:"gemini_ai"`
}

// BotConfig contains Discord bot specific configuration.
type BotConfig struct {
	Discord Discord
}

// WorkerConfig contains worker specific configuration.
type WorkerConfig struct {
	BatchSizes      BatchSizes      `mapstructure:"batch_sizes"`
	ThresholdLimits ThresholdLimits `mapstructure:"threshold_limits"`
}

// Debug contains debug-related configuration.
type Debug struct {
	LogLevel      string `mapstructure:"log_level"`
	MaxLogsToKeep int    `mapstructure:"max_logs_to_keep"`
	MaxLogLines   int    `mapstructure:"max_log_lines"`
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

// GeminiAI contains GeminiAI API configuration.
// APIKey must be provided for authentication.
type GeminiAI struct {
	APIKey string `mapstructure:"api_key"`
	Model  string `mapstructure:"model"`
}

// Discord contains Discord bot configuration.
// Token must be provided for bot authentication.
type Discord struct {
	Token string
}

// BatchSizes configures how many items to process in each batch.
type BatchSizes struct {
	FriendUsers int `mapstructure:"friend_users"`
	PurgeUsers  int `mapstructure:"purge_users"`
	GroupUsers  int `mapstructure:"group_users"`
	PurgeGroups int `mapstructure:"purge_groups"`
}

// ThresholdLimits configures various thresholds for worker operations.
type ThresholdLimits struct {
	FlaggedUsers       int `mapstructure:"flagged_users"`
	MinFlaggedForGroup int `mapstructure:"min_flagged_for_group"`
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
