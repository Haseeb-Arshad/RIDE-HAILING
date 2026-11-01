package httptransport

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/haseeb-arshad/ride-hailing/apps/api/internal/auth"
	"github.com/haseeb-arshad/ride-hailing/apps/api/internal/pricing"
	"github.com/haseeb-arshad/ride-hailing/apps/api/internal/rides"
)

const (
	httpPickupID      = "10000000-0000-4000-8000-000000000001"
	httpDestinationID = "10000000-0000-4000-8000-000000000002"
)

func TestRiderQuoteAndIdempotentRideCreationRoutes(t *testing.T) {
	repository := newHTTPRideRepository()
	authRepository := newHTTPAuthRepository(t)
	router := newRideTestRouter(auth.NewService(authRepository), repository)
	riderCookie, csrfToken := loginForRideTest(t, router, "rider@example.test")

	quoteResponse := performRideRequest(t, router, http.MethodPost, "/api/v1/quotes", quoteRequestJSON(httpPickupID, httpDestinationID, "economy"), riderCookie, csrfToken, "", true)
	if quoteResponse.Code != http.StatusOK {
		t.Fatalf("quote status = %d, body=%s", quoteResponse.Code, quoteResponse.Body.String())
	}
	var quote pricing.Quote
	if err := json.Unmarshal(quoteResponse.Body.Bytes(), &quote); err != nil {
		t.Fatalf("decode quote: %v", err)
	}
	if quote.EstimatedFarePaisa != 23_676 || quote.Currency != "PKR" || quote.PickupETAMinutes == nil || *quote.PickupETAMinutes != 2 {
		t.Fatalf("unexpected quote: %+v", quote)
	}
	if repository.createdCount() != 0 {
		t.Fatal("quote request created a ride")
	}

	createBody := quoteRequestJSON(strings.ToUpper(httpPickupID), httpDestinationID, "economy")
	first := performRideRequest(t, router, http.MethodPost, "/api/v1/rides", createBody, riderCookie, csrfToken, "booking-1", true)
	if first.Code != http.StatusCreated {
		t.Fatalf("first ride creation status = %d, body=%s", first.Code, first.Body.String())
	}
	var firstRide rides.Ride
	if err := json.Unmarshal(first.Body.Bytes(), &firstRide); err != nil {
		t.Fatalf("decode new ride: %v", err)
	}
	if firstRide.Status != "requested" || firstRide.EstimatedFarePaisa != quote.EstimatedFarePaisa || firstRide.Pickup.ID != httpPickupID || firstRide.Assignment != nil {
		t.Fatalf("unexpected created ride: %+v", firstRide)
	}

	replay := performRideRequest(t, router, http.MethodPost, "/api/v1/rides", quoteRequestJSON(httpPickupID, httpDestinationID, "economy"), riderCookie, csrfToken, "booking-1", true)
	if replay.Code != http.StatusOK {
		t.Fatalf("same-key replay status = %d, body=%s", replay.Code, replay.Body.String())
	}
	var replayedRide rides.Ride
	if err := json.Unmarshal(replay.Body.Bytes(), &replayedRide); err != nil {
		t.Fatalf("decode replayed ride: %v", err)
	}
	if replayedRide.ID != firstRide.ID || repository.createdCount() != 1 {
		t.Fatalf("retry did not return the original ride: first=%s replay=%s created=%d", firstRide.ID, replayedRide.ID, repository.createdCount())
	}

	changedPayload := performRideRequest(t, router, http.MethodPost, "/api/v1/rides", quoteRequestJSON(httpPickupID, "10000000-0000-4000-8000-000000000003", "economy"), riderCookie, csrfToken, "booking-1", true)
	if changedPayload.Code != http.StatusConflict {
		t.Fatalf("changed payload under same key status = %d, want 409", changedPayload.Code)
	}
	secondKey := performRideRequest(t, router, http.MethodPost, "/api/v1/rides", quoteRequestJSON(httpPickupID, httpDestinationID, "comfort"), riderCookie, csrfToken, "booking-2", true)
	if secondKey.Code != http.StatusConflict {
		t.Fatalf("second open ride status = %d, want 409", secondKey.Code)
	}

	current := httptest.NewRecorder()
	currentRequest := httptest.NewRequest(http.MethodGet, "/api/v1/rides/current", nil)
	currentRequest.AddCookie(riderCookie)
	router.ServeHTTP(current, currentRequest)
	if current.Code != http.StatusOK || !strings.Contains(current.Body.String(), firstRide.ID) {
		t.Fatalf("current ride response status=%d body=%s", current.Code, current.Body.String())
	}
}

