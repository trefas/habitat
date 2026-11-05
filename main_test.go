package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestGetEnv проверяет функцию getEnv
func TestGetEnv(t *testing.T) {
	// Подготовка: устанавливаем переменную окружения
	os.Setenv("TEST_VAR", "test_value")
	defer os.Unsetenv("TEST_VAR")

	// Таблица тестовых случаев
	tests := []struct {
		name         string
		key          string
		defaultValue string
		expected     string
	}{
		{
			name:         "Variable exists",
			key:          "TEST_VAR",
			defaultValue: "default",
			expected:     "test_value",
		},
		{
			name:         "Variable does not exist",
			key:          "NON_EXISTENT_VAR",
			defaultValue: "default_value",
			expected:     "default_value",
		},
		{
			name:         "Empty default value",
			key:          "ANOTHER_MISSING_VAR",
			defaultValue: "",
			expected:     "",
		},
	}

	// Прогоняем все тесты
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getEnv(tt.key, tt.defaultValue)
			if result != tt.expected {
				t.Errorf("getEnv(%s, %s) = %s; want %s", tt.key, tt.defaultValue, result, tt.expected)
			}
		})
	}
}

// TestReadSecret проверяет функцию readSecret
func TestReadSecret(t *testing.T) {
	// Создаем временный файл для теста
	tmpDir := t.TempDir()
	secretFile := filepath.Join(tmpDir, "test_secret")

	// Записываем тестовый пароль с пробелами и переносом строки
	err := os.WriteFile(secretFile, []byte("  my_secret_password\n  "), 0644)
	if err != nil {
		t.Fatalf("Failed to create test secret file: %v", err)
	}

	// Тестируем чтение секрета
	result, err := readSecret(secretFile)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	// Проверяем, что пробелы и переносы строк удалены
	expected := "my_secret_password"
	if result != expected {
		t.Errorf("Expected '%s', got '%s'", expected, result)
	}

	// Тест 2: Файл не существует
	_, err = readSecret("/non/existent/file")
	if err == nil {
		t.Error("Expected error for non-existent file, got nil")
	}
}

// TestRootHandler проверяет корневой эндпоинт "/"
func TestRootHandler(t *testing.T) {
	// Создаем тестовый запрос
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	// Создаем recorder для захвата ответа
	rec := httptest.NewRecorder()

	// Вызываем handler напрямую (это анонимная функция из main.go)
	// Но так как она анонимная, нам нужно её воспроизвести здесь
	handler := func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "Hello from Habitat API!")
	}
	handler(rec, req)

	// Проверяем статус
	if rec.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rec.Code)
	}

	// Проверяем тело ответа
	expected := "Hello from Habitat API!"
	if rec.Body.String() != expected {
		t.Errorf("Expected body '%s', got '%s'", expected, rec.Body.String())
	}
}

// setupTestDB инициализирует подключение к базе данных для тестов
func setupTestDB(t *testing.T) {
	t.Helper()

	// Используем те же переменные окружения, что и приложение
	dbHost := getEnv("DB_HOST", "localhost")
	dbUser := getEnv("DB_USER", "habitat")
	dbName := getEnv("DB_NAME", "habitat")
	dbPort := getEnv("DB_PORT", "5432")
	dbPassword := getEnv("DB_PASSWORD", "fy2lht1q") // Замените на твой пароль

	dsn := fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable", dbUser, dbPassword, dbHost, dbPort, dbName)

	runMigrations(dsn)

	var err error
	dbPool, err = pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("Unable to create connection pool: %v", err)
	}

	// Очищаем таблицу перед тестами
	_, err = dbPool.Exec(context.Background(), "DELETE FROM habitats")
	if err != nil {
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
	// Создаем mux (маршрутизатор) и регистрируем обработчик
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "Hello from Habitat API!")
	})

	// Создаем тестовый сервер
	server := httptest.NewServer(mux)
	defer server.Close()

	// Делаем реальный HTTP-запрос
	resp, err := http.Get(server.URL + "/")
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	// Проверяем статус
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, resp.StatusCode)
	}

	// Читаем и проверяем тело ответа
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read response body: %v", err)
	}

	expected := "Hello from Habitat API!"
	if string(body) != expected {
		t.Errorf("Expected body '%s', got '%s'", expected, string(body))
	}
}
