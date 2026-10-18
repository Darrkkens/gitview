// Package repo reads and writes clients.
package repo

import (
	"context"
	"database/sql"
	"fmt"
)

type Repo struct{ DB *sql.DB }

type Client struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	PasswordHash string `json:"passwordHash"` // GO-DATA-003: returned by GetClient
	Internal     string `json:"-"`
}

// FindByID has no ownership or tenant condition.
func (r *Repo) FindByID(ctx context.Context, id string) (*Client, error) {
	var c Client
	err := r.DB.QueryRowContext(ctx, "SELECT id, name, password_hash FROM clients WHERE id = $1", id).Scan(&c.ID, &c.Name, &c.PasswordHash)
	return &c, err
}

// FindForCompany restricts the row to the caller's company.
func (r *Repo) FindForCompany(ctx context.Context, companyID, id string) (*Client, error) {
	var c Client
	err := r.DB.QueryRowContext(ctx, "SELECT id, name FROM clients WHERE id = $1 AND company_id = $2", id, companyID).Scan(&c.ID, &c.Name)
	return &c, err
}

// SearchByName concatenates the term into the SQL (GO-INJ-001).
func (r *Repo) SearchByName(ctx context.Context, term string) (*sql.Rows, error) {
	return r.DB.QueryContext(ctx, "SELECT id, name FROM clients WHERE name LIKE '%"+term+"%'")
}

// SafeSearch passes the term as a parameter.
func (r *Repo) SafeSearch(ctx context.Context, term string) (*sql.Rows, error) {
	return r.DB.QueryContext(ctx, "SELECT id, name FROM clients WHERE name LIKE $1", "%"+term+"%")
}

// Report formats a number, never text, into the SQL.
func (r *Repo) Report(ctx context.Context, id int) (*sql.Rows, error) {
	return r.DB.QueryContext(ctx, fmt.Sprintf("SELECT * FROM reports WHERE id = %d", id))
}

// Delete removes a client.
func (r *Repo) Delete(ctx context.Context, id string) error {
	_, err := r.DB.ExecContext(ctx, "DELETE FROM clients WHERE id = $1", id)
	return err
}

// Insert creates a client.
func (r *Repo) Insert(ctx context.Context, name string) error {
	_, err := r.DB.ExecContext(ctx, "INSERT INTO clients (name) VALUES ($1)", name)
	return err
}
