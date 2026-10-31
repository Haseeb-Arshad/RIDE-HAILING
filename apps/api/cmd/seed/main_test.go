package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

func TestPostgresSchemaConstraintsAndRepeatableSeed(t *testing.T) {
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
		t.Fatalf("begin rollback-only integration transaction: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	passwordHash, err := bcrypt.GenerateFromPassword([]byte("integration-only-password"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash integration fixture password: %v", err)
	}
	if err := seedDemoData(ctx, tx, string(passwordHash)); err != nil {
		t.Fatalf("seed demo fixtures: %v", err)
	}
	assertSeedFixtures(t, ctx, tx, "available")
	if _, err := tx.Exec(ctx, `
		UPDATE ride_hailing.driver_profiles
		SET status = 'offline'
		WHERE user_id = '20000000-0000-4000-8000-000000000002'`); err != nil {
		t.Fatalf("prepare driver availability preservation check: %v", err)
	}
	if err := seedDemoData(ctx, tx, string(passwordHash)); err != nil {
		t.Fatalf("repeat demo seed: %v", err)
	}
	assertSeedFixtures(t, ctx, tx, "offline")

	for _, table := range []string{
		"users", "locations", "driver_profiles", "driver_categories",
		"rides", "ride_assignments", "idempotency_keys", "sessions",
	} {
		var enabled, forced bool
		err := tx.QueryRow(ctx, `
			SELECT c.relrowsecurity, c.relforcerowsecurity
			FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = 'ride_hailing' AND c.relname = $1`, table).Scan(&enabled, &forced)
		if err != nil {
			t.Fatalf("read RLS flags for %s: %v", table, err)
		}
		if !enabled || !forced {
			t.Errorf("RLS is not enabled and forced for %s", table)
		}
	}
	var policyCount int
	if err := tx.QueryRow(ctx, `
		SELECT count(*)
		FROM pg_policy p
		JOIN pg_class c ON c.oid = p.polrelid
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = 'ride_hailing'`).Scan(&policyCount); err != nil {
		t.Fatalf("count application RLS policies: %v", err)
	}
	if policyCount != 0 {
		t.Errorf("expected no browser-facing RLS policies, found %d", policyCount)
	}

	name := fmt.Sprintf("schema-check-%d", time.Now().UnixNano())
	riderA := insertTestUser(t, ctx, tx, name+"-rider-a@example.test", "rider")
	riderB := insertTestUser(t, ctx, tx, name+"-rider-b@example.test", "rider")
	driverA := insertTestUser(t, ctx, tx, name+"-driver-a@example.test", "driver")
	driverB := insertTestUser(t, ctx, tx, name+"-driver-b@example.test", "driver")
	for _, driverID := range []string{driverA, driverB} {
		if _, err := tx.Exec(ctx, `
			INSERT INTO ride_hailing.driver_profiles
				(user_id, status, city, demo_latitude, demo_longitude)
			VALUES ($1, 'available', 'Lahore', 31.5113, 74.3436)`, driverID); err != nil {
			t.Fatalf("insert test driver profile: %v", err)
		}
	}
	expectConstraintFailure(t, ctx, tx, "23503", `
		INSERT INTO ride_hailing.driver_profiles
			(user_id, status, city, demo_latitude, demo_longitude)
		VALUES ('30000000-0000-4000-8000-000000000001', 'available', 'Lahore', 31.5113, 74.3436)`)

	locationA := "10000000-0000-4000-8000-000000000001"
	locationB := "10000000-0000-4000-8000-000000000002"
	rideA := insertTestRide(t, ctx, tx, riderA, locationA, locationB)
	rideB := insertTestRide(t, ctx, tx, riderB, locationA, locationB)

	expectConstraintFailure(t, ctx, tx, "23505", `
		INSERT INTO ride_hailing.rides
			(rider_id, pickup_location_id, destination_location_id, category,
			 estimated_fare_paisa, estimated_distance_meters)
		VALUES ($1, $2, $3, 'economy', 1000, 1000)`, riderA, locationA, locationB)
	expectConstraintFailure(t, ctx, tx, "22P02", `
		INSERT INTO ride_hailing.rides
			(rider_id, pickup_location_id, destination_location_id, category,
			 estimated_fare_paisa, estimated_distance_meters)
		VALUES ($1, $2, $3, 'unsupported', 1000, 1000)`, riderB, locationA, locationB)
	expectConstraintFailure(t, ctx, tx, "22P02", `
		INSERT INTO ride_hailing.rides
			(rider_id, pickup_location_id, destination_location_id, category, status,
			 estimated_fare_paisa, estimated_distance_meters)
		VALUES ($1, $2, $3, 'economy', 'unsupported', 1000, 1000)`,
		riderB, locationA, locationB)
	expectConstraintFailure(t, ctx, tx, "23514", `
		INSERT INTO ride_hailing.rides
			(rider_id, pickup_location_id, destination_location_id, category,
			 estimated_fare_paisa, estimated_distance_meters)
		VALUES ($1, $2, $2, 'economy', 1000, 1000)`, riderB, locationA)

	if _, err := tx.Exec(ctx, `
		INSERT INTO ride_hailing.ride_assignments (ride_id, driver_id)
		VALUES ($1, $2)`, rideA, driverA); err != nil {
		t.Fatalf("insert first active assignment: %v", err)
	}
	expectConstraintFailure(t, ctx, tx, "23505", `
		INSERT INTO ride_hailing.ride_assignments (ride_id, driver_id)
		VALUES ($1, $2)`, rideB, driverA)
	expectConstraintFailure(t, ctx, tx, "23505", `
		INSERT INTO ride_hailing.ride_assignments (ride_id, driver_id)
		VALUES ($1, $2)`, rideA, driverB)

	if _, err := tx.Exec(ctx, `
		INSERT INTO ride_hailing.idempotency_keys
			(rider_id, idempotency_key, request_hash, ride_id)
		VALUES ($1, 'schema-check-key', decode(repeat('ab', 32), 'hex'), $2)`, riderA, rideA); err != nil {
		t.Fatalf("insert idempotency fixture: %v", err)
	}
	expectConstraintFailure(t, ctx, tx, "23505", `
		INSERT INTO ride_hailing.idempotency_keys
			(rider_id, idempotency_key, request_hash, ride_id)
		VALUES ($1, 'schema-check-key', decode(repeat('cd', 32), 'hex'), $2)`, riderA, rideA)

	expectConstraintFailure(t, ctx, tx, "23514", `
		UPDATE ride_hailing.rides SET status = 'accepted' WHERE id = $1`, rideB)
}

