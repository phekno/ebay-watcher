package config

import (
	"testing"
	"time"
)

// setEnv sets all required env vars to valid defaults and returns a cleanup func.
func setEnv(t *testing.T) {
	t.Helper()
	t.Setenv("EBAY_CLIENT_ID", "test-id")
	t.Setenv("EBAY_CLIENT_SECRET", "test-secret")
	t.Setenv("DISCORD_WEBHOOK_URL", "https://discord.com/api/webhooks/test")
	t.Setenv("SEARCH_QUERIES", "thinkpad")
	t.Setenv("MAX_PRICE", "500")
}

func TestLoad_Success(t *testing.T) {
	setEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.EbayClientID != "test-id" {
		t.Errorf("EbayClientID = %q, want %q", cfg.EbayClientID, "test-id")
	}
	if cfg.MaxPrice != 500 {
		t.Errorf("MaxPrice = %f, want 500", cfg.MaxPrice)
	}
	if len(cfg.Queries) != 1 || cfg.Queries[0] != "thinkpad" {
		t.Errorf("Queries = %v, want [thinkpad]", cfg.Queries)
	}
	if cfg.PollInterval != time.Hour {
		t.Errorf("PollInterval = %v, want 1h", cfg.PollInterval)
	}
	if cfg.DatabasePath != "/data/seen.db" {
		t.Errorf("DatabasePath = %q, want /data/seen.db", cfg.DatabasePath)
	}
	if cfg.ListenAddr != ":8080" {
		t.Errorf("ListenAddr = %q, want :8080", cfg.ListenAddr)
	}
}

func TestLoad_MultipleQueries(t *testing.T) {
	setEnv(t)
	t.Setenv("SEARCH_QUERIES", " thinkpad , macbook pro , dell xps ")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := []string{"thinkpad", "macbook pro", "dell xps"}
	if len(cfg.Queries) != len(want) {
		t.Fatalf("got %d queries, want %d", len(cfg.Queries), len(want))
	}
	for i, q := range want {
		if cfg.Queries[i] != q {
			t.Errorf("Queries[%d] = %q, want %q", i, cfg.Queries[i], q)
		}
	}
}

func TestLoad_MissingRequiredEnv(t *testing.T) {
	tests := []struct {
		name   string
		unset  string
	}{
		{"missing EBAY_CLIENT_ID", "EBAY_CLIENT_ID"},
		{"missing EBAY_CLIENT_SECRET", "EBAY_CLIENT_SECRET"},
		{"missing DISCORD_WEBHOOK_URL", "DISCORD_WEBHOOK_URL"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setEnv(t)
			t.Setenv(tt.unset, "")

			_, err := Load()
			if err == nil {
				t.Fatal("expected error, got nil")
			}
		})
	}
}

func TestLoad_EmptyQueries(t *testing.T) {
	setEnv(t)
	t.Setenv("SEARCH_QUERIES", "  ,  ,  ")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for empty queries, got nil")
	}
}

func TestLoad_MissingMaxPrice(t *testing.T) {
	setEnv(t)
	t.Setenv("MAX_PRICE", "")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for missing MAX_PRICE, got nil")
	}
}

func TestLoad_InvalidMaxPrice(t *testing.T) {
	setEnv(t)
	t.Setenv("MAX_PRICE", "not-a-number")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for invalid MAX_PRICE, got nil")
	}
}

func TestLoad_InvalidPollInterval(t *testing.T) {
	setEnv(t)
	t.Setenv("POLL_INTERVAL", "bad")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for invalid POLL_INTERVAL, got nil")
	}
}

func TestLoad_CustomPollInterval(t *testing.T) {
	setEnv(t)
	t.Setenv("POLL_INTERVAL", "30m")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.PollInterval != 30*time.Minute {
		t.Errorf("PollInterval = %v, want 30m", cfg.PollInterval)
	}
}

func TestLoad_CustomDefaults(t *testing.T) {
	setEnv(t)
	t.Setenv("DATABASE_PATH", "/tmp/test.db")
	t.Setenv("LISTEN_ADDR", ":9090")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.DatabasePath != "/tmp/test.db" {
		t.Errorf("DatabasePath = %q, want /tmp/test.db", cfg.DatabasePath)
	}
	if cfg.ListenAddr != ":9090" {
		t.Errorf("ListenAddr = %q, want :9090", cfg.ListenAddr)
	}
}
