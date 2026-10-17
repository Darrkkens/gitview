package store

import (
	"context"
	"database/sql"
)

type Store struct{ DB *sql.DB }

// ClientName loads a client's name.
func (s *Store) ClientName(ctx context.Context, id int64) (string, error) {
	var name string
	err := s.DB.QueryRowContext(ctx, "SELECT name FROM clients WHERE id = $1 AND status = 'active'", id).Scan(&name)
	return name, err
}
