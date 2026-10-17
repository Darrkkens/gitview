package pgxpool

import (
	"context"

	"github.com/jackc/pgx/v5"
)

type Pool struct{}

func (p *Pool) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row { return nil }
func (p *Pool) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return nil, nil
}
func (p *Pool) Exec(ctx context.Context, sql string, args ...any) (pgx.CommandTag, error) {
	return pgx.CommandTag{}, nil
}
func (p *Pool) Begin(ctx context.Context) (pgx.Tx, error)                          { return nil, nil }
func (p *Pool) BeginTx(ctx context.Context, o pgx.TxOptions) (pgx.Tx, error)       { return nil, nil }
func (p *Pool) SendBatch(ctx context.Context, b *pgx.Batch) pgx.BatchResults       { return nil }
func (p *Pool) CopyFrom(ctx context.Context, t pgx.Identifier, c []string, s any) (int64, error) {
	return 0, nil
}
