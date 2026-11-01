package rides

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/haseeb-arshad/ride-hailing/apps/api/internal/drivers"
	"github.com/haseeb-arshad/ride-hailing/apps/api/internal/pricing"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPostgresTwoDriversCannotAcceptSameRide(t *testing.T) {
	pool := openRideIntegrationPool(t, 8)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	fixture := createLifecycleFixture(t, ctx, pool, 1, 2, 1)
	defer cleanupLifecycleFixture(t, pool, fixture)
	service := NewService(NewPostgresRepository(pool), pricing.DefaultMatchRadiusKM)

	start := make(chan struct{})
	results := make(chan acceptResult, 2)
	var workers sync.WaitGroup
	for _, driverID := range fixture.DriverIDs {
		workers.Add(1)
		go func(driverID string) {
			defer workers.Done()
			<-start
			ride, err := service.AcceptRide(ctx, fixture.RideIDs[0], driverID)
			results <- acceptResult{ride: ride, err: err}
		}(driverID)
	}
	close(start)
	workers.Wait()
	close(results)

	winners, conflicts := 0, 0
	for result := range results {
		if result.err == nil && result.ride.Status == "accepted" {
			winners++
			continue
		}
		if errors.Is(result.err, ErrStateConflict) || errors.Is(result.err, ErrDriverUnavailable) {
			conflicts++
			continue
		}
		t.Fatalf("unexpected same-ride acceptance outcome: ride=%+v err=%v", result.ride, result.err)
	}
	if winners != 1 || conflicts != 1 {
		t.Fatalf("same-ride race outcomes winners=%d conflicts=%d, want one each", winners, conflicts)
	}
	assertLifecycleState(t, ctx, pool, fixture.RideIDs[0], "accepted", 1, 1)
}

func TestPostgresOneDriverCannotAcceptTwoRides(t *testing.T) {
	pool := openRideIntegrationPool(t, 8)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	fixture := createLifecycleFixture(t, ctx, pool, 2, 1, 2)
	defer cleanupLifecycleFixture(t, pool, fixture)
	service := NewService(NewPostgresRepository(pool), pricing.DefaultMatchRadiusKM)
	driverID := fixture.DriverIDs[0]

	start := make(chan struct{})
	results := make(chan acceptResult, 2)
	var workers sync.WaitGroup
	for _, rideID := range fixture.RideIDs {
		workers.Add(1)
		go func(rideID string) {
			defer workers.Done()
			<-start
			ride, err := service.AcceptRide(ctx, rideID, driverID)
			results <- acceptResult{ride: ride, err: err}
		}(rideID)
	}
	close(start)
	workers.Wait()
	close(results)

	winners, conflicts := 0, 0
	for result := range results {
		if result.err == nil && result.ride.Status == "accepted" {
			winners++
			continue
		}
		if errors.Is(result.err, ErrStateConflict) || errors.Is(result.err, ErrDriverUnavailable) {
			conflicts++
			continue
		}
		t.Fatalf("unexpected same-driver acceptance outcome: ride=%+v err=%v", result.ride, result.err)
	}
	if winners != 1 || conflicts != 1 {
		t.Fatalf("same-driver race outcomes winners=%d conflicts=%d, want one each", winners, conflicts)
	}
	var activeAssignments int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM ride_hailing.ride_assignments WHERE driver_id = $1::uuid AND status = 'active'`, driverID).Scan(&activeAssignments); err != nil {
		t.Fatalf("count active driver assignments: %v", err)
	}
	if activeAssignments != 1 {
		t.Fatalf("active assignment count = %d, want 1", activeAssignments)
	}
}

func TestPostgresAcceptRacingCancellationLeavesConsistentState(t *testing.T) {
	pool := openRideIntegrationPool(t, 6)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	fixture := createLifecycleFixture(t, ctx, pool, 1, 1, 1)
	defer cleanupLifecycleFixture(t, pool, fixture)
	service := NewService(NewPostgresRepository(pool), pricing.DefaultMatchRadiusKM)

	start := make(chan struct{})
	acceptResults := make(chan acceptResult, 1)
	cancelResults := make(chan error, 1)
	var workers sync.WaitGroup
	workers.Add(2)
	go func() {
		defer workers.Done()
		<-start
		ride, err := service.AcceptRide(ctx, fixture.RideIDs[0], fixture.DriverIDs[0])
		acceptResults <- acceptResult{ride: ride, err: err}
	}()
	go func() {
		defer workers.Done()
		<-start
		_, err := service.CancelRide(ctx, fixture.RideIDs[0], fixture.RiderIDs[0])
		cancelResults <- err
	}()
	close(start)
	workers.Wait()
	accept := <-acceptResults
	cancelErr := <-cancelResults
	if cancelErr != nil {
		t.Fatalf("rider cancellation lost its valid race: %v", cancelErr)
	}
	if accept.err != nil && !errors.Is(accept.err, ErrStateConflict) {
		t.Fatalf("unexpected acceptance race error: %v", accept.err)
	}
	assertLifecycleState(t, ctx, pool, fixture.RideIDs[0], "cancelled", 0, 0)
	var assignmentCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM ride_hailing.ride_assignments WHERE ride_id = $1::uuid`, fixture.RideIDs[0]).Scan(&assignmentCount); err != nil {
		t.Fatalf("count race assignment rows: %v", err)
	}
	if assignmentCount != 0 && assignmentCount != 1 {
		t.Fatalf("cancellation race left %d assignment rows, want zero or one closed row", assignmentCount)
	}
	if assignmentCount == 1 {
		var assignmentStatus string
		if err := pool.QueryRow(ctx, `SELECT status::text FROM ride_hailing.ride_assignments WHERE ride_id = $1::uuid`, fixture.RideIDs[0]).Scan(&assignmentStatus); err != nil {
			t.Fatalf("read terminal race assignment: %v", err)
		}
		if assignmentStatus != "cancelled" {
			t.Fatalf("terminal race assignment status = %q, want cancelled", assignmentStatus)
		}
	}
}

