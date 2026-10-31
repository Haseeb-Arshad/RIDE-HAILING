package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
)

type Config struct {
	Port                string
	DatabaseURL         string
	AppOrigin           string
	SessionCookieSecure bool
	DemoSeedEnabled     bool
	DemoAccountPassword string
	RequestTimeout      time.Duration
}

func Load() (Config, error) {
	if err := godotenv.Load("../../.env"); err != nil && !errors.Is(err, os.ErrNotExist) {
		return Config{}, fmt.Errorf("load local environment file: %w", err)
	}

	port := valueOrDefault("PORT", "8080")
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return Config{}, errors.New("PORT must be an integer between 1 and 65535")
	}

	appOrigin := valueOrDefault("APP_ORIGIN", "http://localhost:3000")
	if err := validateOrigin(appOrigin); err != nil {
		return Config{}, err
	}

	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL != "" {
		if _, err := pgxpool.ParseConfig(databaseURL); err != nil {
			return Config{}, errors.New("DATABASE_URL must be a valid PostgreSQL connection string")
		}
	}

	secure, err := boolValue("SESSION_COOKIE_SECURE", false)
	if err != nil {
		return Config{}, err
	}
	seedEnabled, err := boolValue("DEMO_SEED_ENABLED", false)
	if err != nil {
		return Config{}, err
	}
	if seedEnabled && databaseURL == "" {
		return Config{}, errors.New("DATABASE_URL is required when DEMO_SEED_ENABLED is true")
	}
	demoAccountPassword := os.Getenv("DEMO_ACCOUNT_PASSWORD")
	if seedEnabled && len(demoAccountPassword) < 12 {
		return Config{}, errors.New("DEMO_ACCOUNT_PASSWORD must contain at least 12 bytes when demo seeding is enabled")
	}
	if seedEnabled && len(demoAccountPassword) > 72 {
		return Config{}, errors.New("DEMO_ACCOUNT_PASSWORD must contain at most 72 bytes for bcrypt when demo seeding is enabled")
	}

	timeoutText := valueOrDefault("REQUEST_TIMEOUT", "10s")
	requestTimeout, err := time.ParseDuration(timeoutText)
	if err != nil || requestTimeout < time.Millisecond || requestTimeout > time.Minute {
		return Config{}, errors.New("REQUEST_TIMEOUT must be between 1ms and 1m")
	}

	return Config{
		Port:                port,
		DatabaseURL:         databaseURL,
		AppOrigin:           appOrigin,
		SessionCookieSecure: secure,
		DemoSeedEnabled:     seedEnabled,
		DemoAccountPassword: demoAccountPassword,
		RequestTimeout:      requestTimeout,
	}, nil
}

func valueOrDefault(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func boolValue(key string, fallback bool) (bool, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("%s must be true or false", key)
	}
	return parsed, nil
}

func validateOrigin(value string) error {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" || parsed.User != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || (parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("APP_ORIGIN must be an http(s) origin without a path, query, or fragment")
	}
	return nil
}
