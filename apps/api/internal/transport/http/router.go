package httptransport

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/haseeb-arshad/ride-hailing/apps/api/internal/auth"
	"github.com/haseeb-arshad/ride-hailing/apps/api/internal/rides"
	"github.com/jackc/pgx/v5/pgxpool"
)

func NewHandler(pool *pgxpool.Pool, requestTimeout time.Duration, appOrigin, supabaseURL string, matchRadiusKM int, logger *slog.Logger) http.Handler {
	router := chi.NewRouter()
	router.Use(RequestID)
	router.Use(func(next http.Handler) http.Handler { return AccessLogger(logger, next) })
	router.Use(func(next http.Handler) http.Handler { return Recoverer(logger, next) })
	router.Use(func(next http.Handler) http.Handler { return RequestTimeout(requestTimeout, next) })
	router.Use(func(next http.Handler) http.Handler { return BodyLimit(1<<20, next) })

	router.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	router.Get("/readyz", readiness(pool, requestTimeout))

	authRepository := auth.NewPostgresRepository(pool)
	var verifier auth.TokenVerifier
	if supabaseURL != "" {
		configuredVerifier, err := auth.NewJWKSVerifier(supabaseURL, nil)
		if err != nil {
			logger.Error("Supabase Auth verification is misconfigured", "error", err)
		} else {
			verifier = configuredVerifier
		}
	}
	authHandlers := newAuthHandlers(auth.NewService(authRepository, verifier), appOrigin)
	authHandlers.registerRoutes(router)
	rideRepository := rides.NewPostgresRepository(pool)
	rideService := rides.NewService(rideRepository, matchRadiusKM)
	registerRideRoutes(router, authHandlers, rideService)

	return router
}

func readiness(pool *pgxpool.Pool, timeout time.Duration) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if pool == nil {
			writeError(w, r, http.StatusServiceUnavailable, "database_unconfigured", "The database is not configured.")
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), timeout)
		defer cancel()
		if err := pool.Ping(ctx); err != nil {
			writeError(w, r, http.StatusServiceUnavailable, "database_unavailable", "The database is unavailable.")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
	}
}
