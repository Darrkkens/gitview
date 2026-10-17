package repository

import (
	"context"
	"database/sql"
)

// AuditRepository uses database/sql.
type AuditRepository struct{ db *sql.DB }

func (a *AuditRepository) Log(ctx context.Context, clientID int64, action string) error {
	_, err := a.db.ExecContext(ctx, "INSERT INTO audit (client_id, action) VALUES ($1, $2)", clientID, action)
	return err
}

func (a *AuditRepository) Count(ctx context.Context) (int, error) {
	var n int
	err := a.db.QueryRowContext(ctx, "SELECT count(*) FROM audit").Scan(&n)
	return n, err
}

// Purge runs inside a database/sql transaction.
func (a *AuditRepository) Purge(ctx context.Context, before string) error {
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM audit WHERE created_at < $1", before); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}