func TestRideRoutesEnforceRoleCSRFAndStrictRequestFields(t *testing.T) {
	repository := newHTTPRideRepository()
	router := newRideTestRouter(auth.NewService(newHTTPAuthRepository(t)), repository)
	riderCookie, csrfToken := loginForRideTest(t, router, "rider@example.test")
	driverCookie, driverCSRF := loginForRideTest(t, router, "driver.one@example.test")

	unauthenticated := httptest.NewRecorder()
	router.ServeHTTP(unauthenticated, httptest.NewRequest(http.MethodGet, "/api/v1/locations", nil))
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated location status = %d, want 401", unauthenticated.Code)
	}

	locations := httptest.NewRecorder()
	locationRequest := httptest.NewRequest(http.MethodGet, "/api/v1/locations", nil)
	locationRequest.AddCookie(driverCookie)
	router.ServeHTTP(locations, locationRequest)
	if locations.Code != http.StatusOK || !strings.Contains(locations.Body.String(), "Liberty Market") {
		t.Fatalf("authenticated locations status=%d body=%s", locations.Code, locations.Body.String())
	}

	driverQuote := performRideRequest(t, router, http.MethodPost, "/api/v1/quotes", quoteRequestJSON(httpPickupID, httpDestinationID, "economy"), driverCookie, driverCSRF, "", true)
	if driverQuote.Code != http.StatusForbidden {
		t.Fatalf("driver quote status = %d, want 403", driverQuote.Code)
	}

	missingCSRF := performRideRequest(t, router, http.MethodPost, "/api/v1/quotes", quoteRequestJSON(httpPickupID, httpDestinationID, "economy"), riderCookie, "", "", false)
	if missingCSRF.Code != http.StatusForbidden {
		t.Fatalf("missing CSRF quote status = %d, want 403", missingCSRF.Code)
	}

	extraAuthorityField := `{"pickup_location_id":"` + httpPickupID + `","destination_location_id":"` + httpDestinationID + `","category":"economy","rider_id":"20000000-0000-4000-8000-000000000002"}`
	strictFields := performRideRequest(t, router, http.MethodPost, "/api/v1/quotes", extraAuthorityField, riderCookie, csrfToken, "", true)
	if strictFields.Code != http.StatusBadRequest {
		t.Fatalf("extra authority field status = %d, want 400", strictFields.Code)
	}

	invalidLocation := performRideRequest(t, router, http.MethodPost, "/api/v1/quotes", quoteRequestJSON("invalid", httpDestinationID, "economy"), riderCookie, csrfToken, "", true)
	if invalidLocation.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid location ID status = %d, want 422", invalidLocation.Code)
	}

	missingKey := performRideRequest(t, router, http.MethodPost, "/api/v1/rides", quoteRequestJSON(httpPickupID, httpDestinationID, "economy"), riderCookie, csrfToken, "", true)
	if missingKey.Code != http.StatusUnprocessableEntity {
		t.Fatalf("missing idempotency key status = %d, want 422", missingKey.Code)
	}
}

