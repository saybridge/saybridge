package stores

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/saybridge/saybridge/internal/domain"
	"github.com/saybridge/saybridge/pkg/config"
)

type minioStorageRepository struct {
	client     *minio.Client
	bucketName string
	endpoint   string
	useSSL     bool
}

// NewMinioStorageRepository instantiates a new StorageRepository using the MinIO client.
func NewMinioStorageRepository(cfg *config.Config) (domain.StorageRepository, error) {
	client, err := minio.New(cfg.MinioEndpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.MinioAccessKey, cfg.MinioSecretKey, ""),
		Secure: cfg.MinioUseSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to initialize MinIO client: %w", err)
	}

	repo := &minioStorageRepository{
		client:     client,
		bucketName: cfg.MinioBucket,
		endpoint:   cfg.MinioEndpoint,
		useSSL:     cfg.MinioUseSSL,
	}

	// Create bucket on startup if it doesn't exist
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	exists, err := client.BucketExists(ctx, cfg.MinioBucket)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to check bucket existence")
	} else if !exists {
		err = client.MakeBucket(ctx, cfg.MinioBucket, minio.MakeBucketOptions{})
		if err != nil {
			log.Warn().Err(err).Msgf("Failed to create bucket %s", cfg.MinioBucket)
		} else {
			log.Info().Msgf("Successfully created MinIO bucket: %s", cfg.MinioBucket)
		}
	}

	// Set public read-only policy for anonymous access to view avatars and attachments
	policy := fmt.Sprintf(`{
		"Version": "2012-10-17",
		"Statement": [
			{
				"Effect": "Allow",
				"Principal": {"AWS": ["*"]},
				"Action": ["s3:GetObject"],
				"Resource": ["arn:aws:s3:::%s/*"]
			}
		]
	}`, cfg.MinioBucket)

	err = client.SetBucketPolicy(ctx, cfg.MinioBucket, policy)
	if err != nil {
		log.Warn().Err(err).Msgf("Failed to set public read-only policy on bucket %s", cfg.MinioBucket)
	} else {
		log.Info().Msgf("Successfully set public read-only policy on MinIO bucket: %s", cfg.MinioBucket)
	}

	return repo, nil
}

func (r *minioStorageRepository) PresignUpload(ctx context.Context, objectName string, expiry time.Duration) (string, string, error) {
	presignedURL, err := r.client.PresignedPutObject(ctx, r.bucketName, objectName, expiry)
	if err != nil {
		return "", "", fmt.Errorf("failed to generate presigned upload url: %w", err)
	}

	// Compute download URL
	schema := "http"
	if r.useSSL {
		schema = "https"
	}
	downloadURL := fmt.Sprintf("%s://%s/%s/%s", schema, r.endpoint, r.bucketName, objectName)

	return presignedURL.String(), downloadURL, nil
}

func (r *minioStorageRepository) UploadFile(ctx context.Context, objectName string, reader io.Reader, size int64, contentType string) (string, error) {
	_, err := r.client.PutObject(ctx, r.bucketName, objectName, reader, size, minio.PutObjectOptions{
		ContentType: contentType,
	})
	if err != nil {
		return "", fmt.Errorf("failed to upload object to MinIO: %w", err)
	}

	// Compute download URL
	schema := "http"
	if r.useSSL {
		schema = "https"
	}
	downloadURL := fmt.Sprintf("%s://%s/%s/%s", schema, r.endpoint, r.bucketName, objectName)

	return downloadURL, nil
}

func (r *minioStorageRepository) DeleteFile(ctx context.Context, objectName string) error {
	err := r.client.RemoveObject(ctx, r.bucketName, objectName, minio.RemoveObjectOptions{})
	if err != nil {
		return fmt.Errorf("failed to delete object from MinIO: %w", err)
	}
	return nil
}

func (r *minioStorageRepository) GetFileStream(ctx context.Context, objectName string) (io.ReadCloser, error) {
	object, err := r.client.GetObject(ctx, r.bucketName, objectName, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get object from MinIO: %w", err)
	}
	return object, nil
}
