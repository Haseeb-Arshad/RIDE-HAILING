package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const SessionLifetime = 7 * 24 * time.Hour

var (
	ErrUserNotFound       = errors.New("user not found")
	ErrSessionNotFound    = errors.New("session not found")
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrStoreUnavailable   = errors.New("authentication store unavailable")
)

type Credentials struct {
	ID           string
	Email        string
	DisplayName  string
	Role         string
	PasswordHash string
}

type Identity struct {
	ID          string `json:"id"`
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
	Role        string `json:"role"`
}

type StoredSession struct {
	Identity
	CSRFTokenHash []byte
}

type Principal struct {
	Identity
	CSRFToken     string
	CSRFTokenHash []byte
}

type Repository interface {
	FindUserByEmail(ctx context.Context, email string) (Credentials, error)
	CreateSession(ctx context.Context, userID string, tokenHash, csrfTokenHash []byte, expiresAt time.Time) error
	FindActiveSession(ctx context.Context, tokenHash []byte) (StoredSession, error)
	RevokeSession(ctx context.Context, tokenHash []byte) error
}

type Service struct {
	repository Repository
	dummyHash  []byte
	now        func() time.Time
}

func NewService(repository Repository) *Service {
	dummyHash, _ := bcrypt.GenerateFromPassword([]byte("ride-hailing-invalid-account"), bcrypt.DefaultCost)
	return &Service{repository: repository, dummyHash: dummyHash, now: time.Now}
}

func (s *Service) Login(ctx context.Context, email, password string) (Identity, string, string, time.Time, error) {
	if s.repository == nil {
		return Identity{}, "", "", time.Time{}, ErrStoreUnavailable
	}
	credentials, err := s.repository.FindUserByEmail(ctx, NormalizeEmail(email))
	if errors.Is(err, ErrUserNotFound) {
		_ = bcrypt.CompareHashAndPassword(s.dummyHash, []byte(password))
		return Identity{}, "", "", time.Time{}, ErrInvalidCredentials
	}
	if err != nil {
		return Identity{}, "", "", time.Time{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	if bcrypt.CompareHashAndPassword([]byte(credentials.PasswordHash), []byte(password)) != nil {
		return Identity{}, "", "", time.Time{}, ErrInvalidCredentials
	}
	if credentials.Role != "rider" && credentials.Role != "driver" {
		return Identity{}, "", "", time.Time{}, ErrInvalidCredentials
	}

	var randomToken [32]byte
	if _, err := rand.Read(randomToken[:]); err != nil {
		return Identity{}, "", "", time.Time{}, err
	}
	token := base64.RawURLEncoding.EncodeToString(randomToken[:])
	csrfToken := CSRFTokenFor(token)
	tokenHash := HashToken(token)
	csrfHash := HashToken(csrfToken)
	expiresAt := s.now().UTC().Add(SessionLifetime)
	if err := s.repository.CreateSession(ctx, credentials.ID, tokenHash, csrfHash, expiresAt); err != nil {
		return Identity{}, "", "", time.Time{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	identity := Identity{
		ID:          credentials.ID,
		Email:       credentials.Email,
		DisplayName: credentials.DisplayName,
		Role:        credentials.Role,
	}
	return identity, token, csrfToken, expiresAt, nil
}

func (s *Service) ResolveSession(ctx context.Context, token string) (Principal, error) {
	if s.repository == nil {
		return Principal{}, ErrStoreUnavailable
	}
	if !validOpaqueToken(token) {
		return Principal{}, ErrSessionNotFound
	}
	tokenHash := HashToken(token)
	stored, err := s.repository.FindActiveSession(ctx, tokenHash)
	if err != nil {
		if errors.Is(err, ErrSessionNotFound) {
			return Principal{}, ErrSessionNotFound
		}
		if !errors.Is(err, ErrStoreUnavailable) {
			err = fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
		}
		return Principal{}, err
	}
	csrfToken := CSRFTokenFor(token)
	csrfHash := HashToken(csrfToken)
	if len(stored.CSRFTokenHash) != sha256.Size || subtle.ConstantTimeCompare(stored.CSRFTokenHash, csrfHash) != 1 {
		return Principal{}, ErrSessionNotFound
	}
	return Principal{
		Identity:      stored.Identity,
		CSRFToken:     csrfToken,
		CSRFTokenHash: stored.CSRFTokenHash,
	}, nil
}

func (s *Service) RevokeSession(ctx context.Context, token string) error {
	if s.repository == nil {
		return ErrStoreUnavailable
	}
	if !validOpaqueToken(token) {
		return ErrSessionNotFound
	}
	if err := s.repository.RevokeSession(ctx, HashToken(token)); err != nil {
		if !errors.Is(err, ErrStoreUnavailable) {
			err = fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
		}
		return err
	}
	return nil
}

func (s *Service) ValidateCSRF(principal Principal, presented string) bool {
	if presented == "" || len(presented) > 128 || len(principal.CSRFTokenHash) != sha256.Size {
		return false
	}
	presentedHash := HashToken(presented)
	return subtle.ConstantTimeCompare(principal.CSRFTokenHash, presentedHash) == 1
}

func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func CanAccessRide(principal Principal, riderID, assignedDriverID string) bool {
	switch principal.Role {
	case "rider":
		return principal.ID == riderID
	case "driver":
		return principal.ID == assignedDriverID && assignedDriverID != ""
	default:
		return false
	}
}

func HashToken(token string) []byte {
	hash := sha256.Sum256([]byte(token))
	return hash[:]
}

func CSRFTokenFor(sessionToken string) string {
	mac := hmac.New(sha256.New, []byte(sessionToken))
	_, _ = mac.Write([]byte("ride-hailing-session-csrf-v1"))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func validOpaqueToken(token string) bool {
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	return err == nil && len(decoded) == 32
}
