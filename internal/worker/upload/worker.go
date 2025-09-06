package upload

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/bytedance/sonic"
	"github.com/robalyx/rotector/internal/cloudflare/manager"
	"github.com/robalyx/rotector/internal/setup"
	"github.com/robalyx/rotector/internal/tui/components"
	"github.com/robalyx/rotector/internal/worker/core"
	"github.com/robalyx/rotector/pkg/utils"
	"go.uber.org/zap"
	"zombiezen.com/go/sqlite"
)

const (
	DefaultPollInterval = 30 * time.Second
	DefaultBatchSize    = 5
	DownloadTimeout     = 60 * time.Second
	MaxFileSize         = 100 * 1024 * 1024 // 100MB
)

var (
	ErrUnsupportedFileType        = errors.New("unsupported file type")
	ErrDownloadFailed             = errors.New("download failed with status")
	ErrFileTooLarge               = errors.New("file too large")
	ErrUploadAPIBaseNotConfigured = errors.New("upload_api_base not configured in worker.toml")
	ErrUploadTokenNotConfigured   = errors.New("upload_admin_token not configured in worker.toml")
	ErrDownloadURLRequestFailed   = errors.New("download URL request failed")
	ErrDownloadURLEmpty           = errors.New("download URL is empty in response")
	ErrInvalidCSVHeaders          = errors.New("invalid CSV headers: expected [user_id, confidence, reason]")
	ErrInvalidRecord              = errors.New("invalid record: expected 3 fields")
	ErrInvalidUserID              = errors.New("invalid user_id")
	ErrInvalidConfidence          = errors.New("invalid confidence (must be between 0.0 and 1.0)")
	ErrEmptyReason                = errors.New("empty reason")
	ErrSQLiteNoData               = errors.New("SQLite table 'user_flags' contains no data")
	ErrCSVNoDataRecords           = errors.New("CSV file contains no data records")
	ErrSQLiteUserFlagsNotFound    = errors.New("table 'user_flags' not found in SQLite file")
	ErrUnknownProcessingStatus    = errors.New("unknown processing status")
)

// ProcessingStatus represents the processing status of an upload job.
type ProcessingStatus string

const (
	// ProcessingStatusProcessing indicates the job is currently being processed.
	ProcessingStatusProcessing ProcessingStatus = "processing"
	// ProcessingStatusProcessed indicates the job has been successfully processed.
	ProcessingStatusProcessed ProcessingStatus = "processed"
	// ProcessingStatusFailed indicates the job processing failed.
	ProcessingStatusFailed ProcessingStatus = "processing_failed"
)

// Job represents a pending upload job from the D1 database.
type Job struct {
	JobID        string
	SourceID     string
	SourceName   string
	Filename     string
	FileType     string
	DownloadURL  string
	TotalUploads int
}

// DownloadURLResponse represents the API response for download URL requests.
type DownloadURLResponse struct {
	Success bool `json:"success"`
	Data    struct {
		DownloadURL string `json:"downloadUrl"`
		ExpiresAt   int64  `json:"expiresAt"`
	} `json:"data"`
	Error string `json:"error,omitempty"`
}

// Worker processes upload jobs from the D1 database.
type Worker struct {
	app              *setup.App
	bar              *components.ProgressBar
	reporter         *core.StatusReporter
	logger           *zap.Logger
	pollInterval     time.Duration
	batchSize        int
	httpClient       *http.Client
	uploadAPIBase    string
	uploadAdminToken string
}

// New creates a new upload worker.
func New(app *setup.App, bar *components.ProgressBar, logger *zap.Logger, instanceID string) *Worker {
	reporter := core.NewStatusReporter(app.StatusClient, "upload", instanceID, logger)

	httpClient := &http.Client{
		Timeout: DownloadTimeout,
	}

	return &Worker{
		app:              app,
		bar:              bar,
		reporter:         reporter,
		logger:           logger.Named("upload_worker"),
		pollInterval:     DefaultPollInterval,
		batchSize:        DefaultBatchSize,
		httpClient:       httpClient,
		uploadAPIBase:    app.Config.Worker.Cloudflare.UploadAPIBase,
		uploadAdminToken: app.Config.Worker.Cloudflare.UploadAdminToken,
	}
}

