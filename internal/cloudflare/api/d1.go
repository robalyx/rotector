package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/bytedance/sonic"
	"github.com/cenkalti/backoff/v4"
	"github.com/robalyx/rotector/pkg/utils"
)

var (
	ErrUnexpectedStatusCode = errors.New("unexpected status code")
	ErrD1APIUnsuccessful    = errors.New("d1 API returned unsuccessful response")
)

// D1Response is the response from the D1 API.
type D1Response struct {
	Success bool `json:"success"`
	Result  []struct {
		Results []map[string]any `json:"results"`
	} `json:"result"`
}

// D1Client handles D1 database API requests.
type D1Client struct {
	*BaseClient

	dbID string
}

// NewD1Client creates a new D1 API client.
func NewD1Client(accountID, dbID, token, endpoint string) *D1Client {
	return &D1Client{
		BaseClient: NewBaseClient(accountID, token, endpoint),
		dbID:       dbID,
	}
}

// ExecuteSQL executes a SQL statement on D1 and returns the results with retries.
func (c *D1Client) ExecuteSQL(ctx context.Context, sql string, params []any) ([]map[string]any, error) {
	var result []map[string]any

	// Execute the request with retries
	err := utils.WithRetry(ctx, func() error {
		var execErr error

		result, execErr = c.executeRequest(ctx, sql, params)

		// Don't retry on validation errors or permanent failures
		if errors.Is(execErr, ErrD1APIUnsuccessful) {
			return backoff.Permanent(execErr)
		}

		return execErr
	}, DefaultRetryOptions())
	if err != nil {
		return nil, err
	}

	return result, nil
}

// executeRequest executes a single SQL request.
func (c *D1Client) executeRequest(ctx context.Context, sql string, params []any) ([]map[string]any, error) {
	url := fmt.Sprintf("%s/accounts/%s/d1/database/%s/query", c.GetEndpoint(), c.GetAccountID(), c.dbID)

	// Prepare request body
	body := map[string]any{
		"sql":    sql,
		"params": params,
	}

	jsonBody, err := sonic.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("error marshaling request: %w", err)
	}

	// Create request
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("error creating request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.GetToken())
	req.Header.Set("Content-Type", "application/json")

	// Execute request
	resp, err := c.GetHTTPClient().Do(req)
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
	var d1Resp D1Response
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
