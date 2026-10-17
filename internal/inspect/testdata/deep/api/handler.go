package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"example.com/deep/service"
	"github.com/go-chi/chi/v5"
)

type Handler struct{ svc *service.ClientService }

func NewHandler(s *service.ClientService) *Handler { return &Handler{svc: s} }

func (h *Handler) Routes() *chi.Mux {
	r := chi.NewRouter()
	r.Get("/clients/{id}", h.GetClient)
	r.Get("/clients", h.SearchClients)
	return r
}

// GetClient reads the id from the URL and loads the client.
func (h *Handler) GetClient(w http.ResponseWriter, r *http.Request) {
	clientID := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(clientID, 10, 64)
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	c, err := h.svc.GetClientByID(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	json.NewEncoder(w).Encode(c)
}

// SearchClients passes a query-string value down to dynamic SQL.
func (h *Handler) SearchClients(w http.ResponseWriter, r *http.Request) {
	term := r.URL.Query().Get("q")
	list, err := h.svc.Search(r.Context(), term)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(list)
}
