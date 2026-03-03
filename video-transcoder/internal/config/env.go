package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

type Env struct {
	AWSRegion          string
	AWSAccessKeyID     string
	AWSSecretAccessKey string

	TranscodeQueueURL   string
	CompletionQueueURL  string
	OriginalVideoBucket string
	TranscodedBucket    string

	FFmpegBinary string
	WorkDir      string

	SQSWaitTimeSeconds  int32
	SQSMaxMessages      int32
	VisibilityTimeout   int32
	PollInterval        time.Duration
	ShutdownGracePeriod time.Duration
}

func Load() (Env, error) {
	files := []string{
		".env",
		"../.env",
		"../../.env",
	}

	if _, currentFile, _, ok := runtime.Caller(0); ok {
		projectRoot := filepath.Clean(filepath.Join(filepath.Dir(currentFile), "..", ".."))
		files = append(files,
			filepath.Join(projectRoot, ".env"),
		)
	}

	for _, file := range files {
		if _, err := os.Stat(file); err == nil {
			_ = godotenv.Load(file)
		}
	}

	required := func(key string) (string, error) {
		value := strings.TrimSpace(os.Getenv(key))
		if value == "" {
			return "", fmt.Errorf("%s is required", key)
		}
		return value, nil
	}

	region, err := required("AWS_REGION")
	if err != nil {
		return Env{}, err
	}

	accessKeyID, err := required("AWS_ACCESS_KEY_ID")
	if err != nil {
		return Env{}, err
	}

	secretAccessKey, err := required("AWS_SECRET_ACCESS_KEY")
	if err != nil {
		return Env{}, err
	}

	transcodeQueueURL, err := required("TRANSCODE_QUEUE_URL")
	if err != nil {
		return Env{}, err
	}

	completionQueueURL, err := required("COMPLETION_QUEUE_URL")
	if err != nil {
		return Env{}, err
	}

	originalVideoBucket := strings.TrimSpace(os.Getenv("ORIGINAL_VIDEO_BUCKET"))
	if originalVideoBucket == "" {
		originalVideoBucket = "video-app-originals"
	}

	transcodedBucket := strings.TrimSpace(os.Getenv("TRANSCODED_BUCKET"))
	if transcodedBucket == "" {
		transcodedBucket = "video-app-transcoded"
	}

	ffmpegBinary := strings.TrimSpace(os.Getenv("FFMPEG_BINARY"))
	if ffmpegBinary == "" {
		ffmpegBinary = "ffmpeg"
	}

	workDir := strings.TrimSpace(os.Getenv("WORK_DIR"))
	if workDir == "" {
		workDir = "/tmp/transcoder"
	}

	return Env{
		AWSRegion:           region,
		AWSAccessKeyID:      accessKeyID,
		AWSSecretAccessKey:  secretAccessKey,
		TranscodeQueueURL:   transcodeQueueURL,
		CompletionQueueURL:  completionQueueURL,
		OriginalVideoBucket: originalVideoBucket,
		TranscodedBucket:    transcodedBucket,
		FFmpegBinary:        ffmpegBinary,
		WorkDir:             workDir,
		SQSWaitTimeSeconds:  envInt32("SQS_WAIT_TIME_SECONDS", 20),
		SQSMaxMessages:      envInt32("SQS_MAX_MESSAGES", 1),
		VisibilityTimeout:   envInt32("SQS_VISIBILITY_TIMEOUT_SECONDS", 900),
		PollInterval:        envDuration("POLL_INTERVAL", 2*time.Second),
		ShutdownGracePeriod: envDuration("SHUTDOWN_GRACE_PERIOD", 10*time.Second),
	}, nil
}

func envInt32(key string, fallback int32) int32 {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}

	parsed, err := strconv.ParseInt(value, 10, 32)
	if err != nil {
		return fallback
	}

	return int32(parsed)
}

func envDuration(key string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}

	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}

	return parsed
}
