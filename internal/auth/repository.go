package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrUserAlreadyExists = errors.New("user already exists")

type User struct {
	ID           int64
	Name         string
	Email        string
	PasswordHash string
	CreatedAt    time.Time
}

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) EnsureSchema(ctx context.Context) error {
	query := `
CREATE TABLE IF NOT EXISTS users (
	id BIGSERIAL PRIMARY KEY,
	name TEXT NOT NULL,
	email TEXT NOT NULL UNIQUE,
	password_hash TEXT NOT NULL,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS refresh_tokens (
	id BIGSERIAL PRIMARY KEY,
	user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	token_hash TEXT NOT NULL UNIQUE,
	expires_at TIMESTAMPTZ NOT NULL,
	revoked_at TIMESTAMPTZ,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_refresh_tokens_user_id ON refresh_tokens(user_id);
CREATE INDEX IF NOT EXISTS idx_refresh_tokens_token_hash ON refresh_tokens(token_hash);

CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

CREATE TABLE IF NOT EXISTS videos (
	id UUID NOT NULL DEFAULT uuid_generate_v4(),
	user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	title VARCHAR(255) NOT NULL,
	description TEXT,
	duration INTERVAL NOT NULL,
	thumbnail_url VARCHAR(512),
	original_s3_key VARCHAR(512) NOT NULL,
	status VARCHAR(30) NOT NULL DEFAULT 'uploading',
	view_count BIGINT DEFAULT 0,
	upload_date TIMESTAMPTZ DEFAULT NOW(),
	created_at TIMESTAMPTZ DEFAULT NOW(),
	updated_at TIMESTAMPTZ DEFAULT NOW(),
	CONSTRAINT videos_pkey PRIMARY KEY (id, upload_date),
	CONSTRAINT valid_status CHECK (status IN (
		'uploading', 'uploaded', 'transcoding', 'ready', 'failed'
	))
) PARTITION BY RANGE (upload_date);

CREATE TABLE IF NOT EXISTS videos_default PARTITION OF videos DEFAULT;

CREATE INDEX IF NOT EXISTS idx_videos_user_id ON videos(user_id);
CREATE INDEX IF NOT EXISTS idx_videos_upload_date ON videos(upload_date);
DROP INDEX IF EXISTS idx_videos_status;
CREATE INDEX IF NOT EXISTS idx_videos_status ON videos(status) WHERE status = 'ready';

CREATE OR REPLACE FUNCTION update_timestamp()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
	NEW.updated_at = NOW();
	RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS update_videos_timestamp ON videos_default;
CREATE TRIGGER update_videos_timestamp
	BEFORE UPDATE ON videos_default
	FOR EACH ROW EXECUTE FUNCTION update_timestamp();

CREATE TABLE IF NOT EXISTS video_qualities (
	id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
	video_id UUID NOT NULL,
	video_upload_date TIMESTAMPTZ NOT NULL,
	quality_label VARCHAR(50) NOT NULL,
	manifest_url VARCHAR(512) NOT NULL,
	manifest_type VARCHAR(20) NOT NULL DEFAULT 'hls',
	bitrate_kbps INTEGER,
	width INTEGER,
	height INTEGER,
	approx_size_mb BIGINT,
	created_at TIMESTAMPTZ DEFAULT NOW(),
	UNIQUE (video_id, quality_label),
	CONSTRAINT fk_video_qualities_video
		FOREIGN KEY (video_id, video_upload_date)
		REFERENCES videos(id, upload_date)
		ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_video_qualities_video_id ON video_qualities(video_id);
`

	if _, err := r.pool.Exec(ctx, query); err != nil {
		return fmt.Errorf("ensure auth schema: %w", err)
	}

	return nil
}

func (r *Repository) CreateUser(ctx context.Context, name, email, passwordHash string) (User, error) {
	var user User

	err := r.pool.QueryRow(
		ctx,
		`INSERT INTO users (name, email, password_hash)
		 VALUES ($1, $2, $3)
		 RETURNING id, name, email, password_hash, created_at`,
		name,
		email,
		passwordHash,
	).Scan(&user.ID, &user.Name, &user.Email, &user.PasswordHash, &user.CreatedAt)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "duplicate") || strings.Contains(strings.ToLower(err.Error()), "unique") {
			return User{}, ErrUserAlreadyExists
		}
		return User{}, fmt.Errorf("create user: %w", err)
	}

	return user, nil
}

func (r *Repository) GetUserByEmail(ctx context.Context, email string) (User, error) {
	var user User

	err := r.pool.QueryRow(
		ctx,
		`SELECT id, name, email, password_hash, created_at
		 FROM users
		 WHERE email = $1`,
		email,
	).Scan(&user.ID, &user.Name, &user.Email, &user.PasswordHash, &user.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return User{}, pgx.ErrNoRows
		}
		return User{}, fmt.Errorf("get user by email: %w", err)
	}

	return user, nil
}

func (r *Repository) GetUserByID(ctx context.Context, userID int64) (User, error) {
	var user User

	err := r.pool.QueryRow(
		ctx,
		`SELECT id, name, email, password_hash, created_at
		 FROM users
		 WHERE id = $1`,
		userID,
	).Scan(&user.ID, &user.Name, &user.Email, &user.PasswordHash, &user.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return User{}, pgx.ErrNoRows
		}
		return User{}, fmt.Errorf("get user by id: %w", err)
	}

	return user, nil
}

func (r *Repository) StoreRefreshToken(ctx context.Context, userID int64, tokenHash string, expiresAt time.Time) error {
	_, err := r.pool.Exec(
		ctx,
		`INSERT INTO refresh_tokens (user_id, token_hash, expires_at)
		 VALUES ($1, $2, $3)`,
		userID,
		tokenHash,
		expiresAt,
	)
	if err != nil {
		return fmt.Errorf("store refresh token: %w", err)
	}

	return nil
}

func (r *Repository) ConsumeRefreshToken(ctx context.Context, tokenHash string) (int64, error) {
	var userID int64

	err := r.pool.QueryRow(
		ctx,
		`UPDATE refresh_tokens
		 SET revoked_at = NOW()
		 WHERE token_hash = $1
		   AND revoked_at IS NULL
		   AND expires_at > NOW()
		 RETURNING user_id`,
		tokenHash,
	).Scan(&userID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, pgx.ErrNoRows
		}
		return 0, fmt.Errorf("consume refresh token: %w", err)
	}

	return userID, nil
}
