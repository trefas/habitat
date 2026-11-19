package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"habitat/internal/auth"
	"habitat/internal/config"
	"habitat/internal/httpapi"
	"habitat/internal/messaging"
	"habitat/internal/notifications"
	"habitat/internal/storage/postgres"
	"habitat/internal/users"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5/pgxpool"
)

func runMigrations(dsn, path string) {
	m, err := migrate.New(path, dsn)
	if err != nil {
		log.Fatalf("Failed to initialize migrate: %v", err)
	}
	defer func() {
		if srcErr, dbErr := m.Close(); srcErr != nil || dbErr != nil {
			log.Printf("warn: close migrate: src=%v db=%v", srcErr, dbErr)
		}
	}()

	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		log.Fatalf("Failed to run migrations: %v", err)
	}
	log.Println("Миграции успешно применены!")
}

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	dsn := cfg.DSN()
	runMigrations(dsn, cfg.MigrationsPath)

	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		log.Fatalf("Unable to create connection pool: %v", err)
	}
	defer pool.Close()

	if err := pool.Ping(context.Background()); err != nil {
		log.Fatalf("Unable to ping database: %v", err)
	}
	log.Println("Успешно подключено к базе данных!")

	store := postgres.New(pool)

	// Инициализируем сервис аутентификации
	tm := auth.NewTokenManager(cfg.JWTSecret, cfg.AccessTokenTTL)
	hasher := auth.BcryptHasher{Cost: cfg.BcryptCost}
	authSvc := auth.NewService(store, hasher, tm, cfg.RefreshTokenTTL)
	userSvc := users.NewService(store)
	messageSvc := messaging.NewService(store)
	notificationSvc := notifications.NewService(store)

	mux := http.NewServeMux()

	// 1. Healthchecks
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "ok")
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := pool.Ping(ctx); err != nil {
			http.Error(w, "db unavailable", http.StatusServiceUnavailable)
			return
		}
		fmt.Fprint(w, "ready")
	})

	// 2. Публичные маршруты аутентификации (ОБРАТИТЕ ВНИМАНИЕ на пробел между методом и путём!)
	mux.HandleFunc("POST /api/v1/auth/register", httpapi.RegisterHandler(authSvc))
	mux.HandleFunc("POST /api/v1/auth/login", httpapi.LoginHandler(authSvc))
	mux.HandleFunc("POST /api/v1/auth/refresh", httpapi.RefreshHandler(authSvc))
	mux.HandleFunc("POST /api/v1/auth/logout", httpapi.LogoutHandler(authSvc))

	// 3. Защищённые маршруты
	authMW := httpapi.RequireAuth(tm)

	mux.Handle("GET /api/v1/me", authMW(httpapi.MeHandler(store)))
	mux.Handle("PATCH /api/v1/me", authMW(httpapi.UpdateMeHandler(userSvc)))
	mux.Handle("PATCH /api/v1/me/password", authMW(httpapi.ChangePasswordHandler(authSvc)))

	// 3.1. Сообщения
	mux.Handle("POST /api/v1/messages", authMW(httpapi.CreateMessageHandler(messageSvc)))
	mux.Handle("GET /api/v1/messages", authMW(httpapi.ListMessagesHandler(messageSvc)))
	mux.Handle("GET /api/v1/messages/{id}", authMW(httpapi.GetMessageHandler(messageSvc)))
	mux.Handle("POST /api/v1/messages/{id}/read", authMW(httpapi.MarkMessageReadHandler(messageSvc)))
	mux.Handle("GET /api/v1/messages/{id}/receipts", authMW(httpapi.ListMessageReceiptsHandler(messageSvc)))
	mux.Handle("POST /api/v1/messages/{id}/shares", authMW(httpapi.ShareMessageHandler(messageSvc)))
	mux.Handle("DELETE /api/v1/messages/{id}/shares/{granteeID}", authMW(httpapi.RevokeMessageShareHandler(messageSvc)))

	// 3.2. Входящие уведомления
	mux.Handle("GET /api/v1/notifications", authMW(httpapi.ListNotificationsHandler(notificationSvc)))
	mux.Handle("POST /api/v1/notifications/{id}/read", authMW(httpapi.MarkNotificationReadHandler(notificationSvc)))

	// 3.3. Административное управление пользователями
	adminOnly := func(h http.Handler) http.Handler {
		return authMW(httpapi.RequireRole("admin")(h))
	}
	mux.Handle("GET /api/v1/admin/users", adminOnly(httpapi.ListUsersHandler(userSvc)))
	mux.Handle("GET /api/v1/admin/users/{id}", adminOnly(httpapi.GetUserHandler(userSvc)))
	mux.Handle("PATCH /api/v1/admin/users/{id}", adminOnly(httpapi.UpdateUserHandler(userSvc)))
	mux.Handle("DELETE /api/v1/admin/users/{id}", adminOnly(httpapi.DeleteUserHandler(userSvc)))

	// 4. Catch-all для корня (строго "GET /", чтобы не перехватывать /api/v1/... в Go 1.22+)
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "Hello from Habitat API!")
	})

	srv := &http.Server{
		Addr:         cfg.HTTPAddr,
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		log.Printf("Сервер запущен на порту %s...", cfg.HTTPAddr)
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
