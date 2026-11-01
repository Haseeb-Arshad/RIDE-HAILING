package rides

import (
	"context"
	"errors"

	"github.com/haseeb-arshad/ride-hailing/apps/api/internal/drivers"
	"github.com/haseeb-arshad/ride-hailing/apps/api/internal/matching"
	"github.com/jackc/pgx/v5"
)

func (r *PostgresRepository) DriverAvailability(ctx context.Context, driverID string) (drivers.Availability, error) {
	if r.database == nil {
		return "", ErrStoreUnavailable
	}
	var status string
	err := r.database.QueryRow(ctx, `
		SELECT status::text
		FROM ride_hailing.driver_profiles
		WHERE user_id = $1::uuid`, driverID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrDriverNotFound
	}
	if err != nil {
		return "", unavailable(err)
	}
	availability := drivers.Availability(status)
	if availability != drivers.Offline && availability != drivers.Available && availability != drivers.Busy {
		return "", ErrStoreUnavailable
	}
	return availability, nil
}

func (r *PostgresRepository) SetAvailability(ctx context.Context, driverID string, status drivers.Availability) (drivers.Availability, error) {
	if r.database == nil {
		return "", ErrStoreUnavailable
	}
	tx, err := r.database.Begin(ctx)
	if err != nil {
		return "", unavailable(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	var currentStatus string
	err = tx.QueryRow(ctx, `
		SELECT status::text
		FROM ride_hailing.driver_profiles
		WHERE user_id = $1::uuid
		FOR UPDATE`, driverID).Scan(&currentStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrDriverNotFound
	}
	if err != nil {
		return "", unavailable(err)
	}
	if currentStatus == string(drivers.Busy) {
		return "", ErrDriverUnavailable
	}

	var hasActiveAssignment bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM ride_hailing.ride_assignments
			WHERE driver_id = $1::uuid AND status = 'active'
		)`, driverID).Scan(&hasActiveAssignment); err != nil {
		return "", unavailable(err)
	}
	if hasActiveAssignment {
		return "", ErrDriverUnavailable
	}
	if currentStatus != string(status) {
		if _, err := tx.Exec(ctx, `
			UPDATE ride_hailing.driver_profiles
			SET status = $2::ride_hailing.driver_status, updated_at = now()
			WHERE user_id = $1::uuid`, driverID, string(status)); err != nil {
			return "", unavailable(err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return "", unavailable(err)
	}
	return status, nil
}

func (r *PostgresRepository) ListDriverRequests(ctx context.Context, driverID string, limit int, cursor *RequestCursor, matchRadiusKM int) (DriverRequestPage, error) {
	if r.database == nil {
		return DriverRequestPage{}, ErrStoreUnavailable
	}
	var driver matching.Driver
	err := r.database.QueryRow(ctx, `
		SELECT city, status::text, demo_latitude, demo_longitude
		FROM ride_hailing.driver_profiles
		WHERE user_id = $1::uuid`, driverID).Scan(&driver.City, &driver.Status, &driver.Latitude, &driver.Longitude)
	if errors.Is(err, pgx.ErrNoRows) {
		return DriverRequestPage{}, ErrDriverNotFound
	}
	if err != nil {
		return DriverRequestPage{}, unavailable(err)
	}
	if driver.Status != string(drivers.Available) {
		return DriverRequestPage{}, ErrDriverUnavailable
	}
	driver.Categories = make(map[string]struct{})
	categoryRows, err := r.database.Query(ctx, `
		SELECT category::text
		FROM ride_hailing.driver_categories
		WHERE driver_id = $1::uuid`, driverID)
	if err != nil {
		return DriverRequestPage{}, unavailable(err)
	}
	for categoryRows.Next() {
		var category string
		if err := categoryRows.Scan(&category); err != nil {
			categoryRows.Close()
			return DriverRequestPage{}, unavailable(err)
		}
		driver.Categories[category] = struct{}{}
	}
	if err := categoryRows.Err(); err != nil {
		categoryRows.Close()
		return DriverRequestPage{}, unavailable(err)
	}
	categoryRows.Close()

	var cursorTime, cursorRideID any
	if cursor != nil {
		cursorTime = cursor.CreatedAt
		cursorRideID = cursor.RideID
	}
	rows, err := r.database.Query(ctx, `
		SELECT r.id::text,
			p.id::text, p.name, p.area, p.city, p.latitude, p.longitude,
			d.id::text, d.name, d.area, d.city, d.latitude, d.longitude,
			r.category::text, r.estimated_fare_paisa, r.estimated_distance_meters,
			r.currency, r.status::text, r.created_at
		FROM ride_hailing.rides r
		JOIN ride_hailing.locations p ON p.id = r.pickup_location_id
		JOIN ride_hailing.locations d ON d.id = r.destination_location_id
		WHERE r.status = 'requested'
			AND p.city = $1
			AND ( $2::timestamptz IS NULL OR (r.created_at, r.id) > ($2::timestamptz, $3::uuid) )
			AND EXISTS (
				SELECT 1 FROM ride_hailing.driver_categories dc
				WHERE dc.driver_id = $4::uuid AND dc.category = r.category
			)
		ORDER BY r.created_at, r.id`, driver.City, cursorTime, cursorRideID, driverID)
	if err != nil {
		return DriverRequestPage{}, unavailable(err)
	}
	requests := make([]DriverRequest, 0)
	for rows.Next() {
		var request DriverRequest
		var pickupLatitude, pickupLongitude float64
		if err := rows.Scan(
			&request.ID,
			&request.Pickup.ID,
			&request.Pickup.Name,
			&request.Pickup.Area,
			&request.Pickup.City,
			&pickupLatitude,
			&pickupLongitude,
			&request.Destination.ID,
			&request.Destination.Name,
			&request.Destination.Area,
			&request.Destination.City,
			&request.Destination.Latitude,
			&request.Destination.Longitude,
			&request.Category,
			&request.EstimatedFarePaisa,
			&request.EstimatedDistanceMeters,
			&request.Currency,
			&request.Status,
			&request.CreatedAt,
		); err != nil {
			rows.Close()
			return DriverRequestPage{}, unavailable(err)
		}
		if !matching.Eligible(driver, request.Category, request.Pickup.City, pickupLatitude, pickupLongitude, matchRadiusKM) {
			continue
		}
		request.PickupDistanceMeters = matching.PickupDistanceMeters(driver, pickupLatitude, pickupLongitude)
		request.CreatedAt = request.CreatedAt.UTC()
		requests = append(requests, request)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return DriverRequestPage{}, unavailable(err)
	}
	rows.Close()

	page := DriverRequestPage{Requests: requests}
	if len(page.Requests) > limit {
		lastOnPage := page.Requests[limit-1]
		nextCursor := encodeCursorForRequest(lastOnPage)
		page.NextCursor = &nextCursor
		page.Requests = page.Requests[:limit]
	}
	return page, nil
}

func (r *PostgresRepository) CurrentAssignment(ctx context.Context, driverID string) (*AssignmentView, error) {
	if r.database == nil {
		return nil, ErrStoreUnavailable
	}
	var rideID string
	err := r.database.QueryRow(ctx, `
		SELECT ride_id::text
		FROM ride_hailing.ride_assignments
		WHERE driver_id = $1::uuid AND status = 'active'`, driverID).Scan(&rideID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, unavailable(err)
	}
	ride, err := loadRide(ctx, r.database, rideID, driverID, "driver")
	if err != nil {
		return nil, err
	}
	return &AssignmentView{Ride: ride}, nil
}

type lockedRide struct {
	RiderID         string
	Status          string
	Category        string
	PickupCity      string
	PickupLatitude  float64
	PickupLongitude float64
}

func lockRide(ctx context.Context, tx pgx.Tx, rideID string) (lockedRide, error) {
	var ride lockedRide
	err := tx.QueryRow(ctx, `
		SELECT r.rider_id::text, r.status::text, r.category::text,
			p.city, p.latitude, p.longitude
		FROM ride_hailing.rides r
		JOIN ride_hailing.locations p ON p.id = r.pickup_location_id
		WHERE r.id = $1::uuid
		FOR UPDATE OF r`, rideID).Scan(
		&ride.RiderID,
		&ride.Status,
		&ride.Category,
		&ride.PickupCity,
		&ride.PickupLatitude,
		&ride.PickupLongitude,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return lockedRide{}, ErrRideNotFound
	}
	if err != nil {
		return lockedRide{}, unavailable(err)
	}
	return ride, nil
}

type lockedAssignment struct {
	DriverID string
	Status   string
}

func findAssignment(ctx context.Context, runner queryRunner, rideID string) (lockedAssignment, error) {
	var assignment lockedAssignment
	err := runner.QueryRow(ctx, `
		SELECT driver_id::text, status::text
		FROM ride_hailing.ride_assignments
		WHERE ride_id = $1::uuid`, rideID).Scan(&assignment.DriverID, &assignment.Status)
	if errors.Is(err, pgx.ErrNoRows) {
		return lockedAssignment{}, nil
	}
	if err != nil {
		return lockedAssignment{}, unavailable(err)
	}
	return assignment, nil
}

func (r *PostgresRepository) AcceptRide(ctx context.Context, rideID, driverID string, matchRadiusKM int) (Ride, error) {
	if r.database == nil {
		return Ride{}, ErrStoreUnavailable
	}
	tx, err := r.database.Begin(ctx)
	if err != nil {
		return Ride{}, unavailable(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	ride, err := lockRide(ctx, tx, rideID)
	if err != nil {
		return Ride{}, err
	}
	if ride.Status == "accepted" {
		assignment, err := findAssignment(ctx, tx, rideID)
		if err != nil {
			return Ride{}, err
		}
		if assignment.DriverID != driverID || assignment.Status != "active" {
			return Ride{}, ErrStateConflict
		}
		current, err := loadRide(ctx, tx, rideID, driverID, "driver")
		if err != nil {
			return Ride{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return Ride{}, unavailable(err)
		}
		return *current, nil
	}
	if ride.Status != "requested" {
		return Ride{}, ErrStateConflict
	}

	var driver matching.Driver
	err = tx.QueryRow(ctx, `
		SELECT city, status::text, demo_latitude, demo_longitude
		FROM ride_hailing.driver_profiles
		WHERE user_id = $1::uuid
		FOR UPDATE`, driverID).Scan(&driver.City, &driver.Status, &driver.Latitude, &driver.Longitude)
	if errors.Is(err, pgx.ErrNoRows) {
		return Ride{}, ErrDriverNotFound
	}
	if err != nil {
		return Ride{}, unavailable(err)
	}
	driver.Categories = make(map[string]struct{})
	categoryRows, err := tx.Query(ctx, `
		SELECT category::text
		FROM ride_hailing.driver_categories
		WHERE driver_id = $1::uuid`, driverID)
	if err != nil {
		return Ride{}, unavailable(err)
	}
	for categoryRows.Next() {
		var category string
		if err := categoryRows.Scan(&category); err != nil {
			categoryRows.Close()
			return Ride{}, unavailable(err)
		}
		driver.Categories[category] = struct{}{}
	}
	if err := categoryRows.Err(); err != nil {
		categoryRows.Close()
		return Ride{}, unavailable(err)
	}
	categoryRows.Close()
	if !matching.Eligible(driver, ride.Category, ride.PickupCity, ride.PickupLatitude, ride.PickupLongitude, matchRadiusKM) {
		return Ride{}, ErrDriverUnavailable
	}
	var hasActiveAssignment bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM ride_hailing.ride_assignments
			WHERE driver_id = $1::uuid AND status = 'active'
		)`, driverID).Scan(&hasActiveAssignment); err != nil {
		return Ride{}, unavailable(err)
	}
	if hasActiveAssignment {
		return Ride{}, ErrDriverUnavailable
	}
	assignmentTag, err := tx.Exec(ctx, `
		INSERT INTO ride_hailing.ride_assignments (ride_id, driver_id, status)
		VALUES ($1::uuid, $2::uuid, 'active')`, rideID, driverID)
	if err != nil {
		return Ride{}, mapWriteError(err)
	}
	if assignmentTag.RowsAffected() != 1 {
		return Ride{}, ErrStoreUnavailable
	}
	driverTag, err := tx.Exec(ctx, `
		UPDATE ride_hailing.driver_profiles
		SET status = 'busy', updated_at = now()
		WHERE user_id = $1::uuid`, driverID)
	if err != nil {
		return Ride{}, unavailable(err)
	}
	if driverTag.RowsAffected() != 1 {
		return Ride{}, ErrStoreUnavailable
	}
	rideTag, err := tx.Exec(ctx, `
		UPDATE ride_hailing.rides
		SET status = 'accepted', accepted_at = now(), updated_at = now()
		WHERE id = $1::uuid`, rideID)
	if err != nil {
		return Ride{}, unavailable(err)
	}
	if rideTag.RowsAffected() != 1 {
		return Ride{}, ErrStoreUnavailable
	}
	accepted, err := loadRide(ctx, tx, rideID, driverID, "driver")
	if err != nil {
		return Ride{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Ride{}, unavailable(err)
	}
	return *accepted, nil
}

func (r *PostgresRepository) CancelRide(ctx context.Context, rideID, riderID string) (Ride, error) {
	if r.database == nil {
		return Ride{}, ErrStoreUnavailable
	}
	tx, err := r.database.Begin(ctx)
	if err != nil {
		return Ride{}, unavailable(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	ride, err := lockRide(ctx, tx, rideID)
	if err != nil {
		return Ride{}, err
	}
	if ride.RiderID != riderID {
		return Ride{}, ErrRideNotFound
	}
	if ride.Status == "cancelled" {
		current, err := loadRide(ctx, tx, rideID, riderID, "rider")
		if err != nil {
			return Ride{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return Ride{}, unavailable(err)
		}
		return *current, nil
	}
	if ride.Status != "requested" && ride.Status != "accepted" {
		return Ride{}, ErrStateConflict
	}
	if ride.Status == "accepted" {
		assignment, err := findAssignment(ctx, tx, rideID)
		if err != nil {
			return Ride{}, err
		}
		if assignment.DriverID == "" || assignment.Status != "active" {
			return Ride{}, ErrStoreUnavailable
		}
		if err := lockDriver(ctx, tx, assignment.DriverID); err != nil {
			return Ride{}, err
		}
		assignmentTag, err := tx.Exec(ctx, `
			UPDATE ride_hailing.ride_assignments
			SET status = 'cancelled', ended_at = now()
			WHERE ride_id = $1::uuid AND status = 'active'`, rideID)
		if err != nil {
			return Ride{}, unavailable(err)
		}
		if assignmentTag.RowsAffected() != 1 {
			return Ride{}, ErrStoreUnavailable
		}
		driverTag, err := tx.Exec(ctx, `
			UPDATE ride_hailing.driver_profiles
			SET status = 'available', updated_at = now()
			WHERE user_id = $1::uuid AND status = 'busy'`, assignment.DriverID)
		if err != nil {
			return Ride{}, unavailable(err)
		}
		if driverTag.RowsAffected() != 1 {
			return Ride{}, ErrStoreUnavailable
		}
	}
	rideTag, err := tx.Exec(ctx, `
		UPDATE ride_hailing.rides
		SET status = 'cancelled', cancelled_at = now(), updated_at = now()
		WHERE id = $1::uuid`, rideID)
	if err != nil {
		return Ride{}, unavailable(err)
	}
	if rideTag.RowsAffected() != 1 {
		return Ride{}, ErrStoreUnavailable
	}
	cancelled, err := loadRide(ctx, tx, rideID, riderID, "rider")
	if err != nil {
		return Ride{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Ride{}, unavailable(err)
	}
	return *cancelled, nil
}

func (r *PostgresRepository) CompleteRide(ctx context.Context, rideID, driverID string) (Ride, error) {
	if r.database == nil {
		return Ride{}, ErrStoreUnavailable
	}
	tx, err := r.database.Begin(ctx)
	if err != nil {
		return Ride{}, unavailable(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	ride, err := lockRide(ctx, tx, rideID)
	if err != nil {
		return Ride{}, err
	}
	assignment, err := findAssignment(ctx, tx, rideID)
	if err != nil {
		return Ride{}, err
	}
	if assignment.DriverID == "" || assignment.DriverID != driverID {
		return Ride{}, ErrRideNotFound
	}
	if ride.Status == "completed" && assignment.Status == "completed" {
		current, err := loadRide(ctx, tx, rideID, driverID, "driver")
		if err != nil {
			return Ride{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return Ride{}, unavailable(err)
		}
		return *current, nil
	}
	if ride.Status != "accepted" || assignment.Status != "active" {
		return Ride{}, ErrStateConflict
	}
	if err := lockDriver(ctx, tx, driverID); err != nil {
		return Ride{}, err
	}
	assignmentTag, err := tx.Exec(ctx, `
		UPDATE ride_hailing.ride_assignments
		SET status = 'completed', ended_at = now()
		WHERE ride_id = $1::uuid AND status = 'active'`, rideID)
	if err != nil {
		return Ride{}, unavailable(err)
	}
	if assignmentTag.RowsAffected() != 1 {
		return Ride{}, ErrStoreUnavailable
	}
	driverTag, err := tx.Exec(ctx, `
		UPDATE ride_hailing.driver_profiles
		SET status = 'available', updated_at = now()
		WHERE user_id = $1::uuid AND status = 'busy'`, driverID)
	if err != nil {
		return Ride{}, unavailable(err)
	}
	if driverTag.RowsAffected() != 1 {
		return Ride{}, ErrStoreUnavailable
	}
	rideTag, err := tx.Exec(ctx, `
		UPDATE ride_hailing.rides
		SET status = 'completed', completed_at = now(), updated_at = now()
		WHERE id = $1::uuid`, rideID)
	if err != nil {
		return Ride{}, unavailable(err)
	}
	if rideTag.RowsAffected() != 1 {
		return Ride{}, ErrStoreUnavailable
	}
	completed, err := loadRide(ctx, tx, rideID, driverID, "driver")
	if err != nil {
		return Ride{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Ride{}, unavailable(err)
	}
	return *completed, nil
}

func lockDriver(ctx context.Context, tx pgx.Tx, driverID string) error {
	var lockedID string
	err := tx.QueryRow(ctx, `
		SELECT user_id::text
		FROM ride_hailing.driver_profiles
		WHERE user_id = $1::uuid
		FOR UPDATE`, driverID).Scan(&lockedID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrStoreUnavailable
	}
	if err != nil {
		return unavailable(err)
	}
	return nil
}
