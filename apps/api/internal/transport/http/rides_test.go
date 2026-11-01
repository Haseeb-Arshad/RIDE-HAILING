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
	"github.com/haseeb-arshad/ride-hailing/apps/api/internal/drivers"
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

func TestDriverAvailabilityAcceptanceCancellationAndCompletionRoutes(t *testing.T) {
	repository := newHTTPRideRepository()
	router := newRideTestRouter(auth.NewService(newHTTPAuthRepository(t)), repository)
	riderCookie, riderCSRF := loginForRideTest(t, router, "rider@example.test")
	driverOneCookie, driverOneCSRF := loginForRideTest(t, router, "driver.one@example.test")
	driverTwoCookie, driverTwoCSRF := loginForRideTest(t, router, "driver.two@example.test")

	availability := performRideRequest(t, router, http.MethodGet, "/api/v1/drivers/me/availability", "", driverOneCookie, "", "", false)
	if availability.Code != http.StatusOK || !strings.Contains(availability.Body.String(), `"status":"available"`) {
		t.Fatalf("driver availability read status=%d body=%s", availability.Code, availability.Body.String())
	}
	riderAvailabilityRead := performRideRequest(t, router, http.MethodGet, "/api/v1/drivers/me/availability", "", riderCookie, "", "", false)
	if riderAvailabilityRead.Code != http.StatusForbidden {
		t.Fatalf("rider availability read status=%d, want 403", riderAvailabilityRead.Code)
	}

	requests := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/driver/requests", nil)
	request.AddCookie(driverOneCookie)
	router.ServeHTTP(requests, request)
	if requests.Code != http.StatusOK || !strings.Contains(requests.Body.String(), `"requests":[]`) {
		t.Fatalf("empty driver request list status=%d body=%s", requests.Code, requests.Body.String())
	}
	badLimit := httptest.NewRecorder()
	badLimitRequest := httptest.NewRequest(http.MethodGet, "/api/v1/driver/requests?limit=51", nil)
	badLimitRequest.AddCookie(driverOneCookie)
	router.ServeHTTP(badLimit, badLimitRequest)
	if badLimit.Code != http.StatusUnprocessableEntity {
		t.Fatalf("out-of-range driver request limit status = %d, want 422", badLimit.Code)
	}
	badCursor := httptest.NewRecorder()
	badCursorRequest := httptest.NewRequest(http.MethodGet, "/api/v1/driver/requests?cursor=not-base64", nil)
	badCursorRequest.AddCookie(driverOneCookie)
	router.ServeHTTP(badCursor, badCursorRequest)
	if badCursor.Code != http.StatusUnprocessableEntity {
		t.Fatalf("malformed driver request cursor status = %d, want 422", badCursor.Code)
	}

	riderAvailability := performRideRequest(t, router, http.MethodPatch, "/api/v1/drivers/me/availability", `{"status":"offline"}`, riderCookie, riderCSRF, "", true)
	if riderAvailability.Code != http.StatusForbidden {
		t.Fatalf("rider availability status = %d, want 403", riderAvailability.Code)
	}
	noCSRF := performRideRequest(t, router, http.MethodPatch, "/api/v1/drivers/me/availability", `{"status":"offline"}`, driverOneCookie, "", "", false)
	if noCSRF.Code != http.StatusForbidden {
		t.Fatalf("availability without CSRF status = %d, want 403", noCSRF.Code)
	}
	invalidAvailability := performRideRequest(t, router, http.MethodPatch, "/api/v1/drivers/me/availability", `{"status":"busy"}`, driverOneCookie, driverOneCSRF, "", true)
	if invalidAvailability.Code != http.StatusUnprocessableEntity {
		t.Fatalf("manual busy status = %d, want 422", invalidAvailability.Code)
	}
	forgedAvailability := performRideRequest(t, router, http.MethodPatch, "/api/v1/drivers/me/availability", `{"status":"available","driver_id":"20000000-0000-4000-8000-000000000003"}`, driverOneCookie, driverOneCSRF, "", true)
	if forgedAvailability.Code != http.StatusBadRequest {
		t.Fatalf("availability with forged driver ID status = %d, want 400", forgedAvailability.Code)
	}
	offline := performRideRequest(t, router, http.MethodPatch, "/api/v1/drivers/me/availability", `{"status":"offline"}`, driverOneCookie, driverOneCSRF, "", true)
	if offline.Code != http.StatusOK {
		t.Fatalf("set offline status=%d body=%s", offline.Code, offline.Body.String())
	}
	readOffline := performRideRequest(t, router, http.MethodGet, "/api/v1/drivers/me/availability", "", driverOneCookie, "", "", false)
	if readOffline.Code != http.StatusOK || !strings.Contains(readOffline.Body.String(), `"status":"offline"`) {
		t.Fatalf("read updated offline status=%d body=%s", readOffline.Code, readOffline.Body.String())
	}
	offlineRequests := httptest.NewRecorder()
	offlineRequest := httptest.NewRequest(http.MethodGet, "/api/v1/driver/requests", nil)
	offlineRequest.AddCookie(driverOneCookie)
	router.ServeHTTP(offlineRequests, offlineRequest)
	if offlineRequests.Code != http.StatusConflict {
		t.Fatalf("offline driver requests status = %d, want 409", offlineRequests.Code)
	}
	available := performRideRequest(t, router, http.MethodPatch, "/api/v1/drivers/me/availability", `{"status":"available"}`, driverOneCookie, driverOneCSRF, "", true)
	if available.Code != http.StatusOK {
		t.Fatalf("set available status=%d body=%s", available.Code, available.Body.String())
	}

	create := performRideRequest(t, router, http.MethodPost, "/api/v1/rides", quoteRequestJSON(httpPickupID, httpDestinationID, "economy"), riderCookie, riderCSRF, "lifecycle-one", true)
	if create.Code != http.StatusCreated {
		t.Fatalf("create lifecycle ride status=%d body=%s", create.Code, create.Body.String())
	}
	var created rides.Ride
	if err := json.Unmarshal(create.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode lifecycle ride: %v", err)
	}
	forgedDriver := performRideRequest(t, router, http.MethodPost, "/api/v1/rides/"+created.ID+"/accept", `{"driver_id":"20000000-0000-4000-8000-000000000003"}`, driverOneCookie, driverOneCSRF, "", true)
	if forgedDriver.Code != http.StatusBadRequest {
		t.Fatalf("accept with forged driver ID body status = %d, want 400", forgedDriver.Code)
	}
	accepted := performRideRequest(t, router, http.MethodPost, "/api/v1/rides/"+created.ID+"/accept", "", driverOneCookie, driverOneCSRF, "", true)
	if accepted.Code != http.StatusOK {
		t.Fatalf("accept ride status=%d body=%s", accepted.Code, accepted.Body.String())
	}
	var acceptedRide rides.Ride
	if err := json.Unmarshal(accepted.Body.Bytes(), &acceptedRide); err != nil {
		t.Fatalf("decode accepted ride: %v", err)
	}
	if acceptedRide.Status != "accepted" || acceptedRide.Assignment == nil || acceptedRide.Assignment.DriverID != "20000000-0000-4000-8000-000000000002" {
		t.Fatalf("accepted ride used the wrong actor or state: %+v", acceptedRide)
	}
	busy := performRideRequest(t, router, http.MethodGet, "/api/v1/drivers/me/availability", "", driverOneCookie, "", "", false)
	if busy.Code != http.StatusOK || !strings.Contains(busy.Body.String(), `"status":"busy"`) {
		t.Fatalf("assigned driver status=%d body=%s", busy.Code, busy.Body.String())
	}
	replayAccept := performRideRequest(t, router, http.MethodPost, "/api/v1/rides/"+created.ID+"/accept", "", driverOneCookie, driverOneCSRF, "", true)
	if replayAccept.Code != http.StatusOK {
		t.Fatalf("winning driver's repeat acceptance status = %d", replayAccept.Code)
	}
	competingAccept := performRideRequest(t, router, http.MethodPost, "/api/v1/rides/"+created.ID+"/accept", "", driverTwoCookie, driverTwoCSRF, "", true)
	if competingAccept.Code != http.StatusConflict {
		t.Fatalf("competing driver's acceptance status = %d, want 409", competingAccept.Code)
	}

	assignment := httptest.NewRecorder()
	assignmentRequest := httptest.NewRequest(http.MethodGet, "/api/v1/driver/assignment", nil)
	assignmentRequest.AddCookie(driverOneCookie)
	router.ServeHTTP(assignment, assignmentRequest)
	if assignment.Code != http.StatusOK || !strings.Contains(assignment.Body.String(), created.ID) {
		t.Fatalf("current assignment status=%d body=%s", assignment.Code, assignment.Body.String())
	}

	cancelled := performRideRequest(t, router, http.MethodPost, "/api/v1/rides/"+created.ID+"/cancel", "", riderCookie, riderCSRF, "", true)
	if cancelled.Code != http.StatusOK || !strings.Contains(cancelled.Body.String(), `"status":"cancelled"`) {
		t.Fatalf("cancel accepted ride status=%d body=%s", cancelled.Code, cancelled.Body.String())
	}
	replayCancel := performRideRequest(t, router, http.MethodPost, "/api/v1/rides/"+created.ID+"/cancel", "", riderCookie, riderCSRF, "", true)
	if replayCancel.Code != http.StatusOK {
		t.Fatalf("repeat rider cancellation status = %d", replayCancel.Code)
	}
	completeCancelled := performRideRequest(t, router, http.MethodPost, "/api/v1/rides/"+created.ID+"/complete", "", driverOneCookie, driverOneCSRF, "", true)
	if completeCancelled.Code != http.StatusConflict {
		t.Fatalf("completion after cancellation status = %d, want 409", completeCancelled.Code)
	}

	secondCreate := performRideRequest(t, router, http.MethodPost, "/api/v1/rides", quoteRequestJSON(httpPickupID, httpDestinationID, "comfort"), riderCookie, riderCSRF, "lifecycle-two", true)
	if secondCreate.Code != http.StatusCreated {
		t.Fatalf("create second lifecycle ride status=%d body=%s", secondCreate.Code, secondCreate.Body.String())
	}
	var secondRide rides.Ride
	if err := json.Unmarshal(secondCreate.Body.Bytes(), &secondRide); err != nil {
		t.Fatalf("decode second lifecycle ride: %v", err)
	}
	secondAccept := performRideRequest(t, router, http.MethodPost, "/api/v1/rides/"+secondRide.ID+"/accept", "", driverOneCookie, driverOneCSRF, "", true)
	if secondAccept.Code != http.StatusOK {
		t.Fatalf("accept second ride after release status=%d body=%s", secondAccept.Code, secondAccept.Body.String())
	}
	completed := performRideRequest(t, router, http.MethodPost, "/api/v1/rides/"+secondRide.ID+"/complete", "", driverOneCookie, driverOneCSRF, "", true)
	if completed.Code != http.StatusOK || !strings.Contains(completed.Body.String(), `"status":"completed"`) {
		t.Fatalf("complete ride status=%d body=%s", completed.Code, completed.Body.String())
	}
	released := performRideRequest(t, router, http.MethodGet, "/api/v1/drivers/me/availability", "", driverOneCookie, "", "", false)
	if released.Code != http.StatusOK || !strings.Contains(released.Body.String(), `"status":"available"`) {
		t.Fatalf("completed driver status=%d body=%s", released.Code, released.Body.String())
	}
	replayComplete := performRideRequest(t, router, http.MethodPost, "/api/v1/rides/"+secondRide.ID+"/complete", "", driverOneCookie, driverOneCSRF, "", true)
	if replayComplete.Code != http.StatusOK {
		t.Fatalf("repeat driver completion status = %d", replayComplete.Code)
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
	open         map[string]string
	driverStatus map[string]drivers.Availability
	sequence     int
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
		driverStatus: map[string]drivers.Availability{
			"20000000-0000-4000-8000-000000000002": drivers.Available,
			"20000000-0000-4000-8000-000000000003": drivers.Available,
		},
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

func (r *httpRideRepository) SetAvailability(_ context.Context, driverID string, status drivers.Availability) (drivers.Availability, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	current, ok := r.driverStatus[driverID]
	if !ok {
		return "", rides.ErrDriverNotFound
	}
	if current == drivers.Busy {
		return "", rides.ErrDriverUnavailable
	}
	r.driverStatus[driverID] = status
	return status, nil
}

func (r *httpRideRepository) DriverAvailability(_ context.Context, driverID string) (drivers.Availability, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	status, ok := r.driverStatus[driverID]
	if !ok {
		return "", rides.ErrDriverNotFound
	}
	return status, nil
}

func (r *httpRideRepository) ListDriverRequests(_ context.Context, driverID string, _ int, _ *rides.RequestCursor, _ int) (rides.DriverRequestPage, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.driverStatus[driverID]; !ok {
		return rides.DriverRequestPage{}, rides.ErrDriverNotFound
	}
	if r.driverStatus[driverID] != drivers.Available {
		return rides.DriverRequestPage{}, rides.ErrDriverUnavailable
	}
	return rides.DriverRequestPage{Requests: []rides.DriverRequest{}}, nil
}

func (r *httpRideRepository) CurrentAssignment(_ context.Context, driverID string) (*rides.AssignmentView, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, ride := range r.rides {
		if ride.Assignment != nil && ride.Assignment.DriverID == driverID && ride.Assignment.Status == "active" {
			copyOfRide := ride
			return &rides.AssignmentView{Ride: &copyOfRide}, nil
		}
	}
	return nil, nil
}

func (r *httpRideRepository) AcceptRide(_ context.Context, rideID, driverID string, _ int) (rides.Ride, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	ride, ok := r.rides[rideID]
	if !ok {
		return rides.Ride{}, rides.ErrRideNotFound
	}
	if ride.Status == "accepted" && ride.Assignment != nil && ride.Assignment.DriverID == driverID && ride.Assignment.Status == "active" {
		return ride, nil
	}
	if ride.Status != "requested" {
		return rides.Ride{}, rides.ErrStateConflict
	}
	if r.driverStatus[driverID] != drivers.Available {
		return rides.Ride{}, rides.ErrDriverUnavailable
	}
	for _, activeRide := range r.rides {
		if activeRide.Assignment != nil && activeRide.Assignment.DriverID == driverID && activeRide.Assignment.Status == "active" {
			return rides.Ride{}, rides.ErrDriverUnavailable
		}
	}
	ride.Status = "accepted"
	ride.Assignment = &rides.Assignment{
		ID:                "50000000-0000-4000-8000-000000000001",
		DriverID:          driverID,
		DriverDisplayName: "Demo Driver One",
		Status:            "active",
		AssignedAt:        time.Now().UTC(),
	}
	ride.UpdatedAt = time.Now().UTC()
	r.rides[rideID] = ride
	r.driverStatus[driverID] = drivers.Busy
	return ride, nil
}

func (r *httpRideRepository) CancelRide(_ context.Context, rideID, riderID string) (rides.Ride, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	ride, ok := r.rides[rideID]
	if !ok || ride.Rider.ID != riderID {
		return rides.Ride{}, rides.ErrRideNotFound
	}
	if ride.Status == "cancelled" {
		return ride, nil
	}
	if ride.Status != "requested" && ride.Status != "accepted" {
		return rides.Ride{}, rides.ErrStateConflict
	}
	if ride.Assignment != nil && ride.Assignment.Status == "active" {
		ride.Assignment.Status = "cancelled"
		r.driverStatus[ride.Assignment.DriverID] = drivers.Available
	}
	ride.Status = "cancelled"
	ride.UpdatedAt = time.Now().UTC()
	r.rides[rideID] = ride
	delete(r.open, riderID)
	return ride, nil
}

func (r *httpRideRepository) CompleteRide(_ context.Context, rideID, driverID string) (rides.Ride, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	ride, ok := r.rides[rideID]
	if !ok || ride.Assignment == nil || ride.Assignment.DriverID != driverID {
		return rides.Ride{}, rides.ErrRideNotFound
	}
	if ride.Status == "completed" && ride.Assignment.Status == "completed" {
		return ride, nil
	}
	if ride.Status != "accepted" || ride.Assignment.Status != "active" {
		return rides.Ride{}, rides.ErrStateConflict
	}
	ride.Status = "completed"
	ride.Assignment.Status = "completed"
	ride.UpdatedAt = time.Now().UTC()
	r.rides[rideID] = ride
	r.driverStatus[driverID] = drivers.Available
	delete(r.open, ride.Rider.ID)
	return ride, nil
}

func (r *httpRideRepository) createdCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.rides)
}

func formatSequence(sequence int) string {
	return fmt.Sprintf("%012x", sequence)
}
