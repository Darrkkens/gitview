package routes

import (
	"example.com/api/controllers"
	"example.com/api/middlewares"
	"github.com/gin-gonic/gin"
)

// Register receives the router as an interface, like real projects do.
func Register(r gin.IRoutes, auth *middlewares.Auth, ctl *controllers.UserController) {
	user := auth.RequireUser()
	r.GET("/users", user, ctl.List)
	r.POST("/users", user, ctl.Create)
	r.GET("/users/:id", user, ctl.Get)
	r.PUT("/users/:id", user, ctl.Update)
	r.DELETE("/users/:id", user, ctl.Delete)
	r.POST("/users/:id/avatar", user, ctl.Avatar)
	for _, kind := range []string{"a", "b"} {
		// The path is built at runtime.
		r.GET("/kinds/"+kind, ctl.Kind)
	}
}

// RegisterAdmin builds the handler list in a closure that captures the
// middleware, as real projects do: with(h) → [user, h].
func RegisterAdmin(r gin.IRoutes, auth *middlewares.Auth, ctl *controllers.UserController) {
	user := auth.RequireUser()
	with := func(h gin.HandlerFunc) []gin.HandlerFunc { return []gin.HandlerFunc{user, h} }
	r.GET("/admin/users", with(ctl.List)...)
}
