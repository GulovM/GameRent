package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
	shared_auth "rent_game_accs/internal/shared/auth"
	"rent_game_accs/internal/shared/database"
)

type PostgresService struct {
	repo      Repository
	txManager database.TxManager
	jwtSecret string
	jwtTTL    time.Duration
}

func NewPostgresService(repo Repository, txManager database.TxManager, jwtSecret string, jwtTTL time.Duration) *PostgresService {
	return &PostgresService{
		repo:      repo,
		txManager: txManager,
		jwtSecret: jwtSecret,
		jwtTTL:    jwtTTL,
	}
}

func generateRandomString() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (s *PostgresService) Register(ctx context.Context, email, password, firstName, lastName string) (*User, string, string, error) {
	var user *User
	var accessToken string
	var refreshToken string

	err := s.txManager.WithinTransaction(ctx, func(txCtx context.Context) error {
		if existing, err := s.repo.GetUserByEmail(txCtx, email); err == nil && existing != nil {
			return errors.New("user already exists")
		} else if err != nil && !errors.Is(err, ErrUserNotFound) {
			return err
		}

		hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			return err
		}

		now := time.Now()
		user, err = NewUser(email, string(hash), now)
		if err != nil {
			return err
		}
		user.FirstName = firstName
		user.LastName = lastName
		user.Role = roleForEmail(email)
		user.EmailVerified = true

		if err := s.repo.CreateUser(txCtx, user); err != nil {
			return err
		}

		accessToken, err = shared_auth.GenerateTokenWithRole(user.ID, string(user.Role), s.jwtTTL, s.jwtSecret)
		if err != nil {
			return err
		}

		refreshToken = generateRandomString()
		rt, err := NewRefreshToken(user.ID, hashRefreshToken(refreshToken), 30*24*time.Hour, now)
		if err != nil {
			return err
		}
		return s.repo.CreateRefreshToken(txCtx, rt)
	})
	if err != nil {
		return nil, "", "", err
	}

	return user, accessToken, refreshToken, nil
}

func (s *PostgresService) Login(ctx context.Context, email, password string) (string, string, error) {
	user, err := s.repo.GetUserByEmail(ctx, email)
	if err != nil {
		return "", "", errors.New("invalid email or password")
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return "", "", errors.New("invalid email or password")
	}

	if err := user.CanAuthenticate(); err != nil {
		return "", "", err
	}

	accessToken, err := shared_auth.GenerateTokenWithRole(user.ID, string(user.Role), s.jwtTTL, s.jwtSecret)
	if err != nil {
		return "", "", err
	}

	now := time.Now()
	refreshToken := generateRandomString()
	rt, err := NewRefreshToken(user.ID, hashRefreshToken(refreshToken), 30*24*time.Hour, now)
	if err != nil {
		return "", "", err
	}
	if err := s.repo.CreateRefreshToken(ctx, rt); err != nil {
		return "", "", err
	}

	return accessToken, refreshToken, nil
}

func (s *PostgresService) Refresh(ctx context.Context, refreshToken string) (string, string, error) {
	var newAccessToken string
	var newRefreshToken string

	err := s.txManager.WithinTransaction(ctx, func(txCtx context.Context) error {
		rt, err := s.repo.GetRefreshToken(txCtx, hashRefreshToken(refreshToken))
		if err != nil {
			return errors.New("invalid refresh token")
		}

		now := time.Now()
		if rt.IsExpired(now) {
			return ErrTokenExpired
		}
		if rt.IsRevoked() {
			return ErrTokenAlreadyRevoked
		}

		if err := rt.Revoke(now); err != nil {
			return err
		}
		if err := s.repo.UpdateRefreshToken(txCtx, rt); err != nil {
			return err
		}

		user, err := s.repo.GetUserByID(txCtx, rt.UserID)
		if err != nil {
			return err
		}
		if user.IsBlocked {
			return ErrUserBlocked
		}

		newAccessToken, err = shared_auth.GenerateTokenWithRole(user.ID, string(user.Role), s.jwtTTL, s.jwtSecret)
		if err != nil {
			return err
		}

		newRefreshToken = generateRandomString()
		newRT, err := NewRefreshToken(user.ID, hashRefreshToken(newRefreshToken), 30*24*time.Hour, now)
		if err != nil {
			return err
		}
		return s.repo.CreateRefreshToken(txCtx, newRT)
	})
	if err != nil {
		return "", "", err
	}

	return newAccessToken, newRefreshToken, nil
}

func (s *PostgresService) Logout(ctx context.Context, refreshToken string) error {
	rt, err := s.repo.GetRefreshToken(ctx, hashRefreshToken(refreshToken))
	if err != nil {
		return errors.New("invalid refresh token")
	}
	if err := rt.Revoke(time.Now()); err != nil {
		return err
	}
	return s.repo.UpdateRefreshToken(ctx, rt)
}

func hashRefreshToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func roleForEmail(email string) UserRole {
	email = strings.ToLower(strings.TrimSpace(email))
	for _, adminEmail := range strings.Split(os.Getenv("ADMIN_EMAILS"), ",") {
		if strings.ToLower(strings.TrimSpace(adminEmail)) == email && email != "" {
			return RoleAdmin
		}
	}
	return RoleRent
}

func (s *PostgresService) Me(ctx context.Context, userID int64) (*User, error) {
	return s.repo.GetUserByID(ctx, userID)
}
