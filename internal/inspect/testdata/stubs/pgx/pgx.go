// Package pgx is a minimal stub of the pgx API used by the fixtures.
package pgx

import "context"

type CommandTag struct{}

type Row interface{ Scan(dest ...any) error }

type Rows interface {
	Next() bool
	Scan(dest ...any) error
	Close()
	Err() error
}

type Tx interface {
	Exec(ctx context.Context, sql string, args ...any) (CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) Row
	Commit(ctx context.Context) error
	Rollback(ctx context.Context) error
}

type Batch struct{ n int }

func (b *Batch) Queue(sql string, args ...any) { b.n++ }

type BatchResults interface{ Close() error }

type Identifier []string

type TxOptions struct{}
