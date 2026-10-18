package chiapp

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

func New() http.Handler {
	r := chi.NewRouter()
	r.Use(audit)
	r.Mount("/admin", adminRouter())
	r.With(audit).Get("/reports/{reportID}", report)
	return r
}

func adminRouter() chi.Router {
	ar := chi.NewRouter()
	ar.Get("/stats", stats)
	return ar
}

func audit(next http.Handler) http.Handler { return next }

func report(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "reportID")
	w.Write([]byte(id))
}

func stats(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }
