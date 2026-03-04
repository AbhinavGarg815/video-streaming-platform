package video

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

type Service struct {
	repo          *Repository
	awsRegion     string
	bucket        string
	s3Client      *s3.Client
	presignClient *s3.PresignClient
}

var (
	ErrVideoNotReady    = errors.New("video not ready for playback")
	ErrPlaybackNotFound = errors.New("playback artifacts not found")
)

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
		awsRegion:     region,
		bucket:        bucket,
		s3Client:      client,
		presignClient: s3.NewPresignClient(client),
	}, nil
}

type WatchPlayback struct {
	VideoID           string
	Title             string
	Status            string
	DurationSeconds   int64
	ThumbnailURL      string
	MasterPlaylistURL string
	Variants          []WatchVariant
}

type WatchVariant struct {
	Quality     string
	PlaylistURL string
}

type WatchAsset struct {
	Body        io.ReadCloser
	ContentType string
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

func (s *Service) GetWatchPlayback(ctx context.Context, videoID string) (WatchPlayback, error) {
	video, err := s.repo.GetWatchVideo(ctx, videoID)
	if err != nil {
		return WatchPlayback{}, err
	}

	if video.Status != "ready" {
		return WatchPlayback{}, ErrVideoNotReady
	}

	qualities, err := s.repo.ListWatchQualities(ctx, video.VideoID, video.UploadDate)
	if err != nil {
		return WatchPlayback{}, err
	}
	if len(qualities) == 0 {
		return WatchPlayback{}, ErrPlaybackNotFound
	}

	bucket, basePrefix, parsedVariants := s.resolvePlaybackLocation(qualities)
	_ = bucket
	variants := make([]WatchVariant, 0, len(parsedVariants))
	for _, variant := range parsedVariants {
		variants = append(variants, WatchVariant{
			Quality:     variant.quality,
			PlaylistURL: streamURL(video.VideoID, variant.relativeKey),
		})
	}
	masterURL := streamURL(video.VideoID, "master.m3u8")

	if len(variants) == 0 {
		return WatchPlayback{}, ErrPlaybackNotFound
	}
	if basePrefix == "" {
		return WatchPlayback{}, ErrPlaybackNotFound
	}

	if err := s.repo.IncrementViewCount(ctx, video.VideoID, video.UploadDate); err != nil {
		return WatchPlayback{}, err
	}

	return WatchPlayback{
		VideoID:           video.VideoID,
		Title:             video.Title,
		Status:            video.Status,
		DurationSeconds:   video.DurationSecs,
		ThumbnailURL:      video.ThumbnailURL,
		MasterPlaylistURL: masterURL,
		Variants:          variants,
	}, nil
}

func (s *Service) OpenWatchAsset(ctx context.Context, videoID, assetPath string) (WatchAsset, error) {
	video, err := s.repo.GetWatchVideo(ctx, videoID)
	if err != nil {
		return WatchAsset{}, err
	}

	if video.Status != "ready" {
		return WatchAsset{}, ErrVideoNotReady
	}

	qualities, err := s.repo.ListWatchQualities(ctx, video.VideoID, video.UploadDate)
	if err != nil {
		return WatchAsset{}, err
	}
	if len(qualities) == 0 {
		return WatchAsset{}, ErrPlaybackNotFound
	}

	bucket, basePrefix, _ := s.resolvePlaybackLocation(qualities)
	if bucket == "" || basePrefix == "" {
		return WatchAsset{}, ErrPlaybackNotFound
	}

	cleanAsset := strings.TrimPrefix(path.Clean("/"+assetPath), "/")
	if cleanAsset == "" || cleanAsset == "." {
		cleanAsset = "master.m3u8"
	}

	objectKey := filepath.ToSlash(path.Join(basePrefix, cleanAsset))

	resp, err := s.s3Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(objectKey),
	})
	if err != nil {
		var noSuchKey *s3types.NoSuchKey
		if errors.As(err, &noSuchKey) {
			return WatchAsset{}, ErrPlaybackNotFound
		}
		return WatchAsset{}, fmt.Errorf("open watch asset: %w", err)
	}

	contentType := ""
	if resp.ContentType != nil {
		contentType = *resp.ContentType
	}

	if contentType == "" {
		switch {
		case strings.HasSuffix(cleanAsset, ".m3u8"):
			contentType = "application/vnd.apple.mpegurl"
		case strings.HasSuffix(cleanAsset, ".ts"):
			contentType = "video/mp2t"
		}
	}

	return WatchAsset{Body: resp.Body, ContentType: contentType}, nil
}

type variantLocation struct {
	quality     string
	relativeKey string
}

func (s *Service) resolvePlaybackLocation(qualities []WatchQuality) (string, string, []variantLocation) {
	if len(qualities) == 0 {
		return "", "", nil
	}

	bucket := ""
	basePrefix := ""
	variants := make([]variantLocation, 0, len(qualities))

	for _, quality := range qualities {
		parsedBucket, parsedKey, parseErr := parseS3URI(quality.ManifestURL)
		if parseErr != nil {
			continue
		}

		if bucket == "" {
			bucket = parsedBucket
		}
		if basePrefix == "" {
			variantDir := filepath.ToSlash(path.Dir(parsedKey))
			basePrefix = filepath.ToSlash(path.Dir(variantDir))
		}

		relativeKey := strings.TrimPrefix(parsedKey, basePrefix+"/")
		variants = append(variants, variantLocation{
			quality:     quality.Label,
			relativeKey: relativeKey,
		})
	}

	return bucket, basePrefix, variants
}

func parseS3URI(value string) (bucket string, key string, err error) {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil {
		return "", "", err
	}
	if parsed.Scheme != "s3" {
		return "", "", fmt.Errorf("unsupported scheme")
	}

	bucket = parsed.Host
	key = strings.TrimPrefix(parsed.Path, "/")
	if bucket == "" || key == "" {
		return "", "", fmt.Errorf("invalid s3 uri")
	}

	return bucket, key, nil
}

func s3HTTPURL(bucket, key, region string) string {
	return fmt.Sprintf("https://%s.s3.%s.amazonaws.com/%s", bucket, region, key)
}

func streamURL(videoID, assetPath string) string {
	return fmt.Sprintf("/watch/%s/stream/%s", videoID, strings.TrimPrefix(assetPath, "/"))
}
