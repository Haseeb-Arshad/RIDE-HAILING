package httptransport

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/haseeb-arshad/ride-hailing/apps/api/internal/auth"
)

type authHandlers struct {
	service   *auth.Service
	appOrigin string
}

type sessionResponse struct {
	User auth.Identity `json:"user"`
}

func registerAuthRoutes(router chi.Router, service *auth.Service, appOrigin string) {
	newAuthHandlers(service, appOrigin).registerRoutes(router)
}

func newAuthHandlers(service *auth.Service, appOrigin string) *authHandlers {
	return &authHandlers{service: service, appOrigin: appOrigin}
}

func (h *authHandlers) registerRoutes(router chi.Router) {
	router.With(h.requireAuth).Get("/api/v1/auth/me", h.me)
}

func (h *authHandlers) me(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "unauthenticated", "A valid Supabase access token is required.")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	writeJSON(w, http.StatusOK, sessionResponse{User: principal.Identity})
}

func (h *authHandlers) requireOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Origin") != h.appOrigin {
			writeError(w, r, http.StatusForbidden, "origin_forbidden", "The request origin is not allowed.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (h *authHandlers) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		scheme, token, ok := strings.Cut(r.Header.Get("Authorization"), " ")
		if !ok || !strings.EqualFold(scheme, "Bearer") || token == "" || strings.ContainsAny(token, " \t\r\n") {
			writeError(w, r, http.StatusUnauthorized, "unauthenticated", "A valid Supabase access token is required.")
			return
		}
		principal, err := h.service.ResolveAccessToken(r.Context(), token)
		if err != nil {
			h.writeAuthError(w, r, err)
			return
		}
		ctx := context.WithValue(r.Context(), principalContextKey, principal)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (h *authHandlers) requireRole(role string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			principal, ok := principalFromContext(r.Context())
			if !ok {
				writeError(w, r, http.StatusUnauthorized, "unauthenticated", "A valid Supabase access token is required.")
				return
			}
			if principal.Role != role {
				writeError(w, r, http.StatusForbidden, "forbidden", "This account cannot perform that action.")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func principalFromContext(ctx context.Context) (auth.Principal, bool) {
	principal, ok := ctx.Value(principalContextKey).(auth.Principal)
	return principal, ok
}

func (h *authHandlers) writeAuthError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, auth.ErrInvalidAccessToken):
		writeError(w, r, http.StatusUnauthorized, "unauthenticated", "The Supabase access token is invalid or expired.")
	case errors.Is(err, auth.ErrIdentityNotFound):
		writeError(w, r, http.StatusForbidden, "profile_not_found", "This Supabase account is not enabled for the ride demo.")
	case errors.Is(err, auth.ErrStoreUnavailable), errors.Is(err, auth.ErrVerifierUnavailable):
		writeError(w, r, http.StatusServiceUnavailable, "auth_unavailable", "Authentication is temporarily unavailable.")
	default:
		writeError(w, r, http.StatusInternalServerError, "internal_error", "An unexpected error occurred.")
	}
}
