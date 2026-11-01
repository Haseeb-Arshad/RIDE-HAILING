package auth

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

const (
	testIssuer  = "https://ride-hailing-test.supabase.co/auth/v1"
	testSubject = "30100000-0000-4000-8000-000000000001"
)

func TestJWKSVerifierValidatesES256ClaimsAndCachesPublicKeys(t *testing.T) {
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate ES256 key: %v", err)
	}
	var fetches atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fetches.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(jwksResponse{Keys: []jsonWebKey{testECJWK(privateKey, "ec-key-1")}})
	}))
	defer server.Close()

	verifier := testJWKSVerifier(server, time.Now)
	token := signTestJWT(t, privateKey, "ES256", "ec-key-1", map[string]any{
		"iss": testIssuer, "sub": testSubject, "aud": "authenticated", "role": "authenticated", "exp": time.Now().Add(time.Hour).Unix(),
		"user_metadata": map[string]any{"role": "driver"},
	})
	for range 2 {
		subject, err := verifier.Verify(context.Background(), token)
		if err != nil || subject != testSubject {
			t.Fatalf("Verify() = (%q, %v), want subject %q", subject, err, testSubject)
		}
	}
	if fetches.Load() != 1 {
		t.Fatalf("JWKS endpoint fetched %d times, want one cached fetch", fetches.Load())
	}
}

func TestJWKSVerifierValidatesRS256AndRejectsUntrustedClaims(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RS256 key: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(jwksResponse{Keys: []jsonWebKey{testRSAJWK(&privateKey.PublicKey, "rsa-key-1")}})
	}))
	defer server.Close()
	verifier := testJWKSVerifier(server, time.Now)

	validClaims := map[string]any{
		"iss": testIssuer, "sub": testSubject, "aud": []string{"authenticated"}, "role": "authenticated", "exp": time.Now().Add(time.Hour).Unix(),
	}
	validToken := signTestJWT(t, privateKey, "RS256", "rsa-key-1", validClaims)
	if subject, err := verifier.Verify(context.Background(), validToken); err != nil || subject != testSubject {
		t.Fatalf("Verify() = (%q, %v), want subject %q", subject, err, testSubject)
	}

	for name, claims := range map[string]map[string]any{
		"wrong issuer":    {"iss": "https://other.supabase.co/auth/v1", "sub": testSubject, "aud": "authenticated", "role": "authenticated", "exp": time.Now().Add(time.Hour).Unix()},
		"wrong audience":  {"iss": testIssuer, "sub": testSubject, "aud": "anon", "role": "authenticated", "exp": time.Now().Add(time.Hour).Unix()},
		"wrong role":      {"iss": testIssuer, "sub": testSubject, "aud": "authenticated", "role": "anon", "exp": time.Now().Add(time.Hour).Unix()},
		"expired":         {"iss": testIssuer, "sub": testSubject, "aud": "authenticated", "role": "authenticated", "exp": time.Now().Add(-time.Minute).Unix()},
		"invalid subject": {"iss": testIssuer, "sub": "not-a-uuid", "aud": "authenticated", "role": "authenticated", "exp": time.Now().Add(time.Hour).Unix()},
	} {
		t.Run(name, func(t *testing.T) {
			token := signTestJWT(t, privateKey, "RS256", "rsa-key-1", claims)
			if _, err := verifier.Verify(context.Background(), token); !errors.Is(err, ErrInvalidAccessToken) {
				t.Fatalf("Verify() error = %v, want ErrInvalidAccessToken", err)
			}
		})
	}

	wrongAlgorithm := signTestJWT(t, privateKey, "HS256", "rsa-key-1", validClaims)
	if _, err := verifier.Verify(context.Background(), wrongAlgorithm); !errors.Is(err, ErrInvalidAccessToken) {
		t.Fatalf("HS256 Verify() error = %v, want ErrInvalidAccessToken", err)
	}
}

func TestJWKSVerifierRefreshesUnknownKeyAndReportsNetworkFailure(t *testing.T) {
	first, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate first key: %v", err)
	}
	rotated, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate rotated key: %v", err)
	}
	var fetches atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		call := fetches.Add(1)
		w.Header().Set("Content-Type", "application/json")
		keys := []jsonWebKey{testECJWK(first, "ec-key-1")}
		if call > 1 {
			keys = append(keys, testECJWK(rotated, "ec-key-2"))
		}
		_ = json.NewEncoder(w).Encode(jwksResponse{Keys: keys})
	}))
	clock := time.Now()
	verifier := testJWKSVerifier(server, func() time.Time { return clock })
	token := signTestJWT(t, rotated, "ES256", "ec-key-2", map[string]any{
		"iss": testIssuer, "sub": testSubject, "aud": "authenticated", "role": "authenticated", "exp": time.Now().Add(time.Hour).Unix(),
	})
	if _, err := verifier.Verify(context.Background(), token); err != nil {
		t.Fatalf("Verify() after key rotation: %v", err)
	}
	if fetches.Load() != 2 {
		t.Fatalf("JWKS endpoint fetched %d times, want initial and rotation refresh", fetches.Load())
	}
	server.Close()
	clock = clock.Add(jwksCacheLifetime + time.Second)
	if _, err := verifier.Verify(context.Background(), token); !errors.Is(err, ErrVerifierUnavailable) {
		t.Fatalf("Verify() after cache expiry/network failure = %v, want ErrVerifierUnavailable", err)
	}
}

