package video

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrVideoNotFound = errors.New("video not found")

type Repository struct {
	pool *pgxpool.Pool
}

type WatchVideo struct {
	VideoID      string
	Title        string
	Status       string
	DurationSecs int64
	ThumbnailURL string
	ViewCount    int64
	UploadDate   time.Time
}

type WatchQuality struct {
	Label       string
	ManifestURL string
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) CreateUploadingVideo(ctx context.Context, userID int64, title, originalS3Key string) (string, error) {
	var videoID string

	err := r.pool.QueryRow(
		ctx,
		`INSERT INTO videos (user_id, title, duration, original_s3_key, status)
		 VALUES ($1, $2, INTERVAL '0 second', $3, 'uploading')
		 RETURNING id::text`,
		userID,
		title,
		originalS3Key,
	).Scan(&videoID)
	if err != nil {
		return "", fmt.Errorf("create uploading video: %w", err)
	}

	return videoID, nil
}

func (r *Repository) GetWatchVideo(ctx context.Context, videoID string) (WatchVideo, error) {
	var video WatchVideo

	err := r.pool.QueryRow(
		ctx,
		`SELECT id::text,
		        title,
		        status,
		        COALESCE(EXTRACT(EPOCH FROM duration)::bigint, 0),
		        COALESCE(thumbnail_url, ''),
		        view_count,
		        upload_date
		 FROM videos
		 WHERE id = $1::uuid
		 ORDER BY upload_date DESC
		 LIMIT 1`,
		videoID,
	).Scan(
		&video.VideoID,
		&video.Title,
		&video.Status,
		&video.DurationSecs,
		&video.ThumbnailURL,
		&video.ViewCount,
		&video.UploadDate,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return WatchVideo{}, ErrVideoNotFound
		}
		return WatchVideo{}, fmt.Errorf("get watch video: %w", err)
	}

	return video, nil
}

func (r *Repository) IncrementViewCount(ctx context.Context, videoID string, uploadDate time.Time) error {
	_, err := r.pool.Exec(
		ctx,
		`UPDATE videos
		 SET view_count = view_count + 1,
		     updated_at = NOW()
		 WHERE id = $1::uuid
		   AND upload_date = $2`,
		videoID,
		uploadDate,
	)
	if err != nil {
		return fmt.Errorf("increment view count: %w", err)
	}

	return nil
}

func (r *Repository) ListWatchQualities(ctx context.Context, videoID string, uploadDate time.Time) ([]WatchQuality, error) {
	rows, err := r.pool.Query(
		ctx,
		`SELECT quality_label, manifest_url
		 FROM video_qualities
		 WHERE video_id = $1::uuid
		   AND video_upload_date = $2
		 ORDER BY
		   CASE quality_label
		     WHEN '360p' THEN 1
		     WHEN '480p' THEN 2
		     WHEN '720p' THEN 3
		     WHEN '1080p' THEN 4
		     ELSE 99
		   END,
		   quality_label`,
		videoID,
		uploadDate,
	)
	if err != nil {
		return nil, fmt.Errorf("list watch qualities: %w", err)
	}
	defer rows.Close()

	qualities := make([]WatchQuality, 0)
	for rows.Next() {
		var quality WatchQuality
		if err := rows.Scan(&quality.Label, &quality.ManifestURL); err != nil {
			return nil, fmt.Errorf("scan watch quality: %w", err)
		}
		qualities = append(qualities, quality)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate watch qualities: %w", err)
	}

	return qualities, nil
}
