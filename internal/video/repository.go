package video

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	pool *pgxpool.Pool
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
