package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Habitat описывает структуру нашей записи для JSON
type Habitat struct {
	ID        int       `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

func getHabitatsHandler(w http.ResponseWriter, r *http.Request) {
	// Запрашиваем все записи из таблицы
	rows, err := dbPool.Query(context.Background(), "SELECT id, name, created_at FROM habitats ORDER BY id")
	if err != nil {
		http.Error(w, "Failed to query database: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var habitats []Habitat
	for rows.Next() {
		var h Habitat
		if err := rows.Scan(&h.ID, &h.Name, &h.CreatedAt); err != nil {
			http.Error(w, "Failed to scan row: "+err.Error(), http.StatusInternalServerError)
			return
		}
		habitats = append(habitats, h)
	}

	// Возвращаем результат в формате JSON
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(habitats)
}

var dbPool *pgxpool.Pool

// readSecret читает пароль из файла секрета
func readSecret(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

// getEnv получает переменную окружения или возвращает значение по умолчанию
func getEnv(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultValue
}

func dbStatusHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := dbPool.Ping(ctx); err != nil {
		http.Error(w, "Database connection failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	fmt.Fprint(w, "Database connection is successful!")
}

func initDBHandler(w http.ResponseWriter, r *http.Request) {
	// 1. Создаем таблицу, если её нет
	createTableQuery := `
		CREATE TABLE IF NOT EXISTS habitats (
			id SERIAL PRIMARY KEY,
			name VARCHAR(100) NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`

	_, err := dbPool.Exec(context.Background(), createTableQuery)
	if err != nil {
		http.Error(w, "Failed to create table: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// 2. Вставляем тестовую запись
	insertQuery := `INSERT INTO habitats (name) VALUES ($1) RETURNING id`
	var newID int
	err = dbPool.QueryRow(context.Background(), insertQuery, "Тестовый биом").Scan(&newID)
	if err != nil {
		http.Error(w, "Failed to insert data: "+err.Error(), http.StatusInternalServerError)
		return
	}

	fmt.Fprintf(w, "Таблица создана, добавлена запись с ID: %d", newID)
}

func main() {
	// 1. Чтение конфигурации подключения
	dbHost := getEnv("DB_HOST", "localhost")
	dbUser := getEnv("DB_USER", "habitat")
	dbName := getEnv("DB_NAME", "habitat")
	dbPort := getEnv("DB_PORT", "5432")

	// 2. Чтение пароля из секрета (если файл существует)
	dbPassword := getEnv("DB_PASSWORD", "")
	if secretPath := getEnv("DB_PASSWORD_FILE", ""); secretPath != "" {
		if pwd, err := readSecret(secretPath); err == nil {
			dbPassword = pwd
		} else {
			log.Printf("Warning: could not read secret file: %v", err)
		}
	}

	// 3. Формирование строки подключения (DSN)
	dsn := fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable", dbUser, dbPassword, dbHost, dbPort, dbName)

	// 4. Создание пула соединений
	var err error
	dbPool, err = pgxpool.New(context.Background(), dsn)
	if err != nil {
		log.Fatalf("Unable to create connection pool: %v", err)
	}
	defer dbPool.Close()

	// 5. Проверка соединения при старте
	if err := dbPool.Ping(context.Background()); err != nil {
		log.Fatalf("Unable to ping database: %v", err)
	}
	log.Println("Успешно подключено к базе данных!")

	// 6. Регистрация обработчиков
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "Hello from Habitat API!")
	})
	http.HandleFunc("/status", dbStatusHandler)
	http.HandleFunc("/init", initDBHandler)
	http.HandleFunc("/habitats", getHabitatsHandler)

	log.Println("Сервер запущен на порту 8080...")
	if err := http.ListenAndServe(":8080", nil); err != nil {
		log.Fatal(err)
	}
}
