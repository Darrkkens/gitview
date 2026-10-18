// Package legacy is a net/http part of the fixture.
package legacy

import (
	"net/http"
)

// Serve has no timeouts (GO-CONF-003).
func Serve() {
	mux := http.NewServeMux()
	mux.HandleFunc("/legacy/session", session)
	_ = http.ListenAndServe(":8081", mux)
}

// session sets a cookie without Secure, HttpOnly or SameSite (GO-AUTHN-002).
func session(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: "sid", Value: r.URL.Query().Get("sid")})
	w.WriteHeader(http.StatusNoContent)
}

// secureCookie is set correctly: no finding.
func secureCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: "sid", Value: "x", Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode})
}

var _ = secureCookie
