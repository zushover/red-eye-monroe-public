package dashboard

import (
	_ "embed"
	"net/http"

	"weatherbot/internal/store"
)

type Server struct {
	addr     string
	store    *store.Store
	research http.Handler
}

func New(addr string, st *store.Store, research http.Handler) *Server {
	return &Server{addr: addr, store: st, research: research}
}

func (s *Server) ListenAndServe() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/execution-view.js", func(w http.ResponseWriter,r *http.Request){w.Header().Set("Content-Type","application/javascript; charset=utf-8");w.Header().Set("Cache-Control","no-store");w.Write(executionViewJS)})
	if s.research != nil {
		mux.Handle("/research/", http.StripPrefix("/research", s.research))
	}
	mux.HandleFunc("/api/portfolio", func(w http.ResponseWriter, r *http.Request) {
		b, err := s.store.Ledger()
		if err != nil {
			http.Error(w, "ledger unavailable", 503)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.Write(b)
	})
	mux.HandleFunc("/api/latest", func(w http.ResponseWriter, _ *http.Request) {
		b, err := s.store.Latest()
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(b)
	})
	mux.HandleFunc("/api/live-status", func(w http.ResponseWriter, _ *http.Request) {
		b, err := s.store.LiveStatus()
		if err != nil {
			http.Error(w, "live status unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(b)
	})
	mux.HandleFunc("/api/live-ledger", func(w http.ResponseWriter, _ *http.Request) {
		b, err := s.store.LiveLedger()
		if err != nil {
			http.Error(w, "live ledger unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(b)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(indexHTML)
	})
	return http.ListenAndServe(s.addr, mux)
}

//go:embed index.html
var indexHTML []byte

//go:embed execution-view.js
var executionViewJS []byte
