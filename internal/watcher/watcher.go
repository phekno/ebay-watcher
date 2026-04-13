package watcher

import (
	"context"
	"log/slog"

	"github.com/phekno/ebay-watcher/internal/ebay"
	"github.com/phekno/ebay-watcher/internal/notifier"
	"github.com/phekno/ebay-watcher/internal/store"
)

type Watcher struct {
	ebay     *ebay.Client
	store    *store.Store
	notifier *notifier.Discord
}

func New(ebayClientID, ebaySecret string, s *store.Store, n *notifier.Discord) *Watcher {
	return &Watcher{
		ebay:     ebay.NewClient(ebayClientID, ebaySecret),
		store:    s,
		notifier: n,
	}
}

func (w *Watcher) Run(ctx context.Context) {
	if err := w.store.RecordPoll(); err != nil {
		slog.Warn("failed to record poll", "error", err)
	}

	watches, err := w.store.ListEnabledWatches()
	if err != nil {
		slog.Error("failed to list watches", "error", err)
		return
	}
	if len(watches) == 0 {
		slog.Info("no enabled watches — skipping poll")
		return
	}

	for _, watch := range watches {
		slog.Info("searching eBay", "query", watch.Query, "max_price", watch.MaxPrice)

		result, err := w.ebay.Search(ctx, watch.Query, watch.MaxPrice, watch.CategoryID)
		if err != nil {
			slog.Error("ebay search failed", "query", watch.Query, "error", err)
			continue
		}

		slog.Info("search complete", "query", watch.Query, "total", result.Total, "returned", len(result.Listings))

		for _, listing := range result.Listings {
			seen, err := w.store.HasSeen(listing.ID)
			if err != nil {
				slog.Error("store read error", "id", listing.ID, "error", err)
				continue
			}

			sl := store.Listing{
				ID:        listing.ID,
				Query:     watch.Query,
				Title:     listing.Title,
				Price:     listing.Price,
				Currency:  listing.Currency,
				URL:       listing.URL,
				Condition: listing.Condition,
				Seller:    listing.Seller,
				Notified:  seen,
			}
			if err := w.store.UpsertListing(sl); err != nil {
				slog.Error("store upsert error", "id", listing.ID, "error", err)
				continue
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

			if err := w.notifier.Notify(ctx, watch.Query, listing); err != nil {
				slog.Error("discord notify failed", "id", listing.ID, "error", err)
				continue
			}

			if err := w.store.MarkNotified(listing.ID); err != nil {
				slog.Error("store mark notified error", "id", listing.ID, "error", err)
			}
		}
	}
}
