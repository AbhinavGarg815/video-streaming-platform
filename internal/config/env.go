package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/joho/godotenv"
)

type Env struct {
	Port                string
	DatabaseURL         string
	JWTSecret           string
	AccessTTL           time.Duration
	RefreshTTL          time.Duration
	AWSRegion           string
	AWSAccessKeyID      string
	AWSSecretAccessKey  string
	OriginalVideoBucket string
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

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		return Env{}, fmt.Errorf("DATABASE_URL is required (set it in .env)")
	}

	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		return Env{}, fmt.Errorf("JWT_SECRET is required (set it in .env)")
	}

	accessTTL := 15 * time.Minute
	if value := os.Getenv("ACCESS_TOKEN_TTL"); value != "" {
		parsed, err := time.ParseDuration(value)
		if err != nil {
			return Env{}, fmt.Errorf("invalid ACCESS_TOKEN_TTL: %w", err)
		}
		accessTTL = parsed
	}

	refreshTTL := 7 * 24 * time.Hour
	if value := os.Getenv("REFRESH_TOKEN_TTL"); value != "" {
		parsed, err := time.ParseDuration(value)
		if err != nil {
			return Env{}, fmt.Errorf("invalid REFRESH_TOKEN_TTL: %w", err)
		}
		refreshTTL = parsed
	}

	awsRegion := os.Getenv("AWS_REGION")
	if awsRegion == "" {
		return Env{}, fmt.Errorf("AWS_REGION is required (set it in .env)")
	}

	awsAccessKeyID := os.Getenv("AWS_ACCESS_KEY_ID")
	if awsAccessKeyID == "" {
		return Env{}, fmt.Errorf("AWS_ACCESS_KEY_ID is required (set it in .env)")
	}

	awsSecretAccessKey := os.Getenv("AWS_SECRET_ACCESS_KEY")
	if awsSecretAccessKey == "" {
		return Env{}, fmt.Errorf("AWS_SECRET_ACCESS_KEY is required (set it in .env)")
	}

	originalVideoBucket := os.Getenv("ORIGINAL_VIDEO_BUCKET")
	if originalVideoBucket == "" {
		originalVideoBucket = "video-app-originals"
	}

	return Env{
		Port:                port,
		DatabaseURL:         databaseURL,
		JWTSecret:           jwtSecret,
		AccessTTL:           accessTTL,
		RefreshTTL:          refreshTTL,
		AWSRegion:           awsRegion,
		AWSAccessKeyID:      awsAccessKeyID,
		AWSSecretAccessKey:  awsSecretAccessKey,
		OriginalVideoBucket: originalVideoBucket,
	}, nil
}
