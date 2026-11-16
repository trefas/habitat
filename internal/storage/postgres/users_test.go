package postgres

import (
	"context"
	"errors"
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

func setupTestDB(t *testing.T) *Storage {
	t.Helper()
	dsn := os.Getenv("TEST_DSN")
	if dsn == "" {
		t.Skip("TEST_DSN не задан. Пропускаем интеграционный тест.")
	}

	// Применяем миграции
	m, err := migrate.New("file://../../../migrations", dsn)
	require.NoError(t, err)
	defer m.Close()

	err = m.Up()
	if err != nil && !errors.Is(err, migrate.ErrNoChange) {
		t.Fatalf("Не удалось применить миграции: %v", err)
	}

	// Создаем пул
	pool, err := pgxpool.New(t.Context(), dsn)
	require.NoError(t, err)

	// ГАРАНТИРОВАННАЯ очистка таблицы ПЕРЕД тестом.
	// Это критично, так как мы используем ту же БД, что и приложение.
	_, err = pool.Exec(t.Context(), "TRUNCATE TABLE users CASCADE")
	require.NoError(t, err, "Не удалось очистить таблицу users перед тестом")

	t.Cleanup(func() {
		pool.Close()
		// И после теста на всякий случай
		_, _ = pool.Exec(context.Background(), "TRUNCATE TABLE users CASCADE")
	})

	return New(pool)
}

func TestCreateUser_EmailCaseInsensitive(t *testing.T) {
	store := setupTestDB(t)
	ctx := t.Context()

	// 1. Создаем пользователя с email в верхнем регистре
	u1 := &domain.User{
		Email:        "Test@Example.com",
		PasswordHash: "hash1",
		DisplayName:  "Test User",
	}
	err := store.CreateUser(ctx, u1)
	require.NoError(t, err)
	require.NotEmpty(t, u1.ID)
	require.Equal(t, "user", u1.Role)

	// 2. Пытаемся создать пользователя с тем же email, но в нижнем регистре
	u2 := &domain.User{
		Email:        "test@example.com",
		PasswordHash: "hash2",
		DisplayName:  "Another User",
	}
	err = store.CreateUser(ctx, u2)

	// Ожидаем именно эту доменную ошибку
	require.ErrorIs(t, err, domain.ErrEmailTaken)
}

func TestUserByEmail_NotFound(t *testing.T) {
	store := setupTestDB(t)
	ctx := t.Context()

	_, err := store.UserByEmail(ctx, "nonexistent@example.com")
	require.ErrorIs(t, err, domain.ErrNotFound)
}

func TestTouchLastLogin(t *testing.T) {
	store := setupTestDB(t)
	ctx := t.Context()

	u := &domain.User{
		Email:        "login-test@example.com",
		PasswordHash: "hash",
		DisplayName:  "Login User",
	}
	require.NoError(t, store.CreateUser(ctx, u))

	// Проверяем, что last_login_at был null
	var lastLogin *time.Time
	err := store.pool.QueryRow(ctx, "SELECT last_login_at FROM users WHERE id = $1::uuid", u.ID).Scan(&lastLogin)
	require.NoError(t, err)
	require.Nil(t, lastLogin)

	// Обновляем
	require.NoError(t, store.TouchLastLogin(ctx, u.ID))

	// Проверяем, что last_login_at обновился
	err = store.pool.QueryRow(ctx, "SELECT last_login_at FROM users WHERE id = $1::uuid", u.ID).Scan(&lastLogin)
	require.NoError(t, err)
	require.NotNil(t, lastLogin)
	require.WithinDuration(t, time.Now(), *lastLogin, 2*time.Second)
}
