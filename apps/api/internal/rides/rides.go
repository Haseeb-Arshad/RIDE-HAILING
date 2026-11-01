package rides

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/haseeb-arshad/ride-hailing/apps/api/internal/pricing"
)

var (
	ErrLocationNotFound    = errors.New("active Lahore location not found")
	ErrIdempotencyConflict = errors.New("idempotency key was already used for a different request")
	ErrOpenRideConflict    = errors.New("rider already has an open ride")
	ErrRideNotFound        = errors.New("ride not found")
	ErrStoreUnavailable    = errors.New("ride storage unavailable")
)

type ValidationError struct {
	Message string
}

func (e ValidationError) Error() string { return e.Message }

type QuoteRequest struct {
	PickupLocationID      string `json:"pickup_location_id"`
	DestinationLocationID string `json:"destination_location_id"`
	Category              string `json:"category"`
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
	Currency                string           `json:"currency"`
	Approximate             bool             `json:"approximate"`
	Status                  string           `json:"status"`
	Assignment              *Assignment      `json:"assignment"`
	CreatedAt               time.Time        `json:"created_at"`
	UpdatedAt               time.Time        `json:"updated_at"`
}

type Repository interface {
	ListLocations(context.Context) ([]pricing.Location, error)
	Quote(context.Context, QuoteRequest, int) (pricing.Quote, error)
	CreateRide(context.Context, string, QuoteRequest, string, []byte, int) (Ride, bool, error)
	CurrentRide(context.Context, string) (*Ride, error)
	GetRide(context.Context, string, string, string) (*Ride, error)
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
	canonicalRequest := request.PickupLocationID + "\n" + request.DestinationLocationID + "\n" + request.Category
	requestHash := sha256.Sum256([]byte(canonicalRequest))
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

func normalizeRequest(request QuoteRequest) (QuoteRequest, error) {
	pickupID, pickupValid := normalizeUUID(request.PickupLocationID)
	destinationID, destinationValid := normalizeUUID(request.DestinationLocationID)
	if !pickupValid || !destinationValid {
		return QuoteRequest{}, ValidationError{Message: "Pickup and destination must be valid location IDs."}
	}
	if pickupID == destinationID {
		return QuoteRequest{}, ValidationError{Message: "Pickup and destination must be different locations."}
	}
	if request.Category != "economy" && request.Category != "comfort" {
		return QuoteRequest{}, ValidationError{Message: "Category must be economy or comfort."}
	}
	return QuoteRequest{PickupLocationID: pickupID, DestinationLocationID: destinationID, Category: request.Category}, nil
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
