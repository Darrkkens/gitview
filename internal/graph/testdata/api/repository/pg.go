package repository

import (
	"context"

	"example.com/api/services"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PG struct{ pool *pgxpool.Pool }

func NewPG(p *pgxpool.Pool) *PG { return &PG{pool: p} }

func (r *PG) Insert(ctx context.Context, name, email string) (services.User, error) {
	var u services.User
	err := r.pool.QueryRow(ctx, "INSERT INTO users (name, email) VALUES ($1, $2) RETURNING id", name, email).Scan(&u.ID)
	return u, err
}

func (r *PG) Find(ctx context.Context, id string) (services.User, error) {
	var u services.User
	err := r.pool.QueryRow(ctx, "SELECT id, name, email FROM users WHERE id = $1", id).Scan(&u.ID, &u.Name, &u.Email)
	return u, err
}

func (r *PG) List(ctx context.Context, offset, limit int) []services.User {
	r.pool.Query(ctx, "SELECT id, name FROM users ORDER BY name LIMIT $1 OFFSET $2", limit, offset)
	return nil
}