// Start begins the upload worker's main loop.
func (w *Worker) Start(ctx context.Context) {
	w.logger.Info("Upload Worker started", zap.String("workerID", w.reporter.GetWorkerID()))

	w.reporter.Start(ctx)
	defer w.reporter.Stop()

	w.bar.SetTotal(100)

	for {
		select {
		case <-ctx.Done():
			w.logger.Info("Upload worker stopping...")
			return
		default:
			w.processJobs(ctx)
		}
	}
}

// processJobs polls for pending jobs and processes them.
func (w *Worker) processJobs(ctx context.Context) {
	// Step 1: Poll for pending jobs (25%)
	w.bar.SetStepMessage("Polling for pending jobs", 25)

	jobs, err := w.getPendingJobs(ctx)
	if err != nil {
		w.logger.Error("Failed to get pending jobs", zap.Error(err))

		if utils.ContextSleep(ctx, 10*time.Second) == utils.SleepCancelled {
			return
		}

		return
	}

	if len(jobs) == 0 {
		w.bar.SetStepMessage("No pending jobs", 0)

		if utils.ContextSleep(ctx, w.pollInterval) == utils.SleepCancelled {
			return
		}

		return
	}

	w.logger.Info("Found pending jobs", zap.Int("count", len(jobs)))

	// Process each job
	for i, job := range jobs {
		if ctx.Err() != nil {
			return
		}

		progress := 25 + (i * 75 / len(jobs))
		w.bar.SetStepMessage(fmt.Sprintf("Processing job %d/%d: %s", i+1, len(jobs), job.Filename), int64(progress))

		if err := w.processJob(ctx, job); err != nil {
			w.logger.Error("Failed to process job",
				zap.Error(err),
				zap.String("jobID", job.JobID),
				zap.String("filename", job.Filename))
		}
	}

	w.bar.SetStepMessage("Completed batch", 100)

	// Short pause before next poll
	if utils.ContextSleep(ctx, 5*time.Second) == utils.SleepCancelled {
		return
	}
}

// getPendingJobs retrieves pending upload jobs from the D1 database.
func (w *Worker) getPendingJobs(ctx context.Context) ([]Job, error) {
	query := `
		SELECT uj.id, uj.source_id, us.source_name,
		       uj.filename, uj.file_type, uj.r2_object_key, us.total_uploads
		FROM upload_jobs uj
		JOIN upload_sources us ON uj.source_id = us.id
		WHERE uj.processing_status = 'awaiting_processing'
		ORDER BY uj.created_at ASC
		LIMIT ?
	`

	result, err := w.app.D1Client.ExecuteSQL(ctx, query, []any{w.batchSize})
	if err != nil {
		return nil, fmt.Errorf("failed to query pending jobs: %w", err)
	}

	jobs := make([]Job, 0, len(result))
	for _, row := range result {
		job := Job{
			JobID:        getString(row, "id"),
			SourceID:     getString(row, "source_id"),
			SourceName:   getString(row, "source_name"),
			Filename:     getString(row, "filename"),
			FileType:     getString(row, "file_type"),
			DownloadURL:  getString(row, "r2_object_key"), // This will be converted to download URL
			TotalUploads: getInt(row, "total_uploads"),
		}
		jobs = append(jobs, job)
	}

	return jobs, nil
}

