package auth

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const jwksCacheLifetime = 5 * time.Minute

type JWKSVerifier struct {
	issuer  string
	jwksURL string
	client  *http.Client
	now     func() time.Time

	mu        sync.Mutex
	keys      map[string]crypto.PublicKey
	expiresAt time.Time
}

type jwtHeader struct {
	Algorithm string `json:"alg"`
	KeyID     string `json:"kid"`
}

type accessClaims struct {
	Issuer    string          `json:"iss"`
	Subject   string          `json:"sub"`
	Audience  json.RawMessage `json:"aud"`
	Role      string          `json:"role"`
	Expires   int64           `json:"exp"`
	NotBefore int64           `json:"nbf"`
	IssuedAt  int64           `json:"iat"`
}

type jwksResponse struct {
	Keys []jsonWebKey `json:"keys"`
}

type jsonWebKey struct {
	KeyID     string `json:"kid"`
	Algorithm string `json:"alg"`
	Use       string `json:"use"`
	KeyType   string `json:"kty"`
	Modulus   string `json:"n"`
	Exponent  string `json:"e"`
	Curve     string `json:"crv"`
	X         string `json:"x"`
	Y         string `json:"y"`
}

func NewJWKSVerifier(projectURL string, client *http.Client) (*JWKSVerifier, error) {
	parsed, err := url.Parse(strings.TrimSpace(projectURL))
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return nil, errors.New("SUPABASE_URL must be an https project URL without a path, query, or fragment")
	}
	if client == nil {
		client = &http.Client{Timeout: 4 * time.Second}
	}
	issuer := strings.TrimRight(parsed.String(), "/") + "/auth/v1"
	return &JWKSVerifier{
		issuer:  issuer,
		jwksURL: issuer + "/.well-known/jwks.json",
		client:  client,
		now:     time.Now,
	}, nil
}

func (v *JWKSVerifier) Verify(ctx context.Context, token string) (string, error) {
	if v == nil || v.client == nil || v.jwksURL == "" {
		return "", ErrVerifierUnavailable
	}
	if len(token) == 0 || len(token) > 16<<10 {
		return "", ErrInvalidAccessToken
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", ErrInvalidAccessToken
	}
	headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return "", ErrInvalidAccessToken
	}
	var header jwtHeader
	if err := json.Unmarshal(headerBytes, &header); err != nil || header.KeyID == "" || (header.Algorithm != "ES256" && header.Algorithm != "RS256") {
		return "", ErrInvalidAccessToken
	}
	claimsBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", ErrInvalidAccessToken
	}
	var claims accessClaims
	if err := json.Unmarshal(claimsBytes, &claims); err != nil {
		return "", ErrInvalidAccessToken
	}
	if claims.Issuer != v.issuer || claims.Role != "authenticated" || !validSupabaseSubject(claims.Subject) || !hasAuthenticatedAudience(claims.Audience) {
		return "", ErrInvalidAccessToken
	}
	now := v.now().Unix()
	if claims.Expires <= now || (claims.NotBefore != 0 && claims.NotBefore > now) || (claims.IssuedAt != 0 && claims.IssuedAt > now+60) {
		return "", ErrInvalidAccessToken
	}

	key, err := v.lookupKey(ctx, header.KeyID, false)
	if err != nil {
		return "", ErrVerifierUnavailable
	}
	if key == nil {
		key, err = v.lookupKey(ctx, header.KeyID, true)
		if err != nil {
			return "", ErrVerifierUnavailable
		}
	}
	if key == nil {
		return "", ErrInvalidAccessToken
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || !verifyJWTSignature(header.Algorithm, key, []byte(parts[0]+"."+parts[1]), signature) {
		return "", ErrInvalidAccessToken
	}
	return strings.ToLower(claims.Subject), nil
}

