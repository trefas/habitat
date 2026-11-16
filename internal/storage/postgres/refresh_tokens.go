package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"habitat/internal/domain"
)

// CreateRefreshToken создаёт новый refresh-токен в БД.
func (s *Storage) CreateRefreshToken(ctx context.Context, userID string, hash []byte,
	expiresAt time.Time, meta domain.SessionMeta) error {

	const q = `
		INSERT INTO refresh_tokens (user_id, token_hash, expires_at, user_agent, ip)
		VALUES ($1::uuid, $2, $3, $4, NULLIF($5, '')::inet)`
	_, err := s.pool.Exec(ctx, q, userID, hash, expiresAt, meta.UserAgent, meta.IP)
	return err
}

// RotateRefreshToken атомарно ротирует refresh-токен:
// 1. Находит старый по хешу (SELECT FOR UPDATE).
// 2. Если уже отозван — гасит всю семью (сигнал кражи).
// 3. Если истёк или пользователь заблокирован — отказывает.
// 4. Создаёт новый токен в той же семье, старый помечает revoked_at.
// Возвращает userID и role для нового access-токена.
func (s *Storage) RotateRefreshToken(ctx context.Context, oldHash, newHash []byte,
	expiresAt time.Time, meta domain.SessionMeta) (string, string, error) {

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", "", err
	}
	defer tx.Rollback(ctx)

	var (
		id, userID, familyID, role, status string
		expires                            time.Time
		revoked                            bool
	)
	const q = `
		SELECT rt.id::text, rt.user_id::text, rt.family_id::text, rt.expires_at,
		       rt.revoked_at IS NOT NULL, u.role, u.status
		FROM refresh_tokens rt
		JOIN users u ON u.id = rt.user_id
		WHERE rt.token_hash = $1
		FOR UPDATE OF rt`

	err = tx.QueryRow(ctx, q, oldHash).Scan(
		&id, &userID, &familyID, &expires, &revoked, &role, &status)

	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", domain.ErrInvalidRefresh
	}
	if err != nil {
		return "", "", err
	}

	// Переиспользование отозванного токена = вероятная кража → гасим всю семью.
	if revoked {
		if _, err := tx.Exec(ctx,
			`UPDATE refresh_tokens SET revoked_at = now()
			 WHERE family_id = $1::uuid AND revoked_at IS NULL`, familyID); err != nil {
			return "", "", err
		}
		if err := tx.Commit(ctx); err != nil {
			return "", "", err
		}
		return "", "", domain.ErrRefreshReused
	}
	if time.Now().After(expires) {
		return "", "", domain.ErrRefreshExpired
	}
	if status != "active" {
		return "", "", domain.ErrUserBlocked
	}

	var newID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO refresh_tokens (user_id, token_hash, family_id, expires_at, user_agent, ip)
		VALUES ($1::uuid, $2, $3::uuid, $4, $5, NULLIF($6, '')::inet)
		RETURNING id::text`,
		userID, newHash, familyID, expiresAt, meta.UserAgent, meta.IP).Scan(&newID); err != nil {
		return "", "", err
	}

	if _, err := tx.Exec(ctx,
		`UPDATE refresh_tokens SET revoked_at = now(), replaced_by = $2::uuid WHERE id = $1::uuid`,
		id, newID); err != nil {
		return "", "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", "", err
	}
	return userID, role, nil
}

// RevokeFamilyByHash отзывает все токены из семьи, к которой принадлежит указанный токен.
// Используется для logout и смены пароля. Идемпотентна.
func (s *Storage) RevokeFamilyByHash(ctx context.Context, hash []byte) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE refresh_tokens SET revoked_at = now()
		WHERE family_id = (SELECT family_id FROM refresh_tokens WHERE token_hash = $1)
		  AND revoked_at IS NULL`, hash)
	return err
}
