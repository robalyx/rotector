package config

import (
	"github.com/spf13/viper"
)

// Config represents the entire application configuration.
type Config struct {
	Common CommonConfig
	Bot    BotConfig
	Worker WorkerConfig
	RPC    RPCConfig
}

// CommonConfig contains configuration shared between bot and worker.
type CommonConfig struct {
	Debug          Debug          `mapstructure:"debug"`
	RateLimit      RateLimit      `mapstructure:"rate_limit"`
	CircuitBreaker CircuitBreaker `mapstructure:"circuit_breaker"`
	Retry          Retry          `mapstructure:"retry"`
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
	// Log level (debug, info, warn, error)
	LogLevel string `mapstructure:"log_level"`
	// Maximum number of log files to keep before rotation
	MaxLogsToKeep int `mapstructure:"max_logs_to_keep"`
	// Maximum number of lines per log file
	MaxLogLines int `mapstructure:"max_log_lines"`
	// Enable pprof debugging endpoints
	EnablePprof bool `mapstructure:"enable_pprof"`
	// Port for pprof HTTP server if enabled
	PprofPort int `mapstructure:"pprof_port"`
}

// RateLimit contains rate limit configuration.
type RateLimit struct {
	// Maximum number of requests per second
	RequestsPerSecond float64 `mapstructure:"requests_per_second"`
	// Timeout for HTTP requests in seconds
	RequestTimeout int `mapstructure:"request_timeout"`
}

// CircuitBreaker contains circuit breaker configuration.
// It prevents cascading failures by temporarily stopping requests after errors.
type CircuitBreaker struct {
	// Maximum number of consecutive failures before circuit opens
	MaxFailures uint32 `mapstructure:"max_failures"`
	// Time in milliseconds after which a request is considered failed
	FailureThreshold int `mapstructure:"failure_threshold"`
	// Time in milliseconds before attempting to close circuit
	RecoveryTimeout int `mapstructure:"recovery_timeout"`
}

// Retry contains retry configuration.
// It retries a failed request up to a maximum number of times with a delay between each retry.
type Retry struct {
	// Maximum number of retry attempts
	MaxRetries uint64 `mapstructure:"max_retries"`
	// Initial delay between retries in milliseconds
	Delay int `mapstructure:"delay"`
	// Maximum delay between retries in milliseconds
	MaxDelay int `mapstructure:"max_delay"`
}

// PostgreSQL contains database connection configuration.
// DBName specifies which database to use within the PostgreSQL server.
type PostgreSQL struct {
	// PostgreSQL server hostname
	Host string `mapstructure:"host"`
	// PostgreSQL server port
	Port int `mapstructure:"port"`
	// PostgreSQL username
	User string `mapstructure:"user"`
	// PostgreSQL password
	Password string `mapstructure:"password"`
	// PostgreSQL database name
	DBName string `mapstructure:"db_name"`
	// Maximum number of open connections
	MaxOpenConns int `mapstructure:"max_open_conns"`
	// Maximum number of idle connections
	MaxIdleConns int `mapstructure:"max_idle_conns"`
	// Maximum connection lifetime in minutes
	MaxLifetime int `mapstructure:"max_lifetime"`
	// Maximum idle connection time in minutes
	MaxIdleTime int `mapstructure:"max_idle_time"`
}

// Redis contains Redis connection configuration.
// Username is optional and can be empty for default authentication.
type Redis struct {
	// Redis server hostname
	Host string `mapstructure:"host"`
	// Redis server port
	Port int `mapstructure:"port"`
	// Redis username (optional)
	Username string `mapstructure:"username"`
	// Redis password (optional)
	Password string `mapstructure:"password"`
}

// GeminiAI contains GeminiAI API configuration.
// APIKey must be provided for authentication.
type GeminiAI struct {
	// Gemini AI API key for authentication
	APIKey string `mapstructure:"api_key"`
	// Model version to use for AI analysis
	Model string `mapstructure:"model"`
}

// Discord contains Discord bot configuration.
// Token must be provided for bot authentication.
type Discord struct {
	// Discord bot token for authentication
	Token string `mapstructure:"token"`
}

// BatchSizes configures how many items to process in each batch.
type BatchSizes struct {
	// Number of friends to process in one batch
	FriendUsers int `mapstructure:"friend_users"`
	// Number of users to check for purging in one batch
	PurgeUsers int `mapstructure:"purge_users"`
	// Number of group members to process in one batch
	GroupUsers int `mapstructure:"group_users"`
	// Number of groups to check for purging in one batch
	PurgeGroups int `mapstructure:"purge_groups"`
}

// ThresholdLimits configures various thresholds for worker operations.
type ThresholdLimits struct {
	// Maximum number of flagged users before stopping worker
	FlaggedUsers int `mapstructure:"flagged_users"`
	// Minimum number of flagged users needed to flag a group
	MinFlaggedForGroup int `mapstructure:"min_flagged_for_group"`
	// Minimum follower count to consider a user "popular"
	MinFollowersForPopularUser uint64 `mapstructure:"min_followers_for_popular_user"`
}

// RPCConfig contains RPC server specific configuration.
type RPCConfig struct {
	Server    RPCServer    `mapstructure:"server"`
	IP        RPCIPConfig  `mapstructure:"ip"`
	RateLimit RPCRateLimit `mapstructure:"rate_limit"`
}

// RPCServer contains server configuration options.
type RPCServer struct {
	// Host address to listen on
	Host string `mapstructure:"host"`
	// Port number to listen on
	Port int `mapstructure:"port"`
}

// RPCIPConfig contains IP validation configuration.
type RPCIPConfig struct {
	// Enable checking of forwarded headers (X-Forwarded-For, etc.)
	EnableHeaderCheck bool `mapstructure:"enable_header_check"`
	// List of trusted proxy IPs that can set forwarded headers
	TrustedProxies []string `mapstructure:"trusted_proxies"`
	// Headers to check for client IP, in order of precedence
	CustomHeaders []string `mapstructure:"custom_headers"`
	// Allow local IPs (127.0.0.1, etc.) for development/testing
	AllowLocalIPs bool `mapstructure:"allow_local_ips"`
}

// RPCRateLimit contains rate limiting configuration for the RPC server.
type RPCRateLimit struct {
	// Maximum number of requests per second per IP
	RequestsPerSecond float64 `mapstructure:"requests_per_second"`
	// Maximum burst size for rate limiting
	BurstSize int `mapstructure:"burst_size"`
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

	// Load RPC config
	viper.SetConfigName("rpc")
	if err := viper.MergeInConfig(); err != nil {
		return nil, err
	}

	var config Config
	if err := viper.Unmarshal(&config); err != nil {
		return nil, err
	}

	return &config, nil
}