func TestServiceUsesDatabaseProfileForRoleAndIdentity(t *testing.T) {
	repository := &testIdentityRepository{identity: Identity{
		ID: "20000000-0000-4000-8000-000000000002", Email: "driver@example.test", DisplayName: "Demo Driver", Role: "driver",
	}}
	service := NewService(repository, testTokenVerifier{subject: testSubject})
	principal, err := service.ResolveAccessToken(context.Background(), "signed-token")
	if err != nil {
		t.Fatalf("ResolveAccessToken: %v", err)
	}
	if principal.ID != repository.identity.ID || principal.Role != "driver" || repository.lookupID != testSubject {
		t.Fatalf("resolved principal = %+v, looked up Supabase ID %q", principal, repository.lookupID)
	}
}

func TestServiceRejectsUnmappedAccountsAndMissingVerifier(t *testing.T) {
	repository := &testIdentityRepository{err: ErrIdentityNotFound}
	service := NewService(repository, testTokenVerifier{subject: testSubject})
	if _, err := service.ResolveAccessToken(context.Background(), "signed-token"); !errors.Is(err, ErrIdentityNotFound) {
		t.Fatalf("unmapped account error = %v, want ErrIdentityNotFound", err)
	}
	if _, err := NewService(repository, nil).ResolveAccessToken(context.Background(), "signed-token"); !errors.Is(err, ErrVerifierUnavailable) {
		t.Fatalf("missing verifier error = %v, want ErrVerifierUnavailable", err)
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

func TestNewJWKSVerifierRequiresSecureProjectURL(t *testing.T) {
	for _, value := range []string{"", "http://project.supabase.co", "https://project.supabase.co/path", "https://user:pass@project.supabase.co"} {
		if _, err := NewJWKSVerifier(value, nil); err == nil {
			t.Errorf("NewJWKSVerifier(%q) accepted an invalid project URL", value)
		}
	}
}

type testIdentityRepository struct {
	identity Identity
	lookupID string
	err      error
}

func (r *testIdentityRepository) FindIdentityBySupabaseID(_ context.Context, supabaseID string) (Identity, error) {
	r.lookupID = supabaseID
	if r.err != nil {
		return Identity{}, r.err
	}
	return r.identity, nil
}

type testTokenVerifier struct{ subject string }

func (v testTokenVerifier) Verify(_ context.Context, _ string) (string, error) { return v.subject, nil }

func testJWKSVerifier(server *httptest.Server, now func() time.Time) *JWKSVerifier {
	return &JWKSVerifier{issuer: testIssuer, jwksURL: server.URL, client: server.Client(), now: now}
}

func testECJWK(privateKey *ecdsa.PrivateKey, keyID string) jsonWebKey {
	return jsonWebKey{
		KeyID: keyID, Algorithm: "ES256", Use: "sig", KeyType: "EC", Curve: "P-256",
		X: base64.RawURLEncoding.EncodeToString(privateKey.PublicKey.X.FillBytes(make([]byte, 32))),
		Y: base64.RawURLEncoding.EncodeToString(privateKey.PublicKey.Y.FillBytes(make([]byte, 32))),
	}
}

func testRSAJWK(publicKey *rsa.PublicKey, keyID string) jsonWebKey {
	exponent := big.NewInt(int64(publicKey.E)).Bytes()
	return jsonWebKey{
		KeyID: keyID, Algorithm: "RS256", Use: "sig", KeyType: "RSA",
		Modulus:  base64.RawURLEncoding.EncodeToString(publicKey.N.Bytes()),
		Exponent: base64.RawURLEncoding.EncodeToString(exponent),
	}
}

func signTestJWT(t *testing.T, key any, algorithm, keyID string, claims map[string]any) string {
	t.Helper()
	header, err := json.Marshal(map[string]string{"alg": algorithm, "kid": keyID, "typ": "JWT"})
	if err != nil {
		t.Fatalf("marshal JWT header: %v", err)
	}
	claimBytes, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("marshal JWT claims: %v", err)
	}
	message := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(claimBytes)
	digest := sha256.Sum256([]byte(message))
	var signature []byte
	switch algorithm {
	case "ES256":
		privateKey, ok := key.(*ecdsa.PrivateKey)
		if !ok {
			t.Fatalf("ES256 test key has type %T", key)
		}
		r, s, err := ecdsa.Sign(rand.Reader, privateKey, digest[:])
		if err != nil {
			t.Fatalf("sign ES256 token: %v", err)
		}
		signature = append(r.FillBytes(make([]byte, 32)), s.FillBytes(make([]byte, 32))...)
	case "RS256":
		privateKey, ok := key.(*rsa.PrivateKey)
		if !ok {
			t.Fatalf("RS256 test key has type %T", key)
		}
		signature, err = rsa.SignPKCS1v15(rand.Reader, privateKey, crypto.SHA256, digest[:])
		if err != nil {
			t.Fatalf("sign RS256 token: %v", err)
		}
	case "HS256":
		signature = []byte("not-a-supported-signature")
	default:
		t.Fatalf("unsupported test signing algorithm %s", algorithm)
	}
	return fmt.Sprintf("%s.%s", message, base64.RawURLEncoding.EncodeToString(signature))
}
