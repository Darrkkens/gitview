package main

import (
	"example.com/app/client"
	"example.com/app/httpapi"
	"github.com/gin-gonic/gin"
)

func main() {
	svc := client.NewClientService(&client.PGRepo{})
	h := httpapi.NewClientHandler(svc)
	e := gin.New()
	e.Use(httpapi.Logger)
	api := e.Group("/api")
	httpapi.Register(api, h)
	httpapi.RegisterMore(api, h)
}
