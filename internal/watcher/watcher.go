package watcher

import (
	"context"
	"log/slog"

	"github.com/phekno/ebay-watcher/internal/config"
	"github.com/phekno/ebay-watcher/internal/ebay"
	"github.com/phekno/ebay-watcher/internal/notifier"
	"github.com/phekno/ebay-watcher/internal/store"
)

type Watcher struct {
	cfg      *config.Config
	ebay     *ebay.Client
	store    *store.Store
	notifier *notifier.Discord
}

func New(cfg *config.Config, s *store.Store, n *notifier.Discord) *Watcher {
	return &Watcher{
		cfg:      cfg,
		ebay:     ebay.NewClient(cfg.EbayClientID, cfg.EbaySecret),
		store:    s,
		notifier: n,
	}
}

func (w *Watcher) Run(ctx context.Context) {
	if err := w.store.RecordPoll(); err != nil {
		slog.Warn("failed to record poll", "error", err)
	}

	for _, query := range w.cfg.Queries {
		slog.Info("searching eBay", "query", query, "max_price", w.cfg.MaxPrice)

		result, err := w.ebay.Search(ctx, query, w.cfg.MaxPrice)
		if err != nil {
			slog.Error("ebay search failed", "query", query, "error", err)
			continue
		}

		slog.Info("search complete", "query", query, "total", result.Total, "returned", len(result.Listings))

		for _, listing := range result.Listings {
			seen, err := w.store.HasSeen(listing.ID)
			if err != nil {
				slog.Error("store read error", "id", listing.ID, "error", err)
				continue
			}

			// Always upsert — records price history even for already-seen listings
			sl := store.Listing{
				ID:        listing.ID,
				Query:     query,
				Title:     listing.Title,
				Price:     listing.Price,
				Currency:  listing.Currency,
				URL:       listing.URL,
				Condition: listing.Condition,
				Seller:    listing.Seller,
				Notified:  seen, // preserve existing notified state
			}
			if err := w.store.UpsertListing(sl); err != nil {
				slog.Error("store upsert error", "id", listing.ID, "error", err)
			}

			if seen {
				continue
			}

			slog.Info("new listing found",
				"id", listing.ID,
				"title", listing.Title,
				"price", listing.Price,
				"condition", listing.Condition,
			)

			if err := w.notifier.Notify(ctx, query, listing); err != nil {
				slog.Error("discord notify failed", "id", listing.ID, "error", err)
				// Don't mark notified — retry next poll
				continue
			}

			if err := w.store.MarkNotified(listing.ID); err != nil {
				slog.Error("store mark notified error", "id", listing.ID, "error", err)
			}
		}
	}
}
