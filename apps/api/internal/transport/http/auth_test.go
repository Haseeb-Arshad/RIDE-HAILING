package httptransport

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/haseeb-arshad/ride-hailing/apps/api/internal/auth"
)

const testAppOrigin = "http://localhost:3000"

func TestAuthMeRequiresBearerTokenAndReturnsMappedProfile(t *testing.T) {
	repository := newHTTPAuthRepository(t)
	router, _ := newAuthTestRouter(newHTTPAuthService(repository))

	for name, request := range map[string]*http.Request{
		"missing token": httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil),
		"cookie is not accepted": func() *http.Request {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
			req.AddCookie(&http.Cookie{Name: "ride_session", Value: "rider@example.test"})
			return req
		}(),
		"wrong scheme": func() *http.Request {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
			req.Header.Set("Authorization", "Basic rider@example.test")
			return req
		}(),
		"unknown token": func() *http.Request {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
			req.Header.Set("Authorization", "Bearer unrecognized")
			return req
		}(),
	} {
		t.Run(name, func(t *testing.T) {
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401: %s", response.Code, response.Body.String())
			}
		})
	}

	request := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	request.Header.Set("Authorization", "Bearer rider@example.test")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("valid bearer status = %d, body=%s", response.Code, response.Body.String())
	}
	var body sessionResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode profile response: %v", err)
	}
	want := repository.users["rider@example.test"]
	if body.User != want || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("profile response = %+v, cache-control=%q", body, response.Header().Get("Cache-Control"))
	}
	if strings.Contains(response.Body.String(), "csrf_token") || response.Header().Get("Set-Cookie") != "" {
		t.Fatalf("Supabase profile response still uses app-managed session state: %s", response.Body.String())
	}
}

func TestAuthMeRejectsUnlinkedProfilesAndReportsBackendFailures(t *testing.T) {
	repository := newHTTPAuthRepository(t)
	repository.tokenSubjects["unlinked-access"] = "30000000-0000-4000-8000-000000000099"
	router, _ := newAuthTestRouter(newHTTPAuthService(repository))

	request := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	request.Header.Set("Authorization", "Bearer unlinked-access")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "profile_not_found") {
		t.Fatalf("unlinked Supabase user status=%d body=%s", response.Code, response.Body.String())
	}

	repository.unavailable = true
	request = httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	request.Header.Set("Authorization", "Bearer rider@example.test")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("repository failure status = %d, want 503: %s", response.Code, response.Body.String())
	}
}

func TestAuthLoginAndLogoutRoutesAreNotOwnedByGo(t *testing.T) {
	router, _ := newAuthTestRouter(newHTTPAuthService(newHTTPAuthRepository(t)))
	for _, path := range []string{"/api/v1/auth/login", "/api/v1/auth/logout"} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`)))
		if response.Code != http.StatusNotFound {
			t.Errorf("POST %s status = %d, want 404", path, response.Code)
		}
	}
}

func TestOriginCheckAllowsConfiguredLocalAliasesAndRejectsMissingOrUntrustedOrigins(t *testing.T) {
	handled := false
	handlers := newAuthHandlers(newHTTPAuthService(newHTTPAuthRepository(t)), "http://127.0.0.1:3000,http://localhost:3000")
	next := handlers.requireOrigin(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		handled = true
		w.WriteHeader(http.StatusNoContent)
	}))

	for _, test := range []struct {
		name       string
		origin     string
		wantCode   string
		wantStatus int
	}{
		{name: "missing origin", wantCode: "origin_missing", wantStatus: http.StatusForbidden},
		{name: "untrusted origin", origin: "https://attacker.example", wantCode: "origin_forbidden", wantStatus: http.StatusForbidden},
	} {
		t.Run(test.name, func(t *testing.T) {
			handled = false
			request := httptest.NewRequest(http.MethodPost, "/api/v1/rides", nil)
			if test.origin != "" {
				request.Header.Set("Origin", test.origin)
			}
			response := httptest.NewRecorder()
			next.ServeHTTP(response, request)
			if response.Code != test.wantStatus || handled || !strings.Contains(response.Body.String(), test.wantCode) {
				t.Fatalf("origin=%q status=%d handled=%t body=%s", test.origin, response.Code, handled, response.Body.String())
			}
		})
	}

	for _, origin := range []string{"http://127.0.0.1:3000", "http://localhost:3000"} {
		handled = false
		request := httptest.NewRequest(http.MethodPost, "/api/v1/rides", nil)
		request.Header.Set("Origin", origin)
		response := httptest.NewRecorder()
		next.ServeHTTP(response, request)
		if response.Code != http.StatusNoContent || !handled {
			t.Errorf("trusted origin %q status=%d, next handler called=%v", origin, response.Code, handled)
		}
	}
}

func newAuthTestRouter(service *auth.Service) (http.Handler, *authHandlers) {
	router := chi.NewRouter()
	router.Use(RequestID)
	handlers := newAuthHandlers(service, testAppOrigin)
	handlers.registerRoutes(router)
	return router, handlers
}

func newHTTPAuthService(repository *httpAuthRepository) *auth.Service {
	return auth.NewService(repository, httpTokenVerifier{repository: repository})
}

type httpAuthRepository struct {
	users         map[string]auth.Identity
	supabaseIDs   map[string]string
	tokenSubjects map[string]string
	unavailable   bool
}

func newHTTPAuthRepository(t *testing.T) *httpAuthRepository {
	t.Helper()
	repository := &httpAuthRepository{
		users: map[string]auth.Identity{
			"rider@example.test":      {ID: "20000000-0000-4000-8000-000000000001", Email: "rider@example.test", DisplayName: "Demo Rider", Role: "rider"},
			"driver.one@example.test": {ID: "20000000-0000-4000-8000-000000000002", Email: "driver.one@example.test", DisplayName: "Demo Driver One", Role: "driver"},
			"driver.two@example.test": {ID: "20000000-0000-4000-8000-000000000003", Email: "driver.two@example.test", DisplayName: "Demo Driver Two", Role: "driver"},
		},
		supabaseIDs: map[string]string{
			"rider@example.test":      "30000000-0000-4000-8000-000000000001",
			"driver.one@example.test": "30000000-0000-4000-8000-000000000002",
			"driver.two@example.test": "30000000-0000-4000-8000-000000000003",
		},
		tokenSubjects: make(map[string]string),
	}
	for email, subject := range repository.supabaseIDs {
		repository.tokenSubjects[email] = subject
	}
	return repository
}

func (r *httpAuthRepository) FindIdentityBySupabaseID(_ context.Context, supabaseID string) (auth.Identity, error) {
	if r.unavailable {
		return auth.Identity{}, errors.New("test repository is unavailable")
	}
	for email, id := range r.supabaseIDs {
		if id == supabaseID {
			return r.users[email], nil
		}
	}
	return auth.Identity{}, auth.ErrIdentityNotFound
}

type httpTokenVerifier struct{ repository *httpAuthRepository }

func (v httpTokenVerifier) Verify(_ context.Context, token string) (string, error) {
	if subject, ok := v.repository.tokenSubjects[token]; ok {
		return subject, nil
	}
	return "", auth.ErrInvalidAccessToken
}
