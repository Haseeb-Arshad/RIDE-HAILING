package rides

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPostgresCustomLahorePinsPersistFuelBasedQuoteAndStayOutOfSuggestions(t *testing.T) {
	pool := openRideIntegrationPool(t, 4)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	cleanupAbandonedCitywidePinTestRows(t, pool)
	fixture := createLifecycleFixture(t, ctx, pool, 1, 1, 0)
	defer func() { cleanupLifecycleFixture(t, pool, fixture) }()
	service := NewService(NewPostgresRepository(pool), 20)
	request := QuoteRequest{
		Pickup:      &LocationInput{Name: "Street 14 pin", Area: "DHA Phase 6", Latitude: 31.624, Longitude: 74.557},
		Destination: &LocationInput{Name: "Park pin", Area: "Raiwind Road", Latitude: 31.352, Longitude: 74.222},
		Category:    "economy",
	}

	quote, err := service.Quote(ctx, request)
	if err != nil {
		t.Fatalf("quote between arbitrary Lahore pins: %v", err)
	}
	if quote.EstimatedDistanceMeters < 1_000 || quote.FuelPricePaisaPerLiter != 37_791 {
		t.Fatalf("unexpected map-pin quote: %+v", quote)
	}
	if quote.EstimatedFarePaisa != quote.FuelCostPaisa+quote.BaseFarePaisa+quote.DistanceCostPaisa {
		t.Fatalf("fare breakdown does not sum to quote: %+v", quote)
	}

	ride, replay, err := service.CreateRide(ctx, fixture.RiderIDs[0], request, "custom-lahore-pin-ride")
	if err != nil {
		t.Fatalf("create ride from arbitrary pins: %v", err)
	}
	fixture.RideIDs = append(fixture.RideIDs, ride.ID)
	fixture.LocationIDs = append(fixture.LocationIDs, ride.Pickup.ID, ride.Destination.ID)
	if replay || ride.Pickup.Name != request.Pickup.Name || ride.Destination.Name != request.Destination.Name {
		t.Fatalf("custom route was not persisted: replay=%t pickup=%+v destination=%+v", replay, ride.Pickup, ride.Destination)
	}
	if ride.FuelPricePaisaPerLiter == nil || *ride.FuelPricePaisaPerLiter != quote.FuelPricePaisaPerLiter ||
		ride.FuelPriceEffectiveDate == nil || !ride.FuelPriceEffectiveDate.Equal(quote.FuelPriceEffectiveDate) ||
		ride.FuelCostPaisa == nil || ride.BaseFarePaisa == nil || ride.DistanceCostPaisa == nil || ride.PricingVersion == nil {
		t.Fatalf("ride did not retain its pricing snapshot: %+v", ride)
	}
	if ride.EstimatedFarePaisa != *ride.FuelCostPaisa+*ride.BaseFarePaisa+*ride.DistanceCostPaisa {
		t.Fatalf("persisted fare breakdown does not sum to the ride fare: %+v", ride)
	}

	locations, err := service.ListLocations(ctx)
	if err != nil {
		t.Fatalf("list suggested stops: %v", err)
	}
	for _, location := range locations {
		if location.ID == ride.Pickup.ID || location.ID == ride.Destination.ID {
			t.Fatalf("private ride pin leaked into the suggested-stop list: %+v", location)
		}
	}

	var selectablePins int
	if err := pool.QueryRow(ctx, `
		SELECT count(*)
		FROM ride_hailing.locations
		WHERE id IN ($1::uuid, $2::uuid) AND selectable`, ride.Pickup.ID, ride.Destination.ID).Scan(&selectablePins); err != nil {
		t.Fatalf("check saved map-pin visibility: %v", err)
	}
	if selectablePins != 0 {
		t.Fatalf("custom pins marked selectable = %d, want 0", selectablePins)
	}
}

// A failed prior run may leave this test's temporary rider and ride behind.
// Match the synthetic fixture prefix and distinct test pin labels so cleanup
// cannot touch authenticated demo-account rides.
func cleanupAbandonedCitywidePinTestRows(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin stale map-pin fixture cleanup: %v", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	rows, err := tx.Query(ctx, `
		SELECT r.id::text, u.id::text, p.id::text, d.id::text,
			substring(u.email FROM '^(stage6-[0-9]+)-rider-0@example[.]test$')
		FROM ride_hailing.rides r
		JOIN ride_hailing.users u ON u.id = r.rider_id
		JOIN ride_hailing.locations p ON p.id = r.pickup_location_id
		JOIN ride_hailing.locations d ON d.id = r.destination_location_id
		WHERE u.email ~ '^stage6-[0-9]+-rider-0@example[.]test$'
			AND p.name = 'Street 14 pin' AND p.area = 'DHA Phase 6'
			AND d.name = 'Park pin' AND d.area = 'Raiwind Road'`)
	if err != nil {
		t.Fatalf("find stale map-pin fixture: %v", err)
	}
	type staleFixture struct{ rideID, riderID, pickupID, destinationID, prefix string }
	var stale []staleFixture
	for rows.Next() {
		var item staleFixture
		if err := rows.Scan(&item.rideID, &item.riderID, &item.pickupID, &item.destinationID, &item.prefix); err != nil {
			rows.Close()
			t.Fatalf("read stale map-pin fixture: %v", err)
		}
		stale = append(stale, item)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatalf("finish stale map-pin fixture scan: %v", err)
	}
	for _, item := range stale {
		for _, query := range []string{
			`DELETE FROM ride_hailing.ride_assignments WHERE ride_id = $1::uuid`,
			`DELETE FROM ride_hailing.idempotency_keys WHERE ride_id = $1::uuid`,
			`DELETE FROM ride_hailing.rides WHERE id = $1::uuid`,
		} {
			if _, err := tx.Exec(ctx, query, item.rideID); err != nil {
				t.Fatalf("delete stale map-pin ride fixture: %v", err)
			}
		}
		for _, locationID := range []string{item.pickupID, item.destinationID} {
			if _, err := tx.Exec(ctx, `DELETE FROM ride_hailing.locations WHERE id = $1::uuid AND NOT selectable`, locationID); err != nil {
				t.Fatalf("delete stale map-pin locations: %v", err)
			}
		}
		if _, err := tx.Exec(ctx, `
			DELETE FROM ride_hailing.driver_categories
			WHERE driver_id IN (SELECT id FROM ride_hailing.users WHERE email LIKE $1 || '-driver-%@example.test')`, item.prefix); err != nil {
			t.Fatalf("delete stale map-pin driver categories: %v", err)
		}
		if _, err := tx.Exec(ctx, `
			DELETE FROM ride_hailing.driver_profiles
			WHERE user_id IN (SELECT id FROM ride_hailing.users WHERE email LIKE $1 || '-driver-%@example.test')`, item.prefix); err != nil {
			t.Fatalf("delete stale map-pin driver profiles: %v", err)
		}
		if _, err := tx.Exec(ctx, `DELETE FROM ride_hailing.users WHERE id = $1::uuid`, item.riderID); err != nil {
			t.Fatalf("delete stale map-pin rider: %v", err)
		}
		if _, err := tx.Exec(ctx, `DELETE FROM ride_hailing.users WHERE email LIKE $1 || '-driver-%@example.test'`, item.prefix); err != nil {
			t.Fatalf("delete stale map-pin driver: %v", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit stale map-pin fixture cleanup: %v", err)
	}
}
