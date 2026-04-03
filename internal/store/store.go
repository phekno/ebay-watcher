package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

type Listing struct {
	ID        string     `json:"id"`
	Query     string     `json:"query"`
	Title     string     `json:"title"`
	Price     float64    `json:"price"`
	Currency  string     `json:"currency"`
	URL       string     `json:"url"`
	Condition string     `json:"condition"`
	Seller    string     `json:"seller"`
	Notified  bool       `json:"notified"`
	FirstSeen time.Time  `json:"first_seen"`
	LastSeen  time.Time  `json:"last_seen"`
}

type PricePoint struct {
	SeenAt time.Time `json:"seen_at"`
	Price  float64   `json:"price"`
	Query  string    `json:"query"`
}

type Stats struct {
	TotalSeen     int        `json:"total_seen"`
	TotalNotified int        `json:"total_notified"`
	LowestPrice   float64    `json:"lowest_price"`
	AveragePrice  float64    `json:"average_price"`
	LastPollAt    *time.Time `json:"last_poll_at"`
}

func New(path string) (*Store, error) {
	dir := filepath.Dir(path)
	if dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("create data dir: %w", err)
		}
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	if err := migrate(db); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}

	return &Store{db: db}, nil
}

func migrate(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS listings (
			id          TEXT PRIMARY KEY,
			query       TEXT NOT NULL,
			title       TEXT NOT NULL,
			price       REAL NOT NULL,
			currency    TEXT NOT NULL DEFAULT 'USD',
			url         TEXT NOT NULL,
			condition   TEXT NOT NULL DEFAULT '',
			seller      TEXT NOT NULL DEFAULT '',
			notified    INTEGER NOT NULL DEFAULT 0,
			first_seen  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			last_seen   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);

		CREATE TABLE IF NOT EXISTS price_history (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			listing_id TEXT NOT NULL,
			query      TEXT NOT NULL,
			price      REAL NOT NULL,
			seen_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (listing_id) REFERENCES listings(id)
		);

		CREATE TABLE IF NOT EXISTS poll_log (
			id        INTEGER PRIMARY KEY AUTOINCREMENT,
			polled_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);

		CREATE INDEX IF NOT EXISTS idx_price_history_query ON price_history(query, seen_at);
		CREATE INDEX IF NOT EXISTS idx_listings_query ON listings(query);
		CREATE INDEX IF NOT EXISTS idx_listings_notified ON listings(notified);
	`)
	return err
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) HasSeen(id string) (bool, error) {
	var count int
	err := s.db.QueryRow(`SELECT COUNT(1) FROM listings WHERE id = ?`, id).Scan(&count)
	return count > 0, err
}

// UpsertListing records or updates a listing and appends a price history point.
func (s *Store) UpsertListing(l Listing) error {
	_, err := s.db.Exec(`
		INSERT INTO listings (id, query, title, price, currency, url, condition, seller, notified)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			price     = excluded.price,
			last_seen = CURRENT_TIMESTAMP
	`, l.ID, l.Query, l.Title, l.Price, l.Currency, l.URL, l.Condition, l.Seller, l.Notified)
	if err != nil {
		return fmt.Errorf("upsert listing: %w", err)
	}

	_, err = s.db.Exec(
		`INSERT INTO price_history (listing_id, query, price) VALUES (?, ?, ?)`,
		l.ID, l.Query, l.Price,
	)
	return err
}

func (s *Store) MarkNotified(id string) error {
	_, err := s.db.Exec(`UPDATE listings SET notified = 1 WHERE id = ?`, id)
	return err
}

func (s *Store) RecordPoll() error {
	_, err := s.db.Exec(`INSERT INTO poll_log (polled_at) VALUES (CURRENT_TIMESTAMP)`)
	return err
}

func (s *Store) GetNotifiedListings(limit int) ([]Listing, error) {
	rows, err := s.db.Query(`
		SELECT id, query, title, price, currency, url, condition, seller, notified, first_seen, last_seen
		FROM listings
		WHERE notified = 1
		ORDER BY last_seen DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	return scanListings(rows)
}

func (s *Store) GetPriceHistory(query string, days int) ([]PricePoint, error) {
	rows, err := s.db.Query(`
		SELECT seen_at, price, query
		FROM price_history
		WHERE query = ?
		  AND seen_at >= datetime('now', ?)
		ORDER BY seen_at ASC
	`, query, fmt.Sprintf("-%d days", days))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var points []PricePoint
	for rows.Next() {
		var p PricePoint
		var seenAt string
		if err := rows.Scan(&seenAt, &p.Price, &p.Query); err != nil {
			return nil, err
		}
		p.SeenAt, _ = time.Parse("2006-01-02 15:04:05", seenAt)
		points = append(points, p)
	}
	return points, rows.Err()
}

func (s *Store) GetStats() (*Stats, error) {
	stats := &Stats{}

	err := s.db.QueryRow(`
		SELECT
			COUNT(*),
			COUNT(CASE WHEN notified=1 THEN 1 END),
			COALESCE(MIN(price), 0),
			COALESCE(AVG(price), 0)
		FROM listings
	`).Scan(&stats.TotalSeen, &stats.TotalNotified, &stats.LowestPrice, &stats.AveragePrice)
	if err != nil {
		return nil, err
	}

	var lastPoll string
	err = s.db.QueryRow(
		`SELECT polled_at FROM poll_log ORDER BY polled_at DESC LIMIT 1`,
	).Scan(&lastPoll)
	if err == nil {
		t, _ := time.Parse("2006-01-02 15:04:05", lastPoll)
		stats.LastPollAt = &t
	}

	return stats, nil
}

func scanListings(rows *sql.Rows) ([]Listing, error) {
	var listings []Listing
	for rows.Next() {
		var l Listing
		var firstSeen, lastSeen string
		err := rows.Scan(
			&l.ID, &l.Query, &l.Title, &l.Price, &l.Currency,
			&l.URL, &l.Condition, &l.Seller, &l.Notified,
			&firstSeen, &lastSeen,
		)
		if err != nil {
			return nil, err
		}
		l.FirstSeen, _ = time.Parse("2006-01-02 15:04:05", firstSeen)
		l.LastSeen, _ = time.Parse("2006-01-02 15:04:05", lastSeen)
		listings = append(listings, l)
	}
	return listings, rows.Err()
}
