package auth

import (
	"context"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func TestLoginHashesOpaqueSessionAndCSRFValues(t *testing.T) {
	repository := newMemoryRepository(t)
	service := NewService(repository)

	identity, token, csrfToken, expiresAt, err := service.Login(context.Background(), " RIDER@EXAMPLE.TEST ", "correct horse battery staple")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if identity.ID != repository.user.ID || identity.Role != "rider" || identity.Email != "rider@example.test" {
		t.Fatalf("unexpected authenticated identity: %+v", identity)
	}
	if !validOpaqueToken(token) {
		t.Fatal("session token is not a valid 32-byte opaque token")
	}
	if !service.ValidateCSRF(Principal{CSRFTokenHash: HashToken(csrfToken)}, csrfToken) {
		t.Fatal("generated session-bound CSRF token did not validate")
	}
	if service.ValidateCSRF(Principal{CSRFTokenHash: HashToken(csrfToken)}, csrfToken+"x") {
		t.Fatal("modified CSRF token unexpectedly validated")
	}
	if expiresAt.Before(time.Now().Add(SessionLifetime-time.Minute)) || expiresAt.After(time.Now().Add(SessionLifetime+time.Minute)) {
		t.Fatalf("session expiry is outside its bounded lifetime: %s", expiresAt)
	}

	tokenHash := HashToken(token)
	stored, ok := repository.sessions[hex.EncodeToString(tokenHash)]
	if !ok {
		t.Fatal("session was not persisted by its token hash")
	}
	if len(tokenHash) != 32 || len(stored.CSRFTokenHash) != 32 {
		t.Fatal("session token and CSRF values must be stored as SHA-256 hashes")
	}
	if _, err := service.ResolveSession(context.Background(), token); err != nil {
		t.Fatalf("resolve new session: %v", err)
	}

	if err := service.RevokeSession(context.Background(), token); err != nil {
		t.Fatalf("revoke session: %v", err)
	}
	if _, err := service.ResolveSession(context.Background(), token); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("revoked session error = %v, want ErrSessionNotFound", err)
	}
}

func TestInvalidPasswordAndUnknownEmailShareTheSameFailure(t *testing.T) {
	repository := newMemoryRepository(t)
	service := NewService(repository)

	_, _, _, _, badPasswordErr := service.Login(context.Background(), "rider@example.test", "wrong password")
	_, _, _, _, unknownUserErr := service.Login(context.Background(), "missing@example.test", "wrong password")
	if !errors.Is(badPasswordErr, ErrInvalidCredentials) || !errors.Is(unknownUserErr, ErrInvalidCredentials) {
		t.Fatalf("credentials errors differ: bad password=%v unknown user=%v", badPasswordErr, unknownUserErr)
	}
}

func TestSessionSurvivesServiceRecreationAndExpires(t *testing.T) {
	repository := newMemoryRepository(t)
	firstService := NewService(repository)
	_, token, _, _, err := firstService.Login(context.Background(), "rider@example.test", "correct horse battery staple")
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	restartedService := NewService(repository)
	if _, err := restartedService.ResolveSession(context.Background(), token); err != nil {
		t.Fatalf("resolve session after service recreation: %v", err)
	}
	repository.expires[hex.EncodeToString(HashToken(token))] = time.Now().Add(-time.Second)
	if _, err := restartedService.ResolveSession(context.Background(), token); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("expired session error = %v, want ErrSessionNotFound", err)
	}
}

func TestCanAccessRideEnforcesRoleOwnership(t *testing.T) {
	rider := Principal{Identity: Identity{ID: "rider-1", Role: "rider"}}
	driver := Principal{Identity: Identity{ID: "driver-1", Role: "driver"}}
	if !CanAccessRide(rider, "rider-1", "driver-1") || CanAccessRide(rider, "rider-2", "driver-1") {
		t.Fatal("rider ownership check failed")
	}
	if !CanAccessRide(driver, "rider-1", "driver-1") || CanAccessRide(driver, "rider-1", "driver-2") || CanAccessRide(driver, "rider-1", "") {
		t.Fatal("driver assignment ownership check failed")
	}
}

type memoryRepository struct {
	user     Credentials
	sessions map[string]StoredSession
	expires  map[string]time.Time
	revoked  map[string]bool
}

func newMemoryRepository(t *testing.T) *memoryRepository {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte("correct horse battery staple"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash test password: %v", err)
	}
	return &memoryRepository{
		user: Credentials{
			ID:           "20000000-0000-4000-8000-000000000001",
			Email:        "rider@example.test",
			DisplayName:  "Demo Rider",
			Role:         "rider",
			PasswordHash: string(hash),
		},
		sessions: make(map[string]StoredSession),
		expires:  make(map[string]time.Time),
		revoked:  make(map[string]bool),
	}
}

func (r *memoryRepository) FindUserByEmail(_ context.Context, email string) (Credentials, error) {
	if email != r.user.Email {
		return Credentials{}, ErrUserNotFound
	}
	return r.user, nil
}

func (r *memoryRepository) CreateSession(_ context.Context, userID string, tokenHash, csrfTokenHash []byte, expiresAt time.Time) error {
	r.sessions[hex.EncodeToString(tokenHash)] = StoredSession{
		Identity: Identity{
			ID:          userID,
			Email:       r.user.Email,
			DisplayName: r.user.DisplayName,
			Role:        r.user.Role,
		},
		CSRFTokenHash: append([]byte(nil), csrfTokenHash...),
	}
	r.expires[hex.EncodeToString(tokenHash)] = expiresAt
	return nil
}

func (r *memoryRepository) FindActiveSession(_ context.Context, tokenHash []byte) (StoredSession, error) {
	key := hex.EncodeToString(tokenHash)
	if r.revoked[key] || !time.Now().Before(r.expires[key]) {
		return StoredSession{}, ErrSessionNotFound
	}
	session, ok := r.sessions[key]
	if !ok {
		return StoredSession{}, ErrSessionNotFound
	}
	return session, nil
}

func (r *memoryRepository) RevokeSession(_ context.Context, tokenHash []byte) error {
	r.revoked[hex.EncodeToString(tokenHash)] = true
	return nil
}
