package config

import (
	"errors"
	"fmt"
	"os"

	"github.com/knadh/koanf/parsers/toml/v2"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

var (
	ErrConfigFileNotFound    = errors.New("could not find config file in any config path")
	ErrConfigVersionMissing  = errors.New("config file is missing version field")
	ErrConfigVersionMismatch = errors.New("config file version mismatch")
)

// Repository version tag for config file references
const RepositoryVersion = "v1.0.0-beta.1"

// Current version of the config file
const (
	CurrentCommonVersion = 1
	CurrentBotVersion    = 1
	CurrentWorkerVersion = 1
	CurrentAPIVersion    = 1
)

// Config represents the entire application configuration.
type Config struct {
	Common CommonConfig
	Bot    BotConfig
	Worker WorkerConfig
}

// CommonConfig contains configuration shared between bot and worker.
type CommonConfig struct {
	Version        int            `koanf:"version"`
	Debug          Debug          `koanf:"debug"`
	CircuitBreaker CircuitBreaker `koanf:"circuit_breaker"`
	Retry          Retry          `koanf:"retry"`
	PostgreSQL     PostgreSQL     `koanf:"postgresql"`
	Redis          Redis          `koanf:"redis"`
	GeminiAI       GeminiAI       `koanf:"gemini_ai"`
	Proxy          Proxy          `koanf:"proxy"`
	Roverse        Roverse        `koanf:"roverse"`
	Sentry         Sentry         `koanf:"sentry"`
}

// BotConfig contains Discord bot specific configuration.
type BotConfig struct {
	Version        int     `koanf:"version"`
	RequestTimeout int     `koanf:"request_timeout"` // Request timeout in milliseconds
	Discord        Discord `koanf:"discord"`
}

// WorkerConfig contains worker specific configuration.
type WorkerConfig struct {
	Version         int             `koanf:"version"`
	RequestTimeout  int             `koanf:"request_timeout"` // Request timeout in milliseconds
	StartupDelay    int             `koanf:"startup_delay"`
	BatchSizes      BatchSizes      `koanf:"batch_sizes"`
	ThresholdLimits ThresholdLimits `koanf:"threshold_limits"`
}

// Debug contains debug-related configuration.
type Debug struct {
	LogLevel      string `koanf:"log_level"`        // Log level (debug, info, warn, error)
	MaxLogsToKeep int    `koanf:"max_logs_to_keep"` // Maximum log files to keep
	MaxLogLines   int    `koanf:"max_log_lines"`    // Maximum lines per log file
	EnablePprof   bool   `koanf:"enable_pprof"`     // Enable pprof debugging
	PprofPort     int    `koanf:"pprof_port"`       // pprof server port
}

// CircuitBreaker contains circuit breaker configuration.
type CircuitBreaker struct {
	MaxFailures      uint32 `koanf:"max_failures"`      // Number of failures before circuit opens
	FailureThreshold int    `koanf:"failure_threshold"` // Request timeout in milliseconds
	RecoveryTimeout  int    `koanf:"recovery_timeout"`  // Recovery delay in milliseconds
}

// Retry contains retry configuration.
type Retry struct {
	MaxRetries uint64 `koanf:"max_retries"` // Maximum retry attempts
	Delay      int    `koanf:"delay"`       // Initial retry delay in milliseconds
	MaxDelay   int    `koanf:"max_delay"`   // Maximum retry delay in milliseconds
}

// PostgreSQL contains database connection configuration.
type PostgreSQL struct {
	Host         string `koanf:"host"`           // Database hostname
	Port         int    `koanf:"port"`           // Database port
	User         string `koanf:"user"`           // Database username
	Password     string `koanf:"password"`       // Database password
	DBName       string `koanf:"db_name"`        // Database name
	MaxOpenConns int    `koanf:"max_open_conns"` // Maximum open connections
	MaxIdleConns int    `koanf:"max_idle_conns"` // Maximum idle connections
	MaxLifetime  int    `koanf:"max_lifetime"`   // Connection lifetime in minutes
	MaxIdleTime  int    `koanf:"max_idle_time"`  // Idle timeout in minutes
}

// Redis contains Redis connection configuration.
type Redis struct {
	Host     string `koanf:"host"`     // Redis hostname
	Port     int    `koanf:"port"`     // Redis port
	Username string `koanf:"username"` // Redis username
	Password string `koanf:"password"` // Redis password
}

// GeminiAI contains GeminiAI API configuration.
type GeminiAI struct {
	APIKey string `koanf:"api_key"` // API key for authentication
	Model  string `koanf:"model"`   // Model version to use
}

// Discord contains Discord bot configuration.
type Discord struct {
	Token    string         `koanf:"token"`    // Discord bot token for authentication
	Sharding ShardingConfig `koanf:"sharding"` // Sharding configuration
}

// ShardingConfig contains Discord sharding configuration.
type ShardingConfig struct {
	Count      int    `koanf:"count"`       // Number of shards (0 for auto)
	AutoScale  bool   `koanf:"auto_scale"`  // Enable automatic sharding
	SplitCount int    `koanf:"split_count"` // Count to split large shards into (when auto_scale is true)
	ShardIDs   string `koanf:"shard_ids"`   // Comma-separated list of shard IDs to manage (empty for all)
}

// BatchSizes configures how many items to process in each batch.
type BatchSizes struct {
	FriendUsers         int `koanf:"friend_users"`          // Number of friends to process in one batch
	GroupUsers          int `koanf:"group_users"`           // Number of group members to process in one batch
	PurgeUsers          int `koanf:"purge_users"`           // Number of users to check for bans in one batch
	PurgeGroups         int `koanf:"purge_groups"`          // Number of groups to check for bans in one batch
	TrackGroups         int `koanf:"track_groups"`          // Number of group trackings to process in one batch
	QueueItems          int `koanf:"queue_items"`           // Number of queue items to process in one batch
	ThumbnailUsers      int `koanf:"thumbnail_users"`       // Number of users to update thumbnails in one batch
	ThumbnailGroups     int `koanf:"thumbnail_groups"`      // Number of groups to update thumbnails in one batch
	OutfitAnalysis      int `koanf:"outfit_analysis"`       // Maximum concurrent AI requests for outfit analysis
	UserAnalysis        int `koanf:"user_analysis"`         // Maximum concurrent AI requests for user analysis
	FriendAnalysis      int `koanf:"friend_analysis"`       // Maximum concurrent AI requests for friend analysis
	UserAnalysisBatch   int `koanf:"user_analysis_batch"`   // Number of users to analyze in one AI request
	FriendAnalysisBatch int `koanf:"friend_analysis_batch"` // Number of users to analyze in one friend AI request
}

// ThresholdLimits configures various thresholds for worker operations.
type ThresholdLimits struct {
	FlaggedUsers         int     `koanf:"flagged_users"`           // Maximum number of flagged users before stopping worker
	MinGroupFlaggedUsers int     `koanf:"min_group_flagged_users"` // Minimum number of flagged users needed to consider flagging a group
	MinFlaggedPercentage float64 `koanf:"min_flagged_percentage"`  // Minimum percentage of flagged users needed to flag a group
	MinFlaggedOverride   int     `koanf:"min_flagged_override"`    // Flag group if flagged users count exceeds this value
	MaxGroupMembersTrack uint64  `koanf:"max_group_members_track"` // Maximum group members before skipping tracking
}

// Proxy contains proxy-related configuration.
type Proxy struct {
	DefaultCooldown   int                      `koanf:"default_cooldown"`   // Default cooldown in milliseconds
	UnhealthyDuration int                      `koanf:"unhealthy_duration"` // Duration to mark proxy as unhealthy in milliseconds
	Endpoints         map[string]EndpointLimit `koanf:"endpoints"`          // Endpoint-specific cooldowns
}

// EndpointLimit defines the cooldown period for a specific endpoint.
type EndpointLimit struct {
	Pattern  string `koanf:"pattern"`  // URL pattern with placeholders
	Cooldown int    `koanf:"cooldown"` // Time in milliseconds until next request allowed
}

// Roverse contains roverse proxy configuration.
type Roverse struct {
	Domain        string `koanf:"domain"`         // Domain for the roverse proxy service
	SecretKey     string `koanf:"secret_key"`     // Secret key for authentication
	MaxConcurrent int64  `koanf:"max_concurrent"` // Maximum concurrent requests
}

// Sentry contains Sentry error tracking configuration.
type Sentry struct {
	DSN string `koanf:"dsn"` // Sentry DSN for error reporting
}

// LoadConfig loads the configuration from the specified file.
// Returns the config along with the used config directory.
func LoadConfig() (*Config, string, error) {
	k := koanf.New(".")

	// Get user's home directory
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, "", fmt.Errorf("failed to get home directory: %w", err)
	}

	// Define config search paths
	configPaths := []string{
		".rotector",
		homeDir + "/.rotector/config",
		"/etc/rotector/config",
		"/app/config",
		"/config",
		".",
	}

	// Load all config files
	var usedConfigPath string

	configFiles := []string{"common", "bot", "worker", "api"}
	for _, configName := range configFiles {
		configLoaded := false
		for _, path := range configPaths {
			configPath := fmt.Sprintf("%s/%s.toml", path, configName)
			if err := k.Load(file.Provider(configPath), toml.Parser()); err == nil {
				configLoaded = true
				if usedConfigPath == "" {
					usedConfigPath = path
				}
				break
			}
		}
		if !configLoaded {
			return nil, "", fmt.Errorf("%w: %s.toml", ErrConfigFileNotFound, configName)
		}
	}

	var config Config
	if err := k.Unmarshal("", &config); err != nil {
		return nil, "", fmt.Errorf("error unmarshaling config: %w", err)
	}

	// Check versions for each config file
	if err := checkConfigVersion("common", config.Common.Version, CurrentCommonVersion); err != nil {
		return nil, "", err
	}
	if err := checkConfigVersion("bot", config.Bot.Version, CurrentBotVersion); err != nil {
		return nil, "", err
	}
	if err := checkConfigVersion("worker", config.Worker.Version, CurrentWorkerVersion); err != nil {
		return nil, "", err
	}

	return &config, usedConfigPath, nil
}

// checkConfigVersion checks if the config file version is correct.
func checkConfigVersion(name string, current, expected int) error {
	if current == 0 {
		return fmt.Errorf("%w: %s.toml", ErrConfigVersionMissing, name)
	}
	if current != expected {
		return fmt.Errorf(
			"%w: %s.toml (got: %d, expected: %d)\nPlease update your config file from: https://github.com/robalyx/rotector/tree/%s/config/%s.toml",
			ErrConfigVersionMismatch,
			name,
			current,
			expected,
			RepositoryVersion,
			name,
		)
	}
	return nil
}