func (v *JWKSVerifier) lookupKey(ctx context.Context, keyID string, forceRefresh bool) (crypto.PublicKey, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if !forceRefresh && v.now().Before(v.expiresAt) {
		return v.keys[keyID], nil
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, v.jwksURL, nil)
	if err != nil {
		return nil, err
	}
	response, err := v.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("JWKS endpoint returned status %d", response.StatusCode)
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, 1<<20))
	var document jwksResponse
	if err := decoder.Decode(&document); err != nil {
		return nil, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, errors.New("JWKS response contains trailing data")
	}
	keys := make(map[string]crypto.PublicKey, len(document.Keys))
	for _, jwk := range document.Keys {
		if jwk.KeyID == "" || (jwk.Use != "" && jwk.Use != "sig") {
			continue
		}
		key, keyErr := publicKeyFromJWK(jwk)
		if keyErr != nil {
			continue
		}
		if _, duplicate := keys[jwk.KeyID]; duplicate {
			return nil, errors.New("JWKS contains duplicate key IDs")
		}
		keys[jwk.KeyID] = key
	}
	v.keys = keys
	v.expiresAt = v.now().Add(jwksCacheLifetime)
	return keys[keyID], nil
}

func publicKeyFromJWK(jwk jsonWebKey) (crypto.PublicKey, error) {
	switch jwk.KeyType {
	case "RSA":
		modulus, err := base64.RawURLEncoding.DecodeString(jwk.Modulus)
		if err != nil || len(modulus) < 256 {
			return nil, errors.New("invalid RSA modulus")
		}
		exponentBytes, err := base64.RawURLEncoding.DecodeString(jwk.Exponent)
		if err != nil || len(exponentBytes) == 0 || len(exponentBytes) > 4 {
			return nil, errors.New("invalid RSA exponent")
		}
		exponent := 0
		for _, value := range exponentBytes {
			exponent = exponent<<8 | int(value)
		}
		if exponent < 3 || exponent%2 == 0 {
			return nil, errors.New("invalid RSA exponent")
		}
		if jwk.Algorithm != "" && jwk.Algorithm != "RS256" {
			return nil, errors.New("unsupported RSA algorithm")
		}
		return &rsa.PublicKey{N: new(big.Int).SetBytes(modulus), E: exponent}, nil
	case "EC":
		if jwk.Curve != "P-256" || (jwk.Algorithm != "" && jwk.Algorithm != "ES256") {
			return nil, errors.New("unsupported elliptic curve")
		}
		xBytes, err := base64.RawURLEncoding.DecodeString(jwk.X)
		if err != nil || len(xBytes) != 32 {
			return nil, errors.New("invalid elliptic curve x coordinate")
		}
		yBytes, err := base64.RawURLEncoding.DecodeString(jwk.Y)
		if err != nil || len(yBytes) != 32 {
			return nil, errors.New("invalid elliptic curve y coordinate")
		}
		publicKey := &ecdsa.PublicKey{Curve: elliptic.P256(), X: new(big.Int).SetBytes(xBytes), Y: new(big.Int).SetBytes(yBytes)}
		if !publicKey.Curve.IsOnCurve(publicKey.X, publicKey.Y) {
			return nil, errors.New("elliptic curve point is invalid")
		}
		return publicKey, nil
	default:
		return nil, errors.New("unsupported JWT key type")
	}
}

func verifyJWTSignature(algorithm string, key crypto.PublicKey, message, signature []byte) bool {
	digest := sha256.Sum256(message)
	switch algorithm {
	case "RS256":
		publicKey, ok := key.(*rsa.PublicKey)
		return ok && rsa.VerifyPKCS1v15(publicKey, crypto.SHA256, digest[:], signature) == nil
	case "ES256":
		publicKey, ok := key.(*ecdsa.PublicKey)
		if !ok || len(signature) != 64 {
			return false
		}
		r := new(big.Int).SetBytes(signature[:32])
		s := new(big.Int).SetBytes(signature[32:])
		return ecdsa.Verify(publicKey, digest[:], r, s)
	default:
		return false
	}
}

func hasAuthenticatedAudience(raw json.RawMessage) bool {
	var single string
	if json.Unmarshal(raw, &single) == nil {
		return single == "authenticated"
	}
	var multiple []string
	if json.Unmarshal(raw, &multiple) != nil {
		return false
	}
	for _, audience := range multiple {
		if audience == "authenticated" {
			return true
		}
	}
	return false
}

func validSupabaseSubject(subject string) bool {
	if len(subject) != 36 {
		return false
	}
	for index, character := range subject {
		switch index {
		case 8, 13, 18, 23:
			if character != '-' {
				return false
			}
		default:
			if !((character >= '0' && character <= '9') || (character >= 'a' && character <= 'f') || (character >= 'A' && character <= 'F')) {
				return false
			}
		}
	}
	return true
}
