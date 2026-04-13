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
	cfg       *config.Config
	store     *store.Store
	mux       *http.ServeMux
	onNewWatch func()
}

func New(cfg *config.Config, s *store.Store, onNewWatch func()) *Server {
	srv := &Server{cfg: cfg, store: s, mux: http.NewServeMux(), onNewWatch: onNewWatch}
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
	s.mux.HandleFunc("GET /api/watches", s.handleListWatches)
	s.mux.HandleFunc("POST /api/watches", s.handleCreateWatch)
	s.mux.HandleFunc("PUT /api/watches/{id}", s.handleUpdateWatch)
	s.mux.HandleFunc("DELETE /api/watches/{id}", s.handleDeleteWatch)
	s.mux.HandleFunc("GET /health", s.handleHealth)
	s.mux.Handle("/", http.FileServer(http.FS(staticFiles())))
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	stats, err := s.store.GetStats()
	if err != nil {
		httpError(w, err, http.StatusInternalServerError)
		return
	}

	watches, err := s.store.ListEnabledWatches()
	if err != nil {
		httpError(w, err, http.StatusInternalServerError)
		return
	}

	type response struct {
		*store.Stats
		NextPollAt   *string `json:"next_poll_at"`
		WatchCount   int     `json:"watch_count"`
		PollInterval string  `json:"poll_interval"`
	}

	res := response{
		Stats:        stats,
		WatchCount:   len(watches),
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
	if query == "" {
		watches, _ := s.store.ListEnabledWatches()
		if len(watches) > 0 {
			query = watches[0].Query
		}
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
	watches, err := s.store.ListEnabledWatches()
	if err != nil {
		httpError(w, err, http.StatusInternalServerError)
		return
	}
	queries := make([]string, 0, len(watches))
	for _, wt := range watches {
		queries = append(queries, wt.Query)
	}
	jsonOK(w, queries)
}

// ── Watch CRUD ──────────────────────────────────────────────

func (s *Server) handleListWatches(w http.ResponseWriter, r *http.Request) {
	watches, err := s.store.ListWatches()
	if err != nil {
		httpError(w, err, http.StatusInternalServerError)
		return
	}
	if watches == nil {
		watches = []store.Watch{}
	}
	jsonOK(w, watches)
}

func (s *Server) handleCreateWatch(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Query    string  `json:"query"`
		MaxPrice float64 `json:"max_price"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if body.Query == "" || body.MaxPrice <= 0 {
		http.Error(w, "query and max_price (>0) are required", http.StatusBadRequest)
		return
	}

	watch, err := s.store.CreateWatch(body.Query, body.MaxPrice)
	if err != nil {
		httpError(w, err, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(watch); err != nil {
		slog.Error("json encode error", "error", err)
	}

	if s.onNewWatch != nil {
		s.onNewWatch()
	}
}

func (s *Server) handleUpdateWatch(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.Error(w, "invalid watch id", http.StatusBadRequest)
		return
	}

	var body struct {
		Query    string  `json:"query"`
		MaxPrice float64 `json:"max_price"`
		Enabled  *bool   `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	existing, err := s.store.GetWatch(id)
	if err != nil {
		http.Error(w, "watch not found", http.StatusNotFound)
		return
	}

	query := existing.Query
	if body.Query != "" {
		query = body.Query
	}
	maxPrice := existing.MaxPrice
	if body.MaxPrice > 0 {
		maxPrice = body.MaxPrice
	}
	enabled := existing.Enabled
	if body.Enabled != nil {
		enabled = *body.Enabled
	}

	if err := s.store.UpdateWatch(id, query, maxPrice, enabled); err != nil {
		httpError(w, err, http.StatusInternalServerError)
		return
	}

	updated, _ := s.store.GetWatch(id)
	jsonOK(w, updated)
}

func (s *Server) handleDeleteWatch(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.Error(w, "invalid watch id", http.StatusBadRequest)
		return
	}
	if err := s.store.DeleteWatch(id); err != nil {
		http.Error(w, "watch not found", http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
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
