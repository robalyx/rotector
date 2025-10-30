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

// RepositoryVersion is the repository version tag for config file references.
const RepositoryVersion = "v1.0.0-beta.1"

// Current version of the config file.
const (
	CurrentCommonVersion = 1
	CurrentBotVersion    = 1
	CurrentWorkerVersion = 1
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
	Loki           Loki           `koanf:"loki"`
	Discord        DiscordConfig  `koanf:"discord"`
}

// DiscordConfig contains Discord-related configuration.
type DiscordConfig struct {
	// Self-bot tokens for server scanning.
	SyncTokens []string `koanf:"sync_tokens"`
	// Primary verification service configuration.
	VerificationServiceA VerificationServiceConfig `koanf:"verification_service_a"`
	// Secondary verification service configuration.
	VerificationServiceB VerificationServiceConfig `koanf:"verification_service_b"`
}

// VerificationServiceConfig contains configuration for a verification service.
type VerificationServiceConfig struct {
	// Token for verification service bot.
	Token string `koanf:"token"`
	// Guild ID where verification lookups are performed.
	GuildID uint64 `koanf:"guild_id"`
	// Channel ID where verification lookups are performed.
	ChannelID uint64 `koanf:"channel_id"`
	// Command name to execute for verification lookups.
	CommandName string `koanf:"command_name"`
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

// CloudflareConfig contains Cloudflare D1 and R2 configuration.
type CloudflareConfig struct {
	// Cloudflare account ID (shared by D1 and R2)
	AccountID string `koanf:"account_id"`
	// D1 database ID
	DatabaseID string `koanf:"database_id"`
	// API token with D1 access
	APIToken string `koanf:"api_token"`
	// API endpoint for Cloudflare APIs
	APIEndpoint string `koanf:"api_endpoint"`
	// R2 S3-compatible endpoint (https://account-id.r2.cloudflarestorage.com)
	R2Endpoint string `koanf:"r2_endpoint"`
	// R2 access key ID for S3 authentication
	R2AccessKeyID string `koanf:"r2_access_key_id"`
	// R2 secret access key for S3 authentication
	R2SecretAccessKey string `koanf:"r2_secret_access_key"`
	// R2 bucket name for object storage
	R2BucketName string `koanf:"r2_bucket_name"`
	// R2 region (usually "auto" for Cloudflare R2)
	R2Region string `koanf:"r2_region"`
	// R2 use SSL for connections
	R2UseSSL bool `koanf:"r2_use_ssl"`
	// Upload API base URL
	UploadAPIBase string `koanf:"upload_api_base"`
	// Upload admin API token (different from D1/R2 API token)
	UploadAdminToken string `koanf:"upload_admin_token"`
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
	// Maximum number of requests allowed to pass through when the circuit is half-open.
	MaxRequests uint32 `koanf:"max_requests"`
	// The cyclic period of the closed state for the circuit breaker to clear the internal counts.
	Interval int `koanf:"interval"`
	// The period of the open state after which the state of the circuit breaker becomes half-open.
	Timeout int `koanf:"timeout"`
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

// ModelPricing defines cost per million tokens for a model.
type ModelPricing struct {
	// Cost per million input tokens in USD
	Input float64 `koanf:"input"`
	// Cost per million completion tokens in USD
	Completion float64 `koanf:"completion"`
	// Cost per million reasoning tokens in USD
	Reasoning float64 `koanf:"reasoning"`
}

// OpenAI contains OpenAI API configuration.
type OpenAI struct {
	// Base URL for the API
	BaseURL string `koanf:"base_url"`
	// API key for authentication
	APIKey string `koanf:"api_key"`
	// Maximum concurrent requests
	MaxConcurrent int64 `koanf:"max_concurrent"`
	// Model name mappings
	ModelMappings map[string]string `koanf:"model_mappings"`
	// Per-model token pricing
	ModelPricing map[string]ModelPricing `koanf:"model_pricing"`
	// Model to use for user analysis
	UserModel string `koanf:"user_model"`
	// Model to use for user reason analysis
	UserReasonModel string `koanf:"user_reason_model"`
	// Model to use for friend reason analysis
	FriendReasonModel string `koanf:"friend_reason_model"`
	// Model to use for group reason analysis
	GroupReasonModel string `koanf:"group_reason_model"`
	// Model to use for outfit reason analysis
	OutfitReasonModel string `koanf:"outfit_reason_model"`
	// Model to use for category analysis
	CategoryModel string `koanf:"category_model"`
	// Model to use for stats analysis
	StatsModel string `koanf:"stats_model"`
	// Model to use for outfit analysis
	OutfitModel string `koanf:"outfit_model"`
	// Model to use for message analysis
	MessageModel string `koanf:"message_model"`
	// Fallback model to use for user analysis when content is blocked
	UserFallbackModel string `koanf:"user_fallback_model"`
	// Fallback model to use for user reason analysis when content is blocked
	UserReasonFallbackModel string `koanf:"user_reason_fallback_model"`
	// Fallback model to use for friend reason analysis when content is blocked
	FriendReasonFallbackModel string `koanf:"friend_reason_fallback_model"`
	// Fallback model to use for group reason analysis when content is blocked
	GroupReasonFallbackModel string `koanf:"group_reason_fallback_model"`
	// Fallback model to use for outfit reason analysis when content is blocked
	OutfitReasonFallbackModel string `koanf:"outfit_reason_fallback_model"`
	// Fallback model to use for category analysis when content is blocked
	CategoryFallbackModel string `koanf:"category_fallback_model"`
	// Fallback model to use for stats analysis when content is blocked
	StatsFallbackModel string `koanf:"stats_fallback_model"`
	// Fallback model to use for outfit analysis when content is blocked
	OutfitFallbackModel string `koanf:"outfit_fallback_model"`
	// Fallback model to use for message analysis when content is blocked
	MessageFallbackModel string `koanf:"message_fallback_model"`
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
	// Number of users to check for new groups in one batch.
	TrackUserGroups int `koanf:"track_user_groups"`
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
	// Maximum concurrent AI requests for user reason analysis.
	UserReasonAnalysis int `koanf:"user_reason_analysis"`
	// Maximum concurrent AI requests for friend reason analysis.
	FriendReasonAnalysis int `koanf:"friend_reason_analysis"`
	// Maximum concurrent AI requests for group reason analysis.
	GroupReasonAnalysis int `koanf:"group_reason_analysis"`
	// Maximum concurrent AI requests for outfit reason analysis.
	OutfitReasonAnalysis int `koanf:"outfit_reason_analysis"`
	// Maximum concurrent AI requests for category analysis.
	CategoryAnalysis int `koanf:"category_analysis"`
	// Maximum concurrent AI requests for message analysis.
	MessageAnalysis int `koanf:"message_analysis"`
	// Number of outfits to analyze in one AI request.
	OutfitAnalysisBatch int `koanf:"outfit_analysis_batch"`
	// Number of users to analyze in one AI request.
	UserAnalysisBatch int `koanf:"user_analysis_batch"`
	// Number of users to analyze in one user reason AI request.
	UserReasonAnalysisBatch int `koanf:"user_reason_analysis_batch"`
	// Number of users to analyze in one friend reason AI request.
	FriendReasonAnalysisBatch int `koanf:"friend_reason_analysis_batch"`
	// Number of users to analyze in one group reason AI request.
	GroupReasonAnalysisBatch int `koanf:"group_reason_analysis_batch"`
	// Number of users to analyze in one outfit reason AI request.
	OutfitReasonAnalysisBatch int `koanf:"outfit_reason_analysis_batch"`
	// Number of users to analyze in one category AI request.
	CategoryAnalysisBatch int `koanf:"category_analysis_batch"`
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
	MaxGroupMembersTrack int64 `koanf:"max_group_members_track"`
	// Maximum game visits before skipping tracking.
	MaxGameVisitsTrack int64 `koanf:"max_game_visits_track"`
	// Hamming distance threshold for considering outfit images as similar.
	ImageSimilarityThreshold int `koanf:"image_similarity_threshold"`
	// Number of messages to accumulate before processing a channel.
	ChannelProcessThreshold int `koanf:"channel_process_threshold"`
}

// Proxy contains proxy-related configuration.
type Proxy struct {
	// Default cooldown in milliseconds.
	DefaultCooldown int `koanf:"default_cooldown"`
	// Duration to mark proxy as unhealthy in milliseconds.
	UnhealthyDuration int `koanf:"unhealthy_duration"`
	// Proxy API endpoint URL.
	ProxyAPIURL string `koanf:"proxy_api_url"`
	// Proxy API authentication key.
	ProxyAPIKey string `koanf:"proxy_api_key"`
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
	// Proxy API endpoint URL.
	ProxyAPIURL string `koanf:"proxy_api_url"`
	// Proxy API authentication key.
	ProxyAPIKey string `koanf:"proxy_api_key"`
}

// Loki contains Grafana Loki logging configuration.
type Loki struct {
	// Enable Loki integration
	Enabled bool `koanf:"enabled"`
	// Loki server URL (without /loki/api/v1/push suffix)
	URL string `koanf:"url"`
	// Maximum number of log entries per batch
	BatchMaxSize int `koanf:"batch_max_size"`
	// Maximum time to wait before sending a batch (in milliseconds)
	BatchMaxWaitMS int `koanf:"batch_max_wait_ms"`
	// Labels added to all log streams
	Labels map[string]string `koanf:"labels"`
	// Basic authentication username (optional)
	Username string `koanf:"username"`
	// Basic authentication password (optional)
	Password string `koanf:"password"`
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

	// List search paths
	configPaths := []string{
		".rotector",
		homeDir + "/.rotector/config",
		"/etc/rotector/config",
		"/app/config",
		"config",
		".",
	}

	// Load all config files
	var usedConfigPath string

	configFiles := []string{"common", "bot", "worker"}
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
