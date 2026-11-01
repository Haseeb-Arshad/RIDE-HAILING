package rides

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/haseeb-arshad/ride-hailing/apps/api/internal/pricing"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	integrationPickupID      = "10000000-0000-4000-8000-000000000001"
	integrationDestinationID = "10000000-0000-4000-8000-000000000002"
)

func TestPostgresRideQuoteCreationAndReplay(t *testing.T) {
	pool := openRideIntegrationPool(t, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	outer, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatalf("begin rollback-only ride test: %v", err)
	}
	defer func() { _ = outer.Rollback(context.Background()) }()
	riderID := insertIntegrationRider(t, ctx, outer)

	service := NewService(newPostgresRepositoryForTest(outer), pricing.DefaultMatchRadiusKM)
	request := QuoteRequest{PickupLocationID: integrationPickupID, DestinationLocationID: integrationDestinationID, Category: "economy"}
	quote, err := service.Quote(ctx, request)
	if err != nil {
		t.Fatalf("calculate quote from seeded Lahore locations: %v", err)
	}
	if quote.EstimatedDistanceMeters < 1 || quote.EstimatedFarePaisa < 15_000 || quote.Currency != "PKR" || !quote.Approximate {
		t.Fatalf("unexpected persisted fare inputs: %+v", quote)
	}

	first, replay, err := service.CreateRide(ctx, riderID, request, "integration-stage5-key")
	if err != nil || replay {
		t.Fatalf("create first ride: replay=%t err=%v", replay, err)
	}
	second, replay, err := service.CreateRide(ctx, riderID, request, "integration-stage5-key")
	if err != nil || !replay || second.ID != first.ID {
		t.Fatalf("same-key retry did not replay the original ride: first=%s second=%s replay=%t err=%v", first.ID, second.ID, replay, err)
	}

	changed := request
	changed.Category = "comfort"
	if _, _, err := service.CreateRide(ctx, riderID, changed, "integration-stage5-key"); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("changed request under same key error = %v, want ErrIdempotencyConflict", err)
	}
	if _, _, err := service.CreateRide(ctx, riderID, changed, "integration-stage5-new-key"); !errors.Is(err, ErrOpenRideConflict) {
		t.Fatalf("second open ride error = %v, want ErrOpenRideConflict", err)
	}

	current, err := service.CurrentRide(ctx, riderID)
	if err != nil || current == nil || current.ID != first.ID {
		t.Fatalf("current ride = %+v, err=%v; want original ride", current, err)
	}
	owned, err := service.GetRide(ctx, first.ID, riderID, "rider")
	if err != nil || owned == nil || owned.ID != first.ID {
		t.Fatalf("owner ride read = %+v, err=%v", owned, err)
	}
	if _, err := service.GetRide(ctx, first.ID, "30000000-0000-4000-8000-000000000001", "rider"); !errors.Is(err, ErrRideNotFound) {
		t.Fatalf("non-owner ride read error = %v, want concealed ErrRideNotFound", err)
	}

	var rideCount, idempotencyCount int
	if err := outer.QueryRow(ctx, `SELECT count(*) FROM ride_hailing.rides WHERE rider_id = $1::uuid`, riderID).Scan(&rideCount); err != nil {
		t.Fatalf("count rollback-only ride: %v", err)
	}
	if err := outer.QueryRow(ctx, `SELECT count(*) FROM ride_hailing.idempotency_keys WHERE rider_id = $1::uuid`, riderID).Scan(&idempotencyCount); err != nil {
		t.Fatalf("count rollback-only idempotency rows: %v", err)
	}
	if rideCount != 1 || idempotencyCount != 1 {
		t.Fatalf("ride/idempotency rows = %d/%d, want exactly one each", rideCount, idempotencyCount)
	}
}

