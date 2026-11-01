package auth

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type identityQueryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

type PostgresRepository struct {
	db identityQueryer
}

func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	if pool == nil {
		return &PostgresRepository{}
	}
	return &PostgresRepository{db: pool}
}

func newPostgresRepository(db identityQueryer) *PostgresRepository {
	return &PostgresRepository{db: db}
}

func (r *PostgresRepository) FindIdentityBySupabaseID(ctx context.Context, supabaseID string) (Identity, error) {
	if r == nil || r.db == nil {
		return Identity{}, ErrStoreUnavailable
	}
	var identity Identity
	err := r.db.QueryRow(ctx, `
		SELECT id::text, email, display_name, role::text
		FROM ride_hailing.users
		WHERE supabase_auth_id = $1::uuid`, supabaseID).Scan(
		&identity.ID,
		&identity.Email,
		&identity.DisplayName,
		&identity.Role,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Identity{}, ErrIdentityNotFound
	}
	if err != nil {
		return Identity{}, err
	}
	return identity, nil
}
