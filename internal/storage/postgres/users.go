package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"habitat/internal/domain"
)

// userColumns — единый список колонок для чтения пользователя.
const userColumns = `id::text, email, password_hash, display_name, role, status,
	created_at, updated_at, last_login_at`

// scanUser читает пользователя из строки результата.
func scanUser(row pgx.Row, u *domain.User) error {
	return row.Scan(&u.ID, &u.Email, &u.PasswordHash, &u.DisplayName, &u.Role,
		&u.Status, &u.CreatedAt, &u.UpdatedAt, &u.LastLoginAt)
}

// CreateUser создает нового пользователя. Возвращает domain.ErrEmailTaken, если email уже занят.
func (s *Storage) CreateUser(ctx context.Context, u *domain.User) error {
	// Нормализуем email, чтобы удовлетворить CHECK (email = lower(email)) в БД
	email := strings.ToLower(u.Email)

	const q = `
		INSERT INTO users (email, password_hash, display_name)
		VALUES ($1, $2, $3)
		RETURNING ` + userColumns

	err := scanUser(s.pool.QueryRow(ctx, q, email, u.PasswordHash, u.DisplayName), u)

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
		SELECT ` + userColumns + `
		FROM users 
		WHERE email = $1`

	var u domain.User
	if err := scanUser(s.pool.QueryRow(ctx, q, email), &u); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	return &u, nil
}

// UserByID ищет пользователя по UUID.
func (s *Storage) UserByID(ctx context.Context, id string) (*domain.User, error) {
	const q = `
		SELECT ` + userColumns + `
		FROM users 
		WHERE id = $1::uuid`

	var u domain.User
	if err := scanUser(s.pool.QueryRow(ctx, q, id), &u); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	return &u, nil
}

// ListUsers возвращает страницу пользователей и общее количество подходящих записей.
func (s *Storage) ListUsers(ctx context.Context, f domain.UserFilter) ([]domain.User, int, error) {
	const where = `
		WHERE ($1 = '' OR email ILIKE '%' || $1 || '%' OR display_name ILIKE '%' || $1 || '%')
		  AND ($2 = '' OR role = $2)
		  AND ($3 = '' OR status = $3)`

	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*)::int FROM users`+where,
		f.Search, f.Role, f.Status).Scan(&total); err != nil {
		return nil, 0, err
	}

	const q = `
		SELECT ` + userColumns + `
		FROM users` + where + `
		ORDER BY created_at DESC, id
		LIMIT $4 OFFSET $5`

	rows, err := s.pool.Query(ctx, q, f.Search, f.Role, f.Status, f.Limit, f.Offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	users := make([]domain.User, 0, f.Limit)
	for rows.Next() {
		var u domain.User
		if err := scanUser(rows, &u); err != nil {
			return nil, 0, err
		}
		users = append(users, u)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return users, total, nil
}

// UpdateUser применяет частичное обновление и возвращает актуальную запись.
func (s *Storage) UpdateUser(ctx context.Context, id string, p domain.UserPatch) (*domain.User, error) {
	sets := make([]string, 0, 3)
	args := make([]any, 0, 4)
	arg := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}

	if p.DisplayName != nil {
		sets = append(sets, "display_name = "+arg(*p.DisplayName))
	}
	if p.Role != nil {
		sets = append(sets, "role = "+arg(*p.Role))
	}
	if p.Status != nil {
		sets = append(sets, "status = "+arg(*p.Status))
	}
	if len(sets) == 0 {
		return s.UserByID(ctx, id)
	}

	// updated_at всё равно перезапишет триггер users_set_updated_at.
	q := fmt.Sprintf(`UPDATE users SET %s WHERE id = %s::uuid RETURNING %s`,
		strings.Join(sets, ", "), arg(id), userColumns)

	var u domain.User
	if err := scanUser(s.pool.QueryRow(ctx, q, args...), &u); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	return &u, nil
}

// DeleteUser удаляет пользователя. Refresh-токены удаляются каскадно.
func (s *Storage) DeleteUser(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM users WHERE id = $1::uuid`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// CountActiveAdmins возвращает количество активных администраторов.
// Используется, чтобы не оставить систему без администратора.
func (s *Storage) CountActiveAdmins(ctx context.Context) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx,
		`SELECT count(*)::int FROM users WHERE role = 'admin' AND status = 'active'`).Scan(&n)
	return n, err
}

// TouchLastLogin обновляет время последнего входа.
func (s *Storage) TouchLastLogin(ctx context.Context, id string) error {
	const q = `UPDATE users SET last_login_at = now() WHERE id = $1::uuid`
	_, err := s.pool.Exec(ctx, q, id)
	return err
}

// UpdatePassword обновляет хеш пароля пользователя.
func (s *Storage) UpdatePassword(ctx context.Context, id, newHash string) error {
	const q = `UPDATE users SET password_hash = $2, updated_at = now() WHERE id = $1::uuid`
	_, err := s.pool.Exec(ctx, q, id, newHash)
	return err
}

// RevokeAllUserRefreshTokens отзывает все активные refresh-токены пользователя.
// Вызывается при смене пароля или блокировке аккаунта.
func (s *Storage) RevokeAllUserRefreshTokens(ctx context.Context, userID string) error {
	const q = `
		UPDATE refresh_tokens 
		SET revoked_at = now() 
		WHERE user_id = $1::uuid AND revoked_at IS NULL`
	_, err := s.pool.Exec(ctx, q, userID)
	return err
}