func TestRideReadConcealsOtherRidersRide(t *testing.T) {
	repository := newHTTPRideRepository()
	authRepository := newHTTPAuthRepository(t)
	other := authRepository.users["rider@example.test"]
	other.ID = "20000000-0000-4000-8000-000000000004"
	other.Email = "other@example.test"
	other.DisplayName = "Other Rider"
	authRepository.users[other.Email] = other
	router := newRideTestRouter(auth.NewService(authRepository), repository)
	riderCookie, csrfToken := loginForRideTest(t, router, "rider@example.test")
	created := performRideRequest(t, router, http.MethodPost, "/api/v1/rides", quoteRequestJSON(httpPickupID, httpDestinationID, "economy"), riderCookie, csrfToken, "read-ride", true)
	if created.Code != http.StatusCreated {
		t.Fatalf("create ride status = %d, body=%s", created.Code, created.Body.String())
	}
	var ride rides.Ride
	if err := json.Unmarshal(created.Body.Bytes(), &ride); err != nil {
		t.Fatalf("decode created ride: %v", err)
	}

	otherCookie, _ := loginForRideTest(t, router, "other@example.test")
	request := httptest.NewRequest(http.MethodGet, "/api/v1/rides/"+ride.ID, nil)
	request.AddCookie(otherCookie)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("other rider read of ride status = %d, want concealed 404", response.Code)
	}
}

func newRideTestRouter(authService *auth.Service, repository *httpRideRepository) http.Handler {
	router := chi.NewRouter()
	router.Use(RequestID)
	handlers := newAuthHandlers(authService, testAppOrigin, false)
	handlers.registerRoutes(router)
	registerRideRoutes(router, handlers, rides.NewService(repository, pricing.DefaultMatchRadiusKM))
	return router
}

func loginForRideTest(t *testing.T, router http.Handler, email string) (*http.Cookie, string) {
	t.Helper()
	response := performLogin(t, router, email, "correct horse battery staple")
	if response.Code != http.StatusOK {
		t.Fatalf("login for %s status=%d body=%s", email, response.Code, response.Body.String())
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("login for %s returned %d cookies", email, len(cookies))
	}
	var body sessionResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode %s login response: %v", email, err)
	}
	return cookies[0], body.CSRFToken
}

