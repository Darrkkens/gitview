package main

import (
	"net/http"

	"example.com/deep/api"
	"example.com/deep/repository"
	"example.com/deep/service"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	repo := repository.NewClientRepository(&pgxpool.Pool{})
	h := api.NewHandler(service.NewClientService(repo))
	http.ListenAndServe(":8080", h.Routes())
}
