package httptransport

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/haseeb-arshad/ride-hailing/apps/api/internal/pricing"
	"github.com/haseeb-arshad/ride-hailing/apps/api/internal/rides"
)

type rideHandlers struct {
	service *rides.Service
}

func registerRideRoutes(router chi.Router, authHandlers *authHandlers, service *rides.Service) {
	handlers := &rideHandlers{service: service}
	router.With(authHandlers.requireAuth).Get("/api/v1/locations", handlers.listLocations)
	router.With(authHandlers.requireOrigin, authHandlers.requireAuth, authHandlers.requireCSRF, authHandlers.requireRole("rider")).Post("/api/v1/quotes", handlers.quote)
	router.With(authHandlers.requireOrigin, authHandlers.requireAuth, authHandlers.requireCSRF, authHandlers.requireRole("rider")).Post("/api/v1/rides", handlers.createRide)
	router.With(authHandlers.requireAuth, authHandlers.requireRole("rider")).Get("/api/v1/rides/current", handlers.currentRide)
	router.With(authHandlers.requireAuth).Get("/api/v1/rides/{id}", handlers.getRide)
}

func (h *rideHandlers) listLocations(w http.ResponseWriter, r *http.Request) {
	locations, err := h.service.ListLocations(r.Context())
	if err != nil {
		h.writeRideError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, struct {
		Locations []pricing.Location `json:"locations"`
	}{Locations: locations})
}

func (h *rideHandlers) quote(w http.ResponseWriter, r *http.Request) {
	var request rides.QuoteRequest
	if err := decodeQuoteRequest(w, r, &request); err != nil {
		if errors.Is(err, errRequestBodyTooLarge) {
			writeError(w, r, http.StatusRequestEntityTooLarge, "request_too_large", "The request body is too large.")
			return
		}
		writeError(w, r, http.StatusBadRequest, "invalid_request", "The request body must be one valid JSON object.")
		return
	}
	quote, err := h.service.Quote(r.Context(), request)
	if err != nil {
		h.writeRideError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, quote)
}

func (h *rideHandlers) createRide(w http.ResponseWriter, r *http.Request) {
	var request rides.QuoteRequest
	if err := decodeQuoteRequest(w, r, &request); err != nil {
		if errors.Is(err, errRequestBodyTooLarge) {
			writeError(w, r, http.StatusRequestEntityTooLarge, "request_too_large", "The request body is too large.")
			return
		}
		writeError(w, r, http.StatusBadRequest, "invalid_request", "The request body must be one valid JSON object.")
		return
	}
	keys := r.Header.Values("Idempotency-Key")
	if len(keys) != 1 {
		h.writeRideError(w, r, rides.ValidationError{Message: "Provide one Idempotency-Key containing 1 to 128 visible ASCII characters."})
		return
	}
	principal, ok := principalFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "unauthenticated", "A valid session is required.")
		return
	}
	ride, replay, err := h.service.CreateRide(r.Context(), principal.ID, request, keys[0])
	if err != nil {
		h.writeRideError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	status := http.StatusCreated
	if replay {
		status = http.StatusOK
	}
	writeJSON(w, status, ride)
}

func (h *rideHandlers) currentRide(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "unauthenticated", "A valid session is required.")
		return
	}
	ride, err := h.service.CurrentRide(r.Context(), principal.ID)
	if err != nil {
		h.writeRideError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, struct {
		Ride *rides.Ride `json:"ride"`
	}{Ride: ride})
}

func (h *rideHandlers) getRide(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "unauthenticated", "A valid session is required.")
		return
	}
	ride, err := h.service.GetRide(r.Context(), chi.URLParam(r, "id"), principal.ID, principal.Role)
	if err != nil {
		h.writeRideError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, ride)
}

func (h *rideHandlers) writeRideError(w http.ResponseWriter, r *http.Request, err error) {
	var validationError rides.ValidationError
	switch {
	case errors.As(err, &validationError):
		writeError(w, r, http.StatusUnprocessableEntity, "invalid_input", validationError.Message)
	case errors.Is(err, rides.ErrLocationNotFound):
		writeError(w, r, http.StatusUnprocessableEntity, "invalid_location", "Choose active pickup and destination locations in Lahore.")
	case errors.Is(err, rides.ErrIdempotencyConflict):
		writeError(w, r, http.StatusConflict, "idempotency_conflict", "This Idempotency-Key was already used for a different request.")
	case errors.Is(err, rides.ErrOpenRideConflict):
		writeError(w, r, http.StatusConflict, "open_ride_exists", "Finish or cancel your current ride before requesting another.")
	case errors.Is(err, rides.ErrRideNotFound):
		writeError(w, r, http.StatusNotFound, "ride_not_found", "The ride was not found.")
	case errors.Is(err, rides.ErrStoreUnavailable):
		writeError(w, r, http.StatusServiceUnavailable, "database_unavailable", "Ride data is temporarily unavailable.")
	default:
		writeError(w, r, http.StatusInternalServerError, "internal_error", "An unexpected error occurred.")
	}
}

func decodeQuoteRequest(w http.ResponseWriter, r *http.Request, target *rides.QuoteRequest) error {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return errors.New("content type must be application/json")
	}
	limited := http.MaxBytesReader(w, r.Body, 8<<10)
	decoder := json.NewDecoder(limited)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			return errRequestBodyTooLarge
		}
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("request must contain exactly one JSON object")
	}
	return nil
}
