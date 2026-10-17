// Package pgx is a minimal stub of the pgx API.
package pgx

import "context"

type Row interface{ Scan(dest ...any) error }

type Rows interface {
	Next() bool
	Close()
}

type Tx interface {
	Exec(ctx context.Context, sql string, args ...any) (int64, error)
	Commit(ctx context.Context) error
	Rollback(ctx context.Context) error
}
