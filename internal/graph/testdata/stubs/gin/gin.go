// Package gin is a stub of the Gin API for static-analysis tests: the
// signatures match gin v1.x, the bodies do nothing.
package gin

import (
	"mime/multipart"
	"net/http"

	"github.com/gin-gonic/gin/binding"
)

type H map[string]any

type ResponseWriter interface {
	http.ResponseWriter
	Status() int
}

type Context struct {
	Request *http.Request
	Writer  ResponseWriter
}

func (c *Context) Next()  {}
func (c *Context) Abort() {}

func (c *Context) ShouldBindJSON(obj any) error                        { return nil }
func (c *Context) BindJSON(obj any) error                              { return nil }
func (c *Context) ShouldBind(obj any) error                            { return nil }
func (c *Context) ShouldBindQuery(obj any) error                       { return nil }
func (c *Context) ShouldBindUri(obj any) error                         { return nil }
func (c *Context) ShouldBindHeader(obj any) error                      { return nil }
func (c *Context) ShouldBindWith(obj any, b binding.Binding) error     { return nil }
func (c *Context) Param(key string) string                             { return "" }
func (c *Context) Query(key string) string                             { return "" }
func (c *Context) DefaultQuery(key, defaultValue string) string        { return "" }
func (c *Context) GetQuery(key string) (string, bool)                  { return "", false }
func (c *Context) PostForm(key string) string                          { return "" }
func (c *Context) FormFile(name string) (*multipart.FileHeader, error) { return nil, nil }
func (c *Context) GetHeader(key string) string                         { return "" }
func (c *Context) Cookie(name string) (string, error)                  { return "", nil }
func (c *Context) Header(key, value string)                            {}
func (c *Context) JSON(code int, obj any)                              {}
func (c *Context) String(code int, format string, values ...any)       {}
func (c *Context) Status(code int)                                     {}
func (c *Context) AbortWithStatus(code int)                            {}
func (c *Context) AbortWithStatusJSON(code int, jsonObj any)           {}
func (c *Context) Data(code int, contentType string, data []byte)      {}

type HandlerFunc func(*Context)

type IRoutes interface {
	Use(...HandlerFunc) IRoutes
	GET(string, ...HandlerFunc) IRoutes
	POST(string, ...HandlerFunc) IRoutes
	PUT(string, ...HandlerFunc) IRoutes
	DELETE(string, ...HandlerFunc) IRoutes
}

type RouterGroup struct{ prefix string }

type Engine struct{ RouterGroup }

func New() *Engine                         { return &Engine{} }
func (e *Engine) Run(addr ...string) error { return nil }

func (g *RouterGroup) Group(rel string, h ...HandlerFunc) *RouterGroup { return g }
func (g *RouterGroup) Use(h ...HandlerFunc) IRoutes                    { return g }
func (g *RouterGroup) GET(rel string, h ...HandlerFunc) IRoutes        { return g }
func (g *RouterGroup) POST(rel string, h ...HandlerFunc) IRoutes       { return g }
func (g *RouterGroup) PUT(rel string, h ...HandlerFunc) IRoutes        { return g }
func (g *RouterGroup) DELETE(rel string, h ...HandlerFunc) IRoutes     { return g }
