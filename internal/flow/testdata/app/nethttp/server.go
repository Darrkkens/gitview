package nethttp

import "net/http"

type XHandler struct{}

func (XHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) { Health(w, r) }

func Health(w http.ResponseWriter, r *http.Request) {}

func Mux() *http.ServeMux {
	http.HandleFunc("GET /health", Health)
	mux := http.NewServeMux()
	mux.Handle("/x", XHandler{})
	return mux
}

func Dynamic(path string, h http.HandlerFunc) {
	http.HandleFunc(path, h)
}
