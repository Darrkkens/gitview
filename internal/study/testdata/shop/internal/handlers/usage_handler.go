package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"

	"example.com/shop/internal/services"
)

// UsageHandler serves the usage endpoints.
type UsageHandler struct{ Rating *services.RatingService }

// GetCharge rates the usage of a company.
func (h *UsageHandler) GetCharge(w http.ResponseWriter, r *http.Request) {
	companyID, err := strconv.ParseInt(r.PathValue("company"), 10, 64)
	if err != nil {
		http.Error(w, "bad company", http.StatusBadRequest)
		return
	}
	charge, err := h.Rating.RateUsage(r.Context(), companyID, r.URL.Query().Get("period"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(charge)
}
