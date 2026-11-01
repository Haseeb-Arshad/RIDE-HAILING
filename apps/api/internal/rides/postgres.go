package rides

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"

	"github.com/haseeb-arshad/ride-hailing/apps/api/internal/pricing"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type queryRunner interface {
	QueryRow(context.Context, string, ...any) pgx.Row
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

type database interface {
	queryRunner
	Begin(context.Context) (pgx.Tx, error)
}

type PostgresRepository struct {
	database database
}

func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	if pool == nil {
		return &PostgresRepository{}
	}
	return &PostgresRepository{database: pool}
}

func newPostgresRepositoryForTest(database database) *PostgresRepository {
	return &PostgresRepository{database: database}
}

func (r *PostgresRepository) ListLocations(ctx context.Context) ([]pricing.Location, error) {
	if r.database == nil {
		return nil, ErrStoreUnavailable
	}
	rows, err := r.database.Query(ctx, `
		SELECT id::text, name, area, city, latitude, longitude
		FROM ride_hailing.locations
		WHERE city = 'Lahore' AND active
		ORDER BY name, id`)
	if err != nil {
		return nil, unavailable(err)
	}
	defer rows.Close()

	locations := make([]pricing.Location, 0)
	for rows.Next() {
		var location pricing.Location
		if err := rows.Scan(&location.ID, &location.Name, &location.Area, &location.City, &location.Latitude, &location.Longitude); err != nil {
			return nil, unavailable(err)
		}
		locations = append(locations, location)
	}
	if err := rows.Err(); err != nil {
		return nil, unavailable(err)
	}
	return locations, nil
}

func (r *PostgresRepository) Quote(ctx context.Context, request QuoteRequest, matchRadiusKM int) (pricing.Quote, error) {
	if r.database == nil {
		return pricing.Quote{}, ErrStoreUnavailable
	}
	return quoteWith(ctx, r.database, request, matchRadiusKM)
}

