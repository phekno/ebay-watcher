package notifier

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/phekno/ebay-watcher/internal/ebay"
)

type Discord struct {
	webhookURL string
	client     *http.Client
}

func NewDiscord(webhookURL string) *Discord {
	return &Discord{
		webhookURL: webhookURL,
		client:     &http.Client{Timeout: 10 * time.Second},
	}
}

type discordPayload struct {
	Embeds []discordEmbed `json:"embeds"`
}

type discordEmbed struct {
	Title       string         `json:"title"`
	URL         string         `json:"url"`
	Color       int            `json:"color"`
	Description string         `json:"description"`
	Fields      []embedField   `json:"fields"`
	Footer      *embedFooter   `json:"footer,omitempty"`
	Timestamp   string         `json:"timestamp,omitempty"`
}

type embedField struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline"`
}

type embedFooter struct {
	Text string `json:"text"`
}

// Notify sends a Discord embed for a new listing.
func (d *Discord) Notify(ctx context.Context, query string, listing ebay.Listing) error {
	embed := discordEmbed{
		Title:       listing.Title,
		URL:         listing.URL,
		Color:       0x00b0f4, // eBay blue
		Description: fmt.Sprintf("A new listing matched your search for **%s**", query),
		Fields: []embedField{
			{Name: "💰 Price", Value: fmt.Sprintf("$%.2f %s", listing.Price, listing.Currency), Inline: true},
			{Name: "📦 Condition", Value: listing.Condition, Inline: true},
			{Name: "🛒 Seller", Value: listing.Seller, Inline: true},
		},
		Footer:    &embedFooter{Text: "ebay-watcher"},
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}

	payload := discordPayload{Embeds: []discordEmbed{embed}}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.webhookURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.client.Do(req)
	if err != nil {
		return fmt.Errorf("discord request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Discord returns 204 No Content on success
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("discord returned %d", resp.StatusCode)
	}

	return nil
}
