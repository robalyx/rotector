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

// Repository version tag for config file references.
const RepositoryVersion = "v1.0.0-beta.1"

// Current version of the config file.
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
	// Version of the common config.
	Version        int            `koanf:"version"`
	Debug          Debug          `koanf:"debug"`
	CircuitBreaker CircuitBreaker `koanf:"circuit_breaker"`
	Retry          Retry          `koanf:"retry"`
	PostgreSQL     PostgreSQL     `koanf:"postgresql"`
	Redis          Redis          `koanf:"redis"`
	OpenAI         OpenAI         `koanf:"openai"`
	Proxy          Proxy          `koanf:"proxy"`
	Roverse        Roverse        `koanf:"roverse"`
	Uptrace        Uptrace        `koanf:"uptrace"`
	Discord        DiscordConfig  `koanf:"discord"`
}

// DiscordConfig contains Discord-related configuration.
type DiscordConfig struct {
	// Self-bot token for server scanning.
	SyncToken string `koanf:"sync_token"`
}

// BotConfig contains Discord bot specific configuration.
type BotConfig struct {
	// Version of the bot config.
	Version int `koanf:"version"`
	// Request timeout in milliseconds.
	RequestTimeout int `koanf:"request_timeout"`
	// Discord configuration.
	Discord Discord `koanf:"discord"`
}

// WorkerConfig contains worker specific configuration.
type WorkerConfig struct {
	// Version of the worker config.
	Version int `koanf:"version"`
	// Request timeout in milliseconds.
	RequestTimeout int `koanf:"request_timeout"`
	// Startup delay in milliseconds.
	StartupDelay int `koanf:"startup_delay"`
	// Batch sizes for worker operations.
	BatchSizes BatchSizes `koanf:"batch_sizes"`
	// Threshold limits for worker operations.
	ThresholdLimits ThresholdLimits `koanf:"threshold_limits"`
	// Cloudflare configuration
	Cloudflare CloudflareConfig `koanf:"cloudflare"`
}

// CloudflareConfig contains Cloudflare D1 configuration.
type CloudflareConfig struct {
	// Cloudflare account ID
	AccountID string `koanf:"account_id"`
	// D1 database ID
	DatabaseID string `koanf:"database_id"`
	// API token with D1 access
	APIToken string `koanf:"api_token"`
	// API endpoint for D1 queries
	APIEndpoint string `koanf:"api_endpoint"`
}

// Debug contains debug-related configuration.
type Debug struct {
	// Log level (debug, info, warn, error).
	LogLevel string `koanf:"log_level"`
	// Maximum log files to keep.
	MaxLogsToKeep int `koanf:"max_logs_to_keep"`
	// Maximum lines per log file.
	MaxLogLines int `koanf:"max_log_lines"`
	// Enable pprof debugging.
	EnablePprof bool `koanf:"enable_pprof"`
	// pprof server port.
	PprofPort int `koanf:"pprof_port"`
}

// CircuitBreaker contains circuit breaker configuration.
type CircuitBreaker struct {
	// Number of failures before circuit opens.
	MaxFailures uint32 `koanf:"max_failures"`
	// Request timeout in milliseconds.
	FailureThreshold int `koanf:"failure_threshold"`
	// Recovery delay in milliseconds.
	RecoveryTimeout int `koanf:"recovery_timeout"`
}

// Retry contains retry configuration.
type Retry struct {
	// Maximum retry attempts.
	MaxRetries uint64 `koanf:"max_retries"`
	// Initial retry delay in milliseconds.
	Delay int `koanf:"delay"`
	// Maximum retry delay in milliseconds.
	MaxDelay int `koanf:"max_delay"`
}

// PostgreSQL contains database connection configuration.
type PostgreSQL struct {
	// Database hostname.
	Host string `koanf:"host"`
	// Database port.
	Port int `koanf:"port"`
	// Database username.
	User string `koanf:"user"`
	// Database password.
	Password string `koanf:"password"`
	// Database name.
	DBName string `koanf:"db_name"`
	// Maximum open connections.
	MaxOpenConns int `koanf:"max_open_conns"`
	// Maximum idle connections.
	MaxIdleConns int `koanf:"max_idle_conns"`
	// Connection lifetime in minutes.
	MaxLifetime int `koanf:"max_lifetime"`
	// Idle timeout in minutes.
	MaxIdleTime int `koanf:"max_idle_time"`
}

// Redis contains Redis connection configuration.
type Redis struct {
	// Redis hostname.
	Host string `koanf:"host"`
	// Redis port.
	Port int `koanf:"port"`
	// Redis username.
	Username string `koanf:"username"`
	// Redis password.
	Password string `koanf:"password"`
}

// OpenAI contains OpenAI API configuration.
type OpenAI struct {
	// Default model to use
	Model string `koanf:"model"`
	// Model to use for rechecking flagged users
	RecheckModel string `koanf:"recheck_model"`
	// List of providers in order of preference
	Providers []Provider `koanf:"providers"`
}

// Provider defines an OpenAI-compatible API provider.
type Provider struct {
	// Provider name (e.g. "google", "requesty", "openrouter")
	Name string `koanf:"name"`
	// Base URL for the API
	BaseURL string `koanf:"base_url"`
	// API key for authentication
	APIKey string `koanf:"api_key"`
	// Maximum concurrent requests
	MaxConcurrent int64 `koanf:"max_concurrent"`
	// Model name mappings for this provider
	ModelMappings map[string]string `koanf:"model_mappings"`
}

