package repository

import (
	"context"
	"fmt"

	"example.com/deep/model"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const findByEmailSQL = `SELECT id, name, email FROM clients WHERE email = $1`

// ClientRepository stores clients in PostgreSQL through pgxpool.
type ClientRepository struct{ pool *pgxpool.Pool }

func NewClientRepository(p *pgxpool.Pool) *ClientRepository { return &ClientRepository{pool: p} }

// FindByID loads one client with a literal query.
func (r *ClientRepository) FindByID(ctx context.Context, id int64) (*model.Client, error) {
	var c model.Client
	err := r.pool.QueryRow(ctx, "SELECT id, name FROM clients WHERE id = $1", id).Scan(&c.ID, &c.Name)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// FindByEmail uses a SQL constant.
func (r *ClientRepository) FindByEmail(ctx context.Context, email string) (*model.Client, error) {
	var c model.Client
	if err := r.pool.QueryRow(ctx, findByEmailSQL, email).Scan(&c.ID, &c.Name, &c.Email); err != nil {
		return nil, err
	}
	return &c, nil
}

// Search builds SQL by concatenation (dynamic SQL).
func (r *ClientRepository) Search(ctx context.Context, name string) ([]model.Client, error) {
	q := "SELECT id, name FROM clients WHERE name LIKE '%" + name + "%'"
	rows, err := r.pool.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Client
	for rows.Next() {
		var c model.Client
		if err := rows.Scan(&c.ID, &c.Name); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ListByStatus formats the ORDER BY clause at runtime.
func (r *ClientRepository) ListByStatus(ctx context.Context, status, order string) (pgx.Rows, error) {
	q := fmt.Sprintf("SELECT id, name FROM clients WHERE status = $1 ORDER BY %s", order)
	return r.pool.Query(ctx, q, status)
}

// Count picks one of two literal queries.
func (r *ClientRepository) Count(ctx context.Context, onlyActive bool) (int64, error) {
	q := "SELECT count(*) FROM clients"
	if onlyActive {
		q = "SELECT count(*) FROM clients WHERE status = 'active'"
	}
	var n int64
	err := r.pool.QueryRow(ctx, q).Scan(&n)
	return n, err
}

// Rename updates a client inside a transaction.
func (r *ClientRepository) Rename(ctx context.Context, id int64, name string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, "UPDATE clients SET name = $1 WHERE id = $2", name, id); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, "INSERT INTO audit (client_id, action) VALUES ($1, 'rename')", id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Mismatch passes fewer arguments than the SQL has placeholders.
func (r *ClientRepository) Mismatch(ctx context.Context, id int64) error {
	_, err := r.pool.Exec(ctx, "DELETE FROM clients WHERE id = $1 AND status = $2", id)
	return err
}

// QueueAll sends a batch.
func (r *ClientRepository) QueueAll(ctx context.Context, ids []int64) error {
	b := &pgx.Batch{}
	for _, id := range ids {
		b.Queue("UPDATE clients SET status = 'seen' WHERE id = $1", id)
	}
	return r.pool.SendBatch(ctx, b).Close()
}
