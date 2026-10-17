// Package chi is a minimal stub of the chi router API used by the fixtures.
package chi

import "net/http"

type Router interface {
	Get(pattern string, h http.HandlerFunc)
	Post(pattern string, h http.HandlerFunc)
	Route(pattern string, fn func(r Router)) Router
}

type Mux struct{}

func NewRouter() *Mux { return &Mux{} }

func (m *Mux) Get(pattern string, h http.HandlerFunc)           {}
func (m *Mux) Post(pattern string, h http.HandlerFunc)          {}
func (m *Mux) Route(pattern string, fn func(r Router)) Router   { fn(m); return m }
func (m *Mux) ServeHTTP(w http.ResponseWriter, r *http.Request) {}

// URLParam returns the value of a URL path parameter.
func URLParam(r *http.Request, key string) string { return r.PathValue(key) }
