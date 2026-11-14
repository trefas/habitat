package habitat

import "time"

// Habitat представляет запись в таблице habitats
type Habitat struct {
	ID        int       `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}