// processJob processes a single upload job.
func (w *Worker) processJob(ctx context.Context, job Job) error {
	// Mark job as processing
	if err := w.updateJobStatus(ctx, job.JobID, ProcessingStatusProcessing, "", 0, 0); err != nil {
		return fmt.Errorf("failed to mark job as processing: %w", err)
	}

	// Get download URL for the R2 object
	downloadURL, err := w.getDownloadURL(ctx, job.JobID)
	if err != nil {
		if updateErr := w.updateJobStatus(ctx, job.JobID, ProcessingStatusFailed,
			fmt.Sprintf("Failed to get download URL: %v", err), 0, 0); updateErr != nil {
			w.logger.Error("Failed to update job status", zap.Error(updateErr))
		}

		return fmt.Errorf("failed to get download URL: %w", err)
	}

	// Download the file
	fileData, err := w.downloadFile(ctx, downloadURL)
	if err != nil {
		if updateErr := w.updateJobStatus(ctx, job.JobID, ProcessingStatusFailed,
			fmt.Sprintf("Failed to download file: %v", err), 0, 0); updateErr != nil {
			w.logger.Error("Failed to update job status", zap.Error(updateErr))
		}

		return fmt.Errorf("failed to download file: %w", err)
	}

	// Process the file based on type
	var (
		integrationUsers map[int64]*manager.IntegrationUser
		totalRecords     int
	)

	switch strings.ToLower(job.FileType) {
	case "csv":
		integrationUsers, totalRecords, err = w.processCSV(fileData)
	case "sqlite":
		integrationUsers, totalRecords, err = w.processSQLite(fileData)
	default:
		err = fmt.Errorf("%w: %s", ErrUnsupportedFileType, job.FileType)
	}

	if err != nil {
		if updateErr := w.updateJobStatus(ctx, job.JobID, ProcessingStatusFailed,
			fmt.Sprintf("Failed to process file: %v", err), 0, totalRecords); updateErr != nil {
			w.logger.Error("Failed to update job status", zap.Error(updateErr))
		}

		return fmt.Errorf("failed to process %s file: %w", job.FileType, err)
	}

	// Add integration with extracted data
	if err := w.app.D1Client.UserFlags.AddIntegration(ctx, integrationUsers, job.SourceName); err != nil {
		if updateErr := w.updateJobStatus(ctx, job.JobID, ProcessingStatusFailed,
			fmt.Sprintf("Failed to integrate data: %v", err), 0, totalRecords); updateErr != nil {
			w.logger.Error("Failed to update job status", zap.Error(updateErr))
		}

		return fmt.Errorf("failed to integrate data: %w", err)
	}

	// Mark job as completed
	if err := w.updateJobStatus(ctx, job.JobID, ProcessingStatusProcessed, "", len(integrationUsers), totalRecords); err != nil {
		w.logger.Error("Failed to mark job as completed", zap.Error(err), zap.String("jobID", job.JobID))
		return fmt.Errorf("failed to mark job as completed: %w", err)
	}

	w.logger.Info("Successfully processed upload job",
		zap.String("jobID", job.JobID),
		zap.String("filename", job.Filename),
		zap.String("sourceName", job.SourceName),
		zap.Int("usersProcessed", len(integrationUsers)),
		zap.Int("totalRecords", totalRecords))

	return nil
}

// downloadFile downloads a file from the given URL.
func (w *Worker) downloadFile(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := w.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to download file: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: %d", ErrDownloadFailed, resp.StatusCode)
	}

	// Check content length
	if resp.ContentLength > MaxFileSize {
		return nil, fmt.Errorf("%w: %d bytes (max %d)", ErrFileTooLarge, resp.ContentLength, MaxFileSize)
	}

	// Read with size limit
	data, err := io.ReadAll(io.LimitReader(resp.Body, MaxFileSize+1))
	if err != nil {
		return nil, fmt.Errorf("failed to read file data: %w", err)
	}

	if len(data) > MaxFileSize {
		return nil, fmt.Errorf("%w: %d bytes (max %d)", ErrFileTooLarge, len(data), MaxFileSize)
	}

	return data, nil
}

