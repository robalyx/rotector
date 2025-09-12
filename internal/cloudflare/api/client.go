package api

import (
	"net/http"
	"time"

	"github.com/robalyx/rotector/pkg/utils"
)

// BaseClient provides shared HTTP client functionality for Cloudflare APIs.
type BaseClient struct {
	accountID string
	token     string
	endpoint  string
	client    *http.Client
}

// NewBaseClient creates a new base client for Cloudflare APIs.
func NewBaseClient(accountID, token, endpoint string) *BaseClient {
	return &BaseClient{
		accountID: accountID,
		token:     token,
		endpoint:  endpoint,
		client:    &http.Client{},
	}
}

// GetAccountID returns the Cloudflare account ID.
func (c *BaseClient) GetAccountID() string {
	return c.accountID
}

// GetToken returns the API token.
func (c *BaseClient) GetToken() string {
	return c.token
}

// GetEndpoint returns the API endpoint.
func (c *BaseClient) GetEndpoint() string {
	return c.endpoint
}

// GetHTTPClient returns the HTTP client.
func (c *BaseClient) GetHTTPClient() *http.Client {
	return c.client
}

// DefaultRetryOptions returns the default retry configuration for Cloudflare API calls.
func DefaultRetryOptions() utils.RetryOptions {
	return utils.RetryOptions{
		MaxElapsedTime:  30 * time.Second,
		InitialInterval: 1 * time.Second,
		MaxInterval:     5 * time.Second,
		MaxRetries:      3,
	}
}
