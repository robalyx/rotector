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
	d1Client   *api.D1Client
	r2Client   *api.R2Client
	Queue      *manager.Queue
	UserFlags  *manager.UserFlags
	GroupFlags *manager.GroupFlags
	IPTracking *manager.IPTracking
}

// NewClient creates a new cloudflare client with all managers.
func NewClient(cfg *config.Config, db database.Client, logger *zap.Logger) *Client {
	d1API := api.NewD1Client(
		cfg.Worker.Cloudflare.AccountID,
		cfg.Worker.Cloudflare.DatabaseID,
		cfg.Worker.Cloudflare.APIToken,
		cfg.Worker.Cloudflare.APIEndpoint,
	)

	r2API, err := api.NewR2Client(
		cfg.Worker.Cloudflare.R2Endpoint,
		cfg.Worker.Cloudflare.R2AccessKeyID,
		cfg.Worker.Cloudflare.R2SecretAccessKey,
		cfg.Worker.Cloudflare.R2BucketName,
		cfg.Worker.Cloudflare.R2Region,
		cfg.Worker.Cloudflare.R2UseSSL,
	)
	if err != nil {
		logger.Fatal("Failed to create R2 client", zap.Error(err))
	}

	return &Client{
		d1Client:   d1API,
		r2Client:   r2API,
		Queue:      manager.NewQueue(d1API, logger.Named("cloudflare")),
		UserFlags:  manager.NewUserFlags(d1API, db, logger.Named("user_flags")),
		GroupFlags: manager.NewGroupFlags(d1API, logger.Named("group_flags")),
		IPTracking: manager.NewIPTracking(d1API, logger.Named("ip_tracking")),
	}
}

// ExecuteSQL executes an arbitrary SQL query using the D1 API.
func (c *Client) ExecuteSQL(ctx context.Context, query string, params []any) ([]map[string]any, error) {
	return c.d1Client.ExecuteSQL(ctx, query, params)
}

// GetD1Client returns the D1 API client.
func (c *Client) GetD1Client() *api.D1Client {
	return c.d1Client
}

// GetR2Client returns the R2 API client.
func (c *Client) GetR2Client() *api.R2Client {
	return c.r2Client
}
