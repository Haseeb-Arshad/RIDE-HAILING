package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

var (
	ErrInvalidAccessToken  = errors.New("invalid access token")
	ErrIdentityNotFound    = errors.New("application identity not found")
	ErrStoreUnavailable    = errors.New("authentication store unavailable")
	ErrVerifierUnavailable = errors.New("Supabase token verification is not configured")
)

type Identity struct {
	ID          string `json:"id"`
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
	Role        string `json:"role"`
}

type Principal struct {
	Identity
}

type Repository interface {
	FindIdentityBySupabaseID(ctx context.Context, supabaseID string) (Identity, error)
}

type TokenVerifier interface {
	Verify(ctx context.Context, token string) (string, error)
}

type Service struct {
	repository Repository
	verifier   TokenVerifier
}

func NewService(repository Repository, verifier TokenVerifier) *Service {
	return &Service{repository: repository, verifier: verifier}
}

func (s *Service) ResolveAccessToken(ctx context.Context, token string) (Principal, error) {
	if s == nil || s.repository == nil {
		return Principal{}, ErrStoreUnavailable
	}
	if s.verifier == nil {
		return Principal{}, ErrVerifierUnavailable
	}
	if token == "" || len(token) > 16<<10 || strings.TrimSpace(token) != token {
		return Principal{}, ErrInvalidAccessToken
	}
	subject, err := s.verifier.Verify(ctx, token)
	if err != nil {
		return Principal{}, err
	}
	identity, err := s.repository.FindIdentityBySupabaseID(ctx, subject)
	if err != nil {
		if errors.Is(err, ErrIdentityNotFound) {
			return Principal{}, ErrIdentityNotFound
		}
		return Principal{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	if identity.Role != "rider" && identity.Role != "driver" {
		return Principal{}, ErrIdentityNotFound
	}
	return Principal{Identity: identity}, nil
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
