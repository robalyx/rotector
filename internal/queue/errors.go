package queue

import "errors"

var (
	// ErrUserNotFound indicates the user was not found in the queue.
	ErrUserNotFound = errors.New("user not found in queue")
	// ErrUserRecentlyQueued indicates the user was queued within the past 7 days.
	ErrUserRecentlyQueued = errors.New("user was recently queued")
	// ErrUserProcessing indicates the user is currently being processed.
	ErrUserProcessing = errors.New("cannot remove user that is already processed or being processed")
	// ErrBatchSizeExceeded indicates the batch size exceeds the maximum allowed.
	ErrBatchSizeExceeded = errors.New("batch size exceeds maximum capacity")
	// ErrEmptyBatch indicates an empty batch was provided.
	ErrEmptyBatch = errors.New("empty batch provided")
)
