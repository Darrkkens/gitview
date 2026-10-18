package main

import (
	"example.com/api/controllers"
	"example.com/api/middlewares"
	"example.com/api/repository"
	"example.com/api/routes"
	"example.com/api/services"
	"github.com/gin-gonic/gin"
)

func main() {
	r := gin.New()
	r.Use(middlewares.RequestID())
	api := r.Group("/api", middlewares.Logger)
	ctl := controllers.NewUserController(services.NewUserService(repository.NewPG(nil)))
	routes.Register(api, &middlewares.Auth{}, ctl)
	routes.RegisterAdmin(api, &middlewares.Auth{}, ctl)
	r.GET("/health", controllers.Health)
	// Registered after every route and group above: gin does not apply it
	// to them.
	r.Use(middlewares.Late)
	r.Run(":8080")
}