// Discord contains Discord bot configuration.
type Discord struct {
	// Discord bot token for authentication.
	Token string `koanf:"token"`
	// Sharding configuration.
	Sharding ShardingConfig `koanf:"sharding"`
}

// ShardingConfig contains Discord sharding configuration.
type ShardingConfig struct {
	// Number of shards (0 for auto).
	Count int `koanf:"count"`
	// Enable automatic sharding.
	AutoScale bool `koanf:"auto_scale"`
	// Count to split large shards into (when auto_scale is true).
	SplitCount int `koanf:"split_count"`
	// Comma-separated list of shard IDs to manage (empty for all).
	ShardIDs string `koanf:"shard_ids"`
}

// BatchSizes configures how many items to process in each batch.
type BatchSizes struct {
	// Number of friends to process in one batch.
	FriendUsers int `koanf:"friend_users"`
	// Number of group members to process in one batch.
	GroupUsers int `koanf:"group_users"`
	// Number of users to check for bans in one batch.
	PurgeUsers int `koanf:"purge_users"`
	// Number of groups to check for bans in one batch.
	PurgeGroups int `koanf:"purge_groups"`
	// Number of group trackings to process in one batch.
	TrackGroups int `koanf:"track_groups"`
	// Number of queue items to process in one batch.
	QueueItems int `koanf:"queue_items"`
	// Number of users to update thumbnails in one batch.
	ThumbnailUsers int `koanf:"thumbnail_users"`
	// Number of groups to update thumbnails in one batch.
	ThumbnailGroups int `koanf:"thumbnail_groups"`
	// Maximum concurrent AI requests for outfit analysis.
	OutfitAnalysis int `koanf:"outfit_analysis"`
	// Maximum concurrent AI requests for user analysis.
	UserAnalysis int `koanf:"user_analysis"`
	// Maximum concurrent AI requests for friend analysis.
	FriendAnalysis int `koanf:"friend_analysis"`
	// Maximum concurrent AI requests for message analysis.
	MessageAnalysis int `koanf:"message_analysis"`
	// Maximum concurrent AI requests for ivan message analysis.
	IvanMessageAnalysis int `koanf:"ivan_message_analysis"`
	// Maximum concurrent AI requests for recheck analysis.
	RecheckAnalysis int `koanf:"recheck_analysis"`
	// Number of outfits to analyze in one AI request.
	OutfitAnalysisBatch int `koanf:"outfit_analysis_batch"`
	// Number of users to analyze in one AI request.
	UserAnalysisBatch int `koanf:"user_analysis_batch"`
	// Number of users to analyze in one friend AI request.
	FriendAnalysisBatch int `koanf:"friend_analysis_batch"`
	// Number of messages to analyze in one AI request.
	MessageAnalysisBatch int `koanf:"message_analysis_batch"`
	// Number of ivan messages to analyze in one AI request.
	IvanMessageAnalysisBatch int `koanf:"ivan_message_analysis_batch"`
	// Number of users to recheck in one AI request.
	RecheckAnalysisBatch int `koanf:"recheck_analysis_batch"`
}

// ThresholdLimits configures various thresholds for worker operations.
type ThresholdLimits struct {
	// Maximum number of flagged users before stopping worker.
	FlaggedUsers int `koanf:"flagged_users"`
	// Minimum number of flagged users needed to consider flagging a group.
	MinGroupFlaggedUsers int `koanf:"min_group_flagged_users"`
	// Minimum percentage of flagged users needed to flag a group.
	MinFlaggedPercentage float64 `koanf:"min_flagged_percentage"`
	// Flag group if flagged users count exceeds this value.
	MinFlaggedOverride int `koanf:"min_flagged_override"`
	// Maximum group members before skipping tracking.
	MaxGroupMembersTrack uint64 `koanf:"max_group_members_track"`
	// Number of messages to accumulate before processing a channel.
	ChannelProcessThreshold int `koanf:"channel_process_threshold"`
}

// Proxy contains proxy-related configuration.
type Proxy struct {
	// Default cooldown in milliseconds.
	DefaultCooldown int `koanf:"default_cooldown"`
	// Duration to mark proxy as unhealthy in milliseconds.
	UnhealthyDuration int `koanf:"unhealthy_duration"`
	// Endpoint-specific cooldowns.
	Endpoints map[string]EndpointLimit `koanf:"endpoints"`
}

// EndpointLimit defines the cooldown period for a specific endpoint.
type EndpointLimit struct {
	// URL pattern with placeholders.
	Pattern string `koanf:"pattern"`
	// Time in milliseconds until next request allowed.
	Cooldown int `koanf:"cooldown"`
}

// Roverse contains roverse proxy configuration.
type Roverse struct {
	// Domain for the roverse proxy service.
	Domain string `koanf:"domain"`
	// Secret key for authentication.
	SecretKey string `koanf:"secret_key"`
	// Maximum concurrent requests.
	MaxConcurrent int64 `koanf:"max_concurrent"`
}

// Uptrace contains Uptrace telemetry configuration.
type Uptrace struct {
	// Uptrace DSN for telemetry.
	DSN string `koanf:"dsn"`
	// Service name for telemetry.
	ServiceName string `koanf:"service_name"`
	// Service version for telemetry.
	ServiceVersion string `koanf:"service_version"`
	// Deployment environment.
	DeployEnvironment string `koanf:"deploy_environment"`
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
			"%w: %s.toml (got: %d, expected: %d)\n"+
				"Please update your config file from: https://github.com/robalyx/rotector/tree/%s/config/%s.toml",
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
