// Package chi is a minimal stub of the chi router API.
package chi

import "net/http"

type Router interface {
	Get(pattern string, h http.HandlerFunc)
	Post(pattern string, h http.HandlerFunc)
	Route(pattern string, fn func(r Router)) Router
	Use(m ...func(http.Handler) http.Handler)
}

type Mux struct{}

func NewRouter() *Mux { return &Mux{} }

func (m *Mux) Get(pattern string, h http.HandlerFunc)           {}
func (m *Mux) Post(pattern string, h http.HandlerFunc)          {}
func (m *Mux) Route(pattern string, fn func(r Router)) Router   { fn(m); return m }
func (m *Mux) Use(mw ...func(http.Handler) http.Handler)        {}
func (m *Mux) ServeHTTP(w http.ResponseWriter, r *http.Request) {}
