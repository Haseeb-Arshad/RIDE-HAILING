package httptransport

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/haseeb-arshad/ride-hailing/apps/api/internal/auth"
)

const sessionCookieName = "ride_session"

type authHandlers struct {
	service      *auth.Service
	appOrigin    string
	cookieSecure bool
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type sessionResponse struct {
	User      auth.Identity `json:"user"`
	CSRFToken string        `json:"csrf_token"`
}

func registerAuthRoutes(router chi.Router, service *auth.Service, appOrigin string, cookieSecure bool) {
	newAuthHandlers(service, appOrigin, cookieSecure).registerRoutes(router)
}

func newAuthHandlers(service *auth.Service, appOrigin string, cookieSecure bool) *authHandlers {
	return &authHandlers{service: service, appOrigin: appOrigin, cookieSecure: cookieSecure}
}

func (h *authHandlers) registerRoutes(router chi.Router) {
	router.Route("/api/v1/auth", func(routes chi.Router) {
		routes.With(h.requireOrigin).Post("/login", h.login)
		routes.With(h.requireAuth).Get("/me", h.me)
		routes.With(h.requireOrigin, h.requireAuth, h.requireCSRF).Post("/logout", h.logout)
	})
}

func (h *authHandlers) login(w http.ResponseWriter, r *http.Request) {
	var payload loginRequest
	if err := decodeLoginRequest(w, r, &payload); err != nil {
		if errors.Is(err, errRequestBodyTooLarge) {
			writeError(w, r, http.StatusRequestEntityTooLarge, "request_too_large", "The request body is too large.")
			return
		}
		writeError(w, r, http.StatusBadRequest, "invalid_request", "The request body must be valid JSON.")
		return
	}

	email := auth.NormalizeEmail(payload.Email)
	if !validEmail(email) || len(payload.Password) == 0 || len(payload.Password) > 72 {
		writeError(w, r, http.StatusUnprocessableEntity, "invalid_login", "Enter a valid email and password.")
		return
	}

	identity, token, csrfToken, expiresAt, err := h.service.Login(r.Context(), email, payload.Password)
	if err != nil {
		h.writeAuthError(w, r, err)
		return
	}
	h.setSessionCookie(w, token, expiresAt)
	h.writeSessionResponse(w, http.StatusOK, identity, csrfToken)
}

func (h *authHandlers) me(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "unauthenticated", "A valid session is required.")
		return
	}
	h.writeSessionResponse(w, http.StatusOK, principal.Identity, principal.CSRFToken)
}

func (h *authHandlers) logout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		writeError(w, r, http.StatusUnauthorized, "unauthenticated", "A valid session is required.")
		return
	}
	if err := h.service.RevokeSession(r.Context(), cookie.Value); err != nil {
		h.writeAuthError(w, r, err)
		return
	}
	h.clearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
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
		cookie, err := r.Cookie(sessionCookieName)
		if err != nil || cookie.Value == "" {
			writeError(w, r, http.StatusUnauthorized, "unauthenticated", "A valid session is required.")
			return
		}
		principal, err := h.service.ResolveSession(r.Context(), cookie.Value)
		if err != nil {
			h.writeAuthError(w, r, err)
			return
		}
		ctx := context.WithValue(r.Context(), principalContextKey, principal)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (h *authHandlers) requireCSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		principal, ok := principalFromContext(r.Context())
		if !ok || !h.service.ValidateCSRF(principal, r.Header.Get("X-CSRF-Token")) {
			writeError(w, r, http.StatusForbidden, "csrf_forbidden", "A valid CSRF token is required.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (h *authHandlers) requireRole(role string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			principal, ok := principalFromContext(r.Context())
			if !ok {
				writeError(w, r, http.StatusUnauthorized, "unauthenticated", "A valid session is required.")
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
	case errors.Is(err, auth.ErrInvalidCredentials):
		writeError(w, r, http.StatusUnauthorized, "invalid_credentials", "Email or password is invalid.")
	case errors.Is(err, auth.ErrSessionNotFound):
		writeError(w, r, http.StatusUnauthorized, "unauthenticated", "A valid session is required.")
	case errors.Is(err, auth.ErrStoreUnavailable):
		writeError(w, r, http.StatusServiceUnavailable, "database_unavailable", "Authentication is temporarily unavailable.")
	default:
		writeError(w, r, http.StatusInternalServerError, "internal_error", "An unexpected error occurred.")
	}
}

func (h *authHandlers) writeSessionResponse(w http.ResponseWriter, status int, identity auth.Identity, csrfToken string) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	writeJSON(w, status, sessionResponse{User: identity, CSRFToken: csrfToken})
}

func (h *authHandlers) setSessionCookie(w http.ResponseWriter, token string, expiresAt time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		Expires:  expiresAt,
		MaxAge:   int(auth.SessionLifetime.Seconds()),
		HttpOnly: true,
		Secure:   h.cookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
}

func (h *authHandlers) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(1, 0).UTC(),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   h.cookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
}

var errRequestBodyTooLarge = errors.New("request body too large")

func decodeLoginRequest(w http.ResponseWriter, r *http.Request, target *loginRequest) error {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return errors.New("content type must be application/json")
	}
	limited := http.MaxBytesReader(w, r.Body, 8<<10)
	decoder := json.NewDecoder(limited)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			return errRequestBodyTooLarge
		}
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("request must contain one JSON object")
	}
	return nil
}

func validEmail(email string) bool {
	if len(email) > 254 || email == "" || strings.ContainsAny(email, " \t\r\n") {
		return false
	}
	address, err := mail.ParseAddress(email)
	return err == nil && address.Address == email && bytes.Contains([]byte(email), []byte("@"))
}
