package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrInvalidToken       = errors.New("invalid token")
)

type Service struct {
	repo            *Repository
	jwtSecret       []byte
	accessTokenTTL  time.Duration
	refreshTokenTTL time.Duration
}

type TokenPair struct {
	AccessToken           string
	RefreshToken          string
	AccessTokenExpiresAt  time.Time
	RefreshTokenExpiresAt time.Time
}

func NewService(repo *Repository, jwtSecret string, accessTokenTTL, refreshTokenTTL time.Duration) *Service {
	return &Service{
		repo:            repo,
		jwtSecret:       []byte(jwtSecret),
		accessTokenTTL:  accessTokenTTL,
		refreshTokenTTL: refreshTokenTTL,
	}
}

func (s *Service) Register(ctx context.Context, name, email, password string) (User, TokenPair, error) {
	cleanName := strings.TrimSpace(name)
	cleanEmail := strings.TrimSpace(strings.ToLower(email))
	if cleanName == "" || cleanEmail == "" || password == "" {
		return User{}, TokenPair{}, fmt.Errorf("name, email and password are required")
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return User{}, TokenPair{}, fmt.Errorf("hash password: %w", err)
	}

	user, err := s.repo.CreateUser(ctx, cleanName, cleanEmail, string(hashedPassword))
	if err != nil {
		return User{}, TokenPair{}, err
	}

	tokens, err := s.issueTokens(ctx, user)
	if err != nil {
		return User{}, TokenPair{}, err
	}

	return user, tokens, nil
}

func (s *Service) Login(ctx context.Context, email, password string) (User, TokenPair, error) {
	cleanEmail := strings.TrimSpace(strings.ToLower(email))
	if cleanEmail == "" || password == "" {
		return User{}, TokenPair{}, ErrInvalidCredentials
	}

	user, err := s.repo.GetUserByEmail(ctx, cleanEmail)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return User{}, TokenPair{}, ErrInvalidCredentials
		}
		return User{}, TokenPair{}, err
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return User{}, TokenPair{}, ErrInvalidCredentials
	}

	tokens, err := s.issueTokens(ctx, user)
	if err != nil {
		return User{}, TokenPair{}, err
	}

	return user, tokens, nil
}

func (s *Service) Refresh(ctx context.Context, refreshToken string) (User, TokenPair, error) {
	refreshToken = strings.TrimSpace(refreshToken)
	if refreshToken == "" {
		return User{}, TokenPair{}, ErrInvalidToken
	}

	userID, err := s.repo.ConsumeRefreshToken(ctx, hashToken(refreshToken))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return User{}, TokenPair{}, ErrInvalidToken
		}
		return User{}, TokenPair{}, err
	}

	user, err := s.repo.GetUserByID(ctx, userID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return User{}, TokenPair{}, ErrInvalidToken
		}
		return User{}, TokenPair{}, err
	}

	tokens, err := s.issueTokens(ctx, user)
	if err != nil {
		return User{}, TokenPair{}, err
	}

	return user, tokens, nil
}

func (s *Service) issueTokens(ctx context.Context, user User) (TokenPair, error) {
	now := time.Now().UTC()
	accessExpiresAt := now.Add(s.accessTokenTTL)
	refreshExpiresAt := now.Add(s.refreshTokenTTL)

	claims := jwt.RegisteredClaims{
		Subject:   strconv.FormatInt(user.ID, 10),
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(accessExpiresAt),
	}

	accessToken, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.jwtSecret)
	if err != nil {
		return TokenPair{}, fmt.Errorf("sign access token: %w", err)
	}

	refreshToken, err := generateRefreshToken()
	if err != nil {
		return TokenPair{}, err
	}

	if err := s.repo.StoreRefreshToken(ctx, user.ID, hashToken(refreshToken), refreshExpiresAt); err != nil {
		return TokenPair{}, err
	}

	return TokenPair{
		AccessToken:           accessToken,
		RefreshToken:          refreshToken,
		AccessTokenExpiresAt:  accessExpiresAt,
		RefreshTokenExpiresAt: refreshExpiresAt,
	}, nil
}

func generateRefreshToken() (string, error) {
	randomBytes := make([]byte, 32)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", fmt.Errorf("generate refresh token: %w", err)
	}

	return base64.RawURLEncoding.EncodeToString(randomBytes), nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
