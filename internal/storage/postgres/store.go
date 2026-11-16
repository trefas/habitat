package postgres

import (
	"github.com/jackc/pgx/v5/pgxpool"
)

// Storage представляет хранилище данных в PostgreSQL
type Storage struct {
	pool *pgxpool.Pool
}

// New создает новый экземпляр Storage
func New(pool *pgxpool.Pool) *Storage {
	return &Storage{pool: pool}
}
