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
	CircuitBreaker CircuitBreaker `mapstructure:"circuit_breaker"`
	Retry          Retry          `mapstructure:"retry"`
	PostgreSQL     PostgreSQL     `mapstructure:"postgresql"`
	Redis          Redis          `mapstructure:"redis"`
	GeminiAI       GeminiAI       `mapstructure:"gemini_ai"`
	Proxy          Proxy          `mapstructure:"proxy"`
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

// RPCConfig contains RPC server specific configuration.
type RPCConfig struct {
	Server    RPCServer    `mapstructure:"server"`
	IP        RPCIPConfig  `mapstructure:"ip"`
	RateLimit RPCRateLimit `mapstructure:"rate_limit"`
}

// Debug contains debug-related configuration.
type Debug struct {
	LogLevel      string `mapstructure:"log_level"`        // Log level (debug, info, warn, error)
	MaxLogsToKeep int    `mapstructure:"max_logs_to_keep"` // Maximum log files to keep
	MaxLogLines   int    `mapstructure:"max_log_lines"`    // Maximum lines per log file
	EnablePprof   bool   `mapstructure:"enable_pprof"`     // Enable pprof debugging
	PprofPort     int    `mapstructure:"pprof_port"`       // pprof server port
}

// CircuitBreaker contains circuit breaker configuration.
type CircuitBreaker struct {
	MaxFailures      uint32 `mapstructure:"max_failures"`      // Number of failures before circuit opens
	FailureThreshold int    `mapstructure:"failure_threshold"` // Request timeout in milliseconds
	RecoveryTimeout  int    `mapstructure:"recovery_timeout"`  // Recovery delay in milliseconds
}

// Retry contains retry configuration.
type Retry struct {
	MaxRetries uint64 `mapstructure:"max_retries"` // Maximum retry attempts
	Delay      int    `mapstructure:"delay"`       // Initial retry delay in milliseconds
	MaxDelay   int    `mapstructure:"max_delay"`   // Maximum retry delay in milliseconds
}

// PostgreSQL contains database connection configuration.
type PostgreSQL struct {
	Host         string `mapstructure:"host"`           // Database hostname
	Port         int    `mapstructure:"port"`           // Database port
	User         string `mapstructure:"user"`           // Database username
	Password     string `mapstructure:"password"`       // Database password
	DBName       string `mapstructure:"db_name"`        // Database name
	MaxOpenConns int    `mapstructure:"max_open_conns"` // Maximum open connections
	MaxIdleConns int    `mapstructure:"max_idle_conns"` // Maximum idle connections
	MaxLifetime  int    `mapstructure:"max_lifetime"`   // Connection lifetime in minutes
	MaxIdleTime  int    `mapstructure:"max_idle_time"`  // Idle timeout in minutes
}

// Redis contains Redis connection configuration.
type Redis struct {
	Host     string `mapstructure:"host"`     // Redis hostname
	Port     int    `mapstructure:"port"`     // Redis port
	Username string `mapstructure:"username"` // Redis username
	Password string `mapstructure:"password"` // Redis password
}

// GeminiAI contains GeminiAI API configuration.
type GeminiAI struct {
	APIKey string `mapstructure:"api_key"` // API key for authentication
	Model  string `mapstructure:"model"`   // Model version to use
}

// Discord contains Discord bot configuration.
type Discord struct {
	Token string `mapstructure:"token"` // Discord bot token for authentication
}

// BatchSizes configures how many items to process in each batch.
type BatchSizes struct {
	FriendUsers int `mapstructure:"friend_users"` // Number of friends to process in one batch
	PurgeUsers  int `mapstructure:"purge_users"`  // Number of users to check for purging in one batch
	GroupUsers  int `mapstructure:"group_users"`  // Number of group members to process in one batch
	PurgeGroups int `mapstructure:"purge_groups"` // Number of groups to check for purging in one batch
	QueueItems  int `mapstructure:"queue_items"`  // Number of queue items to process in one batch
}

// ThresholdLimits configures various thresholds for worker operations.
type ThresholdLimits struct {
	FlaggedUsers           int    `mapstructure:"flagged_users"`                  // Maximum number of flagged users before stopping worker
	MinFlaggedForGroup     int    `mapstructure:"min_flagged_for_group"`          // Minimum number of flagged users needed to flag a group
	MinFollowersForPopular uint64 `mapstructure:"min_followers_for_popular_user"` // Minimum follower count to consider a user "popular"
	MaxGroupMembersTrack   uint64 `mapstructure:"max_group_members_track"`        // Maximum group members before skipping tracking
}

// RPCServer contains server configuration options.
type RPCServer struct {
	Host string `mapstructure:"host"` // Host address to listen on
	Port int    `mapstructure:"port"` // Port number to listen on
}

// RPCIPConfig contains IP validation configuration.
type RPCIPConfig struct {
	// Enable checking of forwarded headers (X-Forwarded-For, etc.)
	EnableHeaderCheck bool     `mapstructure:"enable_header_check"` // Enable checking of forwarded headers (X-Forwarded-For, etc.)
	TrustedProxies    []string `mapstructure:"trusted_proxies"`     // List of trusted proxy IPs that can set forwarded headers
	CustomHeaders     []string `mapstructure:"custom_headers"`      // Headers to check for client IP, in order of precedence
	AllowLocalIPs     bool     `mapstructure:"allow_local_ips"`     // Allow local IPs (127.0.0.1, etc.) for development/testing
}

// RPCRateLimit contains rate limiting configuration for the RPC server.
type RPCRateLimit struct {
	RequestsPerSecond float64 `mapstructure:"requests_per_second"` // Maximum number of requests per second per IP
	BurstSize         int     `mapstructure:"burst_size"`          // Maximum burst size for rate limiting
}

// Proxy contains proxy-related configuration.
type Proxy struct {
	DefaultCooldown   int                      `mapstructure:"default_cooldown"`   // Default cooldown in milliseconds
	RequestTimeout    int                      `mapstructure:"request_timeout"`    // HTTP request timeout in milliseconds
	UnhealthyDuration int                      `mapstructure:"unhealthy_duration"` // Duration to mark proxy as unhealthy in milliseconds
	Endpoints         map[string]EndpointLimit `mapstructure:"endpoints"`          // Endpoint-specific cooldowns
}

// EndpointCooldown defines the cooldown period for a specific endpoint.
type EndpointLimit struct {
	Pattern  string `mapstructure:"pattern"`  // URL pattern with placeholders
	Cooldown int    `mapstructure:"cooldown"` // Time in milliseconds until next request allowed
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