func TestPostgresEligibilityAvailabilityAndDriverRelease(t *testing.T) {
	pool := openRideIntegrationPool(t, 4)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	fixture := createLifecycleFixture(t, ctx, pool, 3, 1, 3)
	defer cleanupLifecycleFixture(t, pool, fixture)
	service := NewService(NewPostgresRepository(pool), pricing.DefaultMatchRadiusKM)
	driverID := fixture.DriverIDs[0]
	firstRide, secondRide, thirdRide := fixture.RideIDs[0], fixture.RideIDs[1], fixture.RideIDs[2]

	if status, err := service.DriverAvailability(ctx, driverID); err != nil || status != drivers.Available {
		t.Fatalf("initial driver availability = %q, %v; want available", status, err)
	}

	if _, err := service.SetAvailability(ctx, driverID, string(drivers.Offline)); err != nil {
		t.Fatalf("set driver offline: %v", err)
	}
	if status, err := service.DriverAvailability(ctx, driverID); err != nil || status != drivers.Offline {
		t.Fatalf("read offline driver availability = %q, %v; want offline", status, err)
	}
	if _, err := service.AcceptRide(ctx, firstRide, driverID); !errors.Is(err, ErrDriverUnavailable) {
		t.Fatalf("offline driver acceptance error = %v, want ErrDriverUnavailable", err)
	}
	if _, err := service.SetAvailability(ctx, driverID, string(drivers.Available)); err != nil {
		t.Fatalf("set driver available: %v", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM ride_hailing.driver_categories WHERE driver_id = $1::uuid AND category = 'economy'`, driverID); err != nil {
		t.Fatalf("remove economy eligibility: %v", err)
	}
	if _, err := service.AcceptRide(ctx, firstRide, driverID); !errors.Is(err, ErrDriverUnavailable) {
		t.Fatalf("wrong-category driver acceptance error = %v, want ErrDriverUnavailable", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO ride_hailing.driver_categories (driver_id, category) VALUES ($1::uuid, 'economy')`, driverID); err != nil {
		t.Fatalf("restore economy eligibility: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE ride_hailing.driver_profiles SET demo_latitude = 40, demo_longitude = 40 WHERE user_id = $1::uuid`, driverID); err != nil {
		t.Fatalf("move test driver outside matching radius: %v", err)
	}
	if _, err := service.AcceptRide(ctx, firstRide, driverID); !errors.Is(err, ErrDriverUnavailable) {
		t.Fatalf("out-of-radius driver acceptance error = %v, want ErrDriverUnavailable", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE ride_hailing.driver_profiles SET demo_latitude = 31.5113, demo_longitude = 74.3436 WHERE user_id = $1::uuid`, driverID); err != nil {
		t.Fatalf("restore test driver location: %v", err)
	}

	if _, err := service.CancelRide(ctx, firstRide, fixture.RiderIDs[0]); err != nil {
		t.Fatalf("cancel requested ride: %v", err)
	}
	if _, err := service.AcceptRide(ctx, firstRide, driverID); !errors.Is(err, ErrStateConflict) {
		t.Fatalf("cancelled ride acceptance error = %v, want ErrStateConflict", err)
	}
	if _, err := service.AcceptRide(ctx, secondRide, driverID); err != nil {
		t.Fatalf("accept eligible ride after prior conflicts: %v", err)
	}
	if _, err := service.SetAvailability(ctx, driverID, string(drivers.Offline)); !errors.Is(err, ErrDriverUnavailable) {
		t.Fatalf("busy driver availability change error = %v, want ErrDriverUnavailable", err)
	}
	if _, err := service.CompleteRide(ctx, secondRide, fixture.DriverIDs[0]+"-wrong"); !errors.Is(err, ErrRideNotFound) {
		t.Fatalf("wrong driver completion error = %v, want concealed ErrRideNotFound", err)
	}
	if _, err := service.CompleteRide(ctx, secondRide, driverID); err != nil {
		t.Fatalf("complete assigned ride: %v", err)
	}
	if _, err := service.AcceptRide(ctx, secondRide, driverID); !errors.Is(err, ErrStateConflict) {
		t.Fatalf("completed ride acceptance error = %v, want ErrStateConflict", err)
	}
	if _, err := service.AcceptRide(ctx, thirdRide, driverID); err != nil {
		t.Fatalf("driver was not released after completion: %v", err)
	}
	if _, err := service.CancelRide(ctx, thirdRide, fixture.RiderIDs[2]); err != nil {
		t.Fatalf("cancel final accepted ride: %v", err)
	}
	var driverStatus string
	if err := pool.QueryRow(ctx, `SELECT status::text FROM ride_hailing.driver_profiles WHERE user_id = $1::uuid`, driverID).Scan(&driverStatus); err != nil {
		t.Fatalf("read released driver status: %v", err)
	}
	if driverStatus != "available" {
		t.Fatalf("released driver status = %q, want available", driverStatus)
	}
	if status, err := service.DriverAvailability(ctx, driverID); err != nil || status != drivers.Available {
		t.Fatalf("read released driver availability = %q, %v; want available", status, err)
	}
}

func TestPostgresDriverRequestsUseTheDriversEligibleCategoriesAndAvailability(t *testing.T) {
	pool := openRideIntegrationPool(t, 4)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	fixture := createLifecycleFixture(t, ctx, pool, 2, 1, 2)
	defer cleanupLifecycleFixture(t, pool, fixture)
	service := NewService(NewPostgresRepository(pool), pricing.DefaultMatchRadiusKM)
	driverID := fixture.DriverIDs[0]

	if _, err := pool.Exec(ctx, `DELETE FROM ride_hailing.driver_categories WHERE driver_id = $1::uuid AND category = 'comfort'`, driverID); err != nil {
		t.Fatalf("limit driver test categories to economy: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE ride_hailing.rides SET category = 'comfort' WHERE id = $1::uuid`, fixture.RideIDs[1]); err != nil {
		t.Fatalf("change second test ride category: %v", err)
	}
	var allRequests []DriverRequest
	cursor := ""
	for pageNumber := 0; pageNumber < 1000; pageNumber++ {
		page, err := service.ListDriverRequests(ctx, driverID, 50, cursor)
		if err != nil {
			t.Fatalf("list eligible driver requests: %v", err)
		}
		allRequests = append(allRequests, page.Requests...)
		if page.NextCursor == nil {
			break
		}
		cursor = *page.NextCursor
		if pageNumber == 999 {
			t.Fatal("driver request pagination exceeded 1000 pages")
		}
	}
	foundEconomy, foundComfort := false, false
	for _, request := range allRequests {
		if request.ID == fixture.RideIDs[0] {
			foundEconomy = true
			if request.Category != "economy" || request.PickupDistanceMeters > 20_000 || request.Status != "requested" {
				t.Errorf("eligible request has unexpected fields: %+v", request)
			}
		}
		if request.ID == fixture.RideIDs[1] {
			foundComfort = true
		}
	}
	if !foundEconomy || foundComfort {
		t.Fatalf("category filter results economy=%t comfort=%t, want economy present and comfort absent", foundEconomy, foundComfort)
	}
	if _, err := service.SetAvailability(ctx, driverID, string(drivers.Offline)); err != nil {
		t.Fatalf("set driver offline: %v", err)
	}
	if _, err := service.ListDriverRequests(ctx, driverID, 50, ""); !errors.Is(err, ErrDriverUnavailable) {
		t.Fatalf("offline request listing error = %v, want ErrDriverUnavailable", err)
	}
}

type lifecycleFixture struct {
	RiderIDs  []string
	DriverIDs []string
	RideIDs   []string
}

type acceptResult struct {
	ride Ride
	err  error
}

func createLifecycleFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool, riderCount, driverCount, rideCount int) lifecycleFixture {
	t.Helper()
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatalf("begin lifecycle fixture transaction: %v", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	fixture := lifecycleFixture{
		RiderIDs:  make([]string, 0, riderCount),
		DriverIDs: make([]string, 0, driverCount),
		RideIDs:   make([]string, 0, rideCount),
	}
	fixturePrefix := fmt.Sprintf("stage6-%d", time.Now().UnixNano())
	for index := 0; index < riderCount; index++ {
		var id string
		email := fmt.Sprintf("%s-rider-%d@example.test", fixturePrefix, index)
		if err := tx.QueryRow(ctx, `
			INSERT INTO ride_hailing.users (email, display_name, role, password_hash)
			VALUES ($1, 'Stage 6 Test Rider', 'rider', 'integration-test-hash')
			RETURNING id::text`, email).Scan(&id); err != nil {
			t.Fatalf("insert lifecycle rider: %v", err)
		}
		fixture.RiderIDs = append(fixture.RiderIDs, id)
	}
	for index := 0; index < driverCount; index++ {
		var id string
		email := fmt.Sprintf("%s-driver-%d@example.test", fixturePrefix, index)
		if err := tx.QueryRow(ctx, `
			INSERT INTO ride_hailing.users (email, display_name, role, password_hash)
			VALUES ($1, 'Stage 6 Test Driver', 'driver', 'integration-test-hash')
			RETURNING id::text`, email).Scan(&id); err != nil {
			t.Fatalf("insert lifecycle driver account: %v", err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO ride_hailing.driver_profiles (user_id, status, city, demo_latitude, demo_longitude)
			VALUES ($1::uuid, 'available', 'Lahore', 31.5113, 74.3436)`, id); err != nil {
			t.Fatalf("insert lifecycle driver profile: %v", err)
		}
		for _, category := range []string{"economy", "comfort"} {
			if _, err := tx.Exec(ctx, `
				INSERT INTO ride_hailing.driver_categories (driver_id, category)
				VALUES ($1::uuid, $2::ride_hailing.ride_category)`, id, category); err != nil {
				t.Fatalf("insert lifecycle driver category: %v", err)
			}
		}
		fixture.DriverIDs = append(fixture.DriverIDs, id)
	}
	for index := 0; index < rideCount; index++ {
		riderID := fixture.RiderIDs[index%len(fixture.RiderIDs)]
		var rideID string
		if err := tx.QueryRow(ctx, `
			INSERT INTO ride_hailing.rides
				(rider_id, pickup_location_id, destination_location_id, category,
				 estimated_fare_paisa, estimated_distance_meters, pickup_eta_minutes)
			VALUES ($1::uuid, $2::uuid, $3::uuid, 'economy', 23676, 1446, 2)
			RETURNING id::text`, riderID, integrationPickupID, integrationDestinationID).Scan(&rideID); err != nil {
			t.Fatalf("insert lifecycle ride: %v", err)
		}
		fixture.RideIDs = append(fixture.RideIDs, rideID)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit lifecycle fixtures: %v", err)
	}
	return fixture
}

func cleanupLifecycleFixture(t *testing.T, pool *pgxpool.Pool, fixture lifecycleFixture) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Errorf("begin lifecycle cleanup: %v", err)
		return
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	for _, rideID := range fixture.RideIDs {
		if _, err := tx.Exec(ctx, `DELETE FROM ride_hailing.ride_assignments WHERE ride_id = $1::uuid`, rideID); err != nil {
			t.Errorf("delete lifecycle assignment: %v", err)
			return
		}
		if _, err := tx.Exec(ctx, `DELETE FROM ride_hailing.rides WHERE id = $1::uuid`, rideID); err != nil {
			t.Errorf("delete lifecycle ride: %v", err)
			return
		}
	}
	for _, driverID := range fixture.DriverIDs {
		if _, err := tx.Exec(ctx, `DELETE FROM ride_hailing.driver_categories WHERE driver_id = $1::uuid`, driverID); err != nil {
			t.Errorf("delete lifecycle categories: %v", err)
			return
		}
		if _, err := tx.Exec(ctx, `DELETE FROM ride_hailing.driver_profiles WHERE user_id = $1::uuid`, driverID); err != nil {
			t.Errorf("delete lifecycle driver profile: %v", err)
			return
		}
	}
	for _, userID := range append(append([]string{}, fixture.RiderIDs...), fixture.DriverIDs...) {
		if _, err := tx.Exec(ctx, `DELETE FROM ride_hailing.users WHERE id = $1::uuid`, userID); err != nil {
			t.Errorf("delete lifecycle user: %v", err)
			return
		}
	}
	if err := tx.Commit(ctx); err != nil {
		t.Errorf("commit lifecycle cleanup: %v", err)
	}
}

func assertLifecycleState(t *testing.T, ctx context.Context, pool *pgxpool.Pool, rideID, expectedStatus string, expectedActiveAssignments, expectedBusyDrivers int) {
	t.Helper()
	var rideStatus string
	if err := pool.QueryRow(ctx, `SELECT status::text FROM ride_hailing.rides WHERE id = $1::uuid`, rideID).Scan(&rideStatus); err != nil {
		t.Fatalf("read lifecycle ride status: %v", err)
	}
	if rideStatus != expectedStatus {
		t.Fatalf("lifecycle ride status = %q, want %q", rideStatus, expectedStatus)
	}
	var activeAssignments int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM ride_hailing.ride_assignments WHERE ride_id = $1::uuid AND status = 'active'`, rideID).Scan(&activeAssignments); err != nil {
		t.Fatalf("count lifecycle active assignments: %v", err)
	}
	if activeAssignments != expectedActiveAssignments {
		t.Fatalf("active assignment count = %d, want %d", activeAssignments, expectedActiveAssignments)
	}
	var busyDrivers int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM ride_hailing.driver_profiles d
		JOIN ride_hailing.ride_assignments a ON a.driver_id = d.user_id
		WHERE a.ride_id = $1::uuid AND a.status = 'active' AND d.status = 'busy'`, rideID).Scan(&busyDrivers); err != nil {
		t.Fatalf("count lifecycle busy drivers: %v", err)
	}
	if busyDrivers != expectedBusyDrivers {
		t.Fatalf("busy assigned driver count = %d, want %d", busyDrivers, expectedBusyDrivers)
	}
}
