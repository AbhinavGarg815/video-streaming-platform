package completion

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) ApplyCompletion(ctx context.Context, message Message) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	resolvedVideoID, resolvedUploadDate, err := resolveVideoTarget(ctx, tx, message)
	if err != nil {
		return err
	}

	videoStatus := normalizeStatus(message.Status)
	durationInterval := secondsToIntervalLiteral(message.DurationSeconds)
	thumbnail := strings.TrimSpace(message.ThumbnailURL)

	cmdTag, err := tx.Exec(
		ctx,
		`UPDATE videos v
		SET status = $2,
			duration = COALESCE($3::interval, v.duration),
			thumbnail_url = COALESCE(NULLIF($4, ''), v.thumbnail_url),
			updated_at = NOW()
		WHERE v.id = $1::uuid
		  AND v.upload_date = $5`,
		resolvedVideoID,
		videoStatus,
		durationInterval,
		thumbnail,
		resolvedUploadDate,
	)
	if err != nil {
		return fmt.Errorf("update video: %w", err)
	}
	if cmdTag.RowsAffected() == 0 {
		return fmt.Errorf("video not found for completion message")
	}

	for _, output := range message.Outputs {
		label := buildQualityLabel(output)
		if label == "master" {
			continue
		}
		manifestURL := fmt.Sprintf("s3://%s/%s", output.Bucket, output.Key)

		_, err := tx.Exec(
			ctx,
			`INSERT INTO video_qualities (
				video_id,
				video_upload_date,
				quality_label,
				manifest_url,
				manifest_type,
				approx_size_mb
			)
			VALUES (
				$1::uuid,
				$2,
				$3,
				$4,
				$5,
				$6
			)
			ON CONFLICT (video_id, quality_label)
			DO UPDATE SET
				manifest_url = EXCLUDED.manifest_url,
				manifest_type = EXCLUDED.manifest_type,
				approx_size_mb = EXCLUDED.approx_size_mb`,
			resolvedVideoID,
			resolvedUploadDate,
			label,
			manifestURL,
			"hls",
			bytesToApproxMB(output.SizeBytes),
		)
		if err != nil {
			return fmt.Errorf("upsert video quality: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}

	return nil
}

func resolveVideoTarget(ctx context.Context, tx pgx.Tx, message Message) (string, time.Time, error) {
	if strings.TrimSpace(message.VideoID) != "" {
		var uploadDate time.Time
		err := tx.QueryRow(
			ctx,
			`SELECT upload_date
			 FROM videos
			 WHERE id = $1::uuid
			 ORDER BY upload_date DESC
			 LIMIT 1`,
			message.VideoID,
		).Scan(&uploadDate)
		if err != nil {
			return "", time.Time{}, fmt.Errorf("video not found by id %s: %w", message.VideoID, err)
		}

		return message.VideoID, uploadDate, nil
	}

	sourceKey := strings.TrimSpace(message.SourceKey)
	if sourceKey == "" {
		return "", time.Time{}, fmt.Errorf("video_id or source_key is required")
	}

	var resolvedVideoID string
	var uploadDate time.Time
	err := tx.QueryRow(
		ctx,
		`SELECT id::text, upload_date
		 FROM videos
		 WHERE original_s3_key = $1
		 ORDER BY upload_date DESC
		 LIMIT 1`,
		sourceKey,
	).Scan(&resolvedVideoID, &uploadDate)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("video not found by source_key %s: %w", sourceKey, err)
	}

	return resolvedVideoID, uploadDate, nil
}

func normalizeStatus(status string) string {
	value := strings.ToLower(strings.TrimSpace(status))
	switch value {
	case "completed", "ready":
		return "ready"
	case "failed":
		return "failed"
	case "transcoding":
		return "transcoding"
	case "uploaded":
		return "uploaded"
	default:
		return "failed"
	}
}

func secondsToIntervalLiteral(seconds int64) *string {
	if seconds <= 0 {
		return nil
	}

	value := fmt.Sprintf("%d seconds", seconds)
	return &value
}

func bytesToApproxMB(sizeBytes int64) int64 {
	if sizeBytes <= 0 {
		return 0
	}

	mb := float64(sizeBytes) / (1024 * 1024)
	return int64(mb + 0.5)
}

func buildQualityLabel(output OutputFile) string {
	resolution := strings.TrimSpace(output.Resolution)
	if resolution == "" {
		return "unknown"
	}
	if strings.EqualFold(resolution, "master") {
		return "master"
	}

	return strings.ToLower(resolution)
}

func parseProcessedAt(value string) time.Time {
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Now().UTC()
	}

	return parsed
}
