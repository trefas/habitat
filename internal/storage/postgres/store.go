package postgres

import (
	"context"

	"habitat/internal/habitat"

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

// List возвращает все habitats
func (s *Store) List(ctx context.Context) ([]habitat.Habitat, error) {
	rows, err := s.pool.Query(ctx, "SELECT id, name, created_at FROM habitats ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var habitats []habitat.Habitat
	for rows.Next() {
		var h habitat.Habitat
		if err := rows.Scan(&h.ID, &h.Name, &h.CreatedAt); err != nil {
			return nil, err
		}
		habitats = append(habitats, h)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return habitats, nil
}
