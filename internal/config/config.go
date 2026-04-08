package config

import (
	"fmt"
	"os"
	"strings"
	"time"
)

type Config struct {
	EbayClientID      string
	EbaySecret        string
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

	pollStr := getOr("POLL_INTERVAL", "1h")
	var err error
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
