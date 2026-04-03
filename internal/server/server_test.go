package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/phekno/ebay-watcher/internal/config"
	"github.com/phekno/ebay-watcher/internal/store"
)

func newTestServer(t *testing.T) (*Server, *store.Store) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := store.New(dbPath)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	cfg := &config.Config{
		Queries:      []string{"thinkpad", "macbook"},
		MaxPrice:     500,
		PollInterval: time.Hour,
	}

	srv := New(cfg, s)
	return srv, s
}

func TestHealthEndpoint(t *testing.T) {
	srv, _ := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var body map[string]string
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("status = %q, want ok", body["status"])
	}
}

func TestQueriesEndpoint(t *testing.T) {
	srv, _ := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/queries", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var queries []string
	if err := json.NewDecoder(w.Body).Decode(&queries); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(queries) != 2 {
		t.Fatalf("got %d queries, want 2", len(queries))
	}
	if queries[0] != "thinkpad" || queries[1] != "macbook" {
		t.Errorf("queries = %v, want [thinkpad macbook]", queries)
	}
}

func TestStatsEndpoint(t *testing.T) {
	srv, st := newTestServer(t)

	// Add some data
	_ = st.UpsertListing(store.Listing{
		ID: "a", Query: "thinkpad", Title: "X1", Price: 400, Currency: "USD", URL: "http://a",
	})
	_ = st.MarkNotified("a")
	_ = st.RecordPoll()

	req := httptest.NewRequest(http.MethodGet, "/api/stats", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var stats struct {
		TotalSeen     int      `json:"total_seen"`
		TotalNotified int      `json:"total_notified"`
		Queries       []string `json:"queries"`
		MaxPrice      float64  `json:"max_price"`
		PollInterval  string   `json:"poll_interval"`
	}
	if err := json.NewDecoder(w.Body).Decode(&stats); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if stats.TotalSeen != 1 {
		t.Errorf("TotalSeen = %d, want 1", stats.TotalSeen)
	}
	if stats.TotalNotified != 1 {
		t.Errorf("TotalNotified = %d, want 1", stats.TotalNotified)
	}
	if stats.MaxPrice != 500 {
		t.Errorf("MaxPrice = %f, want 500", stats.MaxPrice)
	}
	if stats.PollInterval != "1h0m0s" {
		t.Errorf("PollInterval = %q, want 1h0m0s", stats.PollInterval)
	}
}

func TestListingsEndpoint_Empty(t *testing.T) {
	srv, _ := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/listings", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var listings []store.Listing
	if err := json.NewDecoder(w.Body).Decode(&listings); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(listings) != 0 {
		t.Errorf("expected 0 listings, got %d", len(listings))
	}
}

func TestListingsEndpoint_WithLimit(t *testing.T) {
	srv, st := newTestServer(t)

	for _, id := range []string{"a", "b", "c"} {
		_ = st.UpsertListing(store.Listing{
			ID: id, Query: "q", Title: "T", Price: 100, Currency: "USD", URL: "http://x",
		})
		_ = st.MarkNotified(id)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/listings?limit=2", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var listings []store.Listing
	if err := json.NewDecoder(w.Body).Decode(&listings); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(listings) != 2 {
		t.Errorf("expected 2 listings, got %d", len(listings))
	}
}

func TestPriceHistoryEndpoint(t *testing.T) {
	srv, st := newTestServer(t)

	_ = st.UpsertListing(store.Listing{
		ID: "a", Query: "thinkpad", Title: "X1", Price: 400, Currency: "USD", URL: "http://a",
	})

	req := httptest.NewRequest(http.MethodGet, "/api/price-history?query=thinkpad&days=7", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var resp struct {
		Query  string             `json:"query"`
		Points []store.PricePoint `json:"points"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Query != "thinkpad" {
		t.Errorf("query = %q, want thinkpad", resp.Query)
	}
	if len(resp.Points) != 1 {
		t.Errorf("expected 1 price point, got %d", len(resp.Points))
	}
}

func TestPriceHistoryEndpoint_DefaultQuery(t *testing.T) {
	srv, _ := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/price-history", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var resp struct {
		Query string `json:"query"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Query != "thinkpad" {
		t.Errorf("default query = %q, want thinkpad", resp.Query)
	}
}
