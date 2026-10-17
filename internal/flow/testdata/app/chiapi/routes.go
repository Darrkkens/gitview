package chiapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

func Routes() *chi.Mux {
	r := chi.NewRouter()
	r.Route("/v1", func(r chi.Router) {
		r.Get("/ping", Ping)
	})
	r.Post("/items", CreateItem)
	return r
}

func Ping(w http.ResponseWriter, r *http.Request)       {}
func CreateItem(w http.ResponseWriter, r *http.Request) {}
