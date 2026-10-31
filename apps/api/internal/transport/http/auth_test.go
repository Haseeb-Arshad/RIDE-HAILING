package httptransport

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/haseeb-arshad/ride-hailing/apps/api/internal/auth"
	"golang.org/x/crypto/bcrypt"
)

const testAppOrigin = "http://127.0.0.1:3000"

func TestLoginMeAndLogoutSessionFlow(t *testing.T) {
	repository := newHTTPAuthRepository(t)
	service := auth.NewService(repository)
	router, _ := newAuthTestRouter(service, false)

	loginResponse := performLogin(t, router, "rider@example.test", "correct horse battery staple")
	if loginResponse.Code != http.StatusOK {
		t.Fatalf("login status = %d, body=%s", loginResponse.Code, loginResponse.Body.String())
	}
	var response sessionResponse
	if err := json.Unmarshal(loginResponse.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode login response: %v", err)
	}
	if response.User.ID != "20000000-0000-4000-8000-000000000001" || response.User.Role != "rider" || response.CSRFToken == "" {
		t.Fatalf("unexpected login response: %+v", response)
	}
	if strings.Contains(loginResponse.Body.String(), loginResponse.Result().Cookies()[0].Value) {
		t.Fatal("session cookie token was returned in the response body")
	}
	cookies := loginResponse.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != sessionCookieName || !cookies[0].HttpOnly || cookies[0].Secure || cookies[0].SameSite != http.SameSiteLaxMode || cookies[0].Path != "/" {
		t.Fatalf("unexpected local session cookie: %+v", cookies)
	}
	if cookies[0].MaxAge != int(auth.SessionLifetime.Seconds()) {
		t.Fatalf("cookie MaxAge = %d, want %d", cookies[0].MaxAge, int(auth.SessionLifetime.Seconds()))
	}
	if loginResponse.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("login response is missing Cache-Control: no-store")
	}

	meRequest := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	meRequest.AddCookie(cookies[0])
	meResponse := httptest.NewRecorder()
	router.ServeHTTP(meResponse, meRequest)
	if meResponse.Code != http.StatusOK {
		t.Fatalf("me status = %d, body=%s", meResponse.Code, meResponse.Body.String())
	}
	var me sessionResponse
	if err := json.Unmarshal(meResponse.Body.Bytes(), &me); err != nil {
		t.Fatalf("decode me response: %v", err)
	}
	if me.User.ID != response.User.ID || me.CSRFToken != response.CSRFToken {
		t.Fatalf("me did not return the current session identity and CSRF token: %+v", me)
	}

	missingCSRF := performLogout(router, cookies[0], testAppOrigin, "")
	if missingCSRF.Code != http.StatusForbidden {
		t.Fatalf("missing CSRF status = %d, want 403", missingCSRF.Code)
	}
	badCSRF := performLogout(router, cookies[0], testAppOrigin, "wrong-token")
	if badCSRF.Code != http.StatusForbidden {
		t.Fatalf("mismatched CSRF status = %d, want 403", badCSRF.Code)
	}
	badOrigin := performLogout(router, cookies[0], "https://attacker.example", response.CSRFToken)
	if badOrigin.Code != http.StatusForbidden {
		t.Fatalf("mismatched Origin status = %d, want 403", badOrigin.Code)
	}

	logoutResponse := performLogout(router, cookies[0], testAppOrigin, response.CSRFToken)
	if logoutResponse.Code != http.StatusNoContent {
		t.Fatalf("logout status = %d, body=%s", logoutResponse.Code, logoutResponse.Body.String())
	}
	cleared := logoutResponse.Result().Cookies()
	if len(cleared) != 1 || cleared[0].Name != sessionCookieName || cleared[0].MaxAge != -1 || !cleared[0].HttpOnly {
		t.Fatalf("logout did not clear the session cookie: %+v", cleared)
	}

	meAfterLogout := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	meAfterLogout.AddCookie(cookies[0])
	unauthenticated := httptest.NewRecorder()
	router.ServeHTTP(unauthenticated, meAfterLogout)
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("revoked session status = %d, want 401", unauthenticated.Code)
	}
}

