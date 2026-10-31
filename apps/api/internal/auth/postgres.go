package auth

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type queryer interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

type PostgresRepository struct {
	db queryer
}

func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	if pool == nil {
		return &PostgresRepository{}
	}
	return &PostgresRepository{db: pool}
}

func newPostgresRepository(db queryer) *PostgresRepository {
	return &PostgresRepository{db: db}
}

func (r *PostgresRepository) FindUserByEmail(ctx context.Context, email string) (Credentials, error) {
	if r == nil || r.db == nil {
		return Credentials{}, ErrStoreUnavailable
	}
	var user Credentials
	err := r.db.QueryRow(ctx, `
		SELECT id::text, email, display_name, role::text, password_hash
		FROM ride_hailing.users
		WHERE email = $1`, email).Scan(&user.ID, &user.Email, &user.DisplayName, &user.Role, &user.PasswordHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return Credentials{}, ErrUserNotFound
	}
	if err != nil {
		return Credentials{}, err
	}
	return user, nil
}

func (r *PostgresRepository) CreateSession(ctx context.Context, userID string, tokenHash, csrfTokenHash []byte, expiresAt time.Time) error {
	if r == nil || r.db == nil {
		return ErrStoreUnavailable
	}
	_, err := r.db.Exec(ctx, `
		INSERT INTO ride_hailing.sessions (token_hash, user_id, csrf_token_hash, expires_at)
		VALUES ($1, $2, $3, $4)`, tokenHash, userID, csrfTokenHash, expiresAt)
	return err
}

func (r *PostgresRepository) FindActiveSession(ctx context.Context, tokenHash []byte) (StoredSession, error) {
	if r == nil || r.db == nil {
		return StoredSession{}, ErrStoreUnavailable
	}
	var session StoredSession
	err := r.db.QueryRow(ctx, `
		SELECT u.id::text, u.email, u.display_name, u.role::text, s.csrf_token_hash
		FROM ride_hailing.sessions s
		JOIN ride_hailing.users u ON u.id = s.user_id
		WHERE s.token_hash = $1
		  AND s.revoked_at IS NULL
		  AND s.expires_at > now()`, tokenHash).Scan(
		&session.ID,
		&session.Email,
		&session.DisplayName,
		&session.Role,
		&session.CSRFTokenHash,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return StoredSession{}, ErrSessionNotFound
	}
	if err != nil {
		return StoredSession{}, err
	}
	return session, nil
}

func (r *PostgresRepository) RevokeSession(ctx context.Context, tokenHash []byte) error {
	if r == nil || r.db == nil {
		return ErrStoreUnavailable
	}
	_, err := r.db.Exec(ctx, `
		UPDATE ride_hailing.sessions
		SET revoked_at = now()
		WHERE token_hash = $1 AND revoked_at IS NULL`, tokenHash)
	return err
}
