// Package chi is a stub of the chi router API (v5 signatures).
package chi

import "net/http"

type Router interface {
	http.Handler
	Use(middlewares ...func(http.Handler) http.Handler)
	With(middlewares ...func(http.Handler) http.Handler) Router
	Route(pattern string, fn func(r Router)) Router
	Mount(pattern string, h http.Handler)
	Get(pattern string, h http.HandlerFunc)
	Post(pattern string, h http.HandlerFunc)
}

type Mux struct{}

func NewRouter() *Mux { return &Mux{} }

func (m *Mux) ServeHTTP(w http.ResponseWriter, r *http.Request)           {}
func (m *Mux) Use(middlewares ...func(http.Handler) http.Handler)         {}
func (m *Mux) With(middlewares ...func(http.Handler) http.Handler) Router { return m }
func (m *Mux) Route(pattern string, fn func(r Router)) Router             { fn(m); return m }
func (m *Mux) Mount(pattern string, h http.Handler)                       {}
func (m *Mux) Get(pattern string, h http.HandlerFunc)                     {}
func (m *Mux) Post(pattern string, h http.HandlerFunc)                    {}

func URLParam(r *http.Request, key string) string { return "" }