func performRideRequest(t *testing.T, router http.Handler, method, path, body string, cookie *http.Cookie, csrfToken, idempotencyKey string, includeCSRF bool) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	if cookie != nil {
		request.AddCookie(cookie)
	}
	request.Header.Set("Origin", testAppOrigin)
	if includeCSRF {
		request.Header.Set("X-CSRF-Token", csrfToken)
	}
	if idempotencyKey != "" {
		request.Header.Set("Idempotency-Key", idempotencyKey)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func quoteRequestJSON(pickupID, destinationID, category string) string {
	return `{"pickup_location_id":"` + pickupID + `","destination_location_id":"` + destinationID + `","category":"` + category + `"}`
}

type httpRideRepository struct {
	mu        sync.Mutex
	locations map[string]pricing.Location
	quotes    int
	rides     map[string]rides.Ride
	keys      map[string]struct {
		hash []byte
		ride string
	}
	open     map[string]string
	sequence int
}

func newHTTPRideRepository() *httpRideRepository {
	return &httpRideRepository{
		locations: map[string]pricing.Location{
			httpPickupID:                           {ID: httpPickupID, Name: "Liberty Market", Area: "Gulberg III", City: "Lahore", Latitude: 0, Longitude: 0},
			httpDestinationID:                      {ID: httpDestinationID, Name: "MM Alam Road", Area: "Gulberg III", City: "Lahore", Latitude: 0, Longitude: 0.01},
			"10000000-0000-4000-8000-000000000003": {ID: "10000000-0000-4000-8000-000000000003", Name: "Emporium Mall", Area: "Johar Town", City: "Lahore", Latitude: 0, Longitude: 0.02},
		},
		rides: make(map[string]rides.Ride),
		keys: make(map[string]struct {
			hash []byte
			ride string
		}),
		open: make(map[string]string),
	}
}

func (r *httpRideRepository) ListLocations(context.Context) ([]pricing.Location, error) {
	return []pricing.Location{r.locations[httpPickupID], r.locations[httpDestinationID]}, nil
}

func (r *httpRideRepository) Quote(_ context.Context, request rides.QuoteRequest, _ int) (pricing.Quote, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.quotes++
	pickup, pickupOK := r.locations[request.PickupLocationID]
	destination, destinationOK := r.locations[request.DestinationLocationID]
	if !pickupOK || !destinationOK {
		return pricing.Quote{}, rides.ErrLocationNotFound
	}
	driver := pricing.AvailableDriver{City: "Lahore", Category: request.Category, Latitude: pickup.Latitude, Longitude: pickup.Longitude, Available: true}
	return pricing.CalculateQuote(pickup, destination, request.Category, []pricing.AvailableDriver{driver}, pricing.DefaultMatchRadiusKM)
}

func (r *httpRideRepository) CreateRide(_ context.Context, riderID string, request rides.QuoteRequest, key string, requestHash []byte, _ int) (rides.Ride, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	mapKey := riderID + ":" + key
	if existing, ok := r.keys[mapKey]; ok {
		if !bytes.Equal(existing.hash, requestHash) {
			return rides.Ride{}, false, rides.ErrIdempotencyConflict
		}
		return r.rides[existing.ride], true, nil
	}
	if _, ok := r.open[riderID]; ok {
		return rides.Ride{}, false, rides.ErrOpenRideConflict
	}
	pickup, pickupOK := r.locations[request.PickupLocationID]
	destination, destinationOK := r.locations[request.DestinationLocationID]
	if !pickupOK || !destinationOK {
		return rides.Ride{}, false, rides.ErrLocationNotFound
	}
	driver := pricing.AvailableDriver{City: "Lahore", Category: request.Category, Latitude: pickup.Latitude, Longitude: pickup.Longitude, Available: true}
	quote, err := pricing.CalculateQuote(pickup, destination, request.Category, []pricing.AvailableDriver{driver}, pricing.DefaultMatchRadiusKM)
	if err != nil {
		return rides.Ride{}, false, err
	}
	r.sequence++
	rideID := "40000000-0000-4000-8000-" + formatSequence(r.sequence)
	now := time.Date(2026, 1, 1, 0, 0, r.sequence, 0, time.UTC)
	var riderName string
	if riderID == "20000000-0000-4000-8000-000000000001" {
		riderName = "Demo Rider"
	} else {
		riderName = "Other Rider"
	}
	ride := rides.Ride{
		ID:                      rideID,
		Rider:                   rides.RiderSummary{ID: riderID, DisplayName: riderName},
		Pickup:                  pickup,
		Destination:             destination,
		Category:                request.Category,
		EstimatedFarePaisa:      quote.EstimatedFarePaisa,
		EstimatedDistanceMeters: quote.EstimatedDistanceMeters,
		PickupETAMinutes:        quote.PickupETAMinutes,
		Currency:                quote.Currency,
		Approximate:             quote.Approximate,
		Status:                  "requested",
		CreatedAt:               now,
		UpdatedAt:               now,
	}
	r.rides[rideID] = ride
	r.keys[mapKey] = struct {
		hash []byte
		ride string
	}{hash: append([]byte(nil), requestHash...), ride: rideID}
	r.open[riderID] = rideID
	return ride, false, nil
}

func (r *httpRideRepository) CurrentRide(_ context.Context, riderID string) (*rides.Ride, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if rideID, ok := r.open[riderID]; ok {
		ride := r.rides[rideID]
		return &ride, nil
	}
	return nil, nil
}

func (r *httpRideRepository) GetRide(_ context.Context, rideID, actorID, actorRole string) (*rides.Ride, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	ride, ok := r.rides[rideID]
	if !ok {
		return nil, rides.ErrRideNotFound
	}
	if actorRole == "rider" && ride.Rider.ID != actorID {
		return nil, rides.ErrRideNotFound
	}
	if actorRole == "driver" && (ride.Assignment == nil || ride.Assignment.DriverID != actorID) {
		return nil, rides.ErrRideNotFound
	}
	return &ride, nil
}

func (r *httpRideRepository) createdCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.rides)
}

func formatSequence(sequence int) string {
	return fmt.Sprintf("%012x", sequence)
}
