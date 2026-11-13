package auth

import (
	"context"
	"crypto/sha256"
	"errors"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

type memoryRepository struct {
	accounts   map[string]Account
	sessions   map[[32]byte]Session
	lookupErr  error
	saveErr    error
	sessionErr error
	deleteErr  error
}

func (r *memoryRepository) AccountByUsername(_ context.Context, name string) (Account, error) {
	if r.lookupErr != nil {
		return Account{}, r.lookupErr
	}
	a, ok := r.accounts[name]
	if !ok {
		return Account{}, ErrNotFound
	}
	return a, nil
}

func (r *memoryRepository) SaveSession(_ context.Context, digest [32]byte, accountID int64, verifiedHash string, expires time.Time) error {
	if r.saveErr != nil {
		return r.saveErr
	}
	for _, a := range r.accounts {
		if a.ID == accountID {
			if a.PasswordHash != verifiedHash {
				return ErrUnauthorized
			}
			r.sessions[digest] = Session{Account: a, ExpiresAt: expires}
			return nil
		}
	}
	return ErrNotFound
}

func (r *memoryRepository) SessionByDigest(_ context.Context, digest [32]byte) (Session, error) {
	if r.sessionErr != nil {
		return Session{}, r.sessionErr
	}
	s, ok := r.sessions[digest]
	if !ok {
		return Session{}, ErrNotFound
	}
	return s, nil
}

func (r *memoryRepository) DeleteSession(_ context.Context, digest [32]byte) error {
	if r.deleteErr != nil {
		return r.deleteErr
	}
	delete(r.sessions, digest)
	return nil
}

func newTestService(t *testing.T) (*Service, *memoryRepository, *time.Time) {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte("test-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	r := &memoryRepository{
		accounts: map[string]Account{"owner": {ID: 7, Username: "owner", PasswordHash: string(hash), Role: "owner", StoreID: 1, OwnerID: 7}},
		sessions: make(map[[32]byte]Session),
	}
	now := time.Date(2026, 11, 13, 1, 0, 0, 0, time.UTC)
	return NewService(r, func() time.Time { return now }), r, &now
}

func TestLoginStoresOnlyTokenDigestAndResolvesServerIdentity(t *testing.T) {
	svc, repo, _ := newTestService(t)
	token, err := svc.Login(context.Background(), "owner", "test-password")
	if err != nil || token == "" {
		t.Fatalf("Login() token=%q error=%v", token, err)
	}
	digest := sha256.Sum256([]byte(token))
	if _, ok := repo.sessions[digest]; !ok {
		t.Fatal("session digest was not stored")
	}
	identity, err := svc.Authenticate(context.Background(), token)
	if err != nil || identity.ID != 7 || identity.Role != "owner" || identity.StoreID != 1 || identity.OwnerID != 7 {
		t.Fatalf("Authenticate() identity=%+v error=%v", identity, err)
	}
}

func TestLoginRejectsWrongPasswordAndUnknownAccount(t *testing.T) {
	svc, repo, _ := newTestService(t)
	for _, name := range []string{"owner", "unknown"} {
		if _, err := svc.Login(context.Background(), name, "wrong"); !errors.Is(err, ErrUnauthorized) {
			t.Fatalf("Login(%q) error=%v, want ErrUnauthorized", name, err)
		}
	}
	if len(repo.sessions) != 0 {
		t.Fatal("invalid credentials created a session")
	}
}

func TestSessionExpiresAndLogoutRevokesIt(t *testing.T) {
	svc, _, now := newTestService(t)
	token, err := svc.Login(context.Background(), "owner", "test-password")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Logout(context.Background(), token); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Authenticate(context.Background(), token); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("revoked session error=%v", err)
	}
	token, err = svc.Login(context.Background(), "owner", "test-password")
	if err != nil {
		t.Fatal(err)
	}
	*now = now.Add(8 * time.Hour)
	if _, err := svc.Authenticate(context.Background(), token); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("expired session error=%v", err)
	}
}

func TestRepositoryFailuresPropagateWithoutCreatingIdentity(t *testing.T) {
	svc, repo, _ := newTestService(t)
	dbDown := errors.New("database unavailable")
	repo.lookupErr = dbDown
	if _, err := svc.Login(context.Background(), "owner", "test-password"); !errors.Is(err, dbDown) {
		t.Fatalf("account lookup error=%v", err)
	}
	repo.lookupErr = nil
	repo.saveErr = dbDown
	if _, err := svc.Login(context.Background(), "owner", "test-password"); !errors.Is(err, dbDown) {
		t.Fatalf("session write error=%v", err)
	}
	repo.saveErr = nil
	token, err := svc.Login(context.Background(), "owner", "test-password")
	if err != nil {
		t.Fatal(err)
	}
	repo.sessionErr = dbDown
	if _, err := svc.Authenticate(context.Background(), token); !errors.Is(err, dbDown) {
		t.Fatalf("session read error=%v", err)
	}
	repo.sessionErr = nil
	repo.deleteErr = dbDown
	if err := svc.Logout(context.Background(), token); !errors.Is(err, dbDown) {
		t.Fatalf("session deletion error=%v", err)
	}
	if _, err := svc.Authenticate(context.Background(), ""); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("empty token error=%v", err)
	}
	if err := svc.Logout(context.Background(), ""); err != nil {
		t.Fatalf("empty logout error=%v", err)
	}
}
