package postgres

import (
	"context"
	"errors"
	"strings" // <-- Добавили этот импорт

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"habitat/internal/domain"
)

// CreateUser создает нового пользователя. Возвращает domain.ErrEmailTaken, если email уже занят.
func (s *Storage) CreateUser(ctx context.Context, u *domain.User) error {
	// Нормализуем email, чтобы удовлетворить CHECK (email = lower(email)) в БД
	email := strings.ToLower(u.Email)

	const q = `
		INSERT INTO users (email, password_hash, display_name)
		VALUES ($1, $2, $3)
		RETURNING id::text, role, status, created_at, updated_at`

	err := s.pool.QueryRow(ctx, q, email, u.PasswordHash, u.DisplayName).
		Scan(&u.ID, &u.Role, &u.Status, &u.CreatedAt, &u.UpdatedAt)

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" { // unique_violation
		return domain.ErrEmailTaken
	}
	return err
}

// UserByEmail ищет пользователя по email (регистронезависимо).
func (s *Storage) UserByEmail(ctx context.Context, email string) (*domain.User, error) {
	// Нормализуем email для поиска, чтобы он гарантированно совпал с тем, что в БД
	email = strings.ToLower(email)

	const q = `
		SELECT id::text, email, password_hash, display_name, role, status, created_at, updated_at
		FROM users 
		WHERE email = $1`

	var u domain.User
	err := s.pool.QueryRow(ctx, q, email).Scan(
		&u.ID, &u.Email, &u.PasswordHash, &u.DisplayName, &u.Role, &u.Status, &u.CreatedAt, &u.UpdatedAt)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// UserByID ищет пользователя по UUID.
func (s *Storage) UserByID(ctx context.Context, id string) (*domain.User, error) {
	const q = `
		SELECT id::text, email, password_hash, display_name, role, status, created_at, updated_at
		FROM users 
		WHERE id = $1::uuid`

	var u domain.User
	err := s.pool.QueryRow(ctx, q, id).Scan(
		&u.ID, &u.Email, &u.PasswordHash, &u.DisplayName, &u.Role, &u.Status, &u.CreatedAt, &u.UpdatedAt)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// TouchLastLogin обновляет время последнего входа.
func (s *Storage) TouchLastLogin(ctx context.Context, id string) error {
	const q = `UPDATE users SET last_login_at = now() WHERE id = $1::uuid`
	_, err := s.pool.Exec(ctx, q, id)
	return err
}
