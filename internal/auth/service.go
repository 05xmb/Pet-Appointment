package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"time"

	"golang.org/x/crypto/bcrypt"
)

var (
	ErrNotFound     = errors.New("not found")
	ErrUnauthorized = errors.New("unauthorized")
)

const sessionLifetime = 8 * time.Hour

type Account struct {
	ID           int64
	Username     string
	PasswordHash string
	Role         string
	StoreID      int64
	OwnerID      int64
}

type Identity struct {
	ID      int64
	Role    string
	StoreID int64
	OwnerID int64
}

type Session struct {
	Account   Account
	ExpiresAt time.Time
}

type Repository interface {
	AccountByUsername(context.Context, string) (Account, error)
	SaveSession(context.Context, [32]byte, int64, string, time.Time) error
	SessionByDigest(context.Context, [32]byte) (Session, error)
	DeleteSession(context.Context, [32]byte) error
}

type Service struct {
	repository Repository
	now        func() time.Time
}

func NewService(repository Repository, now func() time.Time) *Service {
	return &Service{repository: repository, now: now}
}

func (s *Service) Login(ctx context.Context, username, password string) (string, error) {
	account, err := s.repository.AccountByUsername(ctx, username)
	if errors.Is(err, ErrNotFound) {
		return "", ErrUnauthorized
	}
	if err != nil {
		return "", err
	}
	if bcrypt.CompareHashAndPassword([]byte(account.PasswordHash), []byte(password)) != nil {
		return "", ErrUnauthorized
	}

	var secret [32]byte
	if _, err := rand.Read(secret[:]); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(secret[:])
	if err := s.repository.SaveSession(ctx, sha256.Sum256([]byte(token)), account.ID, account.PasswordHash, s.now().Add(sessionLifetime)); err != nil {
		return "", err
	}
	return token, nil
}

func (s *Service) Authenticate(ctx context.Context, token string) (Identity, error) {
	if token == "" {
		return Identity{}, ErrUnauthorized
	}
	session, err := s.repository.SessionByDigest(ctx, sha256.Sum256([]byte(token)))
	if errors.Is(err, ErrNotFound) {
		return Identity{}, ErrUnauthorized
	}
	if err != nil {
		return Identity{}, err
	}
	if !s.now().Before(session.ExpiresAt) {
		return Identity{}, ErrUnauthorized
	}
	a := session.Account
	return Identity{ID: a.ID, Role: a.Role, StoreID: a.StoreID, OwnerID: a.OwnerID}, nil
}

func (s *Service) Logout(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	return s.repository.DeleteSession(ctx, sha256.Sum256([]byte(token)))
}
