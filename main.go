package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5/pgxpool"
)

var dbPool *pgxpool.Pool

func readSecret(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

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

type Habitat struct {
	ID        int       `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

func getHabitatsHandler(w http.ResponseWriter, r *http.Request) {
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

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(habitats)
}

func runMigrations(dsn string) {
	m, err := migrate.New("file://migrations", dsn)
	if err != nil {
		log.Fatalf("Failed to initialize migrate: %v", err)
	}

	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		log.Fatalf("Failed to run migrations: %v", err)
	}

	log.Println("Миграции успешно применены!")
}

func main() {
	dbHost := getEnv("DB_HOST", "localhost")
	dbUser := getEnv("DB_USER", "habitat")
	dbName := getEnv("DB_NAME", "habitat")
	dbPort := getEnv("DB_PORT", "5432")

	dbPassword := getEnv("DB_PASSWORD", "")
	if secretPath := getEnv("DB_PASSWORD_FILE", ""); secretPath != "" {
		if pwd, err := readSecret(secretPath); err == nil {
			dbPassword = pwd
		} else {
			log.Printf("Warning: could not read secret file: %v", err)
		}
	}

	dsn := fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable", dbUser, dbPassword, dbHost, dbPort, dbName)

	runMigrations(dsn)

	var err error
	dbPool, err = pgxpool.New(context.Background(), dsn)
	if err != nil {
		log.Fatalf("Unable to create connection pool: %v", err)
	}
	defer dbPool.Close()

	if err := dbPool.Ping(context.Background()); err != nil {
		log.Fatalf("Unable to ping database: %v", err)
	}
	log.Println("Успешно подключено к базе данных!")

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "Hello from Habitat API!")
	})
	mux.HandleFunc("/status", dbStatusHandler)
	mux.HandleFunc("/habitats", getHabitatsHandler)

	srv := &http.Server{
		Addr:         ":8080",
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		log.Println("Сервер запущен на порту 8080...")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP server error: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("Получен сигнал завершения, начинаем graceful shutdown...")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("Server shutdown failed: %v", err)
	}
	log.Println("Сервер корректно остановлен")
}