func assertSeedFixtures(t *testing.T, ctx context.Context, tx pgx.Tx, expectedFirstDriverStatus string) {
	t.Helper()
	var userCount, locationCount, driverCount, categoryCount int
	err := tx.QueryRow(ctx, `
		SELECT
			(SELECT count(*) FROM ride_hailing.users WHERE email IN
			 ('rider@example.test', 'driver.one@example.test', 'driver.two@example.test')),
			(SELECT count(*) FROM ride_hailing.locations WHERE id::text LIKE '10000000-0000-4000-8000-00000000000%'),
			(SELECT count(*) FROM ride_hailing.driver_profiles WHERE user_id IN
			 ('20000000-0000-4000-8000-000000000002', '20000000-0000-4000-8000-000000000003')),
			(SELECT count(*) FROM ride_hailing.driver_categories WHERE driver_id IN
			 ('20000000-0000-4000-8000-000000000002', '20000000-0000-4000-8000-000000000003'))`).Scan(
		&userCount, &locationCount, &driverCount, &categoryCount)
	if err != nil {
		t.Fatalf("count seeded fixtures: %v", err)
	}
	if userCount != 3 || locationCount != 7 || driverCount != 2 || categoryCount != 4 {
		t.Fatalf("unexpected seed counts: users=%d locations=%d drivers=%d categories=%d", userCount, locationCount, driverCount, categoryCount)
	}
	var status, secondDriverStatus, hashPrefix string
	if err := tx.QueryRow(ctx, `
		SELECT d1.status::text, d2.status::text, left(u.password_hash, 4)
		FROM ride_hailing.driver_profiles d1
		JOIN ride_hailing.users u ON u.id = d1.user_id
		JOIN ride_hailing.driver_profiles d2 ON d2.user_id = '20000000-0000-4000-8000-000000000003'
		WHERE u.email = 'driver.one@example.test'`).Scan(&status, &secondDriverStatus, &hashPrefix); err != nil {
		t.Fatalf("read repeated seed state: %v", err)
	}
	if status != expectedFirstDriverStatus || secondDriverStatus != "available" {
		t.Errorf("unexpected seeded driver availability: first=%q second=%q", status, secondDriverStatus)
	}
	if hashPrefix != "$2a$" && hashPrefix != "$2b$" && hashPrefix != "$2y$" {
		t.Errorf("seeded password does not appear to be bcrypt hashed (prefix %q)", hashPrefix)
	}
	var economyDrivers, comfortDrivers int
	if err := tx.QueryRow(ctx, `
		SELECT count(*) FILTER (WHERE category = 'economy'),
		       count(*) FILTER (WHERE category = 'comfort')
		FROM ride_hailing.driver_categories
		WHERE driver_id IN ('20000000-0000-4000-8000-000000000002', '20000000-0000-4000-8000-000000000003')`).Scan(
		&economyDrivers, &comfortDrivers); err != nil {
		t.Fatalf("read seeded driver categories: %v", err)
	}
	if economyDrivers != 2 || comfortDrivers != 2 {
		t.Errorf("seeded drivers lack common category coverage: economy=%d comfort=%d", economyDrivers, comfortDrivers)
	}
}