func (r *PostgresRepository) CreateRide(ctx context.Context, riderID string, request QuoteRequest, idempotencyKey string, requestHash []byte, matchRadiusKM int) (Ride, bool, error) {
	if r.database == nil {
		return Ride{}, false, ErrStoreUnavailable
	}
	if len(requestHash) != 32 {
		return Ride{}, false, ErrStoreUnavailable
	}

	tx, err := r.database.Begin(ctx)
	if err != nil {
		return Ride{}, false, unavailable(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	// Serializing this rider's booking attempts makes the idempotency lookup,
	// open-ride check, ride insert, and key insert one race-safe operation.
	var lockedRiderID string
	err = tx.QueryRow(ctx, `
		SELECT id::text
		FROM ride_hailing.users
		WHERE id = $1::uuid AND role = 'rider'
		FOR UPDATE`, riderID).Scan(&lockedRiderID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Ride{}, false, ErrRideNotFound
	}
	if err != nil {
		return Ride{}, false, unavailable(err)
	}

	var existingHash []byte
	var existingRideID string
	err = tx.QueryRow(ctx, `
		SELECT request_hash, ride_id::text
		FROM ride_hailing.idempotency_keys
		WHERE rider_id = $1::uuid AND idempotency_key = $2`, riderID, idempotencyKey).Scan(&existingHash, &existingRideID)
	if err == nil {
		if subtle.ConstantTimeCompare(existingHash, requestHash) != 1 {
			return Ride{}, false, ErrIdempotencyConflict
		}
		ride, readErr := loadRide(ctx, tx, existingRideID, riderID, "rider")
		if readErr != nil {
			return Ride{}, false, readErr
		}
		if err := tx.Commit(ctx); err != nil {
			return Ride{}, false, unavailable(err)
		}
		return *ride, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Ride{}, false, unavailable(err)
	}

	var hasOpenRide bool
	err = tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM ride_hailing.rides
			WHERE rider_id = $1::uuid AND status IN ('requested', 'accepted')
		)`, riderID).Scan(&hasOpenRide)
	if err != nil {
		return Ride{}, false, unavailable(err)
	}
	if hasOpenRide {
		return Ride{}, false, ErrOpenRideConflict
	}

	quote, err := quoteWith(ctx, tx, request, matchRadiusKM)
	if err != nil {
		return Ride{}, false, err
	}
	var rideID string
	err = tx.QueryRow(ctx, `
		INSERT INTO ride_hailing.rides
			(rider_id, pickup_location_id, destination_location_id, category,
			 estimated_fare_paisa, estimated_distance_meters, pickup_eta_minutes,
			 currency, approximate)
		VALUES ($1::uuid, $2::uuid, $3::uuid, $4::ride_hailing.ride_category,
			$5, $6, $7, $8, $9)
		RETURNING id::text`,
		riderID,
		request.PickupLocationID,
		request.DestinationLocationID,
		request.Category,
		quote.EstimatedFarePaisa,
		quote.EstimatedDistanceMeters,
		quote.PickupETAMinutes,
		quote.Currency,
		quote.Approximate,
	).Scan(&rideID)
	if err != nil {
		return Ride{}, false, mapWriteError(err)
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO ride_hailing.idempotency_keys
			(rider_id, idempotency_key, request_hash, ride_id)
		VALUES ($1::uuid, $2, $3, $4::uuid)`, riderID, idempotencyKey, requestHash, rideID)
	if err != nil {
		return Ride{}, false, mapWriteError(err)
	}

	ride, err := loadRide(ctx, tx, rideID, riderID, "rider")
	if err != nil {
		return Ride{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Ride{}, false, unavailable(err)
	}
	return *ride, false, nil
}

func (r *PostgresRepository) CurrentRide(ctx context.Context, riderID string) (*Ride, error) {
	if r.database == nil {
		return nil, ErrStoreUnavailable
	}
	var rideID string
	err := r.database.QueryRow(ctx, `
		SELECT id::text
		FROM ride_hailing.rides
		WHERE rider_id = $1::uuid AND status IN ('requested', 'accepted')
		ORDER BY created_at DESC, id DESC
		LIMIT 1`, riderID).Scan(&rideID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, unavailable(err)
	}
	ride, err := loadRide(ctx, r.database, rideID, riderID, "rider")
	if errors.Is(err, ErrRideNotFound) {
		// The ride may have reached a terminal state between the two reads.
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if ride.Status != "requested" && ride.Status != "accepted" {
		return nil, nil
	}
	return ride, nil
}

func (r *PostgresRepository) GetRide(ctx context.Context, rideID, actorID, actorRole string) (*Ride, error) {
	if r.database == nil {
		return nil, ErrStoreUnavailable
	}
	return loadRide(ctx, r.database, rideID, actorID, actorRole)
}

func quoteWith(ctx context.Context, runner queryRunner, request QuoteRequest, matchRadiusKM int) (pricing.Quote, error) {
	pickup, err := lookupLocation(ctx, runner, request.PickupLocationID)
	if err != nil {
		return pricing.Quote{}, err
	}
	destination, err := lookupLocation(ctx, runner, request.DestinationLocationID)
	if err != nil {
		return pricing.Quote{}, err
	}

	rows, err := runner.Query(ctx, `
		SELECT d.city, dc.category::text, d.demo_latitude, d.demo_longitude
		FROM ride_hailing.driver_profiles d
		JOIN ride_hailing.driver_categories dc ON dc.driver_id = d.user_id
		WHERE d.status = 'available'
			AND dc.category = $1::ride_hailing.ride_category
			AND d.city = 'Lahore'`, request.Category)
	if err != nil {
		return pricing.Quote{}, unavailable(err)
	}
	drivers := make([]pricing.AvailableDriver, 0)
	for rows.Next() {
		var driver pricing.AvailableDriver
		if err := rows.Scan(&driver.City, &driver.Category, &driver.Latitude, &driver.Longitude); err != nil {
			rows.Close()
			return pricing.Quote{}, unavailable(err)
		}
		driver.Available = true
		drivers = append(drivers, driver)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return pricing.Quote{}, unavailable(err)
	}
	rows.Close()

	quote, err := pricing.CalculateQuote(pickup, destination, request.Category, drivers, matchRadiusKM)
	if errors.Is(err, pricing.ErrUnsupportedCategory) {
		return pricing.Quote{}, ValidationError{Message: "Category must be economy or comfort."}
	}
	if err != nil {
		return pricing.Quote{}, ValidationError{Message: "Pickup and destination must be different active Lahore locations."}
	}
	return quote, nil
}

func lookupLocation(ctx context.Context, runner queryRunner, locationID string) (pricing.Location, error) {
	var location pricing.Location
	err := runner.QueryRow(ctx, `
		SELECT id::text, name, area, city, latitude, longitude
		FROM ride_hailing.locations
		WHERE id = $1::uuid AND city = 'Lahore' AND active`, locationID).Scan(
		&location.ID,
		&location.Name,
		&location.Area,
		&location.City,
		&location.Latitude,
		&location.Longitude,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return pricing.Location{}, ErrLocationNotFound
	}
	if err != nil {
		return pricing.Location{}, unavailable(err)
	}
	return location, nil
}

func loadRide(ctx context.Context, runner queryRunner, rideID, actorID, actorRole string) (*Ride, error) {
	var ride Ride
	var pickupETA pgtype.Int4
	var assignmentID, assignmentDriverID, assignmentDriverName, assignmentStatus pgtype.Text
	var assignmentCreatedAt pgtype.Timestamptz
	err := runner.QueryRow(ctx, `
		SELECT r.id::text,
			u.id::text, u.display_name,
			p.id::text, p.name, p.area, p.city, p.latitude, p.longitude,
			d.id::text, d.name, d.area, d.city, d.latitude, d.longitude,
			r.category::text, r.estimated_fare_paisa, r.estimated_distance_meters,
			r.pickup_eta_minutes, r.currency, r.approximate, r.status::text,
			r.created_at, r.updated_at,
			a.id::text, a.driver_id::text, du.display_name, a.status::text, a.assigned_at
		FROM ride_hailing.rides r
		JOIN ride_hailing.users u ON u.id = r.rider_id
		JOIN ride_hailing.locations p ON p.id = r.pickup_location_id
		JOIN ride_hailing.locations d ON d.id = r.destination_location_id
		LEFT JOIN ride_hailing.ride_assignments a ON a.ride_id = r.id
		LEFT JOIN ride_hailing.users du ON du.id = a.driver_id
		WHERE r.id = $1::uuid
			AND (($3 = 'rider' AND r.rider_id = $2::uuid)
				OR ($3 = 'driver' AND a.driver_id = $2::uuid))`, rideID, actorID, actorRole).Scan(
		&ride.ID,
		&ride.Rider.ID,
		&ride.Rider.DisplayName,
		&ride.Pickup.ID,
		&ride.Pickup.Name,
		&ride.Pickup.Area,
		&ride.Pickup.City,
		&ride.Pickup.Latitude,
		&ride.Pickup.Longitude,
		&ride.Destination.ID,
		&ride.Destination.Name,
		&ride.Destination.Area,
		&ride.Destination.City,
		&ride.Destination.Latitude,
		&ride.Destination.Longitude,
		&ride.Category,
		&ride.EstimatedFarePaisa,
		&ride.EstimatedDistanceMeters,
		&pickupETA,
		&ride.Currency,
		&ride.Approximate,
		&ride.Status,
		&ride.CreatedAt,
		&ride.UpdatedAt,
		&assignmentID,
		&assignmentDriverID,
		&assignmentDriverName,
		&assignmentStatus,
		&assignmentCreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrRideNotFound
	}
	if err != nil {
		return nil, unavailable(err)
	}
	if pickupETA.Valid {
		minutes := int(pickupETA.Int32)
		ride.PickupETAMinutes = &minutes
	}
	if assignmentID.Valid {
		ride.Assignment = &Assignment{
			ID:                assignmentID.String,
			DriverID:          assignmentDriverID.String,
			DriverDisplayName: assignmentDriverName.String,
			Status:            assignmentStatus.String,
			AssignedAt:        assignmentCreatedAt.Time.UTC(),
		}
	}
	ride.CreatedAt = ride.CreatedAt.UTC()
	ride.UpdatedAt = ride.UpdatedAt.UTC()
	return &ride, nil
}

func mapWriteError(err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) && postgresError.Code == "23505" {
		switch postgresError.ConstraintName {
		case "rides_one_open_per_rider_uidx":
			return ErrOpenRideConflict
		case "idempotency_keys_pkey":
			return ErrIdempotencyConflict
		}
	}
	return unavailable(err)
}

func unavailable(err error) error {
	if err == nil {
		return ErrStoreUnavailable
	}
	return fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
}
