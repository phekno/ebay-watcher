package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/phekno/ebay-watcher/internal/config"
	"github.com/phekno/ebay-watcher/internal/store"
)

func newTestServer(t *testing.T) (*Server, *store.Store) {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://localhost:5432/ebay_watcher_test?sslmode=disable"
	}
	s, err := store.New(dsn)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	if err := s.Truncate(); err != nil {
		t.Fatalf("truncate tables: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	cfg := &config.Config{
		PollInterval: time.Hour,
	}

	srv := New(cfg, s, nil)
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
	srv, st := newTestServer(t)

	_, _ = st.CreateWatch("thinkpad", 500, "")
	_, _ = st.CreateWatch("macbook", 800, "")

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

	_, _ = st.CreateWatch("thinkpad", 500, "")
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
		TotalSeen     int    `json:"total_seen"`
		TotalNotified int    `json:"total_notified"`
		WatchCount    int    `json:"watch_count"`
		PollInterval  string `json:"poll_interval"`
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
	if stats.WatchCount != 1 {
		t.Errorf("WatchCount = %d, want 1", stats.WatchCount)
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
	srv, st := newTestServer(t)

	// Create a watch so the default query comes from there
	_, _ = st.CreateWatch("thinkpad", 500, "")

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

// ── Watch CRUD endpoint tests ───────────────────────────────

func TestCreateWatchEndpoint(t *testing.T) {
	srv, _ := newTestServer(t)

	body := `{"query":"thinkpad","max_price":500}`
	req := httptest.NewRequest(http.MethodPost, "/api/watches", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", w.Code)
	}

	var watch store.Watch
	if err := json.NewDecoder(w.Body).Decode(&watch); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if watch.Query != "thinkpad" {
		t.Errorf("Query = %q, want thinkpad", watch.Query)
	}
	if watch.MaxPrice != 500 {
		t.Errorf("MaxPrice = %f, want 500", watch.MaxPrice)
	}
	if !watch.Enabled {
		t.Error("expected enabled")
	}
}

func TestCreateWatchEndpoint_InvalidBody(t *testing.T) {
	srv, _ := newTestServer(t)

	body := `{"query":"","max_price":0}`
	req := httptest.NewRequest(http.MethodPost, "/api/watches", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestListWatchesEndpoint(t *testing.T) {
	srv, st := newTestServer(t)

	_, _ = st.CreateWatch("thinkpad", 500, "")
	_, _ = st.CreateWatch("macbook", 800, "")

	req := httptest.NewRequest(http.MethodGet, "/api/watches", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var watches []store.Watch
	if err := json.NewDecoder(w.Body).Decode(&watches); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(watches) != 2 {
		t.Fatalf("expected 2 watches, got %d", len(watches))
	}
}

func TestUpdateWatchEndpoint(t *testing.T) {
	srv, st := newTestServer(t)

	created, _ := st.CreateWatch("thinkpad", 500, "")

	body := `{"query":"dell xps","max_price":700,"enabled":false}`
	req := httptest.NewRequest(http.MethodPut, "/api/watches/"+itoa(created.ID), strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var watch store.Watch
	if err := json.NewDecoder(w.Body).Decode(&watch); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if watch.Query != "dell xps" {
		t.Errorf("Query = %q, want dell xps", watch.Query)
	}
	if watch.Enabled {
		t.Error("expected disabled")
	}
}

func TestDeleteWatchEndpoint(t *testing.T) {
	srv, st := newTestServer(t)

	created, _ := st.CreateWatch("thinkpad", 500, "")

	req := httptest.NewRequest(http.MethodDelete, "/api/watches/"+itoa(created.ID), nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", w.Code)
	}

	watches, _ := st.ListWatches()
	if len(watches) != 0 {
		t.Errorf("expected 0 watches after delete, got %d", len(watches))
	}
}

func TestDeleteWatchEndpoint_NotFound(t *testing.T) {
	srv, _ := newTestServer(t)

	req := httptest.NewRequest(http.MethodDelete, "/api/watches/999", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}

func itoa(n int) string {
	return strconv.Itoa(n)
}
