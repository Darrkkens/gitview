package main

import (
	"net/http"

	"example.com/repo/algo"
	"example.com/repo/report"
	"example.com/repo/store"
	"example.com/repo/users"
)

var svc = users.New(store.NewMem())

func userHandler(w http.ResponseWriter, r *http.Request) {
	u, err := svc.GetUser(1)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	w.Write([]byte(users.Format(u) + report.Format(u)))
}

func main() {
	_ = algo.Fact(5) + algo.Run()
	_ = algo.Even(4)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /users", userHandler)
	http.ListenAndServe(":8080", mux)
}
