package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	EbayClientID      string
	EbaySecret        string
	Queries           []string
	MaxPrice          float64
	DiscordWebhookURL string
	DatabasePath      string
	PollInterval      time.Duration
	ListenAddr        string
}

func Load() (*Config, error) {
	cfg := &Config{
		EbayClientID:      os.Getenv("EBAY_CLIENT_ID"),
		EbaySecret:        os.Getenv("EBAY_CLIENT_SECRET"),
		DiscordWebhookURL: os.Getenv("DISCORD_WEBHOOK_URL"),
		DatabasePath:      getOr("DATABASE_PATH", "/data/seen.db"),
		ListenAddr:        getOr("LISTEN_ADDR", ":8080"),
	}

	var missing []string
	for _, f := range []struct{ k, v string }{
		{"EBAY_CLIENT_ID", cfg.EbayClientID},
		{"EBAY_CLIENT_SECRET", cfg.EbaySecret},
		{"DISCORD_WEBHOOK_URL", cfg.DiscordWebhookURL},
	} {
		if f.v == "" {
			missing = append(missing, f.k)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("missing required env vars: %s", strings.Join(missing, ", "))
	}

	raw := os.Getenv("SEARCH_QUERIES")
	for _, q := range strings.Split(raw, ",") {
		if q = strings.TrimSpace(q); q != "" {
			cfg.Queries = append(cfg.Queries, q)
		}
	}
	if len(cfg.Queries) == 0 {
		return nil, fmt.Errorf("SEARCH_QUERIES must contain at least one query")
	}

	maxStr := os.Getenv("MAX_PRICE")
	if maxStr == "" {
		return nil, fmt.Errorf("MAX_PRICE is required")
	}
	p, err := strconv.ParseFloat(maxStr, 64)
	if err != nil {
		return nil, fmt.Errorf("MAX_PRICE must be a number: %w", err)
	}
	cfg.MaxPrice = p

	pollStr := getOr("POLL_INTERVAL", "1h")
	cfg.PollInterval, err = time.ParseDuration(pollStr)
	if err != nil {
		return nil, fmt.Errorf("POLL_INTERVAL invalid: %w", err)
	}

	return cfg, nil
}

func getOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
