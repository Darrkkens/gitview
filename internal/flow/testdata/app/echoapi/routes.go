package echoapi

import "github.com/labstack/echo/v4"

func Setup() *echo.Echo {
	e := echo.New()
	g := e.Group("/admin")
	g.GET("/users", ListUsers, AdminOnly)
	e.GET("/status", Status)
	return e
}

func ListUsers(c echo.Context) error { return c.String(200, "ok") }
func Status(c echo.Context) error    { return nil }

func AdminOnly(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error { return next(c) }
}
