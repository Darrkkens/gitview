// Package unused contains look-alikes that are not HTTP routes.
package unused

type Router struct{}

// GET has the name of a route registration method on a module type.
func (Router) GET(path string, h func()) {}

type CreateClientRequest struct {
	Name string `json:"name" binding:"required"`
}

func Wire() { Router{}.GET("/not-a-route", func() {}) }
