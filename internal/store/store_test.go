package store

import (
	"path/filepath"
	"testing"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := New(dbPath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestHasSeen_NotSeen(t *testing.T) {
	s := newTestStore(t)

	seen, err := s.HasSeen("item-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if seen {
		t.Error("expected item not to be seen")
	}
}

func TestUpsertListing_And_HasSeen(t *testing.T) {
	s := newTestStore(t)

	l := Listing{
		ID:       "item-1",
		Query:    "thinkpad",
		Title:    "Thinkpad X1 Carbon",
		Price:    450.00,
		Currency: "USD",
		URL:      "https://ebay.com/item/1",
	}

	if err := s.UpsertListing(l); err != nil {
		t.Fatalf("upsert error: %v", err)
	}

	seen, err := s.HasSeen("item-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !seen {
		t.Error("expected item to be seen after upsert")
	}
}

func TestUpsertListing_UpdatesPrice(t *testing.T) {
	s := newTestStore(t)

	l := Listing{
		ID:       "item-1",
		Query:    "thinkpad",
		Title:    "Thinkpad X1",
		Price:    500.00,
		Currency: "USD",
		URL:      "https://ebay.com/item/1",
	}
	if err := s.UpsertListing(l); err != nil {
		t.Fatalf("first upsert: %v", err)
	}

	l.Price = 450.00
	if err := s.UpsertListing(l); err != nil {
		t.Fatalf("second upsert: %v", err)
	}

	// Verify price history has two entries
	points, err := s.GetPriceHistory("thinkpad", 30)
	if err != nil {
		t.Fatalf("get price history: %v", err)
	}
	if len(points) != 2 {
		t.Fatalf("expected 2 price history points, got %d", len(points))
	}
	if points[0].Price != 500.00 {
		t.Errorf("first price = %f, want 500", points[0].Price)
	}
	if points[1].Price != 450.00 {
		t.Errorf("second price = %f, want 450", points[1].Price)
	}
}

func TestMarkNotified(t *testing.T) {
	s := newTestStore(t)

	l := Listing{
		ID:       "item-1",
		Query:    "thinkpad",
		Title:    "Thinkpad X1",
		Price:    400.00,
		Currency: "USD",
		URL:      "https://ebay.com/item/1",
	}
	if err := s.UpsertListing(l); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := s.MarkNotified("item-1"); err != nil {
		t.Fatalf("mark notified: %v", err)
	}

	listings, err := s.GetNotifiedListings(10)
	if err != nil {
		t.Fatalf("get notified: %v", err)
	}
	if len(listings) != 1 {
		t.Fatalf("expected 1 notified listing, got %d", len(listings))
	}
	if listings[0].ID != "item-1" {
		t.Errorf("listing ID = %q, want item-1", listings[0].ID)
	}
}

func TestGetNotifiedListings_Empty(t *testing.T) {
	s := newTestStore(t)

	listings, err := s.GetNotifiedListings(10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(listings) != 0 {
		t.Errorf("expected 0 listings, got %d", len(listings))
	}
}

func TestGetNotifiedListings_RespectsLimit(t *testing.T) {
	s := newTestStore(t)

	for i := 0; i < 5; i++ {
		l := Listing{
			ID:       "item-" + string(rune('a'+i)),
			Query:    "test",
			Title:    "Test Item",
			Price:    100.00,
			Currency: "USD",
			URL:      "https://ebay.com/item",
		}
		if err := s.UpsertListing(l); err != nil {
			t.Fatalf("upsert: %v", err)
		}
		if err := s.MarkNotified(l.ID); err != nil {
			t.Fatalf("mark notified: %v", err)
		}
	}

	listings, err := s.GetNotifiedListings(3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(listings) != 3 {
		t.Errorf("expected 3 listings, got %d", len(listings))
	}
}

func TestRecordPoll_And_Stats(t *testing.T) {
	s := newTestStore(t)

	if err := s.RecordPoll(); err != nil {
		t.Fatalf("record poll: %v", err)
	}

	stats, err := s.GetStats()
	if err != nil {
		t.Fatalf("get stats: %v", err)
	}
	if stats.LastPollAt == nil {
		t.Fatal("expected LastPollAt to be set")
	}
}

func TestGetStats_WithListings(t *testing.T) {
	s := newTestStore(t)

	listings := []Listing{
		{ID: "a", Query: "q", Title: "A", Price: 100, Currency: "USD", URL: "http://a"},
		{ID: "b", Query: "q", Title: "B", Price: 200, Currency: "USD", URL: "http://b"},
		{ID: "c", Query: "q", Title: "C", Price: 300, Currency: "USD", URL: "http://c"},
	}

	for _, l := range listings {
		if err := s.UpsertListing(l); err != nil {
			t.Fatalf("upsert: %v", err)
		}
	}
	if err := s.MarkNotified("a"); err != nil {
		t.Fatalf("mark notified: %v", err)
	}

	stats, err := s.GetStats()
	if err != nil {
		t.Fatalf("get stats: %v", err)
	}
	if stats.TotalSeen != 3 {
		t.Errorf("TotalSeen = %d, want 3", stats.TotalSeen)
	}
	if stats.TotalNotified != 1 {
		t.Errorf("TotalNotified = %d, want 1", stats.TotalNotified)
	}
	if stats.LowestPrice != 100 {
		t.Errorf("LowestPrice = %f, want 100", stats.LowestPrice)
	}
	if stats.AveragePrice != 200 {
		t.Errorf("AveragePrice = %f, want 200", stats.AveragePrice)
	}
}

func TestGetStats_Empty(t *testing.T) {
	s := newTestStore(t)

	stats, err := s.GetStats()
	if err != nil {
		t.Fatalf("get stats: %v", err)
	}
	if stats.TotalSeen != 0 {
		t.Errorf("TotalSeen = %d, want 0", stats.TotalSeen)
	}
	if stats.LastPollAt != nil {
		t.Errorf("LastPollAt = %v, want nil", stats.LastPollAt)
	}
}
