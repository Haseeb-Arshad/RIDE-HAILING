package rides

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/haseeb-arshad/ride-hailing/apps/api/internal/drivers"
	"github.com/haseeb-arshad/ride-hailing/apps/api/internal/pricing"
)

var (
	ErrLocationNotFound    = errors.New("active Lahore location not found")
	ErrIdempotencyConflict = errors.New("idempotency key was already used for a different request")
	ErrOpenRideConflict    = errors.New("rider already has an open ride")
	ErrRideNotFound        = errors.New("ride not found")
	ErrDriverNotFound      = errors.New("driver profile not found")
	ErrDriverUnavailable   = errors.New("driver is not available")
	ErrStateConflict       = errors.New("ride state conflict")
	ErrStoreUnavailable    = errors.New("ride storage unavailable")
)

type ValidationError struct {
	Message string
}

func (e ValidationError) Error() string { return e.Message }

type QuoteRequest struct {
	PickupLocationID      string         `json:"pickup_location_id,omitempty"`
	DestinationLocationID string         `json:"destination_location_id,omitempty"`
	Pickup                *LocationInput `json:"pickup,omitempty"`
	Destination           *LocationInput `json:"destination,omitempty"`
	Category              string         `json:"category"`
}

type LocationInput struct {
	Name      string  `json:"name"`
	Area      string  `json:"area"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

type RiderSummary struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
}

type Assignment struct {
	ID                string    `json:"id"`
	DriverID          string    `json:"driver_id"`
	DriverDisplayName string    `json:"driver_display_name"`
	Status            string    `json:"status"`
	AssignedAt        time.Time `json:"assigned_at"`
}

type Ride struct {
	ID                      string           `json:"id"`
	Rider                   RiderSummary     `json:"rider"`
	Pickup                  pricing.Location `json:"pickup"`
	Destination             pricing.Location `json:"destination"`
	Category                string           `json:"category"`
	EstimatedFarePaisa      int64            `json:"estimated_fare_paisa"`
	EstimatedDistanceMeters int64            `json:"estimated_distance_meters"`
	PickupETAMinutes        *int             `json:"pickup_eta_minutes"`
	FuelPricePaisaPerLiter  *int64           `json:"fuel_price_paisa_per_liter,omitempty"`
	FuelPriceEffectiveDate  *time.Time       `json:"fuel_price_effective_date,omitempty"`
	FuelCostPaisa           *int64           `json:"fuel_cost_paisa,omitempty"`
	BaseFarePaisa           *int64           `json:"base_fare_paisa,omitempty"`
	DistanceCostPaisa       *int64           `json:"distance_cost_paisa,omitempty"`
	PricingVersion          *int             `json:"pricing_version,omitempty"`
	Currency                string           `json:"currency"`
	Approximate             bool             `json:"approximate"`
	Status                  string           `json:"status"`
	Assignment              *Assignment      `json:"assignment"`
	CreatedAt               time.Time        `json:"created_at"`
	UpdatedAt               time.Time        `json:"updated_at"`
}

type DriverRequest struct {
	ID                      string           `json:"id"`
	Pickup                  pricing.Location `json:"pickup"`
	Destination             pricing.Location `json:"destination"`
	Category                string           `json:"category"`
	EstimatedFarePaisa      int64            `json:"estimated_fare_paisa"`
	EstimatedDistanceMeters int64            `json:"estimated_distance_meters"`
	Currency                string           `json:"currency"`
	Status                  string           `json:"status"`
	CreatedAt               time.Time        `json:"created_at"`
	PickupDistanceMeters    int64            `json:"pickup_distance_meters"`
}

type DriverRequestPage struct {
	Requests   []DriverRequest `json:"requests"`
	NextCursor *string         `json:"next_cursor"`
}

type AssignmentView struct {
	Ride *Ride `json:"ride"`
}

type RequestCursor struct {
	CreatedAt time.Time
	RideID    string
}

type Repository interface {
	ListLocations(context.Context) ([]pricing.Location, error)
	Quote(context.Context, QuoteRequest, int) (pricing.Quote, error)
	CreateRide(context.Context, string, QuoteRequest, string, []byte, int) (Ride, bool, error)
	CurrentRide(context.Context, string) (*Ride, error)
	GetRide(context.Context, string, string, string) (*Ride, error)
	DriverAvailability(context.Context, string) (drivers.Availability, error)
	SetAvailability(context.Context, string, drivers.Availability) (drivers.Availability, error)
	ListDriverRequests(context.Context, string, int, *RequestCursor, int) (DriverRequestPage, error)
	CurrentAssignment(context.Context, string) (*AssignmentView, error)
	AcceptRide(context.Context, string, string, int) (Ride, error)
	CancelRide(context.Context, string, string) (Ride, error)
	CompleteRide(context.Context, string, string) (Ride, error)
}

type Service struct {
	repository    Repository
	matchRadiusKM int
}

func NewService(repository Repository, matchRadiusKM int) *Service {
	if matchRadiusKM < 1 {
		matchRadiusKM = pricing.DefaultMatchRadiusKM
	}
	return &Service{repository: repository, matchRadiusKM: matchRadiusKM}
}

func (s *Service) ListLocations(ctx context.Context) ([]pricing.Location, error) {
	locations, err := s.repository.ListLocations(ctx)
	if err != nil {
		return nil, err
	}
	if locations == nil {
		locations = []pricing.Location{}
	}
	return locations, nil
}

func (s *Service) Quote(ctx context.Context, rawRequest QuoteRequest) (pricing.Quote, error) {
	request, err := normalizeRequest(rawRequest)
	if err != nil {
		return pricing.Quote{}, err
	}
	return s.repository.Quote(ctx, request, s.matchRadiusKM)
}

func (s *Service) CreateRide(ctx context.Context, riderID string, rawRequest QuoteRequest, idempotencyKey string) (Ride, bool, error) {
	request, err := normalizeRequest(rawRequest)
	if err != nil {
		return Ride{}, false, err
	}
	if !validIdempotencyKey(idempotencyKey) {
		return Ride{}, false, ValidationError{Message: "Idempotency-Key must contain 1 to 128 visible ASCII characters."}
	}
	var canonicalRequest []byte
	if request.Pickup == nil && request.Destination == nil {
		// Preserve the fingerprint format used by existing pending demo attempts.
		canonicalRequest = []byte(request.PickupLocationID + "\n" + request.DestinationLocationID + "\n" + request.Category)
	} else {
		canonicalRequest, err = json.Marshal(request)
		if err != nil {
			return Ride{}, false, ErrStoreUnavailable
		}
	}
	requestHash := sha256.Sum256(canonicalRequest)
	ride, replay, err := s.repository.CreateRide(ctx, riderID, request, idempotencyKey, requestHash[:], s.matchRadiusKM)
	if err != nil {
		return Ride{}, false, err
	}
	return ride, replay, nil
}

func (s *Service) CurrentRide(ctx context.Context, riderID string) (*Ride, error) {
	return s.repository.CurrentRide(ctx, riderID)
}

func (s *Service) GetRide(ctx context.Context, rideID, actorID, actorRole string) (*Ride, error) {
	normalizedID, valid := normalizeUUID(rideID)
	if !valid {
		return nil, ErrRideNotFound
	}
	if actorRole != "rider" && actorRole != "driver" {
		return nil, ErrRideNotFound
	}
	return s.repository.GetRide(ctx, normalizedID, actorID, actorRole)
}

func (s *Service) SetAvailability(ctx context.Context, driverID, value string) (drivers.Availability, error) {
	status, err := drivers.ParseAvailability(value)
	if err != nil {
		return "", ValidationError{Message: err.Error() + "."}
	}
	return s.repository.SetAvailability(ctx, driverID, status)
}

func (s *Service) DriverAvailability(ctx context.Context, driverID string) (drivers.Availability, error) {
	return s.repository.DriverAvailability(ctx, driverID)
}

func (s *Service) ListDriverRequests(ctx context.Context, driverID string, limit int, cursorValue string) (DriverRequestPage, error) {
	if limit == 0 {
		limit = 25
	}
	if limit < 1 || limit > 50 {
		return DriverRequestPage{}, ValidationError{Message: "Limit must be between 1 and 50."}
	}
	cursor, err := decodeRequestCursor(cursorValue)
	if err != nil {
		return DriverRequestPage{}, ValidationError{Message: "Cursor is invalid."}
	}
	return s.repository.ListDriverRequests(ctx, driverID, limit, cursor, s.matchRadiusKM)
}

func (s *Service) CurrentAssignment(ctx context.Context, driverID string) (*AssignmentView, error) {
	return s.repository.CurrentAssignment(ctx, driverID)
}

func (s *Service) AcceptRide(ctx context.Context, rideID, driverID string) (Ride, error) {
	normalizedID, valid := normalizeUUID(rideID)
	if !valid {
		return Ride{}, ErrRideNotFound
	}
	return s.repository.AcceptRide(ctx, normalizedID, driverID, s.matchRadiusKM)
}

func (s *Service) CancelRide(ctx context.Context, rideID, riderID string) (Ride, error) {
	normalizedID, valid := normalizeUUID(rideID)
	if !valid {
		return Ride{}, ErrRideNotFound
	}
	return s.repository.CancelRide(ctx, normalizedID, riderID)
}

func (s *Service) CompleteRide(ctx context.Context, rideID, driverID string) (Ride, error) {
	normalizedID, valid := normalizeUUID(rideID)
	if !valid {
		return Ride{}, ErrRideNotFound
	}
	return s.repository.CompleteRide(ctx, normalizedID, driverID)
}

func normalizeRequest(request QuoteRequest) (QuoteRequest, error) {
	pickupID, pickup, pickupValid := normalizeEndpoint(request.PickupLocationID, request.Pickup)
	destinationID, destination, destinationValid := normalizeEndpoint(request.DestinationLocationID, request.Destination)
	if !pickupValid || !destinationValid {
		return QuoteRequest{}, ValidationError{Message: "Choose a saved Lahore stop or provide a labeled map pin inside the Lahore service area for each end of the trip."}
	}
	if pickupID != "" && pickupID == destinationID {
		return QuoteRequest{}, ValidationError{Message: "Pickup and destination must be different locations."}
	}
	if pickup != nil && destination != nil && math.Abs(pickup.Latitude-destination.Latitude) < 0.000001 && math.Abs(pickup.Longitude-destination.Longitude) < 0.000001 {
		return QuoteRequest{}, ValidationError{Message: "Pickup and destination must be different locations."}
	}
	if request.Category != "economy" && request.Category != "comfort" {
		return QuoteRequest{}, ValidationError{Message: "Category must be economy or comfort."}
	}
	return QuoteRequest{PickupLocationID: pickupID, DestinationLocationID: destinationID, Pickup: pickup, Destination: destination, Category: request.Category}, nil
}

func normalizeEndpoint(locationID string, input *LocationInput) (string, *LocationInput, bool) {
	if locationID != "" {
		if input != nil {
			return "", nil, false
		}
		normalizedID, valid := normalizeUUID(locationID)
		return normalizedID, nil, valid
	}
	if input == nil || !pricing.IsInLahoreServiceArea(input.Latitude, input.Longitude) {
		return "", nil, false
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		name = "Map pin"
	}
	area := strings.TrimSpace(input.Area)
	if area == "" {
		area = "Lahore"
	}
	if len([]rune(name)) > 120 || len([]rune(area)) > 120 || strings.ContainsRune(name, '\u0000') || strings.ContainsRune(area, '\u0000') {
		return "", nil, false
	}
	return "", &LocationInput{
		Name:      name,
		Area:      area,
		Latitude:  math.Round(input.Latitude*1_000_000) / 1_000_000,
		Longitude: math.Round(input.Longitude*1_000_000) / 1_000_000,
	}, true
}

func normalizeUUID(value string) (string, bool) {
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		return "", false
	}
	compact := strings.ReplaceAll(value, "-", "")
	if len(compact) != 32 {
		return "", false
	}
	if _, err := hex.DecodeString(compact); err != nil {
		return "", false
	}
	return strings.ToLower(value), true
}

func validIdempotencyKey(value string) bool {
	if len(value) < 1 || len(value) > 128 {
		return false
	}
	for _, character := range []byte(value) {
		if character < 0x21 || character > 0x7e {
			return false
		}
	}
	return true
}

func encodeRequestCursor(createdAt time.Time, rideID string) string {
	value := createdAt.UTC().Format(time.RFC3339Nano) + "\n" + rideID
	return base64.RawURLEncoding.EncodeToString([]byte(value))
}

func decodeRequestCursor(value string) (*RequestCursor, error) {
	if value == "" {
		return nil, nil
	}
	if len(value) > 512 {
		return nil, errors.New("cursor too long")
	}
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return nil, err
	}
	parts := strings.Split(string(decoded), "\n")
	if len(parts) != 2 {
		return nil, errors.New("cursor shape is invalid")
	}
	createdAt, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return nil, err
	}
	rideID, valid := normalizeUUID(parts[1])
	if !valid {
		return nil, errors.New("cursor ride ID is invalid")
	}
	return &RequestCursor{CreatedAt: createdAt.UTC(), RideID: rideID}, nil
}

func ParseRequestLimit(value string) (int, error) {
	if value == "" {
		return 25, nil
	}
	limit, err := strconv.Atoi(value)
	if err != nil || limit < 1 || limit > 50 {
		return 0, ValidationError{Message: "Limit must be an integer between 1 and 50."}
	}
	return limit, nil
}

func encodeCursorForRequest(request DriverRequest) string {
	return encodeRequestCursor(request.CreatedAt, request.ID)
}
