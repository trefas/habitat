package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"habitat/internal/config"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestRootHandler проверяет корневой эндпоинт "/"
func TestRootHandler(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	handler := func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "Hello from Habitat API!")
	}
	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rec.Code)
	}

	expected := "Hello from Habitat API!"
	if rec.Body.String() != expected {
		t.Errorf("Expected body '%s', got '%s'", expected, rec.Body.String())
	}
}

// setupTestDB инициализирует подключение к базе данных для тестов
// Если БД недоступна — тест пропускается (для CI/CD)
func setupTestDB(t *testing.T) {
	t.Helper()

	// Загружаем конфиг
	cfg, err := config.Load()
	if err != nil {
		t.Skipf("Skipping test: failed to load config: %v", err)
	}
	dsn := cfg.DSN()

	// Пробуем подключиться
	ctx, cancel := context.WithTimeout(context.Background(), 2*1e9) // 2 секунды
	defer cancel()

	dbPool, err = pgxpool.New(ctx, dsn)
	if err != nil {
		t.Skipf("Skipping test: database not available: %v", err)
	}

	// Проверяем, что БД действительно отвечает
	if err := dbPool.Ping(ctx); err != nil {
		dbPool.Close()
		t.Skipf("Skipping test: database ping failed: %v", err)
	}

	// Запускаем миграции
	runMigrations(dsn)

	// Очищаем таблицу перед тестами
	_, err = dbPool.Exec(context.Background(), "DELETE FROM habitats")
	if err != nil {
		dbPool.Close()
		t.Fatalf("Unable to clear test data: %v", err)
	}
}

// teardownTestDB закрывает подключение к базе данных
func teardownTestDB() {
	if dbPool != nil {
		dbPool.Close()
	}
}

// TestStatusHandler проверяет эндпоинт /status
func TestStatusHandler(t *testing.T) {
	setupTestDB(t)
	defer teardownTestDB()

	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	rec := httptest.NewRecorder()

	dbStatusHandler(rec, req)

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
	setupTestDB(t)
	defer teardownTestDB()

	// Вставляем тестовую запись
	_, err := dbPool.Exec(context.Background(), "INSERT INTO habitats (name) VALUES ($1)", "Тестовый биом")
	if err != nil {
		t.Fatalf("Failed to insert test data: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/habitats", nil)
	rec := httptest.NewRecorder()

	getHabitatsHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rec.Code)
	}

	// Проверяем, что ответ - валидный JSON
	var habitats []Habitat
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

// TestRootEndpoint тестирует корневой обработчик через httptest
func TestRootEndpoint(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "Hello from Habitat API!")
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	resp, err := http.Get(server.URL + "/")
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read response body: %v", err)
	}

	expected := "Hello from Habitat API!"
	if string(body) != expected {
		t.Errorf("Expected body '%s', got '%s'", expected, string(body))
	}
}
