package routes

import (
	"net/http"

	"example.com/shop/internal/handlers"
)

// Register adds the API routes.
func Register(mux *http.ServeMux, h *handlers.UsageHandler) {
	mux.HandleFunc("GET /api/charges/{company}", h.GetCharge)
}
