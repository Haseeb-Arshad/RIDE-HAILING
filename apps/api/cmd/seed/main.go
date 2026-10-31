package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/haseeb-arshad/ride-hailing/apps/api/internal/config"
	"github.com/haseeb-arshad/ride-hailing/apps/api/internal/persistence"
	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"
)

type locationFixture struct {
	id        string
	name      string
	area      string
	latitude  float64
	longitude float64
}

type userFixture struct {
	id          string
	email       string
	displayName string
	role        string
}

type driverFixture struct {
	userID    string
	latitude  float64
	longitude float64
}

var locationFixtures = []locationFixture{
	{"10000000-0000-4000-8000-000000000001", "Liberty Market", "Gulberg III", 31.5113, 74.3436},
	{"10000000-0000-4000-8000-000000000002", "MM Alam Road", "Gulberg III", 31.5147, 74.3524},
	{"10000000-0000-4000-8000-000000000003", "Lahore Fort", "Walled City", 31.5880, 74.3100},
	{"10000000-0000-4000-8000-000000000004", "Emporium Mall", "Johar Town", 31.4719, 74.2660},
	{"10000000-0000-4000-8000-000000000005", "Packages Mall", "Walton Road", 31.4686, 74.3580},
	{"10000000-0000-4000-8000-000000000006", "DHA Y Block Market", "DHA Phase 3", 31.4702, 74.4092},
	{"10000000-0000-4000-8000-000000000007", "Model Town Park", "Model Town", 31.4840, 74.3250},
}

var userFixtures = []userFixture{
	{"20000000-0000-4000-8000-000000000001", "rider@example.test", "Demo Rider", "rider"},
	{"20000000-0000-4000-8000-000000000002", "driver.one@example.test", "Demo Driver One", "driver"},
	{"20000000-0000-4000-8000-000000000003", "driver.two@example.test", "Demo Driver Two", "driver"},
}

var driverFixtures = []driverFixture{
	{"20000000-0000-4000-8000-000000000002", 31.5113, 74.3436},
	{"20000000-0000-4000-8000-000000000003", 31.4686, 74.3580},
}

func main() {
	if err := run(); err != nil {
		slog.Error("demo seed failed", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if !cfg.DemoSeedEnabled {
		return errors.New("set DEMO_SEED_ENABLED=true in the ignored .env file before running the seed command")
	}

	passwordHash, err := bcrypt.GenerateFromPassword([]byte(cfg.DemoAccountPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash demo account password: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := persistence.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin demo seed transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := seedDemoData(ctx, tx, string(passwordHash)); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit demo seed transaction: %w", err)
	}
	slog.Info("demo seed applied", "locations", len(locationFixtures), "users", len(userFixtures), "drivers", len(driverFixtures))
	return nil
}

func seedDemoData(ctx context.Context, tx pgx.Tx, passwordHash string) error {
	for _, location := range locationFixtures {
		result, err := tx.Exec(ctx, `
			INSERT INTO ride_hailing.locations AS fixture_location
				(id, name, area, city, latitude, longitude)
			VALUES ($1, $2, $3, 'Lahore', $4, $5)
			ON CONFLICT (id) DO UPDATE SET
				name = EXCLUDED.name,
				area = EXCLUDED.area,
				city = EXCLUDED.city,
				latitude = EXCLUDED.latitude,
				longitude = EXCLUDED.longitude,
				active = true
			WHERE fixture_location.name = EXCLUDED.name`,
			location.id, location.name, location.area, location.latitude, location.longitude)
		if err != nil {
			return fmt.Errorf("seed location %s: %w", location.id, err)
		}
		if result.RowsAffected() != 1 {
			return fmt.Errorf("seed location %s: fixture ID is already used by another location", location.id)
		}
	}

	for _, user := range userFixtures {
		result, err := tx.Exec(ctx, `
			INSERT INTO ride_hailing.users AS fixture_user
				(id, email, display_name, role, password_hash)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (id) DO UPDATE SET
				email = EXCLUDED.email,
				display_name = EXCLUDED.display_name,
				role = EXCLUDED.role,
				password_hash = EXCLUDED.password_hash,
				updated_at = now()
			WHERE fixture_user.email = EXCLUDED.email
			RETURNING id`,
			user.id, user.email, user.displayName, user.role, passwordHash)
		if err != nil {
			return fmt.Errorf("seed demo user %s: %w", user.email, err)
		}
		if result.RowsAffected() != 1 {
			return fmt.Errorf("seed demo user %s: fixture ID is already used by another account", user.email)
		}
	}

	for _, driver := range driverFixtures {
		_, err := tx.Exec(ctx, `
			INSERT INTO ride_hailing.driver_profiles
				(user_id, status, city, demo_latitude, demo_longitude)
			VALUES ($1, 'available', 'Lahore', $2, $3)
			ON CONFLICT (user_id) DO UPDATE SET
				city = 'Lahore',
				demo_latitude = EXCLUDED.demo_latitude,
				demo_longitude = EXCLUDED.demo_longitude,
				updated_at = now()`,
			driver.userID, driver.latitude, driver.longitude)
		if err != nil {
			return fmt.Errorf("seed driver profile %s: %w", driver.userID, err)
		}
		for _, category := range []string{"economy", "comfort"} {
			_, err = tx.Exec(ctx, `
				INSERT INTO ride_hailing.driver_categories (driver_id, category)
				VALUES ($1, $2)
				ON CONFLICT (driver_id, category) DO NOTHING`, driver.userID, category)
			if err != nil {
				return fmt.Errorf("seed driver category for %s: %w", driver.userID, err)
			}
		}
	}
	return nil
}
