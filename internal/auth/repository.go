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
