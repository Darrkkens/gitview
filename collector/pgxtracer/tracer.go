// Package pgxtracer records the statements executed through pgx v5 in
// GitView's collector, attributed to the application's call site.
//
//	cfg, _ := pgxpool.ParseConfig(dsn)
//	cfg.ConnConfig.Tracer = pgxtracer.New()
//
// Only the statement text (with literals removed) is recorded, never the
// arguments. For Query, the statement ends when its rows are closed, so its
// duration includes the time the application spends reading them.
package pgxtracer

import (
	"context"

	"github.com/athos/gitview/collector"
	"github.com/jackc/pgx/v5"
)

// Tracer implements pgx.QueryTracer.
type Tracer struct {
	// Collector receives the statements; nil means collector.Default().
	Collector *collector.Collector
}

// New returns a tracer for the default collector.
func New() *Tracer { return &Tracer{} }

var _ pgx.QueryTracer = (*Tracer)(nil)

type spanKey struct{}

func (t *Tracer) collector() *collector.Collector {
	if t.Collector != nil {
		return t.Collector
	}
	return collector.Default()
}

// TraceQueryStart starts recording a statement.
func (t *Tracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	span := t.collector().StartSQL(ctx, data.SQL, "github.com/athos/gitview/collector/pgxtracer.(*Tracer)")
	if span == nil {
		return ctx
	}
	return context.WithValue(ctx, spanKey{}, span)
}

// TraceQueryEnd finishes it, with the rows the command tag reports.
func (t *Tracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	if span, ok := ctx.Value(spanKey{}).(*collector.Span); ok {
		span.EndRows(data.Err, data.CommandTag.RowsAffected())
	}
}
