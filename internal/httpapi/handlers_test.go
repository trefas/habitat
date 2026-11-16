package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"habitat/internal/config"
	"habitat/internal/habitat"
	"habitat/internal/storage/postgres"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5/pgxpool"
)

// setupTestDB инициализирует подключение к базе данных для тестов.
// Если БД недоступна — тест пропускается (для CI/CD).
func setupTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()

	cfg, err := config.Load()
	if err != nil {
		t.Skipf("Skipping test: failed to load config: %v", err)
	}
	dsn := cfg.DSN()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Skipf("Skipping test: database not available: %v", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("Skipping test: database ping failed: %v", err)
	}

	// Применяем миграции
	m, err := migrate.New("file://../../migrations", dsn)
	if err != nil {
		pool.Close()
		t.Skipf("Skipping test: failed to initialize migrate: %v", err)
	}
	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		pool.Close()
		t.Skipf("Skipping test: failed to run migrations: %v", err)
	}

	// Очищаем таблицу перед тестами
	_, err = pool.Exec(context.Background(), "DELETE FROM habitats")
	if err != nil {
		pool.Close()
		t.Fatalf("Unable to clear test data: %v", err)
	}

	return pool
}

// TestDBStatusHandler проверяет эндпоинт /status
func TestDBStatusHandler(t *testing.T) {
	pool := setupTestDB(t)
	defer pool.Close()

	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	rec := httptest.NewRecorder()

	handler := DBStatusHandler(pool)
	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rec.Code)
	}

	expected := "Database connection is successful!"
	if rec.Body.String() != expected {
		t.Errorf("Expected body '%s', got '%s'", expected, rec.Body.String())
	}
}

// TestGetHabitatsHandler проверяет эндпоинт /habitats
func TestGetHabitatsHandler(t *testing.T) {
	pool := setupTestDB(t)
	defer pool.Close()

	_, err := pool.Exec(context.Background(), "INSERT INTO habitats (name) VALUES ($1)", "Тестовый биом")
	if err != nil {
		t.Fatalf("Failed to insert test data: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/habitats", nil)
	rec := httptest.NewRecorder()

	store := postgres.New(pool)
	handler := GetHabitatsHandler(store)
	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rec.Code)
	}

	var habitats []habitat.Habitat
	err = json.Unmarshal(rec.Body.Bytes(), &habitats)
	if err != nil {
		t.Errorf("Failed to parse JSON response: %v", err)
	}

	if len(habitats) != 1 {
		t.Errorf("Expected 1 habitat, got %d", len(habitats))
	}

	if habitats[0].Name != "Тестовый биом" {
		t.Errorf("Expected name 'Тестовый биом', got '%s'", habitats[0].Name)
	}
}
