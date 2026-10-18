// Package gin is a minimal stand-in for github.com/gin-gonic/gin: the
// signatures the audit fixture uses, with no behaviour.
package gin

import "net/http"

type H map[string]any

type HandlerFunc func(*Context)

type Context struct {
	Request *http.Request
	Writer  http.ResponseWriter
	Keys    map[string]any
}

func (c *Context) Next()                                         {}
func (c *Context) Abort()                                        {}
func (c *Context) Param(key string) string                       { return "" }
func (c *Context) Query(key string) string                       { return "" }
func (c *Context) DefaultQuery(key, def string) string           { return "" }
func (c *Context) PostForm(key string) string                    { return "" }
func (c *Context) GetHeader(key string) string                   { return "" }
func (c *Context) Cookie(name string) (string, error)            { return "", nil }
func (c *Context) ShouldBindJSON(obj any) error                  { return nil }
func (c *Context) Set(key string, value any)                     {}
func (c *Context) Get(key string) (any, bool)                    { return nil, false }
func (c *Context) GetString(key string) string                   { return "" }
func (c *Context) MustGet(key string) any                        { return nil }
func (c *Context) Header(key, value string)                      {}
func (c *Context) JSON(code int, obj any)                        {}
func (c *Context) String(code int, format string, values ...any) {}
func (c *Context) Status(code int)                               {}
func (c *Context) AbortWithStatus(code int)                      {}
func (c *Context) AbortWithStatusJSON(code int, jsonObj any)     {}
func (c *Context) File(filepath string)                          {}
func (c *Context) Redirect(code int, location string)            {}
func (c *Context) SetCookie(name, value string, maxAge int, path, domain string, secure, httpOnly bool) {
}

const (
	DebugMode   = "debug"
	ReleaseMode = "release"
)

func SetMode(value string) {}

type IRoutes interface{}

type RouterGroup struct{}

type Engine struct{ RouterGroup }

func New() *Engine                         { return &Engine{} }
func (e *Engine) Run(addr ...string) error { return nil }

func (g *RouterGroup) Group(rel string, h ...HandlerFunc) *RouterGroup { return g }
func (g *RouterGroup) Use(h ...HandlerFunc) IRoutes                    { return g }
func (g *RouterGroup) GET(rel string, h ...HandlerFunc) IRoutes        { return g }
func (g *RouterGroup) POST(rel string, h ...HandlerFunc) IRoutes       { return g }
func (g *RouterGroup) PUT(rel string, h ...HandlerFunc) IRoutes        { return g }
func (g *RouterGroup) DELETE(rel string, h ...HandlerFunc) IRoutes     { return g }
