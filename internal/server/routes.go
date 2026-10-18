package server

import (
	"compress/gzip"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/athos/gitview/internal/webui"
)

// Handler returns the server's routes behind the guard.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.page)
	mux.HandleFunc("GET /api/status", s.api(s.status))
	mux.HandleFunc("GET /api/section/{name}", s.section)
	mux.HandleFunc("POST /api/section/{name}/refresh", s.api(s.refresh))
	mux.HandleFunc("GET /api/sources", s.sources)
	mux.HandleFunc("GET /api/events", s.stream)
	for pattern, fn := range s.cfg.API {
		mux.HandleFunc(pattern, s.api(fn))
	}
	return s.guard(mux)
}

// guard accepts only requests addressed to the local server by name
// (against DNS rebinding) and, for the API, with the token and from the
// page's own origin.
func (s *Server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, port, _ := net.SplitHostPort(s.host)
		if r.Host != s.host && r.Host != "localhost:"+port {
			http.Error(w, "forbidden host", http.StatusForbidden)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Frame-Options", "DENY")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			if o := r.Header.Get("Origin"); o != "" && o != "http://"+s.host && o != "http://localhost:"+port {
				http.Error(w, "forbidden origin", http.StatusForbidden)
				return
			}
			tok := r.Header.Get("X-GitView-Token")
			if r.URL.Path == "/api/events" && tok == "" {
				tok = r.URL.Query().Get("t") // EventSource cannot set headers
			}
			if !s.validToken(tok) {
				http.Error(w, "missing or wrong token", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) validToken(t string) bool {
	return subtle.ConstantTimeCompare([]byte(t), []byte(s.token)) == 1
}

func (s *Server) offered() []string {
	names := make([]string, 0, len(s.sections))
	for n := range s.sections {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

func (s *Server) page(w http.ResponseWriter, r *http.Request) {
	if !s.validToken(r.URL.Query().Get("t")) {
		http.Error(w, "open the address printed by gitview serve", http.StatusForbidden)
		return
	}
	data, err := webui.Render(webui.Document{Kind: "serve", Title: "GitView · " + s.cfg.Repository,
		Repository: s.cfg.Repository, Generated: time.Now(), View: s.cfg.View, Offered: s.offered(),
		Live: &webui.Live{Token: s.token}})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", webui.CSP(true))
	writeMaybeGzip(w, r, data)
}

func (s *Server) status(r *http.Request, _ []byte) (any, int, error) {
	secs := map[string]sectionStatus{}
	for name, sec := range s.sections {
		secs[name] = sec.status()
	}
	out := map[string]any{"repository": s.cfg.Repository, "root": s.cfg.Root, "sections": secs}
	if s.cfg.Branch != nil {
		out["branch"] = s.cfg.Branch()
	}
	s.mu.Lock()
	if !s.changed.IsZero() {
		out["changedAt"] = s.changed.Format(time.RFC3339)
	}
	s.mu.Unlock()
	if s.cfg.Status != nil {
		for k, v := range s.cfg.Status(r.Context()) {
			out[k] = v
		}
	}
	return out, 0, nil
}

func (s *Server) section(w http.ResponseWriter, r *http.Request) {
	sec := s.sections[r.PathValue("name")]
	if sec == nil {
		writeJSONError(w, http.StatusNotFound, fmt.Errorf("no section %q", r.PathValue("name")))
		return
	}
	data, err := sec.get(s.ctx, r.Context())
	if err != nil {
		if r.Context().Err() != nil {
			return // the page went away
		}
		writeJSONError(w, http.StatusUnprocessableEntity, err)
		return
	}
	body, err := json.Marshal(data)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	writeMaybeGzip(w, r, body)
}

func (s *Server) refresh(r *http.Request, _ []byte) (any, int, error) {
	sec := s.sections[r.PathValue("name")]
	if sec == nil {
		return nil, http.StatusNotFound, fmt.Errorf("no section %q", r.PathValue("name"))
	}
	sec.refresh(s.ctx)
	return sec.status(), 0, nil
}

func (s *Server) sources(w http.ResponseWriter, r *http.Request) {
	enc := ""
	if s.cfg.Sources != nil {
		var err error
		if enc, err = s.cfg.Sources(r.Context()); err != nil {
			writeJSONError(w, http.StatusUnprocessableEntity, err)
			return
		}
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	io.WriteString(w, enc) // already compressed
}

func (s *Server) stream(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Connection", "keep-alive")
	ch := s.events.subscribe()
	defer s.events.unsubscribe(ch)
	io.WriteString(w, ": gitview\n\n")
	fl.Flush()
	ping := time.NewTicker(25 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ping.C:
			io.WriteString(w, ": ping\n\n")
			fl.Flush()
		case ev, ok := <-ch:
			if !ok {
				return
			}
			data, _ := json.Marshal(ev.data)
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.name, data)
			fl.Flush()
		}
	}
}

func (s *Server) api(fn APIFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body []byte
		if r.Method == http.MethodPost {
			if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
				http.Error(w, "JSON expected", http.StatusUnsupportedMediaType)
				return
			}
			var err error
			body, err = io.ReadAll(http.MaxBytesReader(w, r.Body, 64<<10))
			if err != nil {
				http.Error(w, "body too large", http.StatusRequestEntityTooLarge)
				return
			}
		}
		v, code, err := fn(r, body)
		if err != nil {
			if code == 0 {
				code = http.StatusInternalServerError
			}
			writeJSONError(w, code, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if code != 0 {
			w.WriteHeader(code)
		}
		json.NewEncoder(w).Encode(v)
	}
}

func writeJSONError(w http.ResponseWriter, code int, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}

// writeMaybeGzip compresses large bodies for browsers that accept it (a
// graph section of a big repository is several megabytes of JSON).
func writeMaybeGzip(w http.ResponseWriter, r *http.Request, body []byte) {
	if len(body) < 8<<10 || !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
		w.Write(body)
		return
	}
	w.Header().Set("Content-Encoding", "gzip")
	w.Header().Add("Vary", "Accept-Encoding")
	zw, _ := gzip.NewWriterLevel(w, gzip.BestSpeed)
	zw.Write(body)
	zw.Close()
}
