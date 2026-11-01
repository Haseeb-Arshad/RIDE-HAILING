package auth

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPostgresSupabaseIDResolvesApplicationProfile(t *testing.T) {
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
		t.Fatalf("begin rollback-only profile test: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	supabaseID := "30100000-0000-4000-8000-000000000099"
	email := fmt.Sprintf("profile-check-%d@example.test", time.Now().UnixNano())
	var appID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO ride_hailing.users (email, display_name, role, supabase_auth_id)
		VALUES ($1, 'Profile Check', 'driver', $2)
		RETURNING id::text`, email, supabaseID).Scan(&appID); err != nil {
		t.Fatalf("insert rollback-only application profile: %v", err)
	}

	identity, err := newPostgresRepository(tx).FindIdentityBySupabaseID(ctx, supabaseID)
	if err != nil {
		t.Fatalf("find profile by verified Supabase subject: %v", err)
	}
	if identity.ID != appID || identity.Email != email || identity.Role != "driver" || identity.DisplayName != "Profile Check" {
		t.Fatalf("profile lookup returned unexpected identity: %+v", identity)
	}
	if _, err := newPostgresRepository(tx).FindIdentityBySupabaseID(ctx, "30100000-0000-4000-8000-000000000098"); !errors.Is(err, ErrIdentityNotFound) {
		t.Fatalf("unmapped Supabase user error = %v, want ErrIdentityNotFound", err)
	}
}
