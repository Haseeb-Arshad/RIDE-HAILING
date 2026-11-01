package httptransport

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/haseeb-arshad/ride-hailing/apps/api/internal/pricing"
	"github.com/haseeb-arshad/ride-hailing/apps/api/internal/rides"
)

var errRequestBodyTooLarge = errors.New("request body too large")

type rideHandlers struct {
	service *rides.Service
}

func registerRideRoutes(router chi.Router, authHandlers *authHandlers, service *rides.Service) {
	handlers := &rideHandlers{service: service}
	router.With(authHandlers.requireAuth).Get("/api/v1/locations", handlers.listLocations)
	router.With(authHandlers.requireOrigin, authHandlers.requireAuth, authHandlers.requireRole("rider")).Post("/api/v1/quotes", handlers.quote)
	router.With(authHandlers.requireOrigin, authHandlers.requireAuth, authHandlers.requireRole("rider")).Post("/api/v1/rides", handlers.createRide)
	router.With(authHandlers.requireAuth, authHandlers.requireRole("rider")).Get("/api/v1/rides/current", handlers.currentRide)
	router.With(authHandlers.requireAuth).Get("/api/v1/rides/{id}", handlers.getRide)
	router.With(authHandlers.requireAuth, authHandlers.requireRole("driver")).Get("/api/v1/drivers/me/availability", handlers.getAvailability)
	router.With(authHandlers.requireAuth, authHandlers.requireRole("driver")).Get("/api/v1/driver/requests", handlers.driverRequests)
	router.With(authHandlers.requireAuth, authHandlers.requireRole("driver")).Get("/api/v1/driver/assignment", handlers.currentAssignment)
	router.With(authHandlers.requireOrigin, authHandlers.requireAuth, authHandlers.requireRole("driver")).Patch("/api/v1/drivers/me/availability", handlers.setAvailability)
	router.With(authHandlers.requireOrigin, authHandlers.requireAuth, authHandlers.requireRole("driver")).Post("/api/v1/rides/{id}/accept", handlers.acceptRide)
	router.With(authHandlers.requireOrigin, authHandlers.requireAuth, authHandlers.requireRole("rider")).Post("/api/v1/rides/{id}/cancel", handlers.cancelRide)
	router.With(authHandlers.requireOrigin, authHandlers.requireAuth, authHandlers.requireRole("driver")).Post("/api/v1/rides/{id}/complete", handlers.completeRide)
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

func (h *rideHandlers) setAvailability(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Status string `json:"status"`
	}
	if err := decodeStrictJSON(w, r, &payload); err != nil {
		h.writeDecodeError(w, r, err)
		return
	}
	principal, ok := principalFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "unauthenticated", "A valid session is required.")
		return
	}
	status, err := h.service.SetAvailability(r.Context(), principal.ID, payload.Status)
	if err != nil {
		h.writeRideError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, struct {
		Status string `json:"status"`
	}{Status: string(status)})
}

func (h *rideHandlers) getAvailability(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "unauthenticated", "A valid session is required.")
		return
	}
	status, err := h.service.DriverAvailability(r.Context(), principal.ID)
	if err != nil {
		h.writeRideError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, struct {
		Status string `json:"status"`
	}{Status: string(status)})
}

func (h *rideHandlers) driverRequests(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "unauthenticated", "A valid session is required.")
		return
	}
	limit, err := rides.ParseRequestLimit(r.URL.Query().Get("limit"))
	if err != nil {
		h.writeRideError(w, r, err)
		return
	}
	page, err := h.service.ListDriverRequests(r.Context(), principal.ID, limit, r.URL.Query().Get("cursor"))
	if err != nil {
		h.writeRideError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, page)
}

func (h *rideHandlers) currentAssignment(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "unauthenticated", "A valid session is required.")
		return
	}
	assignment, err := h.service.CurrentAssignment(r.Context(), principal.ID)
	if err != nil {
		h.writeRideError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, struct {
		Assignment *rides.AssignmentView `json:"assignment"`
	}{Assignment: assignment})
}

func (h *rideHandlers) acceptRide(w http.ResponseWriter, r *http.Request) {
	h.lifecycleAction(w, r, func(principalID, rideID string) (rides.Ride, error) {
		return h.service.AcceptRide(r.Context(), rideID, principalID)
	})
}

func (h *rideHandlers) cancelRide(w http.ResponseWriter, r *http.Request) {
	h.lifecycleAction(w, r, func(principalID, rideID string) (rides.Ride, error) {
		return h.service.CancelRide(r.Context(), rideID, principalID)
	})
}

func (h *rideHandlers) completeRide(w http.ResponseWriter, r *http.Request) {
	h.lifecycleAction(w, r, func(principalID, rideID string) (rides.Ride, error) {
		return h.service.CompleteRide(r.Context(), rideID, principalID)
	})
}

func (h *rideHandlers) lifecycleAction(w http.ResponseWriter, r *http.Request, action func(string, string) (rides.Ride, error)) {
	if err := decodeEmptyBody(w, r); err != nil {
		h.writeDecodeError(w, r, err)
		return
	}
	principal, ok := principalFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "unauthenticated", "A valid session is required.")
		return
	}
	ride, err := action(principal.ID, chi.URLParam(r, "id"))
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
	case errors.Is(err, rides.ErrDriverNotFound):
		writeError(w, r, http.StatusNotFound, "driver_not_found", "The driver profile was not found.")
	case errors.Is(err, rides.ErrDriverUnavailable):
		writeError(w, r, http.StatusConflict, "driver_unavailable", "The driver is unavailable for this action.")
	case errors.Is(err, rides.ErrStateConflict):
		writeError(w, r, http.StatusConflict, "state_conflict", "The ride changed before this action could be completed.")
	case errors.Is(err, rides.ErrStoreUnavailable):
		writeError(w, r, http.StatusServiceUnavailable, "database_unavailable", "Ride data is temporarily unavailable.")
	default:
		writeError(w, r, http.StatusInternalServerError, "internal_error", "An unexpected error occurred.")
	}
}

func decodeQuoteRequest(w http.ResponseWriter, r *http.Request, target *rides.QuoteRequest) error {
	return decodeStrictJSON(w, r, target)
}

func decodeStrictJSON(w http.ResponseWriter, r *http.Request, target any) error {
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

func decodeEmptyBody(w http.ResponseWriter, r *http.Request) error {
	limited := http.MaxBytesReader(w, r.Body, 8<<10)
	body, err := io.ReadAll(limited)
	if err != nil {
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			return errRequestBodyTooLarge
		}
		return err
	}
	if len(bytes.TrimSpace(body)) != 0 || strings.TrimSpace(r.Header.Get("Content-Type")) != "" {
		return errors.New("this request does not accept a body")
	}
	return nil
}

func (h *rideHandlers) writeDecodeError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, errRequestBodyTooLarge) {
		writeError(w, r, http.StatusRequestEntityTooLarge, "request_too_large", "The request body is too large.")
		return
	}
	writeError(w, r, http.StatusBadRequest, "invalid_request", "The request body is invalid or not accepted.")
}
