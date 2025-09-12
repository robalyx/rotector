package api

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// R2Client handles R2 object storage using MinIO S3 client.
type R2Client struct {
	client     *minio.Client
	bucketName string
}

// NewR2Client creates a new R2 client using MinIO SDK.
func NewR2Client(endpoint, accessKeyID, secretAccessKey, bucketName, region string, useSSL bool) (*R2Client, error) {
	// Clean endpoint URL
	endpoint = strings.TrimPrefix(endpoint, "https://")
	endpoint = strings.TrimPrefix(endpoint, "http://")

	// Create MinIO client
	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKeyID, secretAccessKey, ""),
		Secure: useSSL,
		Region: region,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create MinIO client: %w", err)
	}

	return &R2Client{
		client:     client,
		bucketName: bucketName,
	}, nil
}

// PutObject uploads an object to R2 storage.
func (c *R2Client) PutObject(ctx context.Context, key string, data []byte, contentType string) error {
	reader := strings.NewReader(string(data))

	_, err := c.client.PutObject(ctx, c.bucketName, key, reader, int64(len(data)), minio.PutObjectOptions{
		ContentType: contentType,
	})
	if err != nil {
		return fmt.Errorf("failed to put object %s: %w", key, err)
	}

	return nil
}

// GetObject retrieves an object from R2 storage.
func (c *R2Client) GetObject(ctx context.Context, key string) ([]byte, error) {
	object, err := c.client.GetObject(ctx, c.bucketName, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get object %s: %w", key, err)
	}
	defer object.Close()

	data, err := io.ReadAll(object)
	if err != nil {
		return nil, fmt.Errorf("failed to read object %s: %w", key, err)
	}

	return data, nil
}

// DeleteObject removes an object from R2 storage.
func (c *R2Client) DeleteObject(ctx context.Context, key string) error {
	err := c.client.RemoveObject(ctx, c.bucketName, key, minio.RemoveObjectOptions{})
	if err != nil {
		return fmt.Errorf("failed to delete object %s: %w", key, err)
	}

	return nil
}

// HeadObject checks if an object exists in R2 storage.
func (c *R2Client) HeadObject(ctx context.Context, key string) (bool, error) {
	_, err := c.client.StatObject(ctx, c.bucketName, key, minio.StatObjectOptions{})
	if err != nil {
		// Check if it's a not found error
		errResponse := minio.ToErrorResponse(err)
		if errResponse.Code == "NoSuchKey" {
			return false, nil
		}

		return false, fmt.Errorf("failed to check object %s: %w", key, err)
	}

	return true, nil
}

// ListObjects lists objects with the given prefix from R2 storage.
func (c *R2Client) ListObjects(ctx context.Context, prefix string) ([]string, error) {
	objects := make([]string, 0, 100)

	objectCh := c.client.ListObjects(ctx, c.bucketName, minio.ListObjectsOptions{
		Prefix:    prefix,
		Recursive: true,
	})

	for object := range objectCh {
		if object.Err != nil {
			return nil, fmt.Errorf("failed to list objects with prefix %s: %w", prefix, object.Err)
		}

		objects = append(objects, object.Key)
	}

	return objects, nil
}

// GetBucketName returns the R2 bucket name.
func (c *R2Client) GetBucketName() string {
	return c.bucketName
}

// CreateBucketIfNotExists creates the bucket if it doesn't exist.
func (c *R2Client) CreateBucketIfNotExists(ctx context.Context) error {
	exists, err := c.client.BucketExists(ctx, c.bucketName)
	if err != nil {
		return fmt.Errorf("failed to check if bucket %s exists: %w", c.bucketName, err)
	}

	if !exists {
		err = c.client.MakeBucket(ctx, c.bucketName, minio.MakeBucketOptions{
			Region: "auto", // R2 uses "auto" region
		})
		if err != nil {
			return fmt.Errorf("failed to create bucket %s: %w", c.bucketName, err)
		}
	}

	return nil
}
