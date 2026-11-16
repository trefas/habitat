package postgres

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"habitat/internal/domain"
)

func setupRefreshTestDB(t *testing.T) *Storage {
	t.Helper()
	dsn := os.Getenv("TEST_DSN")
	if dsn == "" {
		t.Skip("TEST_DSN не задан. Пропускаем интеграционный тест.")
	}

	m, err := migrate.New("file://../../../migrations", dsn)
	require.NoError(t, err)
	defer m.Close()

	err = m.Up()
	if err != nil && !errors.Is(err, migrate.ErrNoChange) {
		t.Fatalf("Не удалось применить миграции: %v", err)
	}

	pool, err := pgxpool.New(t.Context(), dsn)
	require.NoError(t, err)

	// Гарантированная очистка ПЕРЕД тестом
	_, err = pool.Exec(t.Context(), "TRUNCATE TABLE refresh_tokens, users CASCADE")
	require.NoError(t, err)

	t.Cleanup(func() {
		pool.Close()
		_, _ = pool.Exec(t.Context(), "TRUNCATE TABLE refresh_tokens, users CASCADE")
	})

	return New(pool)
}

func hashToken(s string) []byte {
	h := sha256.Sum256([]byte(s))
	return h[:]
}

func TestRotateRefreshToken_ReuseRevokesFamily(t *testing.T) {
	store := setupRefreshTestDB(t)
	ctx := t.Context()
	meta := domain.SessionMeta{UserAgent: "test", IP: "127.0.0.1"}
	exp := time.Now().Add(time.Hour)

	// Создаём пользователя
	u := &domain.User{
		Email:        fmt.Sprintf("reuse-%d@test.local", time.Now().UnixNano()),
		PasswordHash: "x",
		DisplayName:  "Reuse Test",
	}
	require.NoError(t, store.CreateUser(ctx, u))

	// Создаём первый refresh-токен
	h1 := hashToken("token-1")
	require.NoError(t, store.CreateRefreshToken(ctx, u.ID, h1, exp, meta))

	// Первая ротация: старый → новый. Должна пройти.
	h2 := hashToken("token-2")
	userID, role, err := store.RotateRefreshToken(ctx, h1, h2, exp, meta)
	require.NoError(t, err, "первая ротация должна пройти")
	require.Equal(t, u.ID, userID)
	require.Equal(t, "user", role)

	// Переиспользование СТАРОГО токена (имитация кражи)
	// Ожидаем ErrRefreshReused и отзыв ВСЕЙ семьи.
	_, _, err = store.RotateRefreshToken(ctx, h1, h2, exp, meta)
	require.ErrorIs(t, err, domain.ErrRefreshReused,
		"повторное использование отозванного токена должно вернуть ErrRefreshReused")

	// Проверяем, что вся семья погашена
	var active int
	err = store.pool.QueryRow(ctx,
		`SELECT count(*) FROM refresh_tokens WHERE user_id = $1::uuid AND revoked_at IS NULL`,
		u.ID).Scan(&active)
	require.NoError(t, err)
	require.Equal(t, 0, active, "вся семья должна быть погашена после reuse detection")
}

func TestRotateRefreshToken_ExpiredToken(t *testing.T) {
	store := setupRefreshTestDB(t)
	ctx := t.Context()
	meta := domain.SessionMeta{UserAgent: "test", IP: "127.0.0.1"}

	u := &domain.User{
		Email:        fmt.Sprintf("exp-%d@test.local", time.Now().UnixNano()),
		PasswordHash: "x",
		DisplayName:  "Expired Test",
	}
	require.NoError(t, store.CreateUser(ctx, u))

	// Создаём токен, который уже истёк
	h1 := hashToken("expired-token")
	pastExp := time.Now().Add(-time.Hour)
	require.NoError(t, store.CreateRefreshToken(ctx, u.ID, h1, pastExp, meta))

	// Пытаемся его обновить — должно вернуть ErrRefreshExpired
	h2 := hashToken("new-token")
	_, _, err := store.RotateRefreshToken(ctx, h1, h2, time.Now().Add(time.Hour), meta)
	require.ErrorIs(t, err, domain.ErrRefreshExpired)
}

func TestRevokeFamilyByHash(t *testing.T) {
	store := setupRefreshTestDB(t)
	ctx := t.Context()
	meta := domain.SessionMeta{UserAgent: "test", IP: "127.0.0.1"}
	exp := time.Now().Add(time.Hour)

	u := &domain.User{
		Email:        fmt.Sprintf("revoke-%d@test.local", time.Now().UnixNano()),
		PasswordHash: "x",
		DisplayName:  "Revoke Test",
	}
	require.NoError(t, store.CreateUser(ctx, u))

	// 1. Создаём первый токен (Семья 1)
	h1 := hashToken("t1")
	require.NoError(t, store.CreateRefreshToken(ctx, u.ID, h1, exp, meta))

	// 2. Ротируем его. Теперь t1 отозван, а t2 активен (та же Семья 1)
	h2 := hashToken("t2")
	_, _, err := store.RotateRefreshToken(ctx, h1, h2, exp, meta)
	require.NoError(t, err)

	// Проверяем, что t2 действительно активен перед отзывом
	var activeBefore int
	err = store.pool.QueryRow(ctx,
		`SELECT count(*) FROM refresh_tokens WHERE user_id = $1::uuid AND revoked_at IS NULL`, u.ID).Scan(&activeBefore)
	require.NoError(t, err)
	require.Equal(t, 1, activeBefore, "должен быть 1 активный токен (t2)")

	// 3. Отзываем всю семью по хешу первого токена
	require.NoError(t, store.RevokeFamilyByHash(ctx, h1))

	// 4. Проверяем, что ВСЕ токены этой семьи (в данном случае t2) теперь отозваны
	var activeAfter int
	err = store.pool.QueryRow(ctx,
		`SELECT count(*) FROM refresh_tokens WHERE user_id = $1::uuid AND revoked_at IS NULL`, u.ID).Scan(&activeAfter)
	require.NoError(t, err)
	require.Equal(t, 0, activeAfter, "все токены семьи должны быть отозваны")
}