// getDownloadURL gets a presigned download URL for the job's R2 object.
func (w *Worker) getDownloadURL(ctx context.Context, jobID string) (string, error) {
	if w.uploadAPIBase == "" {
		return "", ErrUploadAPIBaseNotConfigured
	}

	url := fmt.Sprintf("%s/v1/upload/download/%s", w.uploadAPIBase, jobID)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create download URL request: %w", err)
	}

	// Use upload admin API token for authentication
	if w.uploadAdminToken == "" {
		return "", ErrUploadTokenNotConfigured
	}

	req.Header.Set("X-Auth-Token", w.uploadAdminToken)

	resp, err := w.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to get download URL: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read download URL response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%w %d: %s", ErrDownloadURLRequestFailed, resp.StatusCode, string(body))
	}

	var downloadResp DownloadURLResponse
	if err := sonic.Unmarshal(body, &downloadResp); err != nil {
		return "", fmt.Errorf("failed to parse download URL response: %w", err)
	}

	if !downloadResp.Success {
		return "", fmt.Errorf("%w: %s", ErrDownloadURLRequestFailed, downloadResp.Error)
	}

	if downloadResp.Data.DownloadURL == "" {
		return "", ErrDownloadURLEmpty
	}

	w.logger.Debug("Retrieved download URL",
		zap.String("jobID", jobID),
		zap.Int64("expiresAt", downloadResp.Data.ExpiresAt))

	return downloadResp.Data.DownloadURL, nil
}

// processCSV processes CSV file data and extracts user records.
func (w *Worker) processCSV(data []byte) (map[int64]*manager.IntegrationUser, int, error) {
	reader := csv.NewReader(bytes.NewReader(data))

	// Read and validate headers
	headers, err := reader.Read()
	if err != nil {
		return nil, 0, fmt.Errorf("failed to read CSV headers: %w", err)
	}

	if len(headers) != 3 || headers[0] != "user_id" || headers[1] != "confidence" || headers[2] != "reason" {
		return nil, 0, fmt.Errorf("%w, got %v", ErrInvalidCSVHeaders, headers)
	}

	users := make(map[int64]*manager.IntegrationUser)
	totalRecords := 0

	for {
		record, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			return nil, totalRecords, fmt.Errorf("failed to read CSV record at line %d: %w", totalRecords+2, err)
		}

		totalRecords++

		if len(record) != 3 {
			return nil, totalRecords, fmt.Errorf("%w at line %d, got %d", ErrInvalidRecord, totalRecords+1, len(record))
		}

		// Parse user_id
		userID, err := strconv.ParseInt(strings.TrimSpace(record[0]), 10, 64)
		if err != nil || userID <= 0 {
			return nil, totalRecords, fmt.Errorf("%w at line %d: %s", ErrInvalidUserID, totalRecords+1, record[0])
		}

		// Parse confidence
		confidence, err := strconv.ParseFloat(strings.TrimSpace(record[1]), 64)
		if err != nil || confidence < 0.0 || confidence > 1.0 {
			return nil, totalRecords, fmt.Errorf("%w at line %d: %s", ErrInvalidConfidence, totalRecords+1, record[1])
		}

		// Parse reason
		reason := strings.TrimSpace(record[2])
		if reason == "" {
			return nil, totalRecords, fmt.Errorf("%w at line %d", ErrEmptyReason, totalRecords+1)
		}

		// Create user record
		users[userID] = &manager.IntegrationUser{
			ID:         userID,
			Confidence: confidence,
			Message:    reason,
			Evidence:   nil,
		}
	}

	if totalRecords == 0 {
		return nil, 0, ErrCSVNoDataRecords
	}

	return users, totalRecords, nil
}