func TestLoginOriginAndCredentialErrorsAreGeneric(t *testing.T) {
	repository := newHTTPAuthRepository(t)
	service := auth.NewService(repository)
	router, _ := newAuthTestRouter(service, false)

	missingOriginRequest := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"email":"rider@example.test","password":"correct horse battery staple"}`))
	missingOriginRequest.Header.Set("Content-Type", "application/json")
	missingOriginResponse := httptest.NewRecorder()
	router.ServeHTTP(missingOriginResponse, missingOriginRequest)
	if missingOriginResponse.Code != http.StatusForbidden {
		t.Fatalf("login without Origin status = %d, want 403", missingOriginResponse.Code)
	}

	badPassword := performLogin(t, router, "rider@example.test", "not-the-password")
	unknownUser := performLogin(t, router, "missing@example.test", "not-the-password")
	if badPassword.Code != http.StatusUnauthorized || unknownUser.Code != http.StatusUnauthorized {
		t.Fatalf("credential failures: known=%d unknown=%d, want both 401", badPassword.Code, unknownUser.Code)
	}
	var badError, unknownError errorEnvelope
	if err := json.Unmarshal(badPassword.Body.Bytes(), &badError); err != nil {
		t.Fatalf("decode known-user error: %v", err)
	}
	if err := json.Unmarshal(unknownUser.Body.Bytes(), &unknownError); err != nil {
		t.Fatalf("decode unknown-user error: %v", err)
	}
	if badError.Error.Code != unknownError.Error.Code || badError.Error.Message != unknownError.Error.Message {
		t.Fatalf("credential error leaks account existence: known=%+v unknown=%+v", badError.Error, unknownError.Error)
	}
}

func TestBothSeededDriversGetIndependentRoleSessions(t *testing.T) {
	repository := newHTTPAuthRepository(t)
	router, _ := newAuthTestRouter(auth.NewService(repository), false)
	var sessionCookies []string
	for _, email := range []string{"driver.one@example.test", "driver.two@example.test"} {
		loginResponse := performLogin(t, router, email, "correct horse battery staple")
		if loginResponse.Code != http.StatusOK {
			t.Fatalf("driver login for %s status = %d body=%s", email, loginResponse.Code, loginResponse.Body.String())
		}
		var body sessionResponse
		if err := json.Unmarshal(loginResponse.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode %s session: %v", email, err)
		}
		if body.User.Email != email || body.User.Role != "driver" {
			t.Fatalf("unexpected driver identity for %s: %+v", email, body.User)
		}
		cookies := loginResponse.Result().Cookies()
		if len(cookies) != 1 {
			t.Fatalf("driver %s did not receive one session cookie", email)
		}
		sessionCookies = append(sessionCookies, cookies[0].Value)
		request := httptest.NewRequest(http.MethodGet, "/driver-only", nil)
		request.AddCookie(cookies[0])
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusNoContent {
			t.Fatalf("driver %s role route status = %d, want 204", email, response.Code)
		}
	}
	if sessionCookies[0] == sessionCookies[1] {
		t.Fatal("two driver accounts unexpectedly received the same session token")
	}
}

func TestLoginValidationAndStorageFailure(t *testing.T) {
	repository := newHTTPAuthRepository(t)
	router, _ := newAuthTestRouter(auth.NewService(repository), true)

	invalidJSON := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader("{"))
	invalidJSON.Header.Set("Content-Type", "application/json")
	invalidJSON.Header.Set("Origin", testAppOrigin)
	invalidJSONResponse := httptest.NewRecorder()
	router.ServeHTTP(invalidJSONResponse, invalidJSON)
	if invalidJSONResponse.Code != http.StatusBadRequest {
		t.Fatalf("malformed JSON status = %d, want 400", invalidJSONResponse.Code)
	}

	invalidEmail := performLogin(t, router, "not-an-email", "valid-length-password")
	if invalidEmail.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid email status = %d, want 422", invalidEmail.Code)
	}

	longPassword := strings.Repeat("x", 9<<10)
	tooLarge := performLogin(t, router, "rider@example.test", longPassword)
	if tooLarge.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized login body status = %d, want 413", tooLarge.Code)
	}

	secureCookie := performLogin(t, router, "rider@example.test", "correct horse battery staple")
	if secureCookie.Code != http.StatusOK || len(secureCookie.Result().Cookies()) != 1 || !secureCookie.Result().Cookies()[0].Secure {
		t.Fatalf("configured secure cookie was not set: status=%d cookies=%+v", secureCookie.Code, secureCookie.Result().Cookies())
	}

	failedRepository := newHTTPAuthRepository(t)
	failedRepository.failure = errors.New("database connection detail")
	failedRouter, _ := newAuthTestRouter(auth.NewService(failedRepository), false)
	databaseFailure := performLogin(t, failedRouter, "rider@example.test", "correct horse battery staple")
	if databaseFailure.Code != http.StatusServiceUnavailable || strings.Contains(databaseFailure.Body.String(), "database connection detail") {
		t.Fatalf("storage failure was not sanitized: status=%d body=%s", databaseFailure.Code, databaseFailure.Body.String())
	}
}

func TestUnconfiguredDatabaseReturnsUnavailableInsteadOfPanicking(t *testing.T) {
	service := auth.NewService(auth.NewPostgresRepository(nil))
	router, _ := newAuthTestRouter(service, false)
	response := performLogin(t, router, "rider@example.test", "correct horse battery staple")
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("unconfigured database status = %d, want 503; body=%s", response.Code, response.Body.String())
	}
}

func TestRoleMiddlewareReturns401Then403(t *testing.T) {
	repository := newHTTPAuthRepository(t)
	service := auth.NewService(repository)
	router, _ := newAuthTestRouter(service, false)

	unauthenticatedRequest := httptest.NewRequest(http.MethodGet, "/driver-only", nil)
	unauthenticatedResponse := httptest.NewRecorder()
	router.ServeHTTP(unauthenticatedResponse, unauthenticatedRequest)
	if unauthenticatedResponse.Code != http.StatusUnauthorized {
		t.Fatalf("missing session status = %d, want 401", unauthenticatedResponse.Code)
	}

	loginResponse := performLogin(t, router, "rider@example.test", "correct horse battery staple")
	cookie := loginResponse.Result().Cookies()[0]
	request := httptest.NewRequest(http.MethodGet, "/driver-only", nil)
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("rider on driver route status = %d, want 403", response.Code)
	}
}

func performLogin(t *testing.T, router http.Handler, email, password string) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(loginRequest{Email: email, Password: password})
	if err != nil {
		t.Fatalf("encode login payload: %v", err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", testAppOrigin)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func performLogout(router http.Handler, cookie *http.Cookie, origin, csrfToken string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	request.AddCookie(cookie)
	request.Header.Set("Origin", origin)
	if csrfToken != "" {
		request.Header.Set("X-CSRF-Token", csrfToken)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func newAuthTestRouter(service *auth.Service, cookieSecure bool) (http.Handler, *authHandlers) {
	router := chi.NewRouter()
	router.Use(RequestID)
	handlers := newAuthHandlers(service, testAppOrigin, cookieSecure)
	handlers.registerRoutes(router)
	router.With(handlers.requireAuth, handlers.requireRole("driver")).Get("/driver-only", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	return router, handlers
}

type httpAuthRepository struct {
	users    map[string]auth.Credentials
	sessions map[string]auth.StoredSession
	expires  map[string]time.Time
	revoked  map[string]bool
	failure  error
}

func newHTTPAuthRepository(t *testing.T) *httpAuthRepository {
	t.Helper()
	users := map[string]auth.Credentials{}
	for index, account := range []struct {
		id, email, name, role string
	}{
		{"20000000-0000-4000-8000-000000000001", "rider@example.test", "Demo Rider", "rider"},
		{"20000000-0000-4000-8000-000000000002", "driver.one@example.test", "Demo Driver One", "driver"},
		{"20000000-0000-4000-8000-000000000003", "driver.two@example.test", "Demo Driver Two", "driver"},
	} {
		hash, err := bcrypt.GenerateFromPassword([]byte("correct horse battery staple"), bcrypt.DefaultCost)
		if err != nil {
			t.Fatalf("hash HTTP test account %d password: %v", index, err)
		}
		users[account.email] = auth.Credentials{ID: account.id, Email: account.email, DisplayName: account.name, Role: account.role, PasswordHash: string(hash)}
	}
	return &httpAuthRepository{
		users:    users,
		sessions: make(map[string]auth.StoredSession),
		expires:  make(map[string]time.Time),
		revoked:  make(map[string]bool),
	}
}

func (r *httpAuthRepository) FindUserByEmail(_ context.Context, email string) (auth.Credentials, error) {
	if r.failure != nil {
		return auth.Credentials{}, r.failure
	}
	user, ok := r.users[email]
	if !ok {
		return auth.Credentials{}, auth.ErrUserNotFound
	}
	return user, nil
}

func (r *httpAuthRepository) CreateSession(_ context.Context, userID string, tokenHash, csrfTokenHash []byte, expiresAt time.Time) error {
	if r.failure != nil {
		return r.failure
	}
	var user auth.Credentials
	for _, candidate := range r.users {
		if candidate.ID == userID {
			user = candidate
			break
		}
	}
	if user.ID == "" {
		return auth.ErrUserNotFound
	}
	key := hex.EncodeToString(tokenHash)
	r.sessions[key] = auth.StoredSession{
		Identity:      auth.Identity{ID: user.ID, Email: user.Email, DisplayName: user.DisplayName, Role: user.Role},
		CSRFTokenHash: append([]byte(nil), csrfTokenHash...),
	}
	r.expires[key] = expiresAt
	return nil
}

func (r *httpAuthRepository) FindActiveSession(_ context.Context, tokenHash []byte) (auth.StoredSession, error) {
	if r.failure != nil {
		return auth.StoredSession{}, r.failure
	}
	key := hex.EncodeToString(tokenHash)
	if r.revoked[key] || !time.Now().Before(r.expires[key]) {
		return auth.StoredSession{}, auth.ErrSessionNotFound
	}
	session, ok := r.sessions[key]
	if !ok {
		return auth.StoredSession{}, auth.ErrSessionNotFound
	}
	return session, nil
}

func (r *httpAuthRepository) RevokeSession(_ context.Context, tokenHash []byte) error {
	if r.failure != nil {
		return r.failure
	}
	r.revoked[hex.EncodeToString(tokenHash)] = true
	return nil
}
