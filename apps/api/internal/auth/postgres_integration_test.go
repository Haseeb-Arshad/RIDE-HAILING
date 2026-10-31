package auth

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

func TestPostgresSessionPersistenceExpiryAndRevocation(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("set TEST_DATABASE_URL to a migrated Supabase/PostgreSQL database to run this integration check")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	poolConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatalf("parse TEST_DATABASE_URL: %v", err)
	}
	poolConfig.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatalf("open PostgreSQL connection: %v", err)
	}
	defer pool.Close()

	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatalf("begin rollback-only session test: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	passwordHash, err := bcrypt.GenerateFromPassword([]byte("integration-session-password"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash integration user password: %v", err)
	}
	var userID string
	email := fmt.Sprintf("session-check-%d@example.test", time.Now().UnixNano())
	if err := tx.QueryRow(ctx, `
		INSERT INTO ride_hailing.users (email, display_name, role, password_hash)
		VALUES ($1, 'Session Check', 'rider', $2)
		RETURNING id::text`, email, string(passwordHash)).Scan(&userID); err != nil {
		t.Fatalf("insert rollback-only authentication user: %v", err)
	}

	repository := newPostgresRepository(tx)
	firstService := NewService(repository)
	identity, token, csrfToken, expiresAt, err := firstService.Login(ctx, email, "integration-session-password")
	if err != nil {
		t.Fatalf("authenticate and create session: %v", err)
	}
	if identity.ID != userID || identity.Role != "rider" || time.Until(expiresAt) <= 0 {
		t.Fatalf("unexpected authenticated identity or expiry: identity=%+v expires_at=%s", identity, expiresAt)
	}

	var storedTokenHash, storedCSRFHash []byte
	if err := tx.QueryRow(ctx, `
		SELECT token_hash, csrf_token_hash
		FROM ride_hailing.sessions
		WHERE token_hash = $1`, HashToken(token)).Scan(&storedTokenHash, &storedCSRFHash); err != nil {
		t.Fatalf("read stored session hashes: %v", err)
	}
	if !bytes.Equal(storedTokenHash, HashToken(token)) || bytes.Equal(storedTokenHash, []byte(token)) {
		t.Fatal("the raw session token was stored instead of its SHA-256 hash")
	}
	if !bytes.Equal(storedCSRFHash, HashToken(csrfToken)) || bytes.Equal(storedCSRFHash, []byte(csrfToken)) {
		t.Fatal("the raw CSRF token was stored instead of its SHA-256 hash")
	}

	serviceAfterRestart := NewService(newPostgresRepository(tx))
	principal, err := serviceAfterRestart.ResolveSession(ctx, token)
	if err != nil {
		t.Fatalf("resolve database-backed session after service recreation: %v", err)
	}
	if principal.ID != userID || principal.CSRFToken != csrfToken {
		t.Fatalf("resolved session identity or CSRF token changed: %+v", principal)
	}
	if err := serviceAfterRestart.RevokeSession(ctx, token); err != nil {
		t.Fatalf("revoke session: %v", err)
	}
	if _, err := serviceAfterRestart.ResolveSession(ctx, token); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("revoked session error = %v, want ErrSessionNotFound", err)
	}

	_, expiredToken, _, _, err := firstService.Login(ctx, email, "integration-session-password")
	if err != nil {
		t.Fatalf("create expiry-check session: %v", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE ride_hailing.sessions
		SET created_at = now() - interval '2 seconds',
		    expires_at = now() - interval '1 second'
		WHERE token_hash = $1`, HashToken(expiredToken)); err != nil {
		t.Fatalf("expire test session: %v", err)
	}
	if _, err := firstService.ResolveSession(ctx, expiredToken); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("expired session error = %v, want ErrSessionNotFound", err)
	}
}
