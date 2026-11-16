package postgres

import (
	"github.com/jackc/pgx/v5/pgxpool"
)

// Store реализует habitat.Store для PostgreSQL
type Store struct {
	pool *pgxpool.Pool
}

// New создаёт новый Store
func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}
