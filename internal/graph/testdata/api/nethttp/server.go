// Package nethttp uses the Go 1.22 ServeMux patterns.
package nethttp

import (
	"encoding/json"
	"net/http"
)

type Item struct {
	ID    string  `json:"id"`
	Price float64 `json:"price"`
}

type ItemInput struct {
	Name  string   `json:"name"`
	Price float64  `json:"price"`
	Sizes []string `json:"sizes,omitempty"`
}

func Serve() error {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /items/{id}", getItem)
	mux.HandleFunc("POST /items", createItem)
	return http.ListenAndServe(":9090", mux)
}

func getItem(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	_ = r.URL.Query().Get("expand")
	if r.Header.Get("X-Api-Key") == "" {
		http.Error(w, "missing key", http.StatusUnauthorized)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(Item{ID: id})
}

func createItem(w http.ResponseWriter, r *http.Request) {
	var in ItemInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(Item{ID: "x", Price: in.Price})
}
