package repository

import (
	"context"
	"database/sql"

	"example.com/shop/internal/models"
)

// UsageRepository reads and writes usage in PostgreSQL.
type UsageRepository struct{ DB *sql.DB }

// FindUsage loads the usage of a company with its plan.
func (r *UsageRepository) FindUsage(ctx context.Context, companyID int64, period string) (*models.Usage, *models.Plan, error) {
	var u models.Usage
	var p models.Plan
	err := r.DB.QueryRowContext(ctx, `SELECT u.id, u.total, u.zero_rated, p.quota, p.price FROM usage u JOIN plans p ON p.id = u.plan_id WHERE u.company_id = $1 AND u.period = $2 ORDER BY u.created_at DESC LIMIT 1`, companyID, period).
		Scan(&u.ID, &u.Total, &u.ZeroRated, &p.Quota, &p.Price)
	if err != nil {
		return nil, nil, err
	}
	return &u, &p, nil
}

// SaveCharge stores a charge.
func (r *UsageRepository) SaveCharge(ctx context.Context, c models.Charge) error {
	_, err := r.DB.ExecContext(ctx, "INSERT INTO charges (usage_id, amount) VALUES ($1, $2)", c.UsageID, c.Amount)
	return err
}