// processSQLite processes SQLite file data and extracts user records.
func (w *Worker) processSQLite(data []byte) (map[int64]*manager.IntegrationUser, int, error) {
	// Create temporary file
	tmpFile, err := os.CreateTemp("", "upload_*.sqlite")
	if err != nil {
		return nil, 0, fmt.Errorf("failed to create temp file: %w", err)
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	// Write data to temp file
	if _, err := tmpFile.Write(data); err != nil {
		return nil, 0, fmt.Errorf("failed to write temp file: %w", err)
	}

	tmpFile.Close()

	// Open SQLite connection
	conn, err := sqlite.OpenConn(tmpFile.Name(), sqlite.OpenReadOnly)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to open SQLite file: %w", err)
	}
	defer conn.Close()

	// Check if user_flags table exists
	stmt := conn.Prep("SELECT name FROM sqlite_master WHERE type='table' AND name='user_flags'")

	defer func() {
		if err := stmt.Finalize(); err != nil {
			w.logger.Error("Failed to finalize SQLite statement", zap.Error(err))
		}
	}()

	hasRows, err := stmt.Step()
	if err != nil {
		return nil, 0, fmt.Errorf("failed to check for user_flags table: %w", err)
	}

	if !hasRows {
		return nil, 0, ErrSQLiteUserFlagsNotFound
	}

	// Query user_flags table
	stmt = conn.Prep("SELECT user_id, confidence, reason FROM user_flags")

	defer func() {
		if err := stmt.Finalize(); err != nil {
			w.logger.Error("Failed to finalize SQLite statement", zap.Error(err))
		}
	}()

	users := make(map[int64]*manager.IntegrationUser)
	totalRecords := 0

	for {
		hasRows, err := stmt.Step()
		if err != nil {
			return nil, totalRecords, fmt.Errorf("SQLite query error: %w", err)
		}

		if !hasRows {
			break
		}

		totalRecords++

		userID := stmt.ColumnInt64(0)
		confidence := stmt.ColumnFloat(1)
		reason := stmt.ColumnText(2)

		// Validate data
		if userID <= 0 {
			return nil, totalRecords, fmt.Errorf("%w in row %d: %d", ErrInvalidUserID, totalRecords, userID)
		}

		if confidence < 0.0 || confidence > 1.0 {
			return nil, totalRecords, fmt.Errorf("%w in row %d: %f", ErrInvalidConfidence, totalRecords, confidence)
		}

		if reason == "" {
			return nil, totalRecords, fmt.Errorf("%w in row %d", ErrEmptyReason, totalRecords)
		}

		// Create user record
		users[userID] = &manager.IntegrationUser{
			ID:         userID,
			Confidence: confidence,
			Message:    reason,
			Evidence:   nil,
		}
	}

	if totalRecords == 0 {
		return nil, 0, ErrSQLiteNoData
	}

	return users, totalRecords, nil
}

// updateJobStatus updates the processing status of a job.
func (w *Worker) updateJobStatus(
	ctx context.Context, jobID string, processingStatus ProcessingStatus, errorMessage string, recordsProcessed, recordsTotal int,
) error {
	var (
		query  string
		params []any
	)

	switch processingStatus {
	case ProcessingStatusProcessed:
		// Mark as completed successfully
		query = `
			UPDATE upload_jobs 
			SET processing_status = ?, 
			    status = 'completed',
			    records_processed = ?,
			    records_total = ?,
			    processing_completed_at = ?
			WHERE id = ?
		`
		params = []any{string(processingStatus), recordsProcessed, recordsTotal, time.Now().Unix(), jobID}

	case ProcessingStatusFailed:
		// Mark as failed
		query = `
			UPDATE upload_jobs 
			SET processing_status = ?,
			    status = 'failed',
			    error_message = ?,
			    records_processed = ?,
			    records_total = ?,
			    processing_completed_at = ?
			WHERE id = ?
		`
		params = []any{string(processingStatus), errorMessage, recordsProcessed, recordsTotal, time.Now().Unix(), jobID}

	case ProcessingStatusProcessing:
		// Mark as processing
		query = `
			UPDATE upload_jobs 
			SET processing_status = ?, 
			    processing_started_at = ?
			WHERE id = ?
		`
		params = []any{string(processingStatus), time.Now().Unix(), jobID}

	default:
		return fmt.Errorf("%w: %s", ErrUnknownProcessingStatus, processingStatus)
	}

	_, err := w.app.D1Client.ExecuteSQL(ctx, query, params)
	if err != nil {
		return fmt.Errorf("failed to update job status: %w", err)
	}

	return nil
}

// getString extracts a string value from SQL result maps.
func getString(row map[string]any, key string) string {
	if val, ok := row[key].(string); ok {
		return val
	}

	return ""
}

// getInt extracts an integer value from SQL result maps.
func getInt(row map[string]any, key string) int {
	if val, ok := row[key].(float64); ok {
		return int(val)
	}

	return 0
}
