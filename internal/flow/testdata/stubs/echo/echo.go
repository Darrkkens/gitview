// Package echo is a minimal stub of the Echo API.
package echo

type Context interface {
	String(code int, s string) error
}

type HandlerFunc func(Context) error

type MiddlewareFunc func(HandlerFunc) HandlerFunc

type Echo struct{}
type Group struct{}
type Route struct{}

func New() *Echo { return &Echo{} }

func (e *Echo) GET(path string, h HandlerFunc, m ...MiddlewareFunc) *Route  { return nil }
func (e *Echo) Group(prefix string, m ...MiddlewareFunc) *Group             { return &Group{} }
func (g *Group) GET(path string, h HandlerFunc, m ...MiddlewareFunc) *Route { return nil }
func (g *Group) POST(path string, h HandlerFunc, m ...MiddlewareFunc) *Route {
	return nil
}
