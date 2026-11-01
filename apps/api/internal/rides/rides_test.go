package rides

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/haseeb-arshad/ride-hailing/apps/api/internal/drivers"
	"github.com/haseeb-arshad/ride-hailing/apps/api/internal/pricing"
)

const (
	testPickupID      = "10000000-0000-4000-8000-000000000001"
	testDestinationID = "10000000-0000-4000-8000-000000000002"
)

func TestQuoteNormalizesUUIDsAndUsesConfiguredRadius(t *testing.T) {
	repository := &fakeRepository{}
	service := NewService(repository, 12)
	request := QuoteRequest{
		PickupLocationID:      "10000000-0000-4000-8000-00000000000A",
		DestinationLocationID: testDestinationID,
		Category:              "comfort",
	}
	if _, err := service.Quote(context.Background(), request); err != nil {
		t.Fatalf("quote: %v", err)
	}
	if repository.quoteRequest.PickupLocationID != "10000000-0000-4000-8000-00000000000a" {
		t.Fatalf("pickup ID was not normalized: %s", repository.quoteRequest.PickupLocationID)
	}
	if repository.quoteRadiusKM != 12 {
		t.Fatalf("quote radius = %d km, want configured 12 km", repository.quoteRadiusKM)
	}
}

func TestCreateRideValidatesAndFingerprintsCanonicalRequest(t *testing.T) {
	repository := &fakeRepository{}
	service := NewService(repository, pricing.DefaultMatchRadiusKM)
	request := QuoteRequest{
		PickupLocationID:      "10000000-0000-4000-8000-00000000000A",
		DestinationLocationID: testDestinationID,
		Category:              "economy",
	}
	if _, _, err := service.CreateRide(context.Background(), "rider-id", request, "request-1"); err != nil {
		t.Fatalf("create ride: %v", err)
	}
	if repository.createRequest.PickupLocationID != "10000000-0000-4000-8000-00000000000a" {
		t.Fatalf("created request was not canonicalized: %+v", repository.createRequest)
	}
	if repository.createKey != "request-1" || len(repository.createHash) != 32 {
		t.Fatalf("unexpected idempotency inputs: key=%q hash_length=%d", repository.createKey, len(repository.createHash))
	}

	_, _, err := service.CreateRide(context.Background(), "rider-id", request, "contains a space")
	var validationError ValidationError
	if !errors.As(err, &validationError) {
		t.Fatalf("invalid key error = %v, want ValidationError", err)
	}
}

func TestQuoteRejectsMalformedOrUnsupportedInputBeforeStorage(t *testing.T) {
	repository := &fakeRepository{}
	service := NewService(repository, pricing.DefaultMatchRadiusKM)
	invalidRequests := []QuoteRequest{
		{PickupLocationID: "bad-id", DestinationLocationID: testDestinationID, Category: "economy"},
		{PickupLocationID: testPickupID, DestinationLocationID: testPickupID, Category: "economy"},
		{PickupLocationID: testPickupID, DestinationLocationID: testDestinationID, Category: "Economy"},
	}
	for _, request := range invalidRequests {
		if _, err := service.Quote(context.Background(), request); err == nil {
			t.Errorf("expected invalid request to fail: %+v", request)
		}
	}
	if repository.quoteCalls != 0 {
		t.Fatalf("invalid requests reached repository %d times", repository.quoteCalls)
	}
}

func TestRideReadRejectsMalformedIDsWithoutStorage(t *testing.T) {
	repository := &fakeRepository{}
	service := NewService(repository, pricing.DefaultMatchRadiusKM)
	if _, err := service.GetRide(context.Background(), "not-a-uuid", "actor", "rider"); !errors.Is(err, ErrRideNotFound) {
		t.Fatalf("malformed ride ID error = %v, want concealed not found", err)
	}
	if repository.getRideCalls != 0 {
		t.Fatalf("malformed ID reached repository %d times", repository.getRideCalls)
	}
}

