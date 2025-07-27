package queue

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/robalyx/rotector/pkg/utils"
)

var (
	ErrUnexpectedStatusCode = errors.New("unexpected status code")
	ErrD1APIUnsuccessful    = errors.New("D1 API returned unsuccessful response")
)

// Response is the response from the D1 API.
type Response struct {
	Success bool `json:"success"`
	Result  []struct {
		Results []map[string]any `json:"results"`
	} `json:"result"`
}

// CloudflareAPI handles D1 API requests.
type CloudflareAPI struct {
	accountID string
	dbID      string
	token     string
	endpoint  string
	client    *http.Client
}

// NewCloudflareAPI creates a new Cloudflare API client.
func NewCloudflareAPI(accountID, dbID, token, endpoint string) *CloudflareAPI {
	return &CloudflareAPI{
		accountID: accountID,
		dbID:      dbID,
		token:     token,
		endpoint:  endpoint,
		client:    &http.Client{},
	}
}

// ExecuteSQL executes a SQL statement on D1 and returns the results with retries.
func (c *CloudflareAPI) ExecuteSQL(ctx context.Context, sql string, params []any) ([]map[string]any, error) {
	var result []map[string]any

	// Define retry options
	opts := utils.RetryOptions{
		MaxElapsedTime:  30 * time.Second,
		InitialInterval: 1 * time.Second,
		MaxInterval:     5 * time.Second,
		MaxRetries:      3,
	}

	// Execute the request with retries
	err := utils.WithRetry(ctx, func() error {
		var execErr error

		result, execErr = c.executeRequest(ctx, sql, params)

		// Don't retry on validation errors or permanent failures
		if errors.Is(execErr, ErrD1APIUnsuccessful) {
			return backoff.Permanent(execErr)
		}

		return execErr
	}, opts)
	if err != nil {
		return nil, err
	}

	return result, nil
}

// executeRequest executes a single SQL request.
func (c *CloudflareAPI) executeRequest(ctx context.Context, sql string, params []any) ([]map[string]any, error) {
	url := fmt.Sprintf("%s/accounts/%s/d1/database/%s/query", c.endpoint, c.accountID, c.dbID)

	// Prepare request body
	body := map[string]any{
		"sql":    sql,
		"params": params,
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("error marshaling request: %w", err)
	}

	// Create request
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("error creating request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")

	// Execute request
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error executing request: %w", err)
	}
	defer resp.Body.Close()

	// Check response
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("%w: %d: %s", ErrUnexpectedStatusCode, resp.StatusCode, string(body))
	}

	// Parse response
	var d1Resp Response
	if err := json.NewDecoder(resp.Body).Decode(&d1Resp); err != nil {
		return nil, fmt.Errorf("error decoding response: %w", err)
	}

	if !d1Resp.Success {
		return nil, ErrD1APIUnsuccessful
	}

	if len(d1Resp.Result) == 0 || len(d1Resp.Result[0].Results) == 0 {
		return []map[string]any{}, nil
	}

	return d1Resp.Result[0].Results, nil
}
