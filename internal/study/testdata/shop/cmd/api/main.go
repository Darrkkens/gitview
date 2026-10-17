package main

import (
	"database/sql"
	"net/http"

	"example.com/shop/internal/handlers"
	"example.com/shop/internal/repository"
	"example.com/shop/internal/routes"
	"example.com/shop/internal/services"
)

func main() {
	db, _ := sql.Open("postgres", "")
	svc := &services.RatingService{Repo: &repository.UsageRepository{DB: db}, Pricing: &services.PricingClient{HTTP: http.DefaultClient}}
	mux := http.NewServeMux()
	routes.Register(mux, &handlers.UsageHandler{Rating: svc})
	http.ListenAndServe(":8080", mux)
}
