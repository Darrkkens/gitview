// Package fakedb is a database/sql driver that answers every query with
// one row after a short delay, and reports each statement to GitView's
// collector the way an instrumented driver would.
package fakedb

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"io"
	"time"

	"github.com/athos/gitview/collector"
)

// Delay is the time each statement takes.
var Delay = 3 * time.Millisecond

func init() { sql.Register("fakedb", drv{}) }

type drv struct{}

func (drv) Open(string) (driver.Conn, error) { return conn{}, nil }

type conn struct{}

func (conn) Prepare(string) (driver.Stmt, error) { return nil, driver.ErrSkip }
func (conn) Close() error                        { return nil }
func (conn) Begin() (driver.Tx, error)           { return nil, driver.ErrSkip }

func (conn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	span := collector.Default().StartSQL(ctx, query, "example.com/liveapp/fakedb.")
	time.Sleep(Delay)
	span.EndRows(nil, 1)
	return &rows{}, nil
}

type rows struct{ done bool }

func (*rows) Columns() []string { return []string{"name"} }
func (*rows) Close() error      { return nil }
func (r *rows) Next(dest []driver.Value) error {
	if r.done {
		return io.EOF
	}
	r.done = true
	dest[0] = "client"
	return nil
}