func TestRequestCursorRoundTripsAndRejectsMalformedValues(t *testing.T) {
	createdAt := time.Date(2026, 11, 1, 12, 30, 0, 123_000_000, time.FixedZone("PKT", 5*60*60))
	rideID := "40000000-0000-4000-8000-000000000001"
	encoded := encodeRequestCursor(createdAt, rideID)
	cursor, err := decodeRequestCursor(encoded)
	if err != nil {
		t.Fatalf("decode encoded request cursor: %v", err)
	}
	if cursor.RideID != rideID || !cursor.CreatedAt.Equal(createdAt) || cursor.CreatedAt.Location() != time.UTC {
		t.Fatalf("decoded cursor = %+v, want UTC timestamp and ride ID %s", cursor, rideID)
	}
	if _, err := decodeRequestCursor("not-base64"); err == nil {
		t.Fatal("malformed cursor decoded successfully")
	}
	tooLong := make([]byte, 513)
	for index := range tooLong {
		tooLong[index] = 'a'
	}
	if _, err := decodeRequestCursor(string(tooLong)); err == nil {
		t.Fatal("oversized cursor decoded successfully")
	}
}

func TestParseRequestLimit(t *testing.T) {
	for _, test := range []struct {
		value   string
		want    int
		wantErr bool
	}{
		{value: "", want: 25},
		{value: "1", want: 1},
		{value: "50", want: 50},
		{value: "0", wantErr: true},
		{value: "51", wantErr: true},
		{value: "many", wantErr: true},
	} {
		got, err := ParseRequestLimit(test.value)
		if test.wantErr {
			if err == nil {
				t.Errorf("ParseRequestLimit(%q) succeeded, want validation error", test.value)
			}
			continue
		}
		if err != nil || got != test.want {
			t.Errorf("ParseRequestLimit(%q) = %d, %v; want %d", test.value, got, err, test.want)
		}
	}
}

type fakeRepository struct {
	quoteRequest  QuoteRequest
	quoteRadiusKM int
	quoteCalls    int
	createRequest QuoteRequest
	createKey     string
	createHash    []byte
	getRideCalls  int
}

func (f *fakeRepository) ListLocations(context.Context) ([]pricing.Location, error) {
	return []pricing.Location{}, nil
}

func (f *fakeRepository) Quote(_ context.Context, request QuoteRequest, radiusKM int) (pricing.Quote, error) {
	f.quoteCalls++
	f.quoteRequest = request
	f.quoteRadiusKM = radiusKM
	return pricing.Quote{}, nil
}

func (f *fakeRepository) CreateRide(_ context.Context, _ string, request QuoteRequest, key string, requestHash []byte, _ int) (Ride, bool, error) {
	f.createRequest = request
	f.createKey = key
	f.createHash = append([]byte(nil), requestHash...)
	return Ride{}, false, nil
}

func (f *fakeRepository) CurrentRide(context.Context, string) (*Ride, error) {
	return nil, nil
}

func (f *fakeRepository) GetRide(context.Context, string, string, string) (*Ride, error) {
	f.getRideCalls++
	return nil, nil
}

func (f *fakeRepository) SetAvailability(_ context.Context, _ string, status drivers.Availability) (drivers.Availability, error) {
	return status, nil
}

func (f *fakeRepository) ListDriverRequests(_ context.Context, _ string, _ int, _ *RequestCursor, _ int) (DriverRequestPage, error) {
	return DriverRequestPage{Requests: []DriverRequest{}}, nil
}

func (f *fakeRepository) CurrentAssignment(context.Context, string) (*AssignmentView, error) {
	return nil, nil
}

func (f *fakeRepository) AcceptRide(context.Context, string, string, int) (Ride, error) {
	return Ride{}, ErrRideNotFound
}

func (f *fakeRepository) CancelRide(context.Context, string, string) (Ride, error) {
	return Ride{}, ErrRideNotFound
}

func (f *fakeRepository) CompleteRide(context.Context, string, string) (Ride, error) {
	return Ride{}, ErrRideNotFound
}
