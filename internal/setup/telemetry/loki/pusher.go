package loki

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/robalyx/rotector/internal/setup/config"
)

// ErrUnexpectedStatusCode is returned when Loki responds with an unexpected status code.
var ErrUnexpectedStatusCode = errors.New("unexpected status code from Loki")

// Pusher handles batching and sending log entries to Loki.
type Pusher struct {
	config    config.Loki
	cancel    context.CancelFunc
	client    *http.Client
	quit      chan struct{}
	entry     chan logEntry
	waitGroup sync.WaitGroup
	logsBatch []streamValue
	pushURL   string
}

// NewPusher creates a new Loki pusher with the given configuration.
func NewPusher(ctx context.Context, config config.Loki) *Pusher {
	// Build the full push URL
	pushURL := config.URL + "/loki/api/v1/push"

	ctx, cancel := context.WithCancel(ctx)

	pusher := &Pusher{
		config:    config,
		cancel:    cancel,
		client:    &http.Client{Timeout: 10 * time.Second},
		quit:      make(chan struct{}),
		entry:     make(chan logEntry, config.BatchMaxSize*2), // Buffer entries
		logsBatch: make([]streamValue, 0, config.BatchMaxSize),
		pushURL:   pushURL,
	}

	pusher.waitGroup.Add(1)

	go pusher.run(ctx)

	return pusher
}

// AddEntry adds a log entry to be sent to Loki.
func (p *Pusher) AddEntry(entry logEntry) {
	select {
	case p.entry <- entry:
	default:
		// Channel is full, drop the entry to prevent blocking
		slog.Warn("Loki entry channel full, dropping log entry")
	}
}

// Stop gracefully shuts down the pusher.
func (p *Pusher) Stop() {
	close(p.quit)
	p.waitGroup.Wait()
	p.cancel()
}

// run is the main goroutine that handles batching and sending logs.
func (p *Pusher) run(ctx context.Context) {
	batchMaxWait := time.Duration(p.config.BatchMaxWaitMS) * time.Millisecond

	ticker := time.NewTicker(batchMaxWait)
	defer ticker.Stop()

	defer func() {
		// Send any remaining logs before shutting down
		if len(p.logsBatch) > 0 {
			if err := p.send(ctx); err != nil {
				slog.Error("failed to send final Loki batch", slog.Any("error", err))
			}
		}

		p.waitGroup.Done()
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case <-p.quit:
			return
		case entry := <-p.entry:
			p.logsBatch = append(p.logsBatch, p.createStreamValue(entry))

			// Send batch if it's full
			if len(p.logsBatch) >= p.config.BatchMaxSize {
				if err := p.send(ctx); err != nil {
					slog.Error("failed to send Loki batch", slog.Any("error", err))
				}

				p.logsBatch = p.logsBatch[:0] // Reset slice
			}
		case <-ticker.C:
			// Send batch if there are entries waiting
			if len(p.logsBatch) > 0 {
				if err := p.send(ctx); err != nil {
					slog.Error("failed to send Loki batch", slog.Any("error", err))
				}

				p.logsBatch = p.logsBatch[:0] // Reset slice
			}
		}
	}
}

// createStreamValue converts a logEntry to a Loki stream value.
func (p *Pusher) createStreamValue(entry logEntry) streamValue {
	ts := time.UnixMilli(int64(entry.Timestamp))
	return streamValue{strconv.FormatInt(ts.UnixNano(), 10), entry.raw}
}

// send transmits the current batch to Loki.
func (p *Pusher) send(ctx context.Context) error {
	if len(p.logsBatch) == 0 {
		return nil
	}

	// All entries share the same labels, so create a single stream
	streams := []stream{
		{
			Stream: p.config.Labels,
			Values: p.logsBatch,
		},
	}

	// Create the push request
	pushRequest := lokiPushRequest{
		Streams: streams,
	}

	// Create compressed JSON payload
	var buf bytes.Buffer

	gz := gzip.NewWriter(&buf)

	if err := json.NewEncoder(gz).Encode(pushRequest); err != nil {
		return fmt.Errorf("failed to encode JSON: %w", err)
	}

	if err := gz.Close(); err != nil {
		return fmt.Errorf("failed to compress: %w", err)
	}

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.pushURL, &buf)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Content-Encoding", "gzip")

	// Add basic auth if configured
	if p.config.Username != "" && p.config.Password != "" {
		req.SetBasicAuth(p.config.Username, p.config.Password)
	}

	// Send the request
	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// Check response status
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("%w: %d", ErrUnexpectedStatusCode, resp.StatusCode)
	}

	return nil
}