func TestPostgresConcurrentSameKeyCreatesOneRide(t *testing.T) {
	pool := openRideIntegrationPool(t, 6)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	riderID := insertCommittedIntegrationRider(t, ctx, pool)
	defer cleanupIntegrationRider(t, pool, riderID)

	service := NewService(NewPostgresRepository(pool), pricing.DefaultMatchRadiusKM)
	request := QuoteRequest{PickupLocationID: integrationPickupID, DestinationLocationID: integrationDestinationID, Category: "economy"}
	start := make(chan struct{})
	results := make(chan createResult, 2)
	var workers sync.WaitGroup
	for range 2 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			ride, replay, err := service.CreateRide(ctx, riderID, request, "integration-concurrent-key")
			results <- createResult{ride: ride, replay: replay, err: err}
		}()
	}
	close(start)
	workers.Wait()
	close(results)

	var rideID string
	createdCount, replayCount := 0, 0
	for result := range results {
		if result.err != nil {
			t.Fatalf("concurrent create failed: %v", result.err)
		}
		if rideID == "" {
			rideID = result.ride.ID
		} else if result.ride.ID != rideID {
			t.Fatalf("concurrent requests returned different rides: %s and %s", rideID, result.ride.ID)
		}
		if result.replay {
			replayCount++
		} else {
			createdCount++
		}
	}
	if createdCount != 1 || replayCount != 1 {
		t.Fatalf("concurrent outcomes created=%d replayed=%d, want one of each", createdCount, replayCount)
	}

	var rideCount, keyCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM ride_hailing.rides WHERE rider_id = $1::uuid`, riderID).Scan(&rideCount); err != nil {
		t.Fatalf("count concurrent rides: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM ride_hailing.idempotency_keys WHERE rider_id = $1::uuid AND idempotency_key = 'integration-concurrent-key'`, riderID).Scan(&keyCount); err != nil {
		t.Fatalf("count concurrent idempotency keys: %v", err)
	}
	if rideCount != 1 || keyCount != 1 {
		t.Fatalf("concurrent database rows ride=%d key=%d, want one each", rideCount, keyCount)
	}
}

type createResult struct {
	ride   Ride
	replay bool
	err    error
}

func openRideIntegrationPool(t *testing.T, maxConnections int32) *pgxpool.Pool {
	t.Helper()
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("set TEST_DATABASE_URL to a migrated and seeded Supabase/PostgreSQL database to run ride integration checks")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	poolConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatalf("parse TEST_DATABASE_URL: %v", err)
	}
	poolConfig.MaxConns = maxConnections
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatalf("open ride integration database: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("ping ride integration database: %v", err)
	}
	return pool
}

func insertIntegrationRider(t *testing.T, ctx context.Context, tx pgx.Tx) string {
	t.Helper()
	var riderID string
	email := fmt.Sprintf("stage5-rollback-%d@example.test", time.Now().UnixNano())
	err := tx.QueryRow(ctx, `
		INSERT INTO ride_hailing.users (email, display_name, role, password_hash)
		VALUES ($1, 'Stage 5 Rollback Rider', 'rider', 'integration-test-hash')
		RETURNING id::text`, email).Scan(&riderID)
	if err != nil {
		t.Fatalf("insert rollback-only rider: %v", err)
	}
	return riderID
}

func insertCommittedIntegrationRider(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()
	var riderID string
	email := fmt.Sprintf("stage5-concurrent-%d@example.test", time.Now().UnixNano())
	err := pool.QueryRow(ctx, `
		INSERT INTO ride_hailing.users (email, display_name, role, password_hash)
		VALUES ($1, 'Stage 5 Concurrent Rider', 'rider', 'integration-test-hash')
		RETURNING id::text`, email).Scan(&riderID)
	if err != nil {
		t.Fatalf("insert concurrent integration rider: %v", err)
	}
	return riderID
}

func cleanupIntegrationRider(t *testing.T, pool *pgxpool.Pool, riderID string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Errorf("begin concurrent rider cleanup: %v", err)
		return
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	for _, query := range []string{
		`DELETE FROM ride_hailing.idempotency_keys WHERE rider_id = $1::uuid`,
		`DELETE FROM ride_hailing.rides WHERE rider_id = $1::uuid`,
		`DELETE FROM ride_hailing.users WHERE id = $1::uuid`,
	} {
		if _, err := tx.Exec(ctx, query, riderID); err != nil {
			t.Errorf("clean up concurrent ride fixture: %v", err)
			return
		}
	}
	if err := tx.Commit(ctx); err != nil {
		t.Errorf("commit concurrent rider cleanup: %v", err)
	}
}
