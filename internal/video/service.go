package video

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type Service struct {
	repo          *Repository
	bucket        string
	presignClient *s3.PresignClient
}

type PresignedUpload struct {
	VideoID   string
	UploadURL string
	ObjectKey string
	ExpiresAt time.Time
}

func NewService(ctx context.Context, repo *Repository, region, accessKeyID, secretAccessKey, bucket string) (*Service, error) {
	awsConfig, err := config.LoadDefaultConfig(
		ctx,
		config.WithRegion(region),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(accessKeyID, secretAccessKey, "")),
	)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}

	client := s3.NewFromConfig(awsConfig)

	return &Service{
		repo:          repo,
		bucket:        bucket,
		presignClient: s3.NewPresignClient(client),
	}, nil
}

func (s *Service) CreatePresignedUpload(ctx context.Context, userID int64, fileName, contentType string) (PresignedUpload, error) {
	if userID <= 0 {
		return PresignedUpload{}, fmt.Errorf("invalid user id")
	}

	cleanName := strings.TrimSpace(fileName)
	if cleanName == "" {
		return PresignedUpload{}, fmt.Errorf("file_name is required")
	}

	objectKey := buildObjectKey(cleanName)
	input := &s3.PutObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(objectKey),
	}

	cleanContentType := strings.TrimSpace(contentType)
	if cleanContentType != "" {
		input.ContentType = aws.String(cleanContentType)
	}

	presignDuration := 15 * time.Minute
	result, err := s.presignClient.PresignPutObject(
		ctx,
		input,
		s3.WithPresignExpires(presignDuration),
	)
	if err != nil {
		return PresignedUpload{}, fmt.Errorf("presign put object: %w", err)
	}

	videoID, err := s.repo.CreateUploadingVideo(ctx, userID, cleanName, objectKey)
	if err != nil {
		return PresignedUpload{}, err
	}

	return PresignedUpload{
		VideoID:   videoID,
		UploadURL: result.URL,
		ObjectKey: objectKey,
		ExpiresAt: time.Now().UTC().Add(presignDuration),
	}, nil
}

func buildObjectKey(fileName string) string {
	baseName := path.Base(fileName)
	ext := path.Ext(baseName)
	name := strings.TrimSuffix(baseName, ext)
	name = strings.TrimSpace(strings.ReplaceAll(name, " ", "-"))
	name = strings.ToLower(name)
	if name == "" {
		name = "video"
	}

	randomSuffix := make([]byte, 8)
	if _, err := rand.Read(randomSuffix); err != nil {
		return fmt.Sprintf("uploads/%d-%s%s", time.Now().UTC().Unix(), name, ext)
	}

	return fmt.Sprintf("uploads/%d-%s-%s%s", time.Now().UTC().Unix(), name, hex.EncodeToString(randomSuffix), ext)
}