func insertTestUser(t *testing.T, ctx context.Context, tx pgx.Tx, email, role string) string {
	t.Helper()
	var id string
	err := tx.QueryRow(ctx, `
		INSERT INTO ride_hailing.users (email, display_name, role, password_hash)
		VALUES ($1, 'Schema Check', $2, 'integration-test-hash')
		RETURNING id::text`, email, role).Scan(&id)
	if err != nil {
		t.Fatalf("insert test %s user: %v", role, err)
	}
	return id
}

func insertTestRide(t *testing.T, ctx context.Context, tx pgx.Tx, riderID, pickupID, destinationID string) string {
	t.Helper()
	var id string
	err := tx.QueryRow(ctx, `
		INSERT INTO ride_hailing.rides
			(rider_id, pickup_location_id, destination_location_id, category,
			 estimated_fare_paisa, estimated_distance_meters)
		VALUES ($1, $2, $3, 'economy', 1000, 1000)
		RETURNING id::text`, riderID, pickupID, destinationID).Scan(&id)
	if err != nil {
		t.Fatalf("insert test ride: %v", err)
	}
	return id
}

func expectConstraintFailure(t *testing.T, ctx context.Context, tx pgx.Tx, expectedSQLState, query string, args ...any) {
	t.Helper()
	if _, err := tx.Exec(ctx, "SAVEPOINT expected_constraint_failure"); err != nil {
		t.Fatalf("create constraint-check savepoint: %v", err)
	}
	_, queryErr := tx.Exec(ctx, query, args...)
	if queryErr == nil {
		_, _ = tx.Exec(ctx, "ROLLBACK TO SAVEPOINT expected_constraint_failure")
		_, _ = tx.Exec(ctx, "RELEASE SAVEPOINT expected_constraint_failure")
		t.Fatal("expected PostgreSQL to reject invalid fixture, but it accepted it")
	}
	var postgresError *pgconn.PgError
	if !errors.As(queryErr, &postgresError) {
		_, _ = tx.Exec(ctx, "ROLLBACK TO SAVEPOINT expected_constraint_failure")
		_, _ = tx.Exec(ctx, "RELEASE SAVEPOINT expected_constraint_failure")
		t.Fatalf("expected PostgreSQL SQLSTATE %s, got %v", expectedSQLState, queryErr)
	}
	if postgresError.Code != expectedSQLState {
		_, _ = tx.Exec(ctx, "ROLLBACK TO SAVEPOINT expected_constraint_failure")
		_, _ = tx.Exec(ctx, "RELEASE SAVEPOINT expected_constraint_failure")
		t.Fatalf("expected PostgreSQL SQLSTATE %s, got %s (%v)", expectedSQLState, postgresError.Code, queryErr)
	}
	if _, err := tx.Exec(ctx, "ROLLBACK TO SAVEPOINT expected_constraint_failure"); err != nil {
		t.Fatalf("rollback expected constraint failure: %v", err)
	}
	if _, err := tx.Exec(ctx, "RELEASE SAVEPOINT expected_constraint_failure"); err != nil {
		t.Fatalf("release constraint-check savepoint: %v", err)
	}
}
