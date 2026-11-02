package rides

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestPostgresGuardsRideAndDriverInvariants(t *testing.T) {
	pool := openRideIntegrationPool(t, 2)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	fixture := createLifecycleFixture(t, ctx, pool, 1, 1, 1)
	defer cleanupLifecycleFixture(t, pool, fixture)
	rideID := fixture.RideIDs[0]
	driverID := fixture.DriverIDs[0]

	invalidWrites := []struct {
		name       string
		query      string
		args       []any
		constraint string
	}{
		{
			name:       "ride fare snapshot is immutable",
			query:      `UPDATE ride_hailing.rides SET estimated_fare_paisa = 1 WHERE id = $1::uuid`,
			args:       []any{rideID},
			constraint: "rides_request_snapshot_immutable_check",
		},
		{
			name:       "fuel price snapshot is immutable",
			query:      `UPDATE ride_hailing.rides SET fuel_price_paisa_per_liter = 1 WHERE id = $1::uuid`,
			args:       []any{rideID},
			constraint: "rides_request_snapshot_immutable_check",
		},
		{
			name:       "requested ride cannot skip acceptance",
			query:      `UPDATE ride_hailing.rides SET status = 'completed', completed_at = now() WHERE id = $1::uuid`,
			args:       []any{rideID},
			constraint: "rides_lifecycle_transition_check",
		},
		{
			name:       "accepted ride requires an assignment",
			query:      `UPDATE ride_hailing.rides SET status = 'accepted', accepted_at = now() WHERE id = $1::uuid`,
			args:       []any{rideID},
			constraint: "ride_assignment_consistency_check",
		},
		{
			name:       "busy driver requires an active assignment",
			query:      `UPDATE ride_hailing.driver_profiles SET status = 'busy' WHERE user_id = $1::uuid`,
			args:       []any{driverID},
			constraint: "driver_assignment_status_consistency_check",
		},
	}

	for _, invalidWrite := range invalidWrites {
		t.Run(invalidWrite.name, func(t *testing.T) {
			_, err := pool.Exec(ctx, invalidWrite.query, invalidWrite.args...)
			assertConstraintViolation(t, err, invalidWrite.constraint)
		})
	}

	var rideStatus, driverStatus string
	if err := pool.QueryRow(ctx, `SELECT status::text FROM ride_hailing.rides WHERE id = $1::uuid`, rideID).Scan(&rideStatus); err != nil {
		t.Fatalf("read ride after rejected writes: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT status::text FROM ride_hailing.driver_profiles WHERE user_id = $1::uuid`, driverID).Scan(&driverStatus); err != nil {
		t.Fatalf("read driver after rejected writes: %v", err)
	}
	if rideStatus != "requested" || driverStatus != "available" {
		t.Fatalf("rejected writes changed persisted state: ride=%q driver=%q", rideStatus, driverStatus)
	}
}

func assertConstraintViolation(t *testing.T, err error, constraint string) {
	t.Helper()
	if err == nil {
		t.Fatalf("database accepted an invalid write; want constraint %q", constraint)
	}
	var postgresError *pgconn.PgError
	if !errors.As(err, &postgresError) {
		t.Fatalf("invalid write error = %T %v, want PostgreSQL constraint violation %q", err, err, constraint)
	}
	if postgresError.Code != "23514" || postgresError.ConstraintName != constraint {
		t.Fatalf("invalid write error code/constraint = %q/%q, want 23514/%q", postgresError.Code, postgresError.ConstraintName, constraint)
	}
}
