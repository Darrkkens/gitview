// Package gin is a minimal stub of the Gin API for static-analysis tests.
package gin

type Context struct{}

func (c *Context) JSON(code int, v any)  {}
func (c *Context) Param(k string) string { return "" }

type HandlerFunc func(*Context)

type RouterGroup struct{ prefix string }

type Engine struct{ RouterGroup }

func New() *Engine { return &Engine{} }

func (g *RouterGroup) Group(rel string, h ...HandlerFunc) *RouterGroup { return g }
func (g *RouterGroup) Use(h ...HandlerFunc)                            {}
func (g *RouterGroup) GET(rel string, h ...HandlerFunc)                {}
func (g *RouterGroup) POST(rel string, h ...HandlerFunc)               {}
func (g *RouterGroup) Handle(method, rel string, h ...HandlerFunc)     {}
