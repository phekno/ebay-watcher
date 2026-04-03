package server

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/phekno/ebay-watcher/internal/config"
	"github.com/phekno/ebay-watcher/internal/store"
)

type Server struct {
	cfg   *config.Config
	store *store.Store
	mux   *http.ServeMux
}

func New(cfg *config.Config, s *store.Store) *Server {
	srv := &Server{cfg: cfg, store: s, mux: http.NewServeMux()}
	srv.routes()
	return srv
}

func (s *Server) Handler() http.Handler {
	return s.mux
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /api/stats", s.handleStats)
	s.mux.HandleFunc("GET /api/listings", s.handleListings)
	s.mux.HandleFunc("GET /api/price-history", s.handlePriceHistory)
	s.mux.HandleFunc("GET /api/queries", s.handleQueries)
	s.mux.HandleFunc("GET /health", s.handleHealth)
	s.mux.Handle("/", http.FileServer(http.FS(staticFiles())))
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	stats, err := s.store.GetStats()
	if err != nil {
		httpError(w, err, http.StatusInternalServerError)
		return
	}

	type response struct {
		*store.Stats
		NextPollAt   *string  `json:"next_poll_at"`
		Queries      []string `json:"queries"`
		MaxPrice     float64  `json:"max_price"`
		PollInterval string   `json:"poll_interval"`
	}

	res := response{
		Stats:        stats,
		Queries:      s.cfg.Queries,
		MaxPrice:     s.cfg.MaxPrice,
		PollInterval: s.cfg.PollInterval.String(),
	}
	if stats.LastPollAt != nil {
		next := stats.LastPollAt.Add(s.cfg.PollInterval).UTC().Format(time.RFC3339)
		res.NextPollAt = &next
	}

	jsonOK(w, res)
}

func (s *Server) handleListings(w http.ResponseWriter, r *http.Request) {
	limit := 100
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 {
			limit = n
		}
	}

	listings, err := s.store.GetNotifiedListings(limit)
	if err != nil {
		httpError(w, err, http.StatusInternalServerError)
		return
	}
	if listings == nil {
		listings = []store.Listing{}
	}
	jsonOK(w, listings)
}

func (s *Server) handlePriceHistory(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("query")
	if query == "" && len(s.cfg.Queries) > 0 {
		query = s.cfg.Queries[0]
	}
	days := 30
	if d := r.URL.Query().Get("days"); d != "" {
		if n, err := strconv.Atoi(d); err == nil && n > 0 {
			days = n
		}
	}

	points, err := s.store.GetPriceHistory(query, days)
	if err != nil {
		httpError(w, err, http.StatusInternalServerError)
		return
	}
	if points == nil {
		points = []store.PricePoint{}
	}
	jsonOK(w, map[string]any{"query": query, "points": points})
}

func (s *Server) handleQueries(w http.ResponseWriter, r *http.Request) {
	jsonOK(w, s.cfg.Queries)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	jsonOK(w, map[string]string{"status": "ok"})
}

func jsonOK(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("json encode error", "error", err)
	}
}

func httpError(w http.ResponseWriter, err error, code int) {
	slog.Error("api error", "error", err)
	http.Error(w, err.Error(), code)
}
