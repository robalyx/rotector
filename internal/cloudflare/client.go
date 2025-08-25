package cloudflare

import (
	"context"

	"github.com/robalyx/rotector/internal/cloudflare/api"
	"github.com/robalyx/rotector/internal/cloudflare/manager"
	"github.com/robalyx/rotector/internal/database"
	"github.com/robalyx/rotector/internal/setup/config"
	"go.uber.org/zap"
)

// Client provides access to all cloudflare-related operations.
type Client struct {
	api        *api.Cloudflare
	Queue      *manager.Queue
	UserFlags  *manager.UserFlags
	IPTracking *manager.IPTracking
}

// NewClient creates a new cloudflare client with all managers.
func NewClient(cfg *config.Config, db database.Client, logger *zap.Logger) *Client {
	cloudflareAPI := api.NewCloudflare(
		cfg.Worker.Cloudflare.AccountID,
		cfg.Worker.Cloudflare.DatabaseID,
		cfg.Worker.Cloudflare.APIToken,
		cfg.Worker.Cloudflare.APIEndpoint,
	)

	return &Client{
		api:        cloudflareAPI,
		Queue:      manager.NewQueue(cloudflareAPI, logger.Named("cloudflare")),
		UserFlags:  manager.NewUserFlags(cloudflareAPI, db, logger.Named("user_flags")),
		IPTracking: manager.NewIPTracking(cloudflareAPI, logger.Named("ip_tracking")),
	}
}

// ExecuteSQL executes an arbitrary SQL query using the D1 API.
func (c *Client) ExecuteSQL(ctx context.Context, query string, params []any) ([]map[string]any, error) {
	return c.api.ExecuteSQL(ctx, query, params)
}
