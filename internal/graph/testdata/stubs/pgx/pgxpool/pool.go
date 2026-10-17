package pgxpool

import (
	"context"

	"github.com/jackc/pgx/v5"
)

type Pool struct{}

func (p *Pool) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row        { return nil }
func (p *Pool) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) { return nil, nil }
func (p *Pool) Exec(ctx context.Context, sql string, args ...any) (int64, error)     { return 0, nil }
func (p *Pool) Begin(ctx context.Context) (pgx.Tx, error)                            { return nil, nil }
