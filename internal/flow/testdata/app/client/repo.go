package client

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

type PGRepo struct{ pool *pgxpool.Pool }

func (r *PGRepo) Find(ctx context.Context, id int) (*Client, error) {
	var c Client
	err := r.pool.QueryRow(ctx, "SELECT id, name FROM clients WHERE id = $1", id).Scan(&c.ID, &c.Name)
	return &c, err
}

func (r *PGRepo) Search(ctx context.Context, name string) error {
	q := "SELECT id FROM clients WHERE name = '" + name + "'"
	_, err := r.pool.Query(ctx, q)
	return err
}

func (r *PGRepo) Rename(ctx context.Context, id int, name string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, "UPDATE clients SET name = $1 WHERE id = $2", name, id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
