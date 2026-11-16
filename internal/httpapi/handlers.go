package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"habitat/internal/habitat"

	"github.com/jackc/pgx/v5/pgxpool"
)

// DBStatusHandler возвращает хендлер для проверки статуса БД
func DBStatusHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := pool.Ping(ctx); err != nil {
			http.Error(w, "Database connection failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
		fmt.Fprint(w, "Database connection is successful!")
	}
}

// GetHabitatsHandler возвращает хендлер для получения списка habitats
func GetHabitatsHandler(store habitat.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		habitats, err := store.List(r.Context())
		if err != nil {
			http.Error(w, "Failed to query database: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(habitats)
	}
